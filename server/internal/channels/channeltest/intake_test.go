package channeltest_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
	"github.com/colliedesk/colliedesk/server/internal/intake"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// intakeLoop is a chat type whose groups collect messages for intake (docs/internals/channels.md
// "Intake"); one-to-one chats are not an intake scope.
func intakeLoop() *channeltest.Loopback {
	loop := channeltest.NewLoopback("loopback", channels.Async)
	loop.Defaults.Scopes = map[string]channels.Scope{"group": {Gather: &channels.Gather{QuietSeconds: 300, MaxSeconds: 1200}}}
	loop.IntakeScopes = []string{"group"}
	loop.SharedScopes = []string{"group"}
	return loop
}

// intakeAt turns intake on for the workspace and sets the group scope of the desk's channel to level.
func (d loopDesk) intakeAt(t *testing.T, level string) {
	t.Helper()
	ctx := context.Background()
	if _, err := d.pool.Exec(ctx, `INSERT INTO ai_slots(workspace_id,slot,uses_server,model,enabled,default_level,consented_by,consented_at,updated_by,updated_at)
		VALUES($1,'intake',true,'claude-haiku-5-5',true,'off',$2,now(),$2,now()) ON CONFLICT (workspace_id,slot) DO NOTHING`, d.workspaceID, d.agentID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.pool.Exec(ctx, `INSERT INTO channel_ai_levels(workspace_id,channel_id,scope,slot,level) VALUES($1,$2,'group','intake',$3)
		ON CONFLICT (workspace_id,channel_id,scope,slot) DO UPDATE SET level=EXCLUDED.level`, d.workspaceID, d.channel.ID, level); err != nil {
		t.Fatal(err)
	}
}

// flush sends what was collected: the clock passes the quiet window.
func (d loopDesk) flush(t *testing.T) {
	t.Helper()
	*d.clock = d.clock.Add(10 * time.Minute)
	if _, err := d.pipeline.ProcessDue(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// gatherings lists the gatherings oldest first as "state/rules_action/rules_rule/decided_by:N items".
func (d loopDesk) gatherings(t *testing.T) string {
	t.Helper()
	return d.one(t, `SELECT COALESCE(string_agg(g.state||'/'||g.rules_action||'/'||g.rules_rule||'/'||COALESCE(g.decided_by,'-')||':'||
			(SELECT count(*) FROM channel_inbound_events e WHERE e.workspace_id=g.workspace_id AND e.gathering_id=g.id)::text, ',' ORDER BY g.closed_at,g.created_at),'')
		FROM intake_gatherings g WHERE g.workspace_id=$1`)
}

// Every batch said in an intake scope is a gathering, one per sender even when they write in turn, and
// the rules decide it as they always did; a one-to-one chat (not an intake scope) has none.
func TestEveryBatchOfAnIntakeScopeIsAGathering(t *testing.T) {
	d := newLoopDesk(t, intakeLoop())
	d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "a", Group: "g1", Text: "the stock export fails"},
		channeltest.LoopbackEvent{ID: "b-1", From: "b", Group: "g1", Text: "meeting tomorrow"},
		channeltest.LoopbackEvent{ID: "a-2", From: "a", Group: "g1", Text: "here is the screen"},
		channeltest.LoopbackEvent{ID: "u-1", From: "u", Text: "hello"})
	if got := d.gatherings(t); got != "" {
		t.Fatalf("gatherings while collecting = %s", got)
	}
	d.flush(t)
	if got := d.gatherings(t); got != "created/create/new_thread/rules:2,created/create/new_thread/rules:1" {
		t.Fatalf("gatherings = %s", got)
	}
	if got := d.one(t, `SELECT count(DISTINCT lane_key)::text FROM intake_gatherings WHERE workspace_id=$1 AND conversation_id IS NOT NULL AND thread_id IS NOT NULL`); got != "2" {
		t.Fatalf("lanes = %s", got)
	}
	if got := d.one(t, `SELECT count(*)::text FROM channel_thread_items i JOIN channel_inbound_events e ON e.workspace_id=i.workspace_id AND e.id=i.inbound_event_id
		WHERE i.workspace_id=$1 AND i.gathering_id IS NOT DISTINCT FROM e.gathering_id AND i.gathering_id IS NOT NULL`); got != "3" {
		t.Fatalf("items of gatherings = %s", got)
	}
	// The group's key is everyone's: a's next batch continues a's conversation, b's never joined it.
	d.send(t, channeltest.LoopbackEvent{ID: "a-3", From: "a", Group: "g1", Text: "still failing"})
	d.flush(t)
	if got := d.one(t, `SELECT state||'/'||rules_rule FROM intake_gatherings WHERE workspace_id=$1 ORDER BY closed_at DESC,created_at DESC LIMIT 1`); got != "attached/thread_key" {
		t.Fatalf("a's next batch = %s", got)
	}
	// What the rules hold for a person is a card of the queue.
	review := json.RawMessage(`{"scopes":{"group":{"gather":{"quiet_seconds":300,"max_seconds":1200},"review":true}}}`)
	if _, err := d.store.UpdateChannel(context.Background(), d.workspaceID, d.channel.ID, store.ChannelInput{Settings: review}, store.ChannelActor{AgentID: d.agentID}); err != nil {
		t.Fatal(err)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "c-1", From: "c", Group: "g2", Text: "anyone?"})
	d.flush(t)
	if got := d.one(t, `SELECT state||'/'||review_reason||'/'||rules_rule FROM intake_gatherings WHERE workspace_id=$1 AND lane_key LIKE '%:loopback_user:c'`); got != "review/no_command/review_hold" {
		t.Fatalf("held for review = %s", got)
	}
}

// At auto, a batch the rules would only decide by default waits for the decider, held and counted like
// any held message, with what the rules would do; a batch with evidence (a command) is decided at once.
// Nothing is created for it meanwhile: no conversation, no contact.
func TestAtAutoWhatTheRulesDecideByDefaultWaitsForTheDecider(t *testing.T) {
	d := newLoopDesk(t, intakeLoop())
	d.intakeAt(t, "auto")
	contacts := d.one(t, `SELECT count(*)::text FROM contacts WHERE workspace_id=$1`)
	d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "a", Group: "g1", Text: "the stock export fails"},
		channeltest.LoopbackEvent{ID: "a-2", From: "a", Group: "g1", Text: "since this morning"})
	d.flush(t)
	if got := d.gatherings(t); got != "deciding/create/new_thread/-:2" {
		t.Fatalf("gatherings = %s", got)
	}
	if got := d.one(t, `SELECT string_agg(state||':'||hold_reason,',' ORDER BY received_at) FROM channel_inbound_events WHERE workspace_id=$1`); got != "held:intake,held:intake" {
		t.Fatalf("journal = %s", got)
	}
	if got := d.one(t, `SELECT (decide_by-closed_at)::text FROM intake_gatherings WHERE workspace_id=$1`); got != "00:02:00" {
		t.Fatalf("decide within = %s", got)
	}
	if got := d.one(t, `SELECT count(*)::text FROM contacts WHERE workspace_id=$1`); got != contacts {
		t.Fatalf("contacts %s → %s", contacts, got)
	}
	if got := d.one(t, `SELECT count(*)::text FROM conversations WHERE workspace_id=$1`); got != "0" {
		t.Fatalf("conversations = %s", got)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "b-1", From: "b", Group: "g1", Text: "report bug: the printer is down"})
	d.flush(t)
	if got := d.one(t, `SELECT state||'/'||rules_rule||'/'||decided_by FROM intake_gatherings WHERE workspace_id=$1 AND lane_key LIKE '%:loopback_user:b'`); got != "created/command_open/rules" {
		t.Fatalf("a command at auto = %s", got)
	}
}

func (d loopDesk) intake() *intake.Service {
	return intake.New(d.store, d.pipeline, func() time.Time { return *d.clock })
}

// gatheringOf is the id of the newest gathering of a sender ("a", "b"…).
func (d loopDesk) gatheringOf(t *testing.T, sender string) string {
	t.Helper()
	return d.one(t, `SELECT id::text FROM intake_gatherings WHERE workspace_id=$1 AND lane_key LIKE '%:loopback_user:'||$2 ORDER BY closed_at DESC,created_at DESC LIMIT 1`, sender)
}

// A person's decision of a held batch opens the one message the batch is, says so in the journal and
// the gathering's history, and links its items as a person's.
func TestAPersonDecidesAGatheringAsOneMessage(t *testing.T) {
	d := newLoopDesk(t, intakeLoop())
	d.intakeAt(t, "auto")
	d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "a", Group: "g1", Text: "the stock export fails"},
		channeltest.LoopbackEvent{ID: "a-2", From: "a", Group: "g1", Text: "since this morning"})
	d.flush(t)
	id := d.gatheringOf(t, "a")
	outcome, err := d.intake().Decide(context.Background(), d.workspaceID, id, intake.Decision{Action: intake.Create}, store.DecidedByAgent, store.ChannelActor{AgentID: d.agentID})
	if err != nil || outcome.State != store.GatheringCreated || outcome.ConversationID == "" {
		t.Fatalf("decide = %+v, %v", outcome, err)
	}
	if got := d.one(t, `SELECT string_agg(body,'|') FROM messages WHERE workspace_id=$1 AND kind='incoming'`); got != "the stock export fails\nsince this morning" {
		t.Fatalf("messages = %q", got)
	}
	if got := d.one(t, `SELECT string_agg(state||':'||decision_basis||':'||decision_rule,',') FROM channel_inbound_events WHERE workspace_id=$1`); got != "processed:person:intake_create,processed:person:intake_create" {
		t.Fatalf("journal = %s", got)
	}
	if got := d.one(t, `SELECT decider||'/'||basis||'/'||action||'/'||applied::text||'/'||cardinality(items_seen)::text||'/'||agent_id::text FROM intake_decisions WHERE workspace_id=$1`); got != "agent/person/create/true/2/"+d.agentID {
		t.Fatalf("decision = %s", got)
	}
	if got := d.one(t, `SELECT string_agg(reason,',') FROM conversation_thread_items WHERE workspace_id=$1`); got != "agent,agent" {
		t.Fatalf("links = %s", got)
	}
	// The same decision again is the same; another is refused, with how it stands.
	if again, err := d.intake().Decide(context.Background(), d.workspaceID, id, intake.Decision{Action: intake.Create}, store.DecidedByAgent, store.ChannelActor{AgentID: d.agentID}); err != nil || again != outcome {
		t.Fatalf("again = %+v, %v", again, err)
	}
	var conflict *intake.ConflictError
	if _, err := d.intake().Decide(context.Background(), d.workspaceID, id, intake.Decision{Action: intake.Ignore}, store.DecidedByAgent, store.ChannelActor{AgentID: d.agentID}); !errors.As(err, &conflict) || conflict.Outcome.State != store.GatheringCreated {
		t.Fatalf("another decision = %v", err)
	}
	if got := d.one(t, `SELECT count(*)::text FROM intake_decisions WHERE workspace_id=$1`); got != "1" {
		t.Fatalf("decisions = %s", got)
	}
}

// A batch decided to be a new request opens one even when its sender's later batch opened a
// conversation its thread now finds.
func TestANewRequestNeverJoinsAConversationOpenedSinceItWasHeld(t *testing.T) {
	d := newLoopDesk(t, intakeLoop())
	d.intakeAt(t, "auto")
	d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "a", Group: "g1", Text: "the stock export fails"})
	d.flush(t)
	held := d.gatheringOf(t, "a")
	d.send(t, channeltest.LoopbackEvent{ID: "a-2", From: "a", Group: "g1", Text: "report bug: the printer is down"})
	d.flush(t)
	if _, err := d.intake().Decide(context.Background(), d.workspaceID, held, intake.Decision{Action: intake.Create}, store.DecidedByAgent, store.ChannelActor{AgentID: d.agentID}); err != nil {
		t.Fatal(err)
	}
	if got := d.one(t, `SELECT count(DISTINCT conversation_id)::text FROM messages WHERE workspace_id=$1 AND kind='incoming'`); got != "2" {
		t.Fatalf("conversations of a = %s", got)
	}
}

// A person and the sweep deciding at once apply one decision.
func TestAPersonAndTheSweepDecideAGatheringOnce(t *testing.T) {
	d := newLoopDesk(t, intakeLoop())
	d.intakeAt(t, "auto")
	d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "a", Group: "g1", Text: "the stock export fails"})
	d.flush(t)
	id := d.gatheringOf(t, "a")
	*d.clock = d.clock.Add(3 * time.Minute)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, errs[0] = d.intake().Decide(context.Background(), d.workspaceID, id, intake.Decision{Action: intake.Create}, store.DecidedByAgent, store.ChannelActor{AgentID: d.agentID})
	}()
	go func() {
		defer wg.Done()
		_, errs[1] = d.intake().Sweep(context.Background())
	}()
	wg.Wait()
	var conflict *intake.ConflictError
	if errs[1] != nil || (errs[0] != nil && !errors.As(errs[0], &conflict)) {
		t.Fatalf("errors = %v", errs)
	}
	if got := d.one(t, `SELECT count(*)::text FROM messages WHERE workspace_id=$1 AND kind='incoming'`); got != "1" {
		t.Fatalf("messages = %s", got)
	}
}

// Past its time, a gathering AI did not decide gets the rules' decision (T-R2): what they would open is
// opened, what they hold for a person waits in the queue, and a commands-only scope's chatter goes to the
// queue because AI was meant to look at it.
func TestPastItsTimeTheRulesDecide(t *testing.T) {
	d := newLoopDesk(t, intakeLoop())
	d.intakeAt(t, "auto")
	d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "a", Group: "g1", Text: "the stock export fails"})
	d.flush(t)
	if n, err := d.intake().Sweep(context.Background()); err != nil || n != 0 {
		t.Fatalf("a sweep in time = %d, %v", n, err)
	}
	*d.clock = d.clock.Add(3 * time.Minute)
	if n, err := d.intake().Sweep(context.Background()); err != nil || n != 1 {
		t.Fatalf("sweep = %d, %v", n, err)
	}
	if got := d.one(t, `SELECT state||'/'||decided_by||'/'||fallback::text FROM intake_gatherings WHERE workspace_id=$1`); got != "created/rules/true" {
		t.Fatalf("gathering = %s", got)
	}
	if got := d.one(t, `SELECT decision_basis||':'||decision_rule FROM channel_inbound_events WHERE workspace_id=$1`); got != "default:new_thread" {
		t.Fatalf("journal = %s", got)
	}
	settings := json.RawMessage(`{"scopes":{"group":{"gather":{"quiet_seconds":300,"max_seconds":1200},"mode":"commands"}}}`)
	if _, err := d.store.UpdateChannel(context.Background(), d.workspaceID, d.channel.ID, store.ChannelInput{Settings: settings}, store.ChannelActor{AgentID: d.agentID}); err != nil {
		t.Fatal(err)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "c-1", From: "c", Group: "g2", Text: "good morning"})
	d.flush(t)
	*d.clock = d.clock.Add(3 * time.Minute)
	if _, err := d.intake().Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := d.one(t, `SELECT state||'/'||review_reason||'/'||rules_action||'/'||fallback::text FROM intake_gatherings WHERE workspace_id=$1 AND id=$2::uuid`, d.gatheringOf(t, "c")); got != "review/ai_unavailable/archive/true" {
		t.Fatalf("chatter = %s", got)
	}
	// A batch behind a card of its lane waits in the queue right after it.
	d.send(t, channeltest.LoopbackEvent{ID: "c-2", From: "c", Group: "g2", Text: "hello again"})
	d.flush(t)
	behind := d.gatheringOf(t, "c")
	if got := d.one(t, `SELECT state||'/'||review_reason FROM intake_gatherings WHERE workspace_id=$1 AND id=$2::uuid`, behind); got != "review/lane_blocked" {
		t.Fatalf("behind a card = %s", got)
	}
	items := `SELECT string_agg(disposition||'/'||disposition_reason,',') FROM channel_thread_items WHERE workspace_id=$1 AND gathering_id=$2::uuid`
	if got := d.one(t, items, behind); got != "pending_review/lane_blocked" {
		t.Fatalf("items behind a card = %s", got)
	}
	// Once a person decided the card before it, it goes back to be decided.
	first := d.one(t, `SELECT id::text FROM intake_gatherings WHERE workspace_id=$1 AND review_reason='ai_unavailable'`)
	if _, err := d.intake().Decide(context.Background(), d.workspaceID, first, intake.Decision{Action: intake.Ignore}, store.DecidedByAgent, store.ChannelActor{AgentID: d.agentID}); err != nil {
		t.Fatal(err)
	}
	if got := d.one(t, `SELECT state||'/'||(decide_by IS NOT NULL)::text FROM intake_gatherings WHERE workspace_id=$1 AND id=$2::uuid`, behind); got != "deciding/true" {
		t.Fatalf("after the card before it = %s", got)
	}
	if got := d.one(t, items, behind); got != "gathering/deciding" {
		t.Fatalf("items after the card before it = %s", got)
	}
}

// When the rules decide in AI's place, they decide as they would now (T-6): a sender's second batch,
// held while the first waited too, continues the conversation the first one opened instead of opening
// another, and so does one that waited behind a card a person decided. The journal says which rule it was.
func TestPastItsTimeTheRulesDecideAsTheyWouldNow(t *testing.T) {
	d := newLoopDesk(t, intakeLoop())
	d.intakeAt(t, "auto")
	d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "a", Group: "g1", Text: "the stock export fails"})
	d.flush(t)
	d.send(t, channeltest.LoopbackEvent{ID: "a-2", From: "a", Group: "g1", Text: "since this morning"})
	d.flush(t)
	*d.clock = d.clock.Add(3 * time.Minute)
	if n, err := d.intake().Sweep(context.Background()); err != nil || n != 2 {
		t.Fatalf("sweep = %d, %v", n, err)
	}
	if got := d.gatherings(t); got != "created/create/new_thread/rules:1,attached/create/new_thread/rules:1" {
		t.Fatalf("gatherings = %s", got)
	}
	if got := d.one(t, `SELECT count(DISTINCT conversation_id)::text FROM messages WHERE workspace_id=$1 AND kind='incoming'`); got != "1" {
		t.Fatalf("conversations = %s", got)
	}
	if got := d.one(t, `SELECT decision_basis||':'||decision_rule FROM channel_inbound_events WHERE workspace_id=$1 AND external_event_id='a-2'`); got != "match:thread_key" {
		t.Fatalf("journal of the second = %s", got)
	}
	// Behind a card a person decides: the next batch waits, then the rules continue what the person opened.
	ctx := context.Background()
	actor := store.ChannelActor{AgentID: d.agentID}
	d.send(t, channeltest.LoopbackEvent{ID: "b-1", From: "b", Group: "g1", Text: "printer?"})
	d.flush(t)
	first := d.gatheringOf(t, "b")
	if _, err := d.intake().Decide(ctx, d.workspaceID, first, intake.Decision{Action: intake.Ignore}, store.DecidedByAgent, actor); err != nil {
		t.Fatal(err)
	}
	if _, err := d.intake().Requeue(ctx, d.workspaceID, first, actor); err != nil {
		t.Fatal(err)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "b-2", From: "b", Group: "g1", Text: "it is the one on floor 2"})
	d.flush(t)
	behind := d.gatheringOf(t, "b")
	if _, err := d.intake().Decide(ctx, d.workspaceID, first, intake.Decision{Action: intake.Create}, store.DecidedByAgent, actor); err != nil {
		t.Fatal(err)
	}
	*d.clock = d.clock.Add(3 * time.Minute)
	if _, err := d.intake().Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if got := d.one(t, `SELECT g.state||'/'||(g.conversation_id=f.conversation_id)::text FROM intake_gatherings g, intake_gatherings f
		WHERE g.workspace_id=$1 AND g.id=$2::uuid AND f.id=$3::uuid`, behind, first); got != "attached/true" {
		t.Fatalf("behind the decided card = %s", got)
	}
}

// A decided card keeps the contact of the customer who wrote it, also when a person adds it to another
// customer's conversation.
func TestADecidedCardKeepsItsSendersContact(t *testing.T) {
	d := newLoopDesk(t, intakeLoop())
	d.intakeAt(t, "auto")
	ctx := context.Background()
	actor := store.ChannelActor{AgentID: d.agentID}
	d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "a", Group: "g1", Text: "the stock export fails"})
	d.flush(t)
	cardA := d.gatheringOf(t, "a")
	opened, err := d.intake().Decide(ctx, d.workspaceID, cardA, intake.Decision{Action: intake.Create}, store.DecidedByAgent, actor)
	if err != nil {
		t.Fatal(err)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "b-1", From: "b", Group: "g1", Text: "same for me"})
	d.flush(t)
	cardB := d.gatheringOf(t, "b")
	if _, err := d.intake().Decide(ctx, d.workspaceID, cardB, intake.Decision{Action: intake.Attach, ConversationID: opened.ConversationID}, store.DecidedByAgent, actor); err != nil {
		t.Fatal(err)
	}
	contactOf := `SELECT COALESCE((SELECT contact_id::text FROM contact_identities WHERE workspace_id=$1 AND value=$2),'none')`
	cardContact := `SELECT COALESCE(contact_id::text,'none') FROM intake_gatherings WHERE workspace_id=$1 AND id=$2::uuid`
	if got, want := d.one(t, cardContact, cardA), d.one(t, contactOf, "a"); got != want || got == "none" {
		t.Fatalf("A's card contact = %s, want %s", got, want)
	}
	if got, want := d.one(t, cardContact, cardB), d.one(t, contactOf, "b"); got != want || got == "none" {
		t.Fatalf("B's card contact = %s, want %s", got, want)
	}
}

// The held messages of a gathering are decided in the queue: replaying or discarding held messages one
// by one leaves them; what intake set aside may be thrown away, and then its gathering is over.
func TestHeldMessagesOfTheQueueAreDecidedThere(t *testing.T) {
	d := newLoopDesk(t, intakeLoop())
	d.intakeAt(t, "auto")
	d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "a", Group: "g1", Text: "ok thanks"})
	d.flush(t)
	ctx := context.Background()
	if held, err := d.store.HeldEvents(ctx, d.workspaceID, d.channel.ID, nil); err != nil || len(held) != 0 {
		t.Fatalf("held one by one = %d, %v", len(held), err)
	}
	if n, err := d.store.DiscardHeld(ctx, d.workspaceID, d.channel.ID, nil, store.ChannelActor{AgentID: d.agentID}); err != nil || n != 0 {
		t.Fatalf("discard while deciding = %d, %v", n, err)
	}
	id := d.gatheringOf(t, "a")
	if _, err := d.intake().Decide(ctx, d.workspaceID, id, intake.Decision{Action: intake.Ignore}, store.DecidedByAgent, store.ChannelActor{AgentID: d.agentID}); err != nil {
		t.Fatal(err)
	}
	if got := d.one(t, `SELECT state||'/'||disposition_reason FROM channel_thread_items i JOIN intake_gatherings g ON g.workspace_id=i.workspace_id AND g.id=i.gathering_id WHERE i.workspace_id=$1`); got != "ignored/ignored" {
		t.Fatalf("set aside = %s", got)
	}
	if outcome, err := d.intake().Requeue(ctx, d.workspaceID, id, store.ChannelActor{AgentID: d.agentID}); err != nil || outcome.State != store.GatheringReview {
		t.Fatalf("requeue = %+v, %v", outcome, err)
	}
	if _, err := d.intake().Decide(ctx, d.workspaceID, id, intake.Decision{Action: intake.Ignore}, store.DecidedByAgent, store.ChannelActor{AgentID: d.agentID}); err != nil {
		t.Fatal(err)
	}
	if n, err := d.store.DiscardHeld(ctx, d.workspaceID, d.channel.ID, nil, store.ChannelActor{AgentID: d.agentID}); err != nil || n != 1 {
		t.Fatalf("discard what was set aside = %d, %v", n, err)
	}
	if got := d.one(t, `SELECT state FROM intake_gatherings WHERE workspace_id=$1`); got != "cancelled" {
		t.Fatalf("gathering = %s", got)
	}
	if got := d.one(t, `SELECT count(*)::text FROM intake_decisions WHERE workspace_id=$1 AND action='ignore'`); got != "2" {
		t.Fatalf("ignore decisions = %s", got)
	}
}
