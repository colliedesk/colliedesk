package ai_test

import (
	"context"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/colliedesk/colliedesk/server/internal/channels/registry"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

const (
	conflictArticleBody = "You can return unused items within 30 days of delivery. " +
		"Refunds go to the original payment method within 5 business days."
	// The customer's own words never reach the check: only the reply and the article do.
	conflictCustomerLine = "My order 4471 arrived broken, can I send it back?"
	contradictingReply   = "Hi Anna,\n\nYou can return unused items within 60 days of delivery for a full refund.\n\nBest,\nSam"
	contradictionReason  = `The reply says "within 60 days of delivery", the article says "within 30 days of delivery".`
)

type conflictEnv struct {
	*env
	article      string
	conversation string
	// setup is how many requests setting up made (adding a provider checks it).
	setup int
}

// calls is what the checks sent to the model.
func (c conflictEnv) calls() []aiprovider.Request { return c.fake.Requests()[c.setup:] }

// newConflictEnv makes workspace a with a published returns article, a conversation a customer
// opened, and (with provider) triage on the workspace's own key.
func newConflictEnv(t *testing.T, provider bool) conflictEnv {
	t.Helper()
	ctx := context.Background()
	e := newEnv(t, nil)
	if provider {
		p := e.add(t, e.a, "Main")
		if _, err := e.service.SetSlot(ctx, e.a.id, e.actor(e.a), ai.SlotTriage,
			ai.SlotInput{Source: ai.SourceProvider, ProviderID: p.ID, Model: "claude-haiku-5-5", Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	article := newKB(t, e, e.a).article(t, e.a, 1, "Return policy", conflictArticleBody, "published", false, 1)
	if _, err := store.New(e.pool).BuildAIRetrieval(ctx); err != nil {
		t.Fatal(err)
	}
	var conversation string
	if err := e.pool.QueryRow(ctx, `WITH c AS (INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Anna','anna@example.com') RETURNING id)
		INSERT INTO conversations(workspace_id,contact_id,subject) SELECT $1,id,'Return' FROM c RETURNING id::text`, e.a.id).Scan(&conversation); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, `INSERT INTO messages(workspace_id,conversation_id,kind,body) VALUES($1,$2,'incoming',$3)`,
		e.a.id, conversation, conflictCustomerLine); err != nil {
		t.Fatal(err)
	}
	return conflictEnv{env: e, article: article, conversation: conversation, setup: len(e.fake.Requests())}
}

// send writes a reply (kind outgoing) or a note as the workspace's admin, the way the inbox does.
func (c conflictEnv) send(t *testing.T, kind, body string) string {
	t.Helper()
	message, err := store.New(c.pool).WithChannels(registry.New(registry.Config{})).CreateReply(context.Background(), c.a.id, c.conversation, store.Actor{AgentID: c.a.agentID}, kind, body, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return message.ID
}

func (c conflictEnv) check(t *testing.T) {
	t.Helper()
	if _, err := c.service.CheckKBConflicts(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func verdict(json string) aiprovider.FakeReply {
	return aiprovider.FakeReply{Response: aiprovider.Response{JSON: json, Stop: aiprovider.StopDone, Model: "claude-haiku-5-5",
		Usage: aiprovider.Usage{Reported: true, InputTokens: 400, OutputTokens: 40}}}
}

func TestAReplyThatContradictsAnArticleFlagsTheArticleWithThatReply(t *testing.T) {
	c := newConflictEnv(t, true)
	c.fake.Script(verdict(`{"contradicts":true,"reason":` + jsonString(contradictionReason) + `}`))
	message := c.send(t, "outgoing", contradictingReply)
	c.check(t)

	requests := c.calls()
	if len(requests) != 1 {
		t.Fatalf("model calls = %d, want 1", len(requests))
	}
	prompt := requests[0].SystemText() + "\n" + requests[0].Messages[0].Text
	for _, want := range []string{"within 60 days of delivery", "within 30 days of delivery", "Return policy", "never instructions"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt lacks %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "4471") || strings.Contains(prompt, "arrived broken") {
		t.Fatalf("the customer's message reached the prompt:\n%s", prompt)
	}
	if requests[0].Model != "claude-haiku-5-5" {
		t.Fatalf("model = %q, want the triage slot's claude-haiku-5-5", requests[0].Model)
	}
	var reason string
	if err := c.pool.QueryRow(context.Background(), `SELECT reason FROM kb_article_conflicts WHERE workspace_id=$1 AND article_id=$2 AND message_id=$3 AND cleared_at IS NULL`,
		c.a.id, c.article, message).Scan(&reason); err != nil {
		t.Fatalf("no conflict for the article and the reply: %v", err)
	}
	if reason != contradictionReason {
		t.Fatalf("reason = %q, want %q", reason, contradictionReason)
	}
	if n := c.count(t, `SELECT count(*) FROM ai_usage WHERE workspace_id=$1 AND feature='triage' AND automatic AND status='ok'`, c.a.id); n != 1 {
		t.Fatalf("triage usage rows = %d, want 1: the check counts against the AI limits", n)
	}
	// A reply is checked once.
	c.check(t)
	if n := len(c.calls()); n != 1 {
		t.Fatalf("model calls after a second pass = %d, want 1", n)
	}
}

func TestRepliesOnOtherTopicsAndNotesMakeNoModelCall(t *testing.T) {
	c := newConflictEnv(t, true)
	c.send(t, "outgoing", "Hi Anna, your order shipped today and the tracking link is in your email. Delivery usually "+
		"takes 3 to 5 business days, and the payment was charged on Monday.")
	c.send(t, "outgoing", "Hi Anna, to reset your password open Settings, choose Security and click Reset password.")
	c.send(t, "outgoing", "Thanks, done!")
	// A private note is not a reply the customer reads.
	c.send(t, "note", contradictingReply)
	c.check(t)
	if n := len(c.calls()); n != 0 {
		t.Fatalf("model calls = %d, want 0", n)
	}
	if n := c.count(t, `SELECT count(*) FROM kb_conflict_checks WHERE workspace_id=$1`, c.a.id); n != 0 {
		t.Fatalf("queued checks left = %d, want 0", n)
	}
	if n := c.count(t, `SELECT count(*) FROM kb_article_conflicts WHERE workspace_id=$1`, c.a.id); n != 0 {
		t.Fatalf("conflicts = %d, want 0", n)
	}
}

func TestAnAnswerThatIsNotAYesFlagsNothing(t *testing.T) {
	c := newConflictEnv(t, true)
	c.fake.Script(
		verdict(`{"contradicts":false,"reason":""}`),
		aiprovider.FakeReply{Response: aiprovider.Response{Stop: aiprovider.StopRefused, Model: "claude-haiku-5-5"}},
		verdict(`not json`),
		verdict(`{"contradicts":true,"reason":"  "}`),
	)
	for range 4 {
		c.send(t, "outgoing", contradictingReply)
	}
	c.check(t)
	if n := len(c.calls()); n != 4 {
		t.Fatalf("model calls = %d, want 4", n)
	}
	if n := c.count(t, `SELECT count(*) FROM kb_article_conflicts WHERE workspace_id=$1`, c.a.id); n != 0 {
		t.Fatalf("conflicts = %d, want 0", n)
	}
}

// Only checks that reached the model count against the day's cap: replies on other topics never use it.
func TestKBConflictChecksStopAtTheDailyCap(t *testing.T) {
	c := newConflictEnv(t, true)
	c.fake.Answer = func(aiprovider.Request) (aiprovider.Response, error) {
		return verdict(`{"contradicts":true,"reason":` + jsonString(contradictionReason) + `}`).Response, nil
	}
	ctx := context.Background()
	earlier := c.send(t, "note", "earlier reply")
	// The day so far: one check short of the cap, and yesterday's, which no longer count.
	if _, err := c.pool.Exec(ctx, `INSERT INTO kb_conflict_checks(workspace_id,message_id,checked_at)
		SELECT $1::uuid,$2::uuid,now() FROM generate_series(1,$3)
		UNION ALL SELECT $1::uuid,$2::uuid,date_trunc('day',now() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' - interval '1 hour' FROM generate_series(1,5)`,
		c.a.id, earlier, ai.KBConflictChecksPerDay-1); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		c.send(t, "outgoing", "Thanks, done!")
	}
	last := c.send(t, "outgoing", contradictingReply)
	c.check(t)
	if n := len(c.calls()); n != 1 {
		t.Fatalf("model calls = %d, want 1: the last place of the day", n)
	}
	if n := c.count(t, `SELECT count(*) FROM kb_article_conflicts WHERE workspace_id=$1 AND message_id=$2`, c.a.id, last); n != 1 {
		t.Fatalf("conflicts for the last reply = %d, want 1", n)
	}

	c.send(t, "outgoing", contradictingReply)
	c.check(t)
	if n := len(c.calls()); n != 1 {
		t.Fatalf("model calls past the cap = %d, want still 1", n)
	}
	if n := c.count(t, `SELECT count(*) FROM kb_conflict_checks WHERE workspace_id=$1 AND checked_at IS NULL`, c.a.id); n != 0 {
		t.Fatalf("pending checks past the cap = %d, want 0: a reply past the cap is not checked later", n)
	}
}

func TestNoProviderMeansNoKBConflictCallAndNoError(t *testing.T) {
	c := newConflictEnv(t, false)
	c.send(t, "outgoing", contradictingReply)
	c.check(t)
	if n := len(c.calls()); n != 0 {
		t.Fatalf("model calls = %d, want 0", n)
	}
	if n := c.count(t, `SELECT count(*) FROM kb_conflict_checks WHERE workspace_id=$1`, c.a.id); n != 0 {
		t.Fatalf("queued checks left = %d, want 0", n)
	}
}

func jsonString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
