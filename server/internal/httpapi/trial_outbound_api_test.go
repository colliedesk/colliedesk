package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

const restrictedCode = `"trial_outbound_restricted"`
const limitCode = `"trial_outbound_limit"`

func (f limitsFixture) reply(t *testing.T, token, conversation string) *responseView {
	t.Helper()
	response := request(t, f.api, http.MethodPost, "/api/v1/conversations/"+conversation+"/messages",
		map[string]any{"kind": "outgoing", "body": "Thanks for writing"}, token, nil)
	return &responseView{Code: response.Code, Body: response.Body.String()}
}

func (f limitsFixture) wantReplyQueued(t *testing.T, conversation, what string) {
	t.Helper()
	if got := f.reply(t, f.adminA, conversation); got.Code != http.StatusCreated {
		t.Fatalf("%s: status=%d body=%s, want 201", what, got.Code, got.Body)
	}
}

func (f limitsFixture) outboxCount(t *testing.T, workspace string) string {
	t.Helper()
	return queryString(t, f.pool, `SELECT count(*)::text FROM email_outbox WHERE workspace_id=$1`, workspace)
}

// agentStarted creates a contact and a conversation the way an agent does: no customer message.
func (f limitsFixture) agentStarted(t *testing.T) string {
	t.Helper()
	contact := create(t, f.api, f.adminA, "/api/v1/contacts", map[string]any{"name": "Cold Lead", "email": "lead@cold.example"})
	return create(t, f.api, f.adminA, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Hello from us"})
}

// seedOutbox adds count sent-or-queued deliveries created at the given moment, as if replies had
// gone out then. They hang off one throwaway conversation of the workspace.
func (f limitsFixture) seedOutbox(t *testing.T, workspace string, count int, status string, createdAt time.Time) {
	t.Helper()
	_, err := f.pool.Exec(context.Background(), `
		WITH conv AS (SELECT id FROM conversations WHERE workspace_id=$1 ORDER BY created_at LIMIT 1),
		m AS (INSERT INTO messages(workspace_id,conversation_id,kind,body)
			SELECT $1,conv.id,'outgoing','seeded '||n FROM conv, generate_series(1,$2::int) n RETURNING id)
		INSERT INTO email_outbox(workspace_id,message_id,status,created_at)
		SELECT $1,id,$3,$4 FROM m`, workspace, count, status, createdAt)
	if err != nil {
		t.Fatal(err)
	}
}

func (f limitsFixture) verifyDomain(t *testing.T, workspace string) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `UPDATE email_channels SET domain_verified_at=$2 WHERE workspace_id=$1`, workspace, f.clock.Now()); err != nil {
		t.Fatal(err)
	}
}

func wantStatus(t *testing.T, got *responseView, status int, code, what string) {
	t.Helper()
	if got.Code != status || !strings.Contains(got.Body, code) {
		t.Fatalf("%s: status=%d body=%s, want %d %s", what, got.Code, got.Body, status, code)
	}
}

func TestTrialRepliesToCustomersWhoWroteFirst(t *testing.T) {
	f := newLimitsFixture(t, true)
	conversation := f.conversationA(t)
	f.wantReplyQueued(t, conversation, "reply to inbound mail with auth none")
	if got := queryString(t, f.pool, `SELECT status FROM email_outbox WHERE workspace_id=$1`, f.workspaceA); got != "queued" {
		t.Fatalf("outbox status=%q, want queued", got)
	}
}

func TestTrialRefusesRepliesInAgentStartedConversations(t *testing.T) {
	f := newLimitsFixture(t, true)
	conversation := f.agentStarted(t)
	messages, outbox := f.messageCount(t, f.workspaceA), f.outboxCount(t, f.workspaceA)
	got := f.reply(t, f.adminA, conversation)
	wantStatus(t, got, http.StatusForbidden, restrictedCode, "reply to an agent-started conversation")
	if !strings.Contains(got.Body, "wrote to you first") {
		t.Fatalf("message must say why and how out: %s", got.Body)
	}
	if f.messageCount(t, f.workspaceA) != messages || f.outboxCount(t, f.workspaceA) != outbox {
		t.Fatal("a refused reply must leave no message and no outbox row")
	}
	// A note is never email.
	if note := f.postNote(t, f.adminA, conversation); note.Code != http.StatusCreated {
		t.Fatalf("note status=%d body=%s", note.Code, note.Body)
	}
}

func TestTrialRefusesRepliesToSpoofedSenders(t *testing.T) {
	f := newLimitsFixture(t, true)
	failed := inboundOK(t, f.api, f.secretA, mail([]string{
		"From: victim@customer.example", "To: support@limits-alpha.example.com", "Subject: Forged",
		"Message-ID: <forged@customer.example>",
		"Authentication-Results: mx.example.net; spf=fail smtp.mailfrom=customer.example; dkim=none; dmarc=fail header.from=customer.example",
	}, "Forged."))
	if failed.ConversationID == nil || failed.Authentication != "fail" {
		t.Fatalf("fixture: %+v", failed)
	}
	wantStatus(t, f.reply(t, f.adminA, *failed.ConversationID), http.StatusForbidden, restrictedCode, "reply to dmarc=fail mail")
	passed := inboundOK(t, f.api, f.secretA, mail([]string{
		"From: real@customer.example", "To: support@limits-alpha.example.com", "Subject: Genuine",
		"Message-ID: <genuine@customer.example>", passingAuth,
	}, "Genuine."))
	f.wantReplyQueued(t, *passed.ConversationID, "reply to dmarc=pass mail")
}

func TestTrialRepliesToImportedHistory(t *testing.T) {
	f := newLimitsFixture(t, true)
	conversation := f.agentStarted(t)
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO messages(workspace_id,conversation_id,kind,body,origin)
		VALUES($1,$2,'incoming','old ticket','{"system":"freshdesk","external_id":"9"}')`, f.workspaceA, conversation); err != nil {
		t.Fatal(err)
	}
	f.wantReplyQueued(t, conversation, "reply to an imported conversation")
}

func TestVerifiedDomainOrPaymentLiftsTheFirstContactRule(t *testing.T) {
	t.Run("verified domain", func(t *testing.T) {
		f := newLimitsFixture(t, true)
		conversation := f.agentStarted(t)
		wantStatus(t, f.reply(t, f.adminA, conversation), http.StatusForbidden, restrictedCode, "before verifying")
		f.verifyDomain(t, f.workspaceA)
		f.wantReplyQueued(t, conversation, "agent-started reply with a verified domain")
	})
	t.Run("paid", func(t *testing.T) {
		f := newLimitsFixture(t, true)
		conversation := f.agentStarted(t)
		f.upgrade(t, f.workspaceA, "sub_outbound")
		f.wantReplyQueued(t, conversation, "agent-started reply on a paid plan")
	})
}

func TestTrialCapIs200RepliesInARollingDay(t *testing.T) {
	f := newLimitsFixture(t, true)
	conversation := f.conversationA(t)
	f.seedOutbox(t, f.workspaceA, 199, "sent", f.clock.Now().Add(-time.Hour))
	f.wantReplyQueued(t, conversation, "the 200th reply of the day")
	got := f.reply(t, f.adminA, conversation)
	wantStatus(t, got, http.StatusTooManyRequests, limitCode, "the 201st reply")
	if !strings.Contains(got.Body, `"retry_after_seconds":82800`) {
		t.Fatalf("oldest counted row is 1h old, so a slot frees in 23h = 82800s: %s", got.Body)
	}
	if usage := f.usage(t, f.adminA); usage.Usage.RepliesLast24h != 200 {
		t.Fatalf("usage.replies_last_24h=%d, want 200", usage.Usage.RepliesLast24h)
	}
}

func TestTrialCapWindowEdges(t *testing.T) {
	f := newLimitsFixture(t, true)
	conversation := f.conversationA(t)
	now := f.clock.Now()
	f.seedOutbox(t, f.workspaceA, 100, "sent", now.Add(-24*time.Hour-time.Second))
	f.seedOutbox(t, f.workspaceA, 100, "failed", now.Add(-time.Hour))
	f.seedOutbox(t, f.workspaceA, 199, "sent", now.Add(-24*time.Hour+time.Second))
	f.wantReplyQueued(t, conversation, "199 counted rows: only rows newer than 24h and not failed count")
	wantStatus(t, f.reply(t, f.adminA, conversation), http.StatusTooManyRequests, limitCode, "200 counted rows")
	// One second later the oldest counted rows are exactly 24h old and leave the window.
	f.clock.set(now.Add(time.Second))
	f.wantReplyQueued(t, conversation, "a row exactly 24h old no longer counts")
}

func TestTrialCapIsPerWorkspaceAndPaidPlansHaveNone(t *testing.T) {
	f := newLimitsFixture(t, true)
	conversationA := f.conversationA(t)
	conversationB := *f.receive(t, true, 1, "Bravo question").ConversationID
	f.seedOutbox(t, f.workspaceA, 200, "sent", f.clock.Now().Add(-time.Hour))
	wantStatus(t, f.reply(t, f.adminA, conversationA), http.StatusTooManyRequests, limitCode, "workspace A at the cap")
	if got := f.reply(t, f.adminB, conversationB); got.Code != http.StatusCreated {
		t.Fatalf("workspace B must not feel A's cap: status=%d body=%s", got.Code, got.Body)
	}
	f.upgrade(t, f.workspaceA, "sub_cap")
	f.seedOutbox(t, f.workspaceA, 300, "sent", f.clock.Now().Add(-time.Hour))
	f.wantReplyQueued(t, conversationA, "paid workspace with 500 replies in a day")
}

func TestSelfHostedHasNoOutboundRestrictions(t *testing.T) {
	f := newLimitsFixture(t, false)
	conversation := f.agentStarted(t)
	f.seedOutbox(t, f.workspaceA, 500, "sent", f.clock.Now().Add(-time.Hour))
	f.wantReplyQueued(t, conversation, "self-hosted agent-started reply past 500")
}

func TestRepliesQueuedBeforeTheLockStillSend(t *testing.T) {
	f := newLimitsFixture(t, true)
	f.wantReplyQueued(t, f.conversationA(t), "reply before the lock")
	f.clock.set(trialEnd)
	if got := f.outboxCount(t, f.workspaceA); got != "1" {
		t.Fatalf("outbox=%s: the lock must not remove queued deliveries", got)
	}
	if status := queryString(t, f.pool, `SELECT status FROM email_outbox WHERE workspace_id=$1`, f.workspaceA); status != "queued" {
		t.Fatalf("status=%q, want queued (the worker still sends it)", status)
	}
}

func TestTwoRepliesRacingAtTheCapAdmitExactlyOne(t *testing.T) {
	f := newLimitsFixture(t, true)
	conversation := f.conversationA(t)
	f.seedOutbox(t, f.workspaceA, 199, "sent", f.clock.Now().Add(-time.Hour))
	codes := make([]int, 2)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = f.reply(t, f.adminA, conversation).Code
		}()
	}
	wg.Wait()
	created, limited := 0, 0
	for _, code := range codes {
		switch code {
		case http.StatusCreated:
			created++
		case http.StatusTooManyRequests:
			limited++
		}
	}
	if created != 1 || limited != 1 {
		t.Fatalf("codes=%v, want one 201 and one 429", codes)
	}
}

// The count must stay on its index when the outbox is large and must not slow the reply down.
func TestTrialCapCountStaysFastOnALargeOutbox(t *testing.T) {
	if testing.Short() {
		t.Skip("seeds a large outbox")
	}
	f := newLimitsFixture(t, true)
	conversation := f.conversationA(t)
	f.seedOutbox(t, f.workspaceB, 100000, "sent", f.clock.Now().Add(-48*time.Hour))
	f.seedOutbox(t, f.workspaceA, 100000, "sent", f.clock.Now().Add(-72*time.Hour))
	f.seedOutbox(t, f.workspaceA, 150, "sent", f.clock.Now().Add(-time.Hour))
	if _, err := f.pool.Exec(context.Background(), `ANALYZE email_outbox`); err != nil {
		t.Fatal(err)
	}
	var plan strings.Builder
	rows, err := f.pool.Query(context.Background(), `EXPLAIN SELECT count(*) FROM email_outbox WHERE workspace_id=$1 AND created_at > $2 AND status <> 'failed'`,
		f.workspaceA, f.clock.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line + "\n")
	}
	rows.Close()
	if !strings.Contains(plan.String(), "email_outbox_workspace_created_idx") {
		t.Fatalf("the 24h count does not use the (workspace_id, created_at) index:\n%s", plan.String())
	}
	best := time.Hour
	for i := 0; i < 5; i++ {
		start := time.Now()
		f.wantReplyQueued(t, conversation, "reply under the cap")
		if elapsed := time.Since(start); elapsed < best {
			best = elapsed
		}
	}
	if best > 200*time.Millisecond {
		t.Fatalf("fastest of 5 replies took %s on a 200k-row outbox, want <= 200ms", best)
	}
	t.Logf("fastest of 5 trial replies on a 200k-row outbox: %s", best)
}
