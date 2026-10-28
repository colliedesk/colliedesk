package emailoutbox_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/emailoutbox"
	"github.com/colliedesk/colliedesk/server/internal/google"
	"github.com/colliedesk/colliedesk/server/internal/google/googletest"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
)

const gmailRedirect = "https://desk.example.com/api/v1/channels/email/gmail/callback"

func gmailStandIn(t *testing.T) (*googletest.Server, google.Config) {
	t.Helper()
	fake := googletest.New("client-1", "not-a-secret")
	server := fake.Start()
	t.Cleanup(server.Close)
	keys, err := tokenseal.ParseKeys("k1:AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=")
	if err != nil {
		t.Fatal(err)
	}
	return fake, google.Config{ClientID: "client-1", ClientSecret: "not-a-secret", Keys: keys, Base: server.URL}
}

// connectGmail runs the consent against the stand-in and stores the sealed grant for the workspace's email channel.
func (f fixture) connectGmail(t *testing.T, fake *googletest.Server, config google.Config, account string) store.SealedGrant {
	t.Helper()
	return f.connectGmailTo(t, f.emailChannelID, fake, config, account)
}

// connectGmailTo connects Gmail to one email channel of the workspace.
func (f fixture) connectGmailTo(t *testing.T, channelID string, fake *googletest.Server, config google.Config, account string) store.SealedGrant {
	t.Helper()
	target, err := fake.Consent(config.AuthorizationURL(gmailRedirect, "state", "verifier"), account)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(target)
	grant, err := config.Exchange(context.Background(), parsed.Query().Get("code"), "verifier", gmailRedirect)
	if err != nil {
		t.Fatal(err)
	}
	keyID, sealed, err := config.Keys.Seal([]byte(grant.RefreshToken), google.GrantContext(f.workspaceID))
	if err != nil {
		t.Fatal(err)
	}
	stored := store.SealedGrant{KeyID: keyID, Sealed: sealed, Provider: domain.MailboxGoogle}
	if _, _, err := f.store.SaveMailboxConnection(context.Background(), f.workspaceID, channelID, f.agentID, grant.Address, strings.Join(grant.Scopes, " "), stored, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	return stored
}

func gmailWorker(f fixture, smtp *smtpServer, config google.Config, clock *time.Time, mutate ...func(*emailoutbox.Options)) *emailoutbox.Worker {
	return newWorker(f, smtp, clock, append([]func(*emailoutbox.Options){func(options *emailoutbox.Options) {
		options.FromPolicy = email.FromPolicyVerified
		options.Google = &config
	}}, mutate...)...)
}

func (f fixture) mailboxStatus(t *testing.T) *domain.MailboxConnection {
	t.Helper()
	connection, err := f.store.GetMailboxConnection(context.Background(), f.workspaceID, f.emailChannelID)
	if err != nil {
		t.Fatal(err)
	}
	return connection
}

func TestAConnectedGmailSendsTheThreadedReplyWithCopiesAndFilesInsteadOfSMTP(t *testing.T) {
	f := newFixture(t)
	fake, config := gmailStandIn(t)
	f.connectGmail(t, fake, config, "Help@Acme.example")
	storage, err := attachments.NewLocalDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	smtp := startSMTPServer(t)
	clock := time.Now().UTC().Add(time.Second)
	worker := gmailWorker(f, smtp, config, &clock, func(options *emailoutbox.Options) { options.Storage = storage })
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
	if process(t, worker) != 1 {
		t.Fatal("the reply was not attempted")
	}
	if delivery := f.delivery(t, reply.ID); delivery.Status != "sent" {
		t.Fatalf("delivery = %+v", delivery)
	}
	if got := smtp.mail(); len(got) != 0 {
		t.Fatalf("a Gmail-connected reply also went through SMTP: %d", len(got))
	}
	sent := fake.Messages()
	if len(sent) != 1 || sent[0].Account != "help@acme.example" {
		t.Fatalf("Gmail messages = %+v", sent)
	}
	parsed, err := mail.ReadMessage(bytes.NewReader(sent[0].Raw))
	if err != nil {
		t.Fatal(err)
	}
	from, _ := mail.ParseAddress(parsed.Header.Get("From"))
	replyTo, _ := mail.ParseAddress(parsed.Header.Get("Reply-To"))
	cc, _ := parsed.Header.AddressList("Cc")
	switch {
	case from == nil || from.Address != "help@acme.example" || from.Name != "Outbound Desk":
		t.Errorf("From = %q", parsed.Header.Get("From"))
	case replyTo == nil || !strings.HasPrefix(replyTo.Address, "support+r-") || !strings.HasSuffix(replyTo.Address, "@outbound.example.com"):
		t.Errorf("Reply-To = %q, want the support address with the conversation token", parsed.Header.Get("Reply-To"))
	case parsed.Header.Get("In-Reply-To") != "<export-1@customer.example>" || !strings.Contains(parsed.Header.Get("References"), "<export-1@customer.example>"):
		t.Errorf("threading headers: In-Reply-To %q References %q", parsed.Header.Get("In-Reply-To"), parsed.Header.Get("References"))
	case parsed.Header.Get("Message-ID") == "" || len(cc) != 1 || cc[0].Address != "boss@customer.example":
		t.Errorf("Message-ID %q, Cc %v", parsed.Header.Get("Message-ID"), cc)
	case !bytes.Contains(sent[0].Raw, []byte(`filename="fix.pdf"`)):
		t.Error("the file is not attached")
	}
}

// gmail.send cannot read back what Gmail delivered, so threading must not depend on Gmail keeping our
// Message-ID: the customer's answer threads through the Reply-To token.
func TestTheCustomersAnswerThreadsEvenWhenGmailRewritesTheMessageID(t *testing.T) {
	f := newFixture(t)
	fake, config := gmailStandIn(t)
	fake.Set(func(s *googletest.Server) { s.RewriteMessageID = true })
	f.connectGmail(t, fake, config, "help@acme.example")
	clock := time.Now().UTC().Add(time.Second)
	worker := gmailWorker(f, startSMTPServer(t), config, &clock)
	f.write(t, "outgoing", "Fixed now.")
	process(t, worker)
	sent := fake.Messages()
	if len(sent) != 1 {
		t.Fatalf("gmail messages = %d", len(sent))
	}
	parsed, err := mail.ReadMessage(bytes.NewReader(sent[0].Raw))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Trim(parsed.Header.Get("Message-ID"), "<>") == sent[0].MessageID {
		t.Fatal("the stand-in did not rewrite the Message-ID")
	}
	replyTo, _ := mail.ParseAddress(parsed.Header.Get("Reply-To"))
	answer, err := email.Parse([]byte("From: Olive Customer <olive@customer.example>\r\nTo: "+replyTo.Address+"\r\n"+
		"Subject: Re: Broken export\r\nMessage-ID: <answer-1@customer.example>\r\nIn-Reply-To: <"+sent[0].MessageID+">\r\n\r\nThanks!\r\n"), "")
	if err != nil {
		t.Fatal(err)
	}
	answer.Platform = true
	result, err := ingestEmail(t, context.Background(), f.store, f.workspaceID, answer)
	if err != nil || result.ConversationID == nil || *result.ConversationID != f.conversationID {
		t.Fatalf("answer went to %+v (err %v), want conversation %s", result, err, f.conversationID)
	}
}

func TestARevokedGrantFailsTheReplyVisiblyAndNeverFallsBackToSMTP(t *testing.T) {
	f := newFixture(t)
	fake, config := gmailStandIn(t)
	f.connectGmail(t, fake, config, "help@acme.example")
	fake.RevokeAll()
	smtp := startSMTPServer(t)
	clock := time.Now().UTC().Add(time.Second)
	worker := gmailWorker(f, smtp, config, &clock)
	reply := f.write(t, "outgoing", "Hello")
	process(t, worker)
	delivery := f.delivery(t, reply.ID)
	if delivery.Status != "failed" || delivery.LastError == nil || !strings.Contains(*delivery.LastError, "Reconnect Gmail") {
		t.Fatalf("delivery = %+v", delivery)
	}
	if len(smtp.mail()) != 0 || len(fake.Messages()) != 0 {
		t.Fatal("a reply left after the grant was revoked")
	}
	connection := f.mailboxStatus(t)
	if connection == nil || connection.Status != "failed" || connection.ErrorCode == nil || *connection.ErrorCode != domain.MailboxRevoked || connection.Error == nil {
		t.Fatalf("connection = %+v", connection)
	}
	// While it is broken, a new reply fails at once with the same reason and a retry is refused.
	next := f.write(t, "outgoing", "Second")
	if delivery := f.delivery(t, next.ID); delivery.Status != "failed" || !strings.Contains(*delivery.LastError, "Reconnect Gmail") {
		t.Fatalf("reply while broken = %+v", delivery)
	}
	if _, err := f.store.RetryDelivery(context.Background(), f.workspaceID, f.conversationID, reply.ID); !errors.Is(err, store.ErrOutboundNotReady) {
		t.Fatalf("retry while broken: %v", err)
	}
	// Reconnecting heals it: the retry is accepted and sent through Gmail.
	f.connectGmail(t, fake, config, "help@acme.example")
	if _, err := f.store.RetryDelivery(context.Background(), f.workspaceID, f.conversationID, reply.ID); err != nil {
		t.Fatal(err)
	}
	process(t, worker)
	if delivery := f.delivery(t, reply.ID); delivery.Status != "sent" || len(fake.Messages()) != 1 {
		t.Fatalf("after reconnect: %+v, gmail %d", delivery, len(fake.Messages()))
	}
}

func TestGmailRateLimitsWaitButAWithdrawnPermissionBreaksTheConnection(t *testing.T) {
	f := newFixture(t)
	fake, config := gmailStandIn(t)
	f.connectGmail(t, fake, config, "help@acme.example")
	smtp := startSMTPServer(t)
	clock := time.Now().UTC().Add(time.Second)
	worker := gmailWorker(f, smtp, config, &clock)
	fake.Set(func(s *googletest.Server) {
		s.SendFailures = []googletest.Failure{{Status: http.StatusForbidden, Reason: "userRateLimitExceeded"}}
	})
	reply := f.write(t, "outgoing", "Hello")
	process(t, worker)
	if delivery := f.delivery(t, reply.ID); delivery.Status != "queued" {
		t.Fatalf("rate-limited reply = %+v, want queued for a retry", delivery)
	}
	if connection := f.mailboxStatus(t); connection.Status != "connected" {
		t.Fatalf("a rate limit broke the connection: %+v", connection)
	}
	clock = clock.Add(time.Hour)
	process(t, worker)
	if delivery := f.delivery(t, reply.ID); delivery.Status != "sent" {
		t.Fatalf("after the wait = %+v", delivery)
	}

	fake.Set(func(s *googletest.Server) {
		s.SendFailures = []googletest.Failure{{Status: http.StatusForbidden, Reason: "insufficientPermissions"}}
	})
	second := f.write(t, "outgoing", "Again")
	process(t, worker)
	if delivery := f.delivery(t, second.ID); delivery.Status != "failed" || !strings.Contains(*delivery.LastError, "Reconnect Gmail") {
		t.Fatalf("permission refusal = %+v", delivery)
	}
	if connection := f.mailboxStatus(t); connection.Status != "failed" || *connection.ErrorCode != domain.MailboxPermission {
		t.Fatalf("connection = %+v", connection)
	}
	if len(smtp.mail()) != 0 {
		t.Fatal("SMTP was used")
	}
}

func TestAReplyAcceptedForGmailFailsWhenGmailIsDisconnectedFirst(t *testing.T) {
	f := newFixture(t)
	fake, config := gmailStandIn(t)
	f.connectGmail(t, fake, config, "help@acme.example")
	smtp := startSMTPServer(t)
	clock := time.Now().UTC().Add(time.Second)
	worker := gmailWorker(f, smtp, config, &clock)
	reply := f.write(t, "outgoing", "Hello")
	if _, err := f.store.DeleteMailboxConnection(context.Background(), f.workspaceID, f.emailChannelID, domain.MailboxGoogle); err != nil {
		t.Fatal(err)
	}
	process(t, worker)
	if delivery := f.delivery(t, reply.ID); delivery.Status != "failed" || *delivery.LastError != domain.MailboxDisconnectedNote(domain.MailboxGoogle) {
		t.Fatalf("delivery = %+v", delivery)
	}
	if len(smtp.mail()) != 0 || len(fake.Messages()) != 0 {
		t.Fatal("the reply left anyway")
	}
}

func TestAGmailOnlyServerQueuesAndSendsWithoutAnSMTPRelay(t *testing.T) {
	f := newFixture(t)
	f.store = f.store.WithOutbound(store.OutboundPolicy{Enabled: false, RequireVerifiedDomain: true, GmailEnabled: true})
	fake, config := gmailStandIn(t)
	f.connectGmail(t, fake, config, "help@acme.example")
	clock := time.Now().UTC().Add(time.Second)
	worker := emailoutbox.New(f.store, nil, emailoutbox.Options{Now: func() time.Time { return clock }, Google: &config})
	reply := f.write(t, "outgoing", "Hello")
	if delivery := f.delivery(t, reply.ID); delivery.Status != "queued" {
		t.Fatalf("reply on a Gmail-only server = %+v", delivery)
	}
	process(t, worker)
	if delivery := f.delivery(t, reply.ID); delivery.Status != "sent" || len(fake.Messages()) != 1 {
		t.Fatalf("delivery = %+v, gmail %d", delivery, len(fake.Messages()))
	}
	// Without Gmail and without a relay the reply fails with the reason, as before.
	if _, err := f.store.DeleteMailboxConnection(context.Background(), f.workspaceID, f.emailChannelID, domain.MailboxGoogle); err != nil {
		t.Fatal(err)
	}
	other := f.write(t, "outgoing", "No way out")
	if delivery := f.delivery(t, other.ID); delivery.Status != "failed" || !strings.Contains(*delivery.LastError, "ask whoever runs it to set up outgoing email") || strings.Contains(*delivery.LastError, "SMTP_HOST") {
		t.Fatalf("reply without any transport = %+v", delivery)
	}
}

func TestAGrantSealedForOneWorkspaceNeverSendsForAnother(t *testing.T) {
	f := newFixture(t)
	fake, config := gmailStandIn(t)
	grantA := f.connectGmail(t, fake, config, "a@acme.example")
	other := addWorkspace(t, f.pool, "Other Desk", "other.example.com")
	// Workspace B gets a row holding A's sealed grant, as a bug or a copied backup row would.
	if _, _, err := other.store.SaveMailboxConnection(context.Background(), other.workspaceID, other.emailChannelID, other.agentID, "a@acme.example", google.ScopeGmailSend, grantA, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	smtp := startSMTPServer(t)
	clock := time.Now().UTC().Add(time.Second)
	worker := gmailWorker(other, smtp, config, &clock)
	reply := other.write(t, "outgoing", "Hello")
	for range 4 {
		process(t, worker)
		clock = clock.Add(2 * time.Hour)
	}
	if delivery := other.delivery(t, reply.ID); delivery.Status != "failed" || !strings.Contains(*delivery.LastError, "ask whoever runs it") || strings.Contains(*delivery.LastError, "MAILBOX_TOKEN_KEYS") {
		t.Fatalf("delivery = %+v", delivery)
	}
	if len(fake.Messages()) != 0 {
		t.Fatal("workspace B sent with workspace A's Gmail grant")
	}
}

const (
	keyOne = "k1:AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="
	keyTwo = "k2:BAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQ="
)

func withKeys(t *testing.T, config google.Config, ring string) *google.Config {
	t.Helper()
	keys, err := tokenseal.ParseKeys(ring)
	if err != nil {
		t.Fatal(err)
	}
	config.Keys = keys
	return &config
}

// A server started with the wrong key ring (a rolling deploy, a typo) cannot read the grant: the reply
// waits and retries, the connection is not marked broken, and the right keys send it without reconnecting.
func TestAGrantThatDoesNotOpenWaitsForTheRightKeyInsteadOfBreakingTheConnection(t *testing.T) {
	f := newFixture(t)
	fake, config := gmailStandIn(t)
	f.connectGmail(t, fake, config, "help@acme.example")
	clock := time.Now().UTC().Add(time.Second)
	wrong := gmailWorker(f, startSMTPServer(t), config, &clock, func(options *emailoutbox.Options) { options.Google = withKeys(t, config, keyTwo) })
	reply := f.write(t, "outgoing", "Hello")
	process(t, wrong)
	if delivery := f.delivery(t, reply.ID); delivery.Status != "queued" || !strings.Contains(*delivery.LastError, "its key for stored connections") || strings.Contains(*delivery.LastError, "MAILBOX_TOKEN_KEYS") {
		t.Fatalf("delivery = %+v, want queued for a retry with the reason", delivery)
	}
	if connection := f.mailboxStatus(t); connection.Status != "connected" {
		t.Fatalf("a key problem of this server broke the stored connection: %+v", connection)
	}
	clock = clock.Add(time.Hour)
	process(t, gmailWorker(f, startSMTPServer(t), config, &clock))
	if delivery := f.delivery(t, reply.ID); delivery.Status != "sent" || len(fake.Messages()) != 1 {
		t.Fatalf("with the right keys: %+v, gmail %d", delivery, len(fake.Messages()))
	}
}

// A grant sealed with an older key is sealed again with the first key when it is used, so the old key can
// be removed; the new seal is what the same delivery then marks when Gmail refuses.
func TestAGrantSealedWithAnOlderKeyIsSealedAgainWithTheFirstKey(t *testing.T) {
	f := newFixture(t)
	fake, config := gmailStandIn(t) // seals with k1
	f.connectGmail(t, fake, config, "help@acme.example")
	clock := time.Now().UTC().Add(time.Second)
	rotated := withKeys(t, config, keyTwo+","+keyOne)
	worker := gmailWorker(f, startSMTPServer(t), config, &clock, func(options *emailoutbox.Options) { options.Google = rotated })
	f.write(t, "outgoing", "Hello")
	process(t, worker)
	if keyID := queryOne(t, f, `SELECT token_key_id FROM mailbox_connections`); keyID != "k2" {
		t.Fatalf("grant still sealed with %s", keyID)
	}
	// Only k2 left: the re-sealed grant still sends.
	clock = clock.Add(time.Minute)
	f.write(t, "outgoing", "Again")
	process(t, gmailWorker(f, startSMTPServer(t), config, &clock, func(options *emailoutbox.Options) { options.Google = withKeys(t, config, keyTwo) }))
	if len(fake.Messages()) != 2 {
		t.Fatalf("gmail messages = %d, want 2", len(fake.Messages()))
	}

	// Seal again with k1 first, then make Gmail refuse in the same delivery that re-seals: the connection
	// must be marked with the grant it now holds.
	back := withKeys(t, config, keyOne+","+keyTwo)
	fake.Set(func(s *googletest.Server) {
		s.SendFailures = []googletest.Failure{{Status: http.StatusForbidden, Reason: "insufficientPermissions"}}
	})
	clock = clock.Add(time.Minute)
	f.write(t, "outgoing", "Third")
	process(t, gmailWorker(f, startSMTPServer(t), config, &clock, func(options *emailoutbox.Options) { options.Google = back }))
	if connection := f.mailboxStatus(t); connection.Status != "failed" || *connection.ErrorCode != domain.MailboxPermission {
		t.Fatalf("connection = %+v, want failed: the refusal was recorded against a stale grant", connection)
	}
}

func queryOne(t *testing.T, f fixture, sql string) string {
	t.Helper()
	var value string
	if err := f.pool.QueryRow(context.Background(), sql).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}
