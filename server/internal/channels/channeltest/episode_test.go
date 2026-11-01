package channeltest_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// never is the join policy of a chat whose every request is its own (a command never continues another).
var never = &channels.JoinPolicy{Kind: channels.JoinNever}

// episodeLoop is a chat type whose groups collect a sender's messages for 5 quiet minutes and take what
// they said up to 2 minutes before a command into the command's batch, as support-desk episodes do.
func episodeLoop() *channeltest.Loopback {
	loop := intakeLoop()
	loop.Defaults.Scopes = map[string]channels.Scope{"group": {JoinPolicy: never, Gather: &channels.Gather{QuietSeconds: 300, MaxSeconds: 1200, PreCommandSeconds: 120}}}
	return loop
}

// at sends one event once the clock moved on by step.
func (d loopDesk) at(t *testing.T, step time.Duration, event channeltest.LoopbackEvent) {
	t.Helper()
	*d.clock = d.clock.Add(step)
	d.send(t, event)
}

// bodies lists the customer messages oldest first, each as its conversation saw it.
func (d loopDesk) bodies(t *testing.T) string {
	t.Helper()
	return d.one(t, `SELECT COALESCE(string_agg(replace(body,E'\n','+'),'|' ORDER BY created_at,id),'') FROM messages WHERE workspace_id=$1 AND kind='incoming'`)
}

// Commands of one customer a minute apart are three requests; what they said shortly before a command
// is part of that command's request, what they said after it inside the quiet window too, and what came
// long before stays a batch of its own.
func TestACommandTakesWhatItsSenderSaidJustBefore(t *testing.T) {
	d := newLoopDesk(t, episodeLoop())
	d.at(t, 0, channeltest.LoopbackEvent{ID: "b-1", From: "a", Group: "g1", Text: "report bug: one"})
	d.at(t, time.Minute, channeltest.LoopbackEvent{ID: "b-2", From: "a", Group: "g1", Text: "report bug: two"})
	d.at(t, time.Minute, channeltest.LoopbackEvent{ID: "b-3", From: "a", Group: "g1", Text: "report bug: three"})
	d.flush(t)
	if got := d.one(t, `SELECT count(*)::text FROM conversations WHERE workspace_id=$1`); got != "3" {
		t.Fatalf("three commands = %s conversations (%s)", got, d.bodies(t))
	}
	d.at(t, time.Hour, channeltest.LoopbackEvent{ID: "c-1", From: "c", Group: "g1", Text: "old remark"})
	d.at(t, 3*time.Minute, channeltest.LoopbackEvent{ID: "c-2", From: "c", Group: "g1", Text: "screenshot"})
	d.at(t, time.Minute, channeltest.LoopbackEvent{ID: "c-3", From: "c", Group: "g1", Text: "report bug: the export"})
	d.at(t, 4*time.Minute, channeltest.LoopbackEvent{ID: "c-4", From: "c", Group: "g1", Text: "another screenshot"})
	d.flush(t)
	commanded := `SELECT replace(m.body,E'\n','+') FROM messages m JOIN channel_inbound_events e ON e.workspace_id=m.workspace_id AND e.message_id=m.id
		WHERE m.workspace_id=$1 AND e.external_event_id='c-3'`
	if got := d.one(t, commanded); got != "screenshot+report bug: the export+another screenshot" {
		t.Fatalf("the command's request = %q", got)
	}
	if got := d.one(t, `SELECT decision_rule FROM channel_inbound_events WHERE workspace_id=$1 AND external_event_id='c-1'`); got != "new_thread" && got != "join_policy" {
		t.Fatalf("what came long before = %s", got)
	}
}

// A scope that does not cut on a command adds a second command to the batch being collected.
func TestAScopeThatDoesNotCutOnACommandKeepsOneBatch(t *testing.T) {
	loop := episodeLoop()
	off := false
	loop.Defaults.Scopes = map[string]channels.Scope{"group": {JoinPolicy: never, Gather: &channels.Gather{QuietSeconds: 300, MaxSeconds: 1200}, CutOnCommand: &off}}
	d := newLoopDesk(t, loop)
	d.at(t, 0, channeltest.LoopbackEvent{ID: "b-1", From: "a", Group: "g1", Text: "report bug: one"})
	d.at(t, time.Minute, channeltest.LoopbackEvent{ID: "b-2", From: "a", Group: "g1", Text: "report bug: and more"})
	d.flush(t)
	if got := d.one(t, `SELECT count(*)::text FROM conversations WHERE workspace_id=$1`); got != "1" {
		t.Fatalf("conversations = %s (%s)", got, d.bodies(t))
	}
}

// "Hủy" cancels only a batch of its sender that named a command; with none being collected it is an
// ordinary message.
func TestACancelWithoutACommandedBatchIsAnOrdinaryMessage(t *testing.T) {
	d := newLoopDesk(t, episodeLoop())
	d.at(t, 0, channeltest.LoopbackEvent{ID: "x-1", From: "a", Group: "g1", Text: "Hủy"})
	d.flush(t)
	if got := d.one(t, `SELECT state||'/'||COALESCE(drop_reason,'-') FROM channel_inbound_events WHERE workspace_id=$1 AND external_event_id='x-1'`); got != "processed/-" {
		t.Fatalf("a cancel without a commanded batch = %s", got)
	}
	d.at(t, time.Minute, channeltest.LoopbackEvent{ID: "y-1", From: "b", Group: "g1", Text: "report bug: wrong one"})
	d.at(t, 30*time.Second, channeltest.LoopbackEvent{ID: "y-2", From: "b", Group: "g1", Text: "Hủy"})
	d.flush(t)
	if got := d.one(t, `SELECT string_agg(state||'/'||COALESCE(drop_reason,'-'),',' ORDER BY external_event_id) FROM channel_inbound_events WHERE workspace_id=$1
		AND external_event_id IN ('y-1','y-2')`); got != "dropped/cancelled,dropped/cancelled" {
		t.Fatalf("a cancel of a commanded batch = %s", got)
	}
}

// A one-to-one chat that joins an open conversation joins only when the customer has exactly one open
// on the channel; with none or several, the message is kept unresolved in the chat (seen and counted),
// never lost, and joins nothing.
func TestJoinOpenJoinsOnlyTheOneOpenConversation(t *testing.T) {
	loop := channeltest.NewLoopback("loopback", channels.Async)
	loop.Defaults.Scopes = map[string]channels.Scope{"user": {Mode: channels.ModeCommands, JoinPolicy: never, Unmatched: channels.UnmatchedJoinOpen}}
	d := newLoopDesk(t, loop)
	ctx := context.Background()
	last := func(id string) string {
		return d.one(t, `SELECT e.decision_rule||'/'||COALESCE(e.conversation_id::text,'-')||'/'||i.disposition FROM channel_inbound_events e
			JOIN channel_thread_items i ON i.workspace_id=e.workspace_id AND i.inbound_event_id=e.id WHERE e.workspace_id=$1 AND e.external_event_id=$2`, id)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "u-1", From: "u", Text: "hello?"})
	if got := last("u-1"); got != "unmatched_kept/-/unresolved" {
		t.Fatalf("no open conversation = %s", got)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "u-2", From: "u", Text: "report bug: the printer"})
	first := d.one(t, `SELECT id::text FROM conversations WHERE workspace_id=$1`)
	d.send(t, channeltest.LoopbackEvent{ID: "u-3", From: "u", Text: "here is the model"})
	if got := last("u-3"); got != "unmatched_join_open/"+first+"/conversation" {
		t.Fatalf("one open conversation = %s", got)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "u-4", From: "u", Text: "report bug: the scanner"})
	d.send(t, channeltest.LoopbackEvent{ID: "u-5", From: "u", Text: "which one do you mean"})
	if got := last("u-5"); got != "unmatched_kept/-/unresolved" {
		t.Fatalf("two open conversations = %s", got)
	}
	unresolved, err := d.store.UnresolvedItems(ctx, d.workspaceID, 0, "", 50, *d.clock)
	if err != nil || len(unresolved.Items) != 2 || unresolved.Items[0].Disposition != store.ItemUnresolved {
		t.Fatalf("unresolved = %+v, %v", unresolved, err)
	}
}

// A team member writing in a group on the channel's own app is context, not a customer's batch: it opens
// no gathering and no conversation. A command of theirs still opens a request.
func TestAStaffMessageOpensNoBatchButAStaffCommandDoes(t *testing.T) {
	d := newLoopDesk(t, intakeLoop())
	d.send(t, channeltest.LoopbackEvent{ID: "s-1", From: "nv1", Group: "g1", Text: "we are looking at it", Staff: true})
	d.flush(t)
	if got := d.gatherings(t); got != "" {
		t.Fatalf("gatherings of a staff message = %s", got)
	}
	if got := d.one(t, `SELECT count(*)::text FROM conversations WHERE workspace_id=$1`); got != "0" {
		t.Fatalf("conversations = %s", got)
	}
	if got := d.one(t, `SELECT disposition FROM channel_thread_items WHERE workspace_id=$1`); got != "staff" {
		t.Fatalf("item = %s", got)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "s-2", From: "nv1", Group: "g1", Text: "report bug: the printer on floor 2", Staff: true})
	d.flush(t)
	if got := d.one(t, `SELECT state||'/'||decision_rule||'/'||(message_id IS NOT NULL)::text FROM channel_inbound_events WHERE workspace_id=$1 AND external_event_id='s-2'`); got != "processed/command_open/true" {
		t.Fatalf("a staff command = %s", got)
	}
}

// The chat rules of a scope are checked when an admin sets them.
func TestChatRulesOutOfRangeAreRefused(t *testing.T) {
	d := newLoopDesk(t, episodeLoop())
	for _, settings := range []string{`{"scopes":{"user":{"unmatched":"join_any"}}}`,
		`{"scopes":{"group":{"gather":{"quiet_seconds":300,"max_seconds":1200,"pre_command_seconds":-1}}}}`,
		`{"commands":{"cancel":{"action":"cancel","match":"loose","phrases":["cancel"]}}}`} {
		_, err := d.store.UpdateChannel(context.Background(), d.workspaceID, d.channel.ID, store.ChannelInput{Settings: []byte(settings)}, store.ChannelActor{AgentID: d.agentID})
		var field *channels.FieldError
		if !errors.As(err, &field) {
			t.Fatalf("%s: %v", settings, err)
		}
	}
}

// At auto, a message a join-open scope would join waits for AI like any default; past its time the rules
// join it to the only open conversation, as they would without AI, or queue it when that one closed.
func TestPastItsTimeAJoinOpenMessageJoinsTheOnlyOpenConversation(t *testing.T) {
	loop := intakeLoop()
	loop.Defaults.Scopes = map[string]channels.Scope{"group": {Mode: channels.ModeCommands, JoinPolicy: never, Unmatched: channels.UnmatchedJoinOpen,
		Gather: &channels.Gather{QuietSeconds: 300, MaxSeconds: 1200}}}
	d := newLoopDesk(t, loop)
	d.intakeAt(t, "auto")
	ctx := context.Background()
	d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "a", Group: "g1", Text: "report bug: the printer"})
	d.flush(t)
	d.send(t, channeltest.LoopbackEvent{ID: "a-2", From: "a", Group: "g1", Text: "any news"})
	d.flush(t)
	*d.clock = d.clock.Add(3 * time.Minute)
	if _, err := d.intake().Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	ofEvent := `SELECT g.state||'/'||COALESCE(g.decided_by,g.review_reason)||'/'||COALESCE(e.decision_basis,'-')||':'||COALESCE(e.decision_rule,'-') FROM intake_gatherings g
		JOIN channel_inbound_events e ON e.workspace_id=g.workspace_id AND e.gathering_id=g.id WHERE g.workspace_id=$1 AND e.external_event_id=$2`
	if got := d.one(t, ofEvent, "a-2"); got != "attached/rules/default:unmatched_join_open" {
		t.Fatalf("past its time = %s", got)
	}
	if got := d.one(t, `SELECT count(DISTINCT conversation_id)::text FROM messages WHERE workspace_id=$1 AND kind='incoming'`); got != "1" {
		t.Fatalf("conversations = %s", got)
	}
	// The conversation closed meanwhile: the next waits in the queue instead.
	d.send(t, channeltest.LoopbackEvent{ID: "a-3", From: "a", Group: "g1", Text: "still?"})
	d.flush(t)
	if _, err := d.pool.Exec(ctx, `UPDATE conversations SET status='closed' WHERE workspace_id=$1`, d.workspaceID); err != nil {
		t.Fatal(err)
	}
	*d.clock = d.clock.Add(3 * time.Minute)
	if _, err := d.intake().Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if got := d.one(t, ofEvent, "a-3"); got != "review/target_changed/default:unmatched_join_open" {
		t.Fatalf("target closed meanwhile = %s", got)
	}
}
