package channeltest_test

import (
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
)

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
