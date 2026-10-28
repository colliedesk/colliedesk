package emailoutbox_test

import (
	"bytes"
	"context"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/emailoutbox"
	"github.com/colliedesk/colliedesk/server/internal/microsoft"
	"github.com/colliedesk/colliedesk/server/internal/microsoft/microsofttest"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
)

const microsoftRedirect = "https://desk.example.com/api/v1/channels/email/microsoft/callback"

func microsoftStandIn(t *testing.T) (*microsofttest.Server, microsoft.Config) {
	t.Helper()
	fake := microsofttest.New("ms-client", "ms-secret")
	server := fake.Start()
	t.Cleanup(server.Close)
	keys, err := tokenseal.ParseKeys("k1:AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=")
	if err != nil {
		t.Fatal(err)
	}
	return fake, microsoft.Config{ClientID: "ms-client", ClientSecret: "ms-secret", Tenant: "common", Keys: keys, Base: server.URL}
}

// connectMicrosoft runs the consent against the stand-in and stores the sealed grant for the workspace's email channel.
func (f fixture) connectMicrosoft(t *testing.T, fake *microsofttest.Server, config microsoft.Config, account string) store.SealedGrant {
	t.Helper()
	target, err := fake.Consent(config.AuthorizationURL(microsoftRedirect, "state", "verifier", account), account, microsofttest.Work)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(target)
	grant, err := config.Exchange(context.Background(), parsed.Query().Get("code"), "verifier", microsoftRedirect)
	if err != nil {
		t.Fatal(err)
	}
	keyID, sealed, err := config.Keys.Seal([]byte(grant.RefreshToken), microsoft.GrantContext(f.workspaceID))
	if err != nil {
		t.Fatal(err)
	}
	stored := store.SealedGrant{KeyID: keyID, Sealed: sealed, Provider: domain.MailboxMicrosoft, AccountID: grant.AccountID}
	if _, _, err := f.store.SaveMailboxConnection(context.Background(), f.workspaceID, f.emailChannelID, f.agentID, grant.Address, strings.Join(grant.Scopes, " "), stored, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	return stored
}

func microsoftWorker(f fixture, smtp *smtpServer, config microsoft.Config, clock *time.Time, mutate ...func(*emailoutbox.Options)) *emailoutbox.Worker {
	return newWorker(f, smtp, clock, append([]func(*emailoutbox.Options){func(options *emailoutbox.Options) {
		options.Microsoft = &config
	}}, mutate...)...)
}

func (f fixture) storedGrant(t *testing.T) store.SealedGrant {
	t.Helper()
	grant, err := f.store.MailboxGrant(context.Background(), f.workspaceID, f.emailChannelID)
	if err != nil || grant == nil {
		t.Fatalf("grant %v %v", grant, err)
	}
	return *grant
}

func TestAConnectedMicrosoftMailboxSendsTheThreadedReplyAsMIMEAndKeepsTheRotatedGrant(t *testing.T) {
	f := newFixture(t)
	fake, config := microsoftStandIn(t)
	before := f.connectMicrosoft(t, fake, config, "Help@Acme.example")
	storage, err := attachments.NewLocalDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	smtp := startSMTPServer(t)
	clock := time.Now().UTC().Add(time.Second)
	worker := microsoftWorker(f, smtp, config, &clock, func(options *emailoutbox.Options) { options.Storage = storage })
	pdf := append([]byte("%PDF-1.7\n"), bytes.Repeat([]byte{0, 0xff}, 3000)...)
	object, err := storage.Put(context.Background(), f.workspaceID, bytes.NewReader(pdf), int64(len(pdf)))
	if err != nil {
		t.Fatal(err)
	}
	reply, err := f.store.CreateMessageWithCC(context.Background(), f.workspaceID, f.conversationID, store.Actor{AgentID: f.agentID}, "outgoing",
		"Fixed, see the file.", []string{"boss@customer.example"}, store.NewAttachment{Filename: "fix.pdf", ContentType: "application/pdf", Object: object})
	if err != nil {
		t.Fatal(err)
	}
	process(t, worker)
	if delivery := f.delivery(t, reply.ID); delivery.Status != "sent" {
		t.Fatalf("delivery = %+v", delivery)
	}
	if len(smtp.mail()) != 0 {
		t.Fatal("a Microsoft-connected reply also went through SMTP")
	}
	sent := fake.Messages()
	if len(sent) != 1 || sent[0].Account != "help@acme.example" {
		t.Fatalf("sent = %+v", sent)
	}
	parsed, err := mail.ReadMessage(bytes.NewReader(sent[0].Raw))
	if err != nil {
		t.Fatal(err)
	}
	from, _ := mail.ParseAddress(parsed.Header.Get("From"))
	replyTo, _ := mail.ParseAddress(parsed.Header.Get("Reply-To"))
	switch {
	case from == nil || from.Address != "help@acme.example":
		t.Errorf("From = %q", parsed.Header.Get("From"))
	case replyTo == nil || !strings.HasPrefix(replyTo.Address, "support+r-"):
		t.Errorf("Reply-To = %q", parsed.Header.Get("Reply-To"))
	case parsed.Header.Get("In-Reply-To") != "<export-1@customer.example>" || !strings.Contains(parsed.Header.Get("References"), "<export-1@customer.example>"):
		t.Errorf("threading headers: In-Reply-To %q References %q", parsed.Header.Get("In-Reply-To"), parsed.Header.Get("References"))
	case !bytes.Contains(sent[0].Raw, []byte(`filename="fix.pdf"`)) || !strings.Contains(parsed.Header.Get("Cc"), "boss@customer.example"):
		t.Error("the file or the copy is missing")
	}
	// Microsoft replaced the refresh token on use; the new one is stored, still sealed for this workspace.
	after := f.storedGrant(t)
	if bytes.Equal(after.Sealed, before.Sealed) {
		t.Fatal("the rotated refresh token was not stored")
	}
	opened, err := config.Keys.Open(after.KeyID, after.Sealed, microsoft.GrantContext(f.workspaceID))
	if err != nil || fake.AccountOf(string(opened)) != "help@acme.example" {
		t.Fatalf("stored grant opens to %q (%v)", fake.AccountOf(string(opened)), err)
	}
}

func TestARevokedMicrosoftGrantFailsTheReplyAndNeverFallsBackToSMTP(t *testing.T) {
	f := newFixture(t)
	fake, config := microsoftStandIn(t)
	f.connectMicrosoft(t, fake, config, "help@acme.example")
	fake.RevokeAll()
	smtp := startSMTPServer(t)
	clock := time.Now().UTC().Add(time.Second)
	worker := microsoftWorker(f, smtp, config, &clock)
	reply := f.write(t, "outgoing", "Hello")
	process(t, worker)
	delivery := f.delivery(t, reply.ID)
	if delivery.Status != "failed" || delivery.LastError == nil || !strings.Contains(*delivery.LastError, "Reconnect Microsoft 365") {
		t.Fatalf("delivery = %+v", delivery)
	}
	if len(smtp.mail()) != 0 || len(fake.Messages()) != 0 {
		t.Fatal("a reply left after the grant was revoked")
	}
	if connection := f.mailboxStatus(t); connection.Status != "failed" || *connection.ErrorCode != domain.MailboxRevoked {
		t.Fatalf("connection = %+v", connection)
	}
}

// An expired client secret is the server's fault: the reply waits (spending no attempt) with a note for
// the administrator, and the connection stays connected; the reply leaves once the secret is fixed.
func TestARefusedClientSecretKeepsTheConnectionAndTheReplyWaits(t *testing.T) {
	f := newFixture(t)
	fake, config := microsoftStandIn(t)
	f.connectMicrosoft(t, fake, config, "help@acme.example")
	clock := time.Now().UTC().Add(time.Second)
	broken := config
	broken.ClientSecret = "expired-secret"
	worker := microsoftWorker(f, startSMTPServer(t), broken, &clock)
	reply := f.write(t, "outgoing", "Hello")
	for range 4 {
		process(t, worker)
		clock = clock.Add(16 * time.Minute)
	}
	delivery := f.delivery(t, reply.ID)
	if delivery.Status != "queued" || delivery.LastError == nil || !strings.Contains(*delivery.LastError, "whoever runs this server must replace its Microsoft app secret") || strings.Contains(*delivery.LastError, "MICROSOFT_OAUTH") ||
		delivery.ReasonCode == nil || *delivery.ReasonCode != domain.MailboxServerConfigCode || delivery.Attempts != 0 {
		t.Fatalf("delivery = %+v (reason %v)", delivery, delivery.ReasonCode)
	}
	if connection := f.mailboxStatus(t); connection.Status != "connected" {
		t.Fatalf("a server error broke the connection: %+v", connection)
	}
	waits, err := f.store.MailboxWaitsForServer(context.Background(), f.workspaceID, f.emailChannelID)
	if err != nil || !waits {
		t.Fatalf("waits for the server = %v (%v)", waits, err)
	}
	fixed := microsoftWorker(f, startSMTPServer(t), config, &clock)
	process(t, fixed)
	if delivery := f.delivery(t, reply.ID); delivery.Status != "sent" || len(fake.Messages()) != 1 {
		t.Fatalf("after the fix = %+v", delivery)
	}
}

// An organization that removes or disables the app in its tenant ends that grant (unauthorized_client): the
// connection fails with a reconnect, unlike a refused server secret, which keeps it.
func TestAnAppRemovedByTheOrganizationBreaksTheConnection(t *testing.T) {
	f := newFixture(t)
	fake, config := microsoftStandIn(t)
	f.connectMicrosoft(t, fake, config, "help@acme.example")
	clock := time.Now().UTC().Add(time.Second)
	worker := microsoftWorker(f, startSMTPServer(t), config, &clock)
	fake.Set(func(s *microsofttest.Server) {
		s.TokenFailures = []microsofttest.TokenFailure{{Status: http.StatusBadRequest, Error: "unauthorized_client", Codes: []int{700016}}}
	})
	reply := f.write(t, "outgoing", "Hello")
	process(t, worker)
	if delivery := f.delivery(t, reply.ID); delivery.Status != "failed" || !strings.Contains(*delivery.LastError, "Reconnect Microsoft 365") {
		t.Fatalf("delivery = %+v", delivery)
	}
	if connection := f.mailboxStatus(t); connection.Status != "failed" || *connection.ErrorCode != domain.MailboxRevoked {
		t.Fatalf("connection = %+v", connection)
	}

	// The keep-alive tells the same apart for a quiet channel.
	other := addWorkspace(t, f.pool, "Quiet Desk", "quiet.example.com")
	other.connectMicrosoft(t, fake, config, "help@quiet.example.com")
	keepClock := time.Now().UTC().Add(8 * 24 * time.Hour)
	fake.Set(func(s *microsofttest.Server) {
		s.TokenFailures = []microsofttest.TokenFailure{{Status: http.StatusBadRequest, Error: "unauthorized_client", Codes: []int{7000112}}}
	})
	if _, err := microsoftWorker(other, startSMTPServer(t), config, &keepClock).KeepAlive(context.Background()); err != nil {
		t.Fatal(err)
	}
	if connection := other.mailboxStatus(t); connection.Status != "failed" || *connection.ErrorCode != domain.MailboxRevoked {
		t.Fatalf("quiet connection after the app was disabled = %+v", connection)
	}
}

func TestMicrosoftThrottlingWaitsAndOtherRefusalsSayWhy(t *testing.T) {
	f := newFixture(t)
	fake, config := microsoftStandIn(t)
	f.connectMicrosoft(t, fake, config, "help@acme.example")
	clock := time.Now().UTC().Add(time.Second)
	worker := microsoftWorker(f, startSMTPServer(t), config, &clock)

	fake.Set(func(s *microsofttest.Server) {
		s.SendFailures = []microsofttest.Failure{{Status: http.StatusTooManyRequests, Code: "ApplicationThrottled", RetryAfter: 120}}
	})
	reply := f.write(t, "outgoing", "Hello")
	process(t, worker)
	if delivery := f.delivery(t, reply.ID); delivery.Status != "queued" || delivery.Attempts != 0 {
		t.Fatalf("throttled reply = %+v, want waiting without spending an attempt", delivery)
	}
	clock = clock.Add(time.Minute)
	if process(t, worker) != 0 {
		t.Fatal("the throttled reply was tried before Retry-After")
	}
	clock = clock.Add(2 * time.Minute)
	process(t, worker)
	if delivery := f.delivery(t, reply.ID); delivery.Status != "sent" {
		t.Fatalf("after Retry-After = %+v", delivery)
	}

	// Too large: failed with the reason, the file kept, the connection fine.
	fake.Set(func(s *microsofttest.Server) { s.MaxRequestBytes = 10 })
	large := f.write(t, "outgoing", "A long reply")
	process(t, worker)
	if delivery := f.delivery(t, large.ID); delivery.Status != "failed" || *delivery.LastError != domain.MailboxTooLargeNote {
		t.Fatalf("too large = %+v", delivery)
	}
	if connection := f.mailboxStatus(t); connection.Status != "connected" {
		t.Fatalf("a large reply broke the connection: %+v", connection)
	}
	fake.Set(func(s *microsofttest.Server) { s.MaxRequestBytes = 0 })

	// A mailbox whose licence was removed breaks the connection with its own reason.
	fake.Set(func(s *microsofttest.Server) {
		s.SendFailures = []microsofttest.Failure{{Status: http.StatusNotFound, Code: "MailboxNotEnabledForRESTAPI"}}
	})
	gone := f.write(t, "outgoing", "Again")
	process(t, worker)
	if delivery := f.delivery(t, gone.ID); delivery.Status != "failed" || !strings.Contains(*delivery.LastError, "no Exchange Online mailbox") {
		t.Fatalf("no mailbox = %+v", delivery)
	}
	if connection := f.mailboxStatus(t); connection.Status != "failed" || *connection.ErrorCode != domain.MailboxUnavailable {
		t.Fatalf("connection = %+v", connection)
	}
}

func TestAMicrosoftPermissionRefusalBreaksTheConnection(t *testing.T) {
	f := newFixture(t)
	fake, config := microsoftStandIn(t)
	f.connectMicrosoft(t, fake, config, "help@acme.example")
	clock := time.Now().UTC().Add(time.Second)
	worker := microsoftWorker(f, startSMTPServer(t), config, &clock)
	fake.Set(func(s *microsofttest.Server) {
		s.SendFailures = []microsofttest.Failure{{Status: http.StatusForbidden, Code: "ErrorAccessDenied"}}
	})
	reply := f.write(t, "outgoing", "Hello")
	process(t, worker)
	if delivery := f.delivery(t, reply.ID); delivery.Status != "failed" || !strings.Contains(*delivery.LastError, "refused to send") {
		t.Fatalf("delivery = %+v", delivery)
	}
	if connection := f.mailboxStatus(t); connection.Status != "failed" || *connection.ErrorCode != domain.MailboxPermission {
		t.Fatalf("connection = %+v", connection)
	}
}

// A channel that moves from Gmail to Microsoft 365 never sends a reply queued for Gmail from Microsoft:
// it fails with Gmail's reason and a retry chooses the new way.
func TestAReplyQueuedForGmailFailsWhenTheChannelMovedToMicrosoft(t *testing.T) {
	f := newFixture(t)
	google, googleConfig := gmailStandIn(t)
	f.connectGmail(t, google, googleConfig, "help@acme.example")
	reply := f.write(t, "outgoing", "Hello")
	fake, config := microsoftStandIn(t)
	f.connectMicrosoft(t, fake, config, "help@acme.example")
	clock := time.Now().UTC().Add(time.Second)
	worker := microsoftWorker(f, startSMTPServer(t), config, &clock, func(options *emailoutbox.Options) { options.Google = &googleConfig })
	process(t, worker)
	if delivery := f.delivery(t, reply.ID); delivery.Status != "failed" || *delivery.LastError != domain.MailboxDisconnectedNote(domain.MailboxGoogle) {
		t.Fatalf("delivery = %+v", delivery)
	}
	if len(google.Messages()) != 0 || len(fake.Messages()) != 0 {
		t.Fatal("the reply left anyway")
	}
	if _, err := f.store.RetryDelivery(context.Background(), f.workspaceID, f.conversationID, reply.ID); err != nil {
		t.Fatal(err)
	}
	process(t, worker)
	if delivery := f.delivery(t, reply.ID); delivery.Status != "sent" || len(fake.Messages()) != 1 {
		t.Fatalf("after retry = %+v, microsoft %d", delivery, len(fake.Messages()))
	}
}

// Two replies of one channel sent at once both rotate the refresh token: whichever stores last, the grant
// left keeps working, because Microsoft does not revoke a refresh token when it is used.
func TestTwoSendsAtOnceLeaveAGrantThatStillWorks(t *testing.T) {
	f := newFixture(t)
	fake, config := microsoftStandIn(t)
	f.connectMicrosoft(t, fake, config, "help@acme.example")
	clock := time.Now().UTC().Add(time.Second)
	first, second := f.write(t, "outgoing", "One"), f.write(t, "outgoing", "Two")
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, _ = microsoftWorker(f, startSMTPServer(t), config, &clock, func(options *emailoutbox.Options) { options.BatchSize = 1 }).ProcessDue(context.Background())
		}()
	}
	wait.Wait()
	worker := microsoftWorker(f, startSMTPServer(t), config, &clock)
	process(t, worker)
	for _, reply := range []string{first.ID, second.ID} {
		if delivery := f.delivery(t, reply); delivery.Status != "sent" {
			t.Fatalf("delivery %s = %+v", reply, delivery)
		}
	}
	third := f.write(t, "outgoing", "Three")
	process(t, worker)
	if delivery := f.delivery(t, third.ID); delivery.Status != "sent" {
		t.Fatalf("a later reply after concurrent sends = %+v", delivery)
	}
}

// A quiet channel's grant is refreshed weekly, so it does not expire after 90 days unused, and a grant
// Microsoft refuses shows as broken before a customer waits for a reply.
func TestTheKeepAliveRenewsAQuietGrantAndMarksARevokedOne(t *testing.T) {
	f := newFixture(t)
	fake, config := microsoftStandIn(t)
	before := f.connectMicrosoft(t, fake, config, "help@acme.example")
	clock := time.Now().UTC()
	worker := microsoftWorker(f, startSMTPServer(t), config, &clock)
	if renewed, err := worker.KeepAlive(context.Background()); err != nil || renewed != 0 {
		t.Fatalf("a fresh grant renewed: %d %v", renewed, err)
	}
	clock = clock.Add(8 * 24 * time.Hour)
	if renewed, err := worker.KeepAlive(context.Background()); err != nil || renewed != 1 {
		t.Fatalf("a week-old grant: renewed %d (%v)", renewed, err)
	}
	if after := f.storedGrant(t); bytes.Equal(after.Sealed, before.Sealed) {
		t.Fatal("the keep-alive did not store the new refresh token")
	}
	if renewed, err := worker.KeepAlive(context.Background()); err != nil || renewed != 0 {
		t.Fatalf("renewed again at once: %d %v", renewed, err)
	}
	clock = clock.Add(8 * 24 * time.Hour)
	fake.ExpireAll()
	if _, err := worker.KeepAlive(context.Background()); err != nil {
		t.Fatal(err)
	}
	if connection := f.mailboxStatus(t); connection.Status != "failed" || *connection.ErrorCode != domain.MailboxRevoked {
		t.Fatalf("connection after an expired grant = %+v", connection)
	}
}

// The keep-alive renews only its own workspace's grant: a row of workspace B holding A's sealed grant does
// not open with B's context, and nothing of A changes.
func TestTheKeepAliveNeverOpensAGrantInAnotherWorkspace(t *testing.T) {
	f := newFixture(t)
	fake, config := microsoftStandIn(t)
	grantA := f.connectMicrosoft(t, fake, config, "a@acme.example")
	other := addWorkspace(t, f.pool, "Other Desk", "other.example.com")
	if _, _, err := other.store.SaveMailboxConnection(context.Background(), other.workspaceID, other.emailChannelID, other.agentID, "a@acme.example", "Mail.Send", grantA, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	clock := time.Now().UTC().Add(8 * 24 * time.Hour)
	worker := microsoftWorker(f, startSMTPServer(t), config, &clock)
	if renewed, err := worker.KeepAlive(context.Background()); err != nil || renewed != 1 {
		t.Fatalf("renewed %d (%v), want A's grant only", renewed, err)
	}
	copied := other.storedGrant(t)
	if !bytes.Equal(copied.Sealed, grantA.Sealed) {
		t.Fatal("the copy in workspace B was renewed with A's token")
	}
}
