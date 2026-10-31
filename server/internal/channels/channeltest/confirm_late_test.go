package channeltest_test

import (
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// A request written while the channel was paused is past its question once the channel resumes: it opens
// without one, its clock read from when it arrived.
func TestARequestWrittenWhilePausedOpensWithoutAQuestion(t *testing.T) {
	d := newLoopDesk(t, confirmLoop())
	pause := func(on bool) {
		t.Helper()
		if _, err := d.store.UpdateChannel(t.Context(), d.workspaceID, d.channel.ID, store.ChannelInput{ProcessingPaused: &on}, store.ChannelActor{AgentID: d.agentID}); err != nil {
			t.Fatal(err)
		}
	}
	pause(true)
	d.send(t, channeltest.LoopbackEvent{ID: "c-1", From: "u1", Text: "report bug the printer jams"})
	d.step(t, time.Hour)
	pause(false)
	d.step(t, time.Second)
	d.step(t, time.Second)
	if got := d.notices(t, channels.NoticeAskConfirm); got != "0" {
		t.Fatalf("asks = %s, want none", got)
	}
	if got := d.conversations(t); got != "report bug the printer jams" {
		t.Fatalf("once resumed = %q", got)
	}
}

// Two workers that listed the same due batch: the second, finding it asked about and not due, leaves it to
// its minute.
func TestABatchListedTwiceWaitsForItsConfirmation(t *testing.T) {
	d := newLoopDesk(t, confirmLoop())
	d.send(t, channeltest.LoopbackEvent{ID: "c-1", From: "u1", Text: "report bug the printer jams"})
	*d.clock = d.clock.Add(11 * time.Second)
	stale, err := d.store.DueInbound(t.Context(), *d.clock, 10, 5)
	if err != nil || len(stale) != 1 {
		t.Fatalf("due = %v %v", stale, err)
	}
	d.step(t, 0)
	if err := d.pipeline.Process(t.Context(), stale[0]); err != nil {
		t.Fatal(err)
	}
	if got := d.conversations(t); got != "" {
		t.Fatalf("opened right after the question: %s", got)
	}
}

// What the customer adds while the channel is paused joins the request they started before it: resumed, the
// batch opens with all of it, not with what was listed before the addition was taken.
func TestWhatIsAddedWhilePausedJoinsTheRequestStartedBefore(t *testing.T) {
	d := newLoopDesk(t, confirmLoop())
	pause := func(on bool) {
		t.Helper()
		if _, err := d.store.UpdateChannel(t.Context(), d.workspaceID, d.channel.ID, store.ChannelInput{ProcessingPaused: &on}, store.ChannelActor{AgentID: d.agentID}); err != nil {
			t.Fatal(err)
		}
	}
	d.send(t, channeltest.LoopbackEvent{ID: "c-1", From: "u1", Text: "report bug the printer jams"})
	pause(true)
	d.step(t, time.Minute)
	d.send(t, channeltest.LoopbackEvent{ID: "c-2", From: "u1", Text: "on tray two"})
	d.step(t, 30*time.Minute)
	pause(false)
	for range 3 {
		d.step(t, time.Second)
	}
	if got := d.conversations(t); got != "report bug the printer jams+on tray two" {
		t.Fatalf("once resumed = %q", got)
	}
}
