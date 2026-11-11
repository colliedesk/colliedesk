package channeltest_test

import (
	"context"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
	"github.com/colliedesk/colliedesk/server/internal/channels/outbox"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// While a channel is paused, what arrives waits in its journal as it came and nothing goes out; once it is
// resumed the journal is processed in order by the channel's rules, as if it had just arrived (chatter of a
// commands-only scope is kept, not opened, unlike a held message a person opens), and what was queued is
// sent. The history says who paused and resumed it.
func TestAPausedChannelKeepsWhatArrivesAndProcessesItByItsRulesOnceResumed(t *testing.T) {
	loop := channeltest.NewLoopback("loopback", channels.Async)
	loop.Defaults.Commands = map[string]channels.Command{"bug": {Match: channels.MatchWords, Phrases: []string{"report bug"}}}
	loop.Defaults.Scopes = map[string]channels.Scope{"user": {Mode: channels.ModeCommands}}
	d := newLoopDesk(t, loop)
	ctx := context.Background()
	d.send(t, channeltest.LoopbackEvent{ID: "p-0", From: "u1", Text: "report bug the printer"})
	conversation := d.one(t, `SELECT id::text FROM conversations WHERE workspace_id=$1`)
	pause := func(on bool) {
		t.Helper()
		if _, err := d.store.UpdateChannel(ctx, d.workspaceID, d.channel.ID, store.ChannelInput{ProcessingPaused: &on}, store.ChannelActor{AgentID: d.agentID}); err != nil {
			t.Fatal(err)
		}
	}
	pause(true)
	d.send(t, channeltest.LoopbackEvent{ID: "p-1", From: "u2", Text: "hello there"}, channeltest.LoopbackEvent{ID: "p-2", From: "u2", Text: "report bug the invoice"})
	if _, err := d.store.CreateMessage(ctx, d.workspaceID, conversation, store.Actor{AgentID: d.agentID}, "outgoing", "Looking at it."); err != nil {
		t.Fatal(err)
	}
	worker := outbox.New(d.store, outbox.Options{Now: func() time.Time { return time.Now().UTC().Add(time.Minute) }})
	if _, err := worker.ProcessDue(ctx); err != nil {
		t.Fatal(err)
	}
	if got := d.one(t, `SELECT string_agg(external_event_id||'='||state,',' ORDER BY external_event_id) FROM channel_inbound_events WHERE workspace_id=$1 AND external_event_id<>'p-0'`); got != "p-1=received,p-2=received" {
		t.Fatalf("journal while paused = %s", got)
	}
	if got := d.one(t, `SELECT count(*)::text FROM conversations WHERE workspace_id=$1`); got != "1" {
		t.Fatalf("conversations while paused = %s", got)
	}
	if got := d.one(t, `SELECT string_agg(status,',') FROM channel_outbox WHERE workspace_id=$1`); got != "queued" {
		t.Fatalf("outbox while paused = %s", got)
	}
	pause(false)
	if _, err := d.pipeline.ProcessDue(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.ProcessDue(ctx); err != nil {
		t.Fatal(err)
	}
	if got := d.one(t, `SELECT string_agg(external_event_id||'='||state||'/'||COALESCE(decision_rule,''),',' ORDER BY external_event_id) FROM channel_inbound_events
		WHERE workspace_id=$1 AND external_event_id<>'p-0'`); got != "p-1=processed/scope_archive,p-2=processed/command_open" {
		t.Fatalf("journal once resumed = %s", got)
	}
	if got := d.one(t, `SELECT string_agg(status,',') FROM channel_outbox WHERE workspace_id=$1`); got != "sent" {
		t.Fatalf("outbox once resumed = %s", got)
	}
	if got := d.one(t, `SELECT string_agg(kind,',' ORDER BY seq) FROM channel_events WHERE workspace_id=$1 AND kind IN ('paused','resumed')`); got != "paused,resumed" {
		t.Fatalf("history = %s", got)
	}
}
