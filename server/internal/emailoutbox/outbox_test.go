package emailoutbox_test

import (
	"context"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/emailoutbox"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

type fixture struct {
	store          *store.Store
	pool           *pgxpool.Pool
	workspaceID    string
	agentID        string
	conversationID string
	incomingID     string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	pool := testPool(t)
	if err := database.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	return addWorkspace(t, pool, "Outbound Desk", "outbound.example.com")
}

// addWorkspace creates a workspace with a connected email channel and one inbound conversation on the
// pool; its domain also names its admin and its customer's Message-ID.
func addWorkspace(t *testing.T, pool *pgxpool.Pool, name, domainName string) fixture {
	t.Helper()
	ctx := context.Background()
	workspace, _, err := auth.New(pool, "https://desk.example.com").CreateWorkspace(ctx, name, "admin@"+domainName)
	if err != nil {
		t.Fatal(err)
	}
	st := channelStore(t, pool).WithOutbound(store.OutboundPolicy{Enabled: true, GmailEnabled: true})
	if _, err := st.UpsertEmailChannel(ctx, workspace.ID, "support@"+domainName, nil); err != nil {
		t.Fatal(err)
	}
	parsed, err := email.Parse([]byte("From: Olive Customer <olive@customer.example>\r\nTo: support@"+domainName+"\r\n"+
		"Subject: Broken export\r\nMessage-ID: <export-1@customer.example>\r\n\r\nExport fails.\r\n"), "")
	if err != nil {
		t.Fatal(err)
	}
	parsed.Platform = true // customer mail arrives through the platform forwarder
	result, err := ingestEmail(t, ctx, st, workspace.ID, parsed)
	if err != nil || result.ConversationID == nil {
		t.Fatalf("ingest: %+v %v", result, err)
	}
	var agentID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM agents WHERE workspace_id=$1`, workspace.ID).Scan(&agentID); err != nil {
		t.Fatal(err)
	}
	return fixture{store: st, pool: pool, workspaceID: workspace.ID, agentID: agentID, conversationID: *result.ConversationID, incomingID: *result.MessageID}
}

func (f fixture) write(t *testing.T, kind, body string) domain.Message {
	t.Helper()
	message, err := f.store.CreateMessage(context.Background(), f.workspaceID, f.conversationID, store.Actor{AgentID: f.agentID}, kind, body)
	if err != nil {
		t.Fatal(err)
	}
	return message
}

func (f fixture) delivery(t *testing.T, messageID string) *domain.MessageDelivery {
	t.Helper()
	items, err := f.store.ListMessages(context.Background(), f.workspaceID, f.conversationID)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ID == messageID {
			return item.Delivery
		}
	}
	t.Fatalf("message %s not found", messageID)
	return nil
}

func newWorker(f fixture, server *smtpServer, clock *time.Time, mutate ...func(*emailoutbox.Options)) *emailoutbox.Worker {
	sender := email.SMTPSender{Config: email.SMTPConfig{Host: "127.0.0.1", Port: server.port(), StartTLS: false}}
	options := emailoutbox.Options{Now: func() time.Time { return *clock }, MaxAttempts: 3, Lease: time.Minute, FromPolicy: email.FromPolicyAny}
	for _, change := range mutate {
		change(&options)
	}
	return emailoutbox.New(f.store, sender, options)
}

func process(t *testing.T, worker *emailoutbox.Worker) int {
	t.Helper()
	count, err := worker.ProcessDue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return count
}

func TestOutboundRetriesWithBackoffThenSendsThreadedReply(t *testing.T) {
	f := newFixture(t)
	server := startSMTPServer(t, "451 4.3.0 Try again later")
	clock := time.Now().UTC().Add(time.Second)
	worker := newWorker(f, server, &clock)
	reply := f.write(t, "outgoing", "Please retry the **export**.")

	if process(t, worker) != 1 {
		t.Fatal("first attempt was not made")
	}
	delivery := f.delivery(t, reply.ID)
	if delivery == nil || delivery.Status != "queued" || delivery.Attempts != 1 || delivery.LastError == nil || !strings.Contains(*delivery.LastError, "451") {
		t.Fatalf("transient failure must stay queued with the error: %+v", delivery)
	}
	if process(t, worker) != 0 {
		t.Fatal("retry must wait for the backoff")
	}
	clock = clock.Add(31 * time.Second)
	if process(t, worker) != 1 {
		t.Fatal("retry after backoff was not made")
	}
	delivery = f.delivery(t, reply.ID)
	if delivery.Status != "sent" || delivery.Attempts != 2 || delivery.SentAt == nil {
		t.Fatalf("delivery not marked sent: %+v", delivery)
	}

	received := server.mail()
	if len(received) != 1 || received[0].From != "support@outbound.example.com" || len(received[0].To) != 1 || received[0].To[0] != "olive@customer.example" {
		t.Fatalf("unexpected envelope: %+v", received)
	}
	parsed, err := mail.ReadMessage(strings.NewReader(received[0].Data))
	if err != nil {
		t.Fatal(err)
	}
	var stored, token string
	if err := f.pool.QueryRow(context.Background(), `SELECT m.email_message_id,c.email_reply_token FROM messages m JOIN conversations c ON c.id=m.conversation_id WHERE m.id=$1`, reply.ID).Scan(&stored, &token); err != nil {
		t.Fatal(err)
	}
	header := parsed.Header
	checks := map[string]string{
		"Message-Id":  "<" + stored + ">",
		"In-Reply-To": "<export-1@customer.example>",
		"References":  "<export-1@customer.example>",
		"Subject":     "Re: Broken export",
		"To":          `"Olive Customer" <olive@customer.example>`,
		"From":        `"Outbound Desk" <support@outbound.example.com>`,
		"Reply-To":    `"Outbound Desk" <support+r-` + token + `@outbound.example.com>`,
	}
	for name, want := range checks {
		if got := header.Get(name); got != want {
			t.Errorf("%s=%q, want %q", name, got, want)
		}
	}
	if !strings.HasSuffix(stored, "@outbound.example.com") || token == "" {
		t.Fatalf("Message-ID must be on our domain and the token set: %q %q", stored, token)
	}
	if !strings.Contains(received[0].Data, "Please retry the **export**.") {
		t.Fatalf("body missing: %s", received[0].Data)
	}
}

func TestOutboundPermanentFailureIsVisibleAndManualRetrySends(t *testing.T) {
	f := newFixture(t)
	server := startSMTPServer(t, "550 5.1.1 Mailbox unavailable")
	clock := time.Now().UTC().Add(time.Second)
	worker := newWorker(f, server, &clock)
	reply := f.write(t, "outgoing", "Hello.")
	process(t, worker)
	if delivery := f.delivery(t, reply.ID); delivery.Status != "failed" || delivery.Attempts != 1 {
		t.Fatalf("5xx must fail without retrying: %+v", delivery)
	}
	clock = clock.Add(2 * time.Hour)
	if process(t, worker) != 0 {
		t.Fatal("a failed delivery must not be retried automatically")
	}
	retried, err := f.store.RetryDelivery(context.Background(), f.workspaceID, f.conversationID, reply.ID)
	if err != nil || retried.Delivery == nil || retried.Delivery.Status != "queued" || retried.Delivery.Attempts != 0 {
		t.Fatalf("manual retry: %+v %v", retried.Delivery, err)
	}
	clock = time.Now().UTC().Add(time.Second)
	process(t, worker)
	if delivery := f.delivery(t, reply.ID); delivery.Status != "sent" {
		t.Fatalf("manual retry did not send: %+v", delivery)
	}
	if len(server.mail()) != 1 {
		t.Fatalf("want exactly one accepted mail, got %d", len(server.mail()))
	}
}

func TestOutboundGivesUpAfterMaxAttempts(t *testing.T) {
	f := newFixture(t)
	server := startSMTPServer(t, "421 busy", "421 busy", "421 busy", "421 busy")
	clock := time.Now().UTC().Add(time.Second)
	worker := newWorker(f, server, &clock)
	reply := f.write(t, "outgoing", "Hello.")
	for range 3 {
		process(t, worker)
		clock = clock.Add(2 * time.Hour)
	}
	if delivery := f.delivery(t, reply.ID); delivery.Status != "failed" || delivery.Attempts != 3 {
		t.Fatalf("delivery must fail after max attempts: %+v", delivery)
	}
}

func TestNotesAreNeverEmailed(t *testing.T) {
	f := newFixture(t)
	server := startSMTPServer(t)
	clock := time.Now().UTC().Add(time.Second)
	worker := newWorker(f, server, &clock)
	note := f.write(t, "note", "Internal: customer is on the old plan.")
	reply := f.write(t, "outgoing", "Public answer.")
	process(t, worker)
	if f.delivery(t, note.ID) != nil {
		t.Fatal("note has a delivery")
	}
	received := server.mail()
	if len(received) != 1 || strings.Contains(received[0].Data, "old plan") || !strings.Contains(received[0].Data, "Public answer.") {
		t.Fatalf("note leaked or reply missing: %+v", received)
	}
	if f.delivery(t, reply.ID).Status != "sent" {
		t.Fatal("reply not sent")
	}
}

func TestExpiredLeaseIsResentWithTheSameMessageID(t *testing.T) {
	f := newFixture(t)
	server := startSMTPServer(t)
	clock := time.Now().UTC().Add(time.Second)
	worker := newWorker(f, server, &clock)
	f.write(t, "outgoing", "At least once.")
	claimed, err := f.store.ClaimDueOutbox(context.Background(), clock, 10, time.Minute)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: %d %v", len(claimed), err)
	}
	leased, err := f.store.EmailDelivery(context.Background(), claimed[0].WorkspaceID, claimed[0].OutboxID, claimed[0].Attempts)
	if err != nil {
		t.Fatal(err)
	}
	if process(t, worker) != 0 {
		t.Fatal("a leased delivery must not be sent twice concurrently")
	}
	clock = clock.Add(2 * time.Minute)
	if process(t, worker) != 1 {
		t.Fatal("an expired lease must be picked up again")
	}
	received := server.mail()
	if len(received) != 1 || !strings.Contains(received[0].Data, "Message-ID: <"+leased.EmailMessageID+">") {
		t.Fatalf("resend must reuse the Message-ID %s: %+v", leased.EmailMessageID, received)
	}
}

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return databasetest.Pool(t)
}

type writtenReply struct{ ID, ConversationID string }

// secondConversationReply writes a reply on a new conversation with its own contact.
func (f fixture) secondConversationReply(t *testing.T, contactEmail, body string) writtenReply {
	t.Helper()
	ctx := context.Background()
	contact, err := f.store.CreateContact(ctx, f.workspaceID, "Second", contactEmail)
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := f.store.CreateConversation(ctx, f.workspaceID, contact.ID, "Second thread")
	if err != nil {
		t.Fatal(err)
	}
	message, err := f.store.CreateMessage(ctx, f.workspaceID, conversation.ID, store.Actor{AgentID: f.agentID}, "outgoing", body)
	if err != nil {
		t.Fatal(err)
	}
	return writtenReply{ID: message.ID, ConversationID: conversation.ID}
}

func (f fixture) deliveryIn(t *testing.T, conversationID, messageID string) *domain.MessageDelivery {
	t.Helper()
	items, err := f.store.ListMessages(context.Background(), f.workspaceID, conversationID)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ID == messageID {
			return item.Delivery
		}
	}
	t.Fatalf("message %s not found", messageID)
	return nil
}

// An agent who pressed send while the workspace was open has finished that act; the lock must not
// strand the reply in the queue.
func TestReplyQueuedBeforeTheLockIsStillSent(t *testing.T) {
	f := newFixture(t)
	server := startSMTPServer(t)
	clock := time.Now().UTC().Add(time.Second)
	f.store = f.store.WithPlanLimits(true)
	worker := newWorker(f, server, &clock)
	reply := f.write(t, "outgoing", "Sent just before the trial ended.")
	if _, err := f.pool.Exec(context.Background(), `UPDATE workspaces SET trial_ends_at=$2 WHERE id=$1`, f.workspaceID, clock.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if process(t, worker) != 1 {
		t.Fatal("the queued reply of a locked workspace was not attempted")
	}
	if delivery := f.delivery(t, reply.ID); delivery == nil || delivery.Status != "sent" {
		t.Fatalf("delivery=%+v, want sent", delivery)
	}
	if received := server.mail(); len(received) != 1 || !strings.Contains(received[0].Data, "Sent just before the trial ended.") {
		t.Fatalf("mail=%+v, want the reply delivered", received)
	}
}
