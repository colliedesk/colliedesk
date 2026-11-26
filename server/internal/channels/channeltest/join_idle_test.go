package channeltest_test

import (
	"context"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
)

// idleLoop is a chat type whose groups several customers share and join only open conversations, as a
// Zalo OA's groups do.
func idleLoop() *channeltest.Loopback {
	loop := channeltest.NewLoopback("loopback", channels.Async)
	loop.SharedScopes = []string{"group"}
	loop.Defaults.JoinPolicy = channels.JoinPolicy{Kind: channels.JoinOpenOnly}
	return loop
}

// In a group, a customer's message joins the conversation the place continues only while that
// conversation had a customer message or a reply less than two hours before; later it opens a new one.
// A reply quoting a message of the old conversation still joins it, and the team's reply counts.
func TestInAGroupAMessageTwoHoursAfterTheConversationsLastOneOpensANewOne(t *testing.T) {
	d := newLoopDesk(t, idleLoop())
	d.at(t, 0, channeltest.LoopbackEvent{ID: "i-1", From: "a", Group: "g1", Text: "the report is wrong"})
	d.at(t, time.Hour, channeltest.LoopbackEvent{ID: "i-2", From: "a", Group: "g1", Text: "here is a screenshot"})
	d.at(t, 90*time.Minute, channeltest.LoopbackEvent{ID: "i-3", From: "a", Group: "g1", Text: "still wrong"})
	if got := d.decisions(t, "i-1", "i-2", "i-3"); got != "i-1:default:new_thread,i-2:match:thread_key,i-3:match:thread_key" {
		t.Fatalf("within two hours of the last message: %s", got)
	}
	d.at(t, 2*time.Hour, channeltest.LoopbackEvent{ID: "i-4", From: "a", Group: "g1", Text: "new: the address does not fill in"})
	if got := d.decisions(t, "i-4"); got != "i-4:default:join_policy" {
		t.Fatalf("two hours after the last message: %s", got)
	}
	if got := d.one(t, `SELECT count(*)::text FROM conversations WHERE workspace_id=$1`); got != "2" {
		t.Fatalf("conversations = %s", got)
	}
	d.at(t, 3*time.Hour, channeltest.LoopbackEvent{ID: "i-5", From: "a", Group: "g1", Reference: "i-1", Text: "about the report"})
	if got := d.decisions(t, "i-5"); got != "i-5:match:thread_reference" {
		t.Fatalf("a quote of the old conversation: %s", got)
	}
	// The team answered the newest conversation an hour before the customer writes again.
	if _, err := d.pool.Exec(context.Background(), `INSERT INTO messages(workspace_id,conversation_id,kind,body,created_at)
		SELECT m.workspace_id,m.conversation_id,'outgoing','which address?',$2::timestamptz FROM messages m JOIN channel_inbound_events e
			ON e.workspace_id=m.workspace_id AND e.message_id=m.id WHERE m.workspace_id=$1 AND e.external_event_id='i-4'`,
		d.workspaceID, d.clock.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	d.at(t, 3*time.Hour, channeltest.LoopbackEvent{ID: "i-6", From: "a", Group: "g1", Text: "the one in Ninh Binh"})
	if got := d.decisions(t, "i-6"); got != "i-6:match:thread_key" {
		t.Fatalf("an hour after the team's reply: %s", got)
	}
	if got := d.one(t, `SELECT string_agg(m.body,'|' ORDER BY m.created_at) FROM messages m JOIN channel_inbound_events e
		ON e.workspace_id=m.workspace_id AND e.message_id=m.id WHERE m.workspace_id=$1 AND e.external_event_id IN ('i-4','i-6')
		GROUP BY m.conversation_id`); got != "new: the address does not fill in|the one in Ninh Binh" {
		t.Fatalf("the answer joined %q", got)
	}
}

// A one-to-one chat starts a new request two hours after its conversation's last message too.
func TestOneToOneAMessageTwoHoursAfterTheConversationsLastOneOpensANewOne(t *testing.T) {
	d := newLoopDesk(t, idleLoop())
	d.at(t, 0, channeltest.LoopbackEvent{ID: "o-1", From: "a", Text: "the report is wrong"})
	d.at(t, 90*time.Minute, channeltest.LoopbackEvent{ID: "o-2", From: "a", Text: "still wrong"})
	d.at(t, 2*time.Hour, channeltest.LoopbackEvent{ID: "o-3", From: "a", Text: "new: the address does not fill in"})
	if got := d.decisions(t, "o-1", "o-2", "o-3"); got != "o-1:default:new_thread,o-2:match:thread_key,o-3:default:join_policy" {
		t.Fatalf("decisions = %s", got)
	}
}
