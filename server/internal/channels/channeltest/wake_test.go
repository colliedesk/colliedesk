package channeltest_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
	"github.com/colliedesk/colliedesk/server/internal/channels/outbox"
	"github.com/colliedesk/colliedesk/server/internal/channels/pipeline"
)

// The question goes out once the customer has been quiet for quiet_seconds, give or take the journal's
// round: what the journal queues wakes the outbox, which does not wait for its own interval.
func TestTheQuestionGoesOutOnceTheCustomerIsQuiet(t *testing.T) {
	loop := confirmLoop()
	loop.Defaults.Scopes["user"].Gather.QuietSeconds = 1
	d := newLoopDesk(t, loop)
	wake := make(chan struct{}, 1)
	journal := pipeline.New(d.store, nil).WithWake(wake)
	worker := outbox.New(d.store, outbox.Options{Wake: wake})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go journal.Run(ctx, 200*time.Millisecond)
	go worker.Run(ctx, time.Hour)
	sent := time.Now()
	if _, err := journal.Receive(ctx, d.channel, []channels.Raw{loopRaw(channeltest.LoopbackEvent{ID: "w-1", From: "u1", Text: "report bug the printer jams"})}); err != nil {
		t.Fatal(err)
	}
	for time.Since(sent) < 5*time.Second && len(loop.Endpoint.Sent()) == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if len(loop.Endpoint.Sent()) == 0 {
		t.Fatal("the question was not sent within 5 s")
	}
	if took := time.Since(sent); took < time.Second || took > 2*time.Second {
		t.Fatalf("the question went out after %s, want quiet_seconds (1 s) give or take the journal's round", took)
	}
}

func loopRaw(event channeltest.LoopbackEvent) channels.Raw {
	encoded, _ := json.Marshal(event)
	return channels.Raw{ExternalEventID: event.ID, JSON: encoded}
}
