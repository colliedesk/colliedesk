package channeltest_test

import (
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
)

// The received notice can name the code of the conversation it acknowledges, which the customer types back.
func TestTheReceivedNoticeNamesTheConversationsCode(t *testing.T) {
	loop := channeltest.NewLoopback("loopback", channels.Async)
	loop.Defaults.Notices.Received = "Got it: {ref}. Quote it when you write about this again."
	d := newLoopDesk(t, loop)
	d.send(t, channeltest.LoopbackEvent{ID: "r-1", From: "u1", Text: "the printer jams"})
	ref := d.one(t, `SELECT display_ref FROM conversations WHERE workspace_id=$1`)
	if got := d.one(t, `SELECT body FROM messages WHERE workspace_id=$1 AND notice_kind='received'`); got != "Got it: "+ref+". Quote it when you write about this again." {
		t.Fatalf("received notice = %q (code %s)", got, ref)
	}
}
