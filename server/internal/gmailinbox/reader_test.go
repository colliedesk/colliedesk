package gmailinbox_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channelemail"
	"github.com/colliedesk/colliedesk/server/internal/channels/pipeline"
	"github.com/colliedesk/colliedesk/server/internal/channels/registry"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/gmailinbox"
	"github.com/colliedesk/colliedesk/server/internal/google"
	"github.com/colliedesk/colliedesk/server/internal/google/googletest"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
	"github.com/jackc/pgx/v5/pgxpool"
)

const redirect = "https://desk.example.com/api/v1/channels/email/gmail/callback"

type desk struct {
	pool        *pgxpool.Pool
	store       *store.Store
	workspaceID string
	channelID   string
	agentID     string
}

type harness struct {
	t      *testing.T
	pool   *pgxpool.Pool
	fake   *googletest.Server
	config google.Config
	clock  time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	pool := databasetest.Pool(t)
	if err := database.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	fake := googletest.New("client-1", "not-a-secret")
	server := fake.Start()
	t.Cleanup(server.Close)
	keys, err := tokenseal.ParseKeys("k1:AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=")
	if err != nil {
		t.Fatal(err)
	}
	config := google.Config{ClientID: "client-1", ClientSecret: "not-a-secret", Keys: keys, Base: server.URL, ReadMailbox: true}
	return &harness{t: t, pool: pool, fake: fake, config: config, clock: time.Now().UTC()}
}

func (h *harness) store() *store.Store {
	storage, err := attachments.NewLocalDisk(h.t.TempDir())
	if err != nil {
		h.t.Fatal(err)
	}
	return store.New(h.pool).WithChannels(registry.New(registry.Config{})).WithObjects(storage)
}

// desk creates a workspace whose email channel has the support address.
func (h *harness) desk(name, support string) desk {
	h.t.Helper()
	ctx := context.Background()
	workspace, _, err := auth.New(h.pool, "https://desk.example.com").CreateWorkspace(ctx, name, "admin@"+strings.Split(support, "@")[1])
	if err != nil {
		h.t.Fatal(err)
	}
	st := h.store()
	channel, err := st.UpsertEmailChannel(ctx, workspace.ID, support, nil)
	if err != nil {
		h.t.Fatal(err)
	}
	var agentID string
	if err := h.pool.QueryRow(ctx, `SELECT id::text FROM agents WHERE workspace_id=$1`, workspace.ID).Scan(&agentID); err != nil {
		h.t.Fatal(err)
	}
	return desk{pool: h.pool, store: st, workspaceID: workspace.ID, channelID: channel.ChannelID, agentID: agentID}
}

// connect runs the consent as account, granting scope (the consent's own when empty), and stores the grant.
func (h *harness) connect(d desk, account string, config google.Config, scope string) {
	h.t.Helper()
	h.fake.Set(func(s *googletest.Server) { s.GrantScope = scope })
	target, err := h.fake.Consent(config.AuthorizationURL(redirect, "state", "verifier"), account)
	if err != nil {
		h.t.Fatal(err)
	}
	parsed, _ := url.Parse(target)
	grant, err := config.Exchange(context.Background(), parsed.Query().Get("code"), "verifier", redirect)
	if err != nil {
		h.t.Fatal(err)
	}
	keyID, sealed, err := config.Keys.Seal([]byte(grant.RefreshToken), google.GrantContext(d.workspaceID))
	if err != nil {
		h.t.Fatal(err)
	}
	h.clock = h.clock.Add(time.Second)
	if _, _, err := d.store.SaveMailboxConnection(context.Background(), d.workspaceID, d.channelID, d.agentID, grant.Address, strings.Join(grant.Scopes, " "),
		store.SealedGrant{KeyID: keyID, Sealed: sealed, Provider: domain.MailboxGoogle}, h.clock); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) reader(config google.Config) *gmailinbox.Reader {
	st := h.store()
	return &gmailinbox.Reader{Store: st, Google: config, Pipeline: pipeline.New(st, nil)}
}

func (h *harness) readDue(reader *gmailinbox.Reader) int {
	h.t.Helper()
	count, err := reader.ReadDue(context.Background())
	if err != nil {
		h.t.Fatal(err)
	}
	return count
}

// customerMail is a message as Gmail keeps it: its own Authentication-Results on top.
func customerMail(from, to, subject, messageID string) []byte {
	return []byte("Delivered-To: " + to + "\r\nAuthentication-Results: mx.google.com;\r\n       dkim=pass header.i=@customer.example;\r\n       spf=pass smtp.mailfrom=" + from +
		"\r\nFrom: Olive <" + from + ">\r\nTo: " + to + "\r\nSubject: " + subject + "\r\nMessage-ID: <" + messageID + ">\r\nDate: Thu, 19 Nov 2026 10:00:00 +0000\r\n\r\n" + subject + " body.\r\n")
}

type stored struct {
	workspaceID, via, authentication string
}

func (h *harness) message(messageID string) []stored {
	h.t.Helper()
	rows, err := h.pool.Query(context.Background(), `SELECT workspace_id::text,COALESCE(email_meta->>'via',''),COALESCE(email_meta->>'authentication','')
		FROM messages WHERE external_id=$1`, "email:"+messageID)
	if err != nil {
		h.t.Fatal(err)
	}
	defer rows.Close()
	var found []stored
	for rows.Next() {
		var row stored
		if err := rows.Scan(&row.workspaceID, &row.via, &row.authentication); err != nil {
			h.t.Fatal(err)
		}
		found = append(found, row)
	}
	return found
}

func (h *harness) inboxState(d desk) (history string, errorCode string, status string) {
	h.t.Helper()
	if err := h.pool.QueryRow(context.Background(), `SELECT COALESCE(inbox_history_id,''),COALESCE(inbox_error_code,''),status FROM mailbox_connections
		WHERE workspace_id=$1 AND channel_id=$2`, d.workspaceID, d.channelID).Scan(&history, &errorCode, &status); err != nil {
		h.t.Fatal(err)
	}
	return
}

func TestNewInboxMailOpensConversationsInItsOwnWorkspaceOnlyFromTheConnection(t *testing.T) {
	h := newHarness(t)
	acme := h.desk("Acme", "support@acme.example")
	beta := h.desk("Beta", "help@beta.example")
	h.connect(acme, "support@acme.example", h.config, "")
	h.connect(beta, "help@beta.example", h.config, "")
	// Mail already in the inbox at the connection is not brought in.
	h.fake.Deliver("support@acme.example", customerMail("old@customer.example", "support@acme.example", "Old", "old-1@customer.example"), h.clock.Add(-time.Hour))
	// Mail that arrives between the consent and the first read is the connection's.
	h.fake.Deliver("support@acme.example", customerMail("early@customer.example", "support@acme.example", "Early", "early-1@customer.example"), h.clock.Add(2*time.Second))
	reader := h.reader(h.config)
	if got := h.readDue(reader); got != 1 || len(h.message("early-1@customer.example")) != 1 {
		t.Fatalf("the first read brought in %d messages, want only the one after the connection", got)
	}
	if history, _, _ := h.inboxState(acme); history == "" {
		t.Fatal("the first read recorded no history id")
	}
	h.fake.Deliver("support@acme.example", customerMail("olive@customer.example", "support@acme.example", "Broken export", "export-1@customer.example"), h.clock)
	h.fake.Deliver("support@acme.example", customerMail("support@acme.example", "olive@customer.example", "Our own reply", "reply-1@acme.example"), h.clock, "SENT")
	h.fake.Deliver("help@beta.example", customerMail("bob@customer.example", "help@beta.example", "Invoice", "invoice-1@customer.example"), h.clock)
	if got := h.readDue(reader); got != 2 {
		t.Fatalf("read brought in %d messages, want 2 (one per workspace)", got)
	}
	export := h.message("export-1@customer.example")
	if len(export) != 1 || export[0].workspaceID != acme.workspaceID || export[0].via != domain.ViaGmail || export[0].authentication != email.AuthPass {
		t.Fatalf("Acme's message = %+v", export)
	}
	if invoice := h.message("invoice-1@customer.example"); len(invoice) != 1 || invoice[0].workspaceID != beta.workspaceID {
		t.Fatalf("Beta's message = %+v", invoice)
	}
	// Mail read from the support mailbox shows the mailbox reaches Colliedesk, as forwarded mail does.
	state, err := acme.store.EmailSettingsState(context.Background(), acme.workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range state.Channels {
		if item.ChannelID == acme.channelID && !item.Forwarded {
			t.Fatal("mail read from the Gmail inbox does not count as mail through the support mailbox")
		}
	}
	if old, sent := h.message("old-1@customer.example"), h.message("reply-1@acme.example"); len(old) != 0 || len(sent) != 0 {
		t.Fatalf("mail from before the connection or a sent copy was brought in: %+v %+v", old, sent)
	}
	// Nothing new: nothing brought in, and the same message forwarded later is a duplicate, not a second copy.
	if got := h.readDue(reader); got != 0 {
		t.Fatalf("a read with nothing new brought in %d", got)
	}
	parsed, err := email.Parse(customerMail("olive@customer.example", "support@acme.example", "Broken export", "export-1@customer.example"), "")
	if err != nil {
		t.Fatal(err)
	}
	parsed.Platform = true
	channel, _, err := acme.store.DefaultChannelOfType(context.Background(), acme.workspaceID, channelemail.Type)
	if err != nil {
		t.Fatal(err)
	}
	results, err := pipeline.New(h.store(), nil).Receive(context.Background(), channel, []channels.Raw{{Parsed: parsed}})
	if err != nil || results[0].Status != channels.ResultDuplicate {
		t.Fatalf("forwarded copy: %+v %v", results, err)
	}
}

func TestAnAccountLargerThanTheSupportAddressBringsInOnlyMailToIt(t *testing.T) {
	h := newHarness(t)
	acme := h.desk("Acme", "support@acme.example")
	h.connect(acme, "olivia@acme.example", h.config, "")
	reader := h.reader(h.config)
	h.readDue(reader)
	h.fake.Deliver("olivia@acme.example", customerMail("c@customer.example", "support@acme.example", "For support", "alias-1@customer.example"), h.clock)
	h.fake.Deliver("olivia@acme.example", []byte("From: Mum <mum@family.example>\r\nTo: olivia@acme.example\r\nSubject: Dinner\r\nMessage-ID: <dinner@family.example>\r\n\r\nSunday?\r\n"), h.clock)
	if got := h.readDue(reader); got != 1 {
		t.Fatalf("brought in %d, want only the mail to support@", got)
	}
	if personal := h.message("dinner@family.example"); len(personal) != 0 {
		t.Fatalf("personal mail of the account was brought in: %+v", personal)
	}
}

func TestAHistoryGoneListsTheInboxAgainWithoutDuplicates(t *testing.T) {
	h := newHarness(t)
	acme := h.desk("Acme", "support@acme.example")
	h.connect(acme, "support@acme.example", h.config, "")
	reader := h.reader(h.config)
	h.readDue(reader)
	h.fake.Deliver("support@acme.example", customerMail("a@customer.example", "support@acme.example", "First", "first@customer.example"), time.Now())
	if got := h.readDue(reader); got != 1 {
		t.Fatalf("first read brought in %d", got)
	}
	h.fake.ForgetHistory()
	h.fake.Deliver("support@acme.example", customerMail("b@customer.example", "support@acme.example", "Second", "second@customer.example"), time.Now())
	if got := h.readDue(reader); got != 1 {
		t.Fatalf("read after the history was gone brought in %d, want only the new one", got)
	}
	if first, second := h.message("first@customer.example"), h.message("second@customer.example"); len(first) != 1 || len(second) != 1 {
		t.Fatalf("first %+v second %+v", first, second)
	}
	h.fake.Deliver("support@acme.example", customerMail("c@customer.example", "support@acme.example", "Third", "third@customer.example"), time.Now())
	if got := h.readDue(reader); got != 1 {
		t.Fatalf("history was not picked up again after listing: %d", got)
	}
}

func TestReadingStopsWhenGoogleRefusesItAndSendingIsUntouched(t *testing.T) {
	h := newHarness(t)
	acme := h.desk("Acme", "support@acme.example")
	sendOnly := h.config
	sendOnly.ReadMailbox = false
	// A send-only grant is never read, and a server with reading off reads nothing.
	h.connect(acme, "support@acme.example", sendOnly, "")
	reads := h.fake.Reads()
	h.readDue(h.reader(h.config))
	if h.fake.Reads() != reads {
		t.Fatal("a send-only grant was read")
	}
	h.connect(acme, "support@acme.example", h.config, "")
	h.readDue(h.reader(sendOnly))
	if h.fake.Reads() != reads {
		t.Fatal("a server with reading off read a mailbox")
	}
	reader := h.reader(h.config)
	h.readDue(reader)
	h.fake.Set(func(s *googletest.Server) {
		s.ReadFailures = []googletest.Failure{{Status: http.StatusTooManyRequests, Reason: "rateLimitExceeded"}}
	})
	h.readDue(reader)
	if _, code, _ := h.inboxState(acme); code != "" {
		t.Fatalf("a rate limit stopped reading: %q", code)
	}
	h.fake.Set(func(s *googletest.Server) {
		s.ReadFailures = []googletest.Failure{{Status: http.StatusForbidden, Reason: "insufficientPermissions"}}
	})
	h.readDue(reader)
	if _, code, status := h.inboxState(acme); code != domain.MailboxPermission || status != domain.MailboxConnected {
		t.Fatalf("after Google refused reading: inbox error %q, connection %q", code, status)
	}
	reads = h.fake.Reads()
	h.readDue(reader)
	if h.fake.Reads() != reads {
		t.Fatal("a refused inbox was read again")
	}
	// Reconnecting is the way out.
	h.connect(acme, "support@acme.example", h.config, "")
	if history, code, _ := h.inboxState(acme); code != "" || history != "" {
		t.Fatalf("reconnect kept the refusal or the old history: %q %q", code, history)
	}
	h.readDue(reader)
	if history, _, _ := h.inboxState(acme); history == "" {
		t.Fatal("the reconnected mailbox is not read")
	}
}

func TestARevokedGrantIsMarkedFailedLikeASend(t *testing.T) {
	h := newHarness(t)
	acme := h.desk("Acme", "support@acme.example")
	h.connect(acme, "support@acme.example", h.config, "")
	h.fake.RevokeAll()
	if _, err := h.reader(h.config).Read(context.Background(), acme.workspaceID, acme.channelID); err == nil {
		t.Fatal("a revoked grant read without error")
	}
	if _, _, status := h.inboxState(acme); status != domain.MailboxFailed {
		t.Fatalf("connection = %q, want failed", status)
	}

}
