package channeltest_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
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
