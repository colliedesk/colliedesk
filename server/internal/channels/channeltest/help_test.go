package channeltest_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// helpLoop is a chat whose customers ask for its guide: one-to-one requests are collected and asked
// about, groups open on a command, a message naming none is answered once in five minutes.
func helpLoop() *channeltest.Loopback {
	loop := channeltest.NewLoopback("loopback", channels.Async)
	loop.Defaults.Commands = map[string]channels.Command{
		"bug":  {Match: channels.MatchWords, Phrases: []string{"report bug"}},
		"help": {Action: channels.ActionHelp, Match: channels.MatchWords, Phrases: []string{"help", "/help"}},
	}
	loop.Defaults.Notices.Help = "Write report bug and what happened."
	loop.Defaults.Notices.Unmatched = "Write report bug, or help."
	loop.Defaults.Scopes = map[string]channels.Scope{
		"user":  {Mode: channels.ModeCommands, Unmatched: channels.UnmatchedNotice, Gather: &channels.Gather{QuietSeconds: 10, MaxSeconds: 1800}},
		"group": {Mode: channels.ModeCommands, Gather: &channels.Gather{QuietSeconds: 300, MaxSeconds: 1200}},
	}
	loop.SharedScopes = []string{"group"}
	return loop
}

// A help command is answered with the help notice, one-to-one and in a group, and opens nothing. It is
// no part of a request being written, does not hold back the next unmatched notice, and a channel that is
// off keeps it unanswered like every message.
func TestAHelpCommandIsAnsweredWithTheGuideAndOpensNothing(t *testing.T) {
	d := newLoopDesk(t, helpLoop())
	d.send(t, channeltest.LoopbackEvent{ID: "h-1", From: "u1", Text: "help"})
	d.send(t, channeltest.LoopbackEvent{ID: "h-2", From: "u2", Group: "g1", Text: "/help"})
	if got := d.one(t, `SELECT count(*)::text FROM conversations WHERE workspace_id=$1`); got != "0" {
		t.Fatalf("conversations = %s, want none", got)
	}
	if got := d.one(t, `SELECT string_agg(i.external_id||':'||i.disposition||'/'||COALESCE(i.disposition_reason,''),',' ORDER BY i.external_id) FROM channel_thread_items i
		WHERE i.workspace_id=$1 AND i.direction='in'`); got != "h-1:not_request/help,h-2:not_request/help" {
		t.Fatalf("the customers' items = %s", got)
	}
	if got := d.one(t, `SELECT string_agg(i.disposition_reason||':'||i.body||':'||COALESCE(i.quote_external_id,''),',' ORDER BY i.quote_external_id) FROM channel_thread_items i
		JOIN channel_outbox o ON o.workspace_id=i.workspace_id AND o.thread_item_id=i.id WHERE i.workspace_id=$1 AND i.direction='out'`); got !=
		"help:Write report bug and what happened.:h-1,help:Write report bug and what happened.:h-2" {
		t.Fatalf("the notices = %s", got)
	}
	// The unmatched notice still answers the next message naming nothing.
	d.send(t, channeltest.LoopbackEvent{ID: "h-3", From: "u1", Text: "hello?"})
	d.step(t, 11*time.Second)
	if got := d.notices(t, channels.NoticeUnmatched); got != "1" {
		t.Fatalf("unmatched notices after a help = %s, want 1", got)
	}
	// Asked while writing a request: the request goes on without it.
	d.send(t, channeltest.LoopbackEvent{ID: "h-4", From: "u3", Text: "report bug the printer jams"})
	d.step(t, time.Second)
	d.send(t, channeltest.LoopbackEvent{ID: "h-5", From: "u3", Text: "help"})
	d.step(t, time.Second)
	d.send(t, channeltest.LoopbackEvent{ID: "h-6", From: "u3", Text: "on tray two"})
	d.step(t, 11*time.Second)
	if got := d.one(t, `SELECT count(*)||':'||string_agg(replace(m.body,E'\n','+'),'|' ORDER BY m.created_at) FROM conversations c JOIN messages m ON m.workspace_id=c.workspace_id AND m.conversation_id=c.id AND m.kind='incoming' WHERE c.workspace_id=$1 GROUP BY c.id`); got != "1:report bug the printer jams+on tray two" {
		t.Fatalf("the request = %q", got)
	}
	if got := d.notices(t, channels.NoticeHelp); got != "3" {
		t.Fatalf("help notices = %s, want 3", got)
	}
	// Off: kept for an admin, nobody answered.
	off := false
	if _, err := d.store.UpdateChannel(context.Background(), d.workspaceID, d.channel.ID, store.ChannelInput{Enabled: &off}, store.ChannelActor{AgentID: d.agentID}); err != nil {
		t.Fatal(err)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "h-7", From: "u1", Text: "help"})
	if got := d.notices(t, channels.NoticeHelp); got != "3" {
		t.Fatalf("help notices while off = %s, want 3", got)
	}
	if got := d.one(t, `SELECT state FROM channel_inbound_events WHERE workspace_id=$1 AND external_event_id='h-7'`); got != "held" {
		t.Fatalf("the help while off = %s, want held", got)
	}
	// Opened later from what was kept: dropped, the guide does not come late.
	on := true
	if _, err := d.store.UpdateChannel(context.Background(), d.workspaceID, d.channel.ID, store.ChannelInput{Enabled: &on}, store.ChannelActor{AgentID: d.agentID}); err != nil {
		t.Fatal(err)
	}
	held := d.one(t, `SELECT id::text FROM channel_inbound_events WHERE workspace_id=$1 AND external_event_id='h-7'`)
	if _, err := d.pipeline.ReplayHeld(context.Background(), d.workspaceID, d.channel.ID, []string{held}, "", store.ChannelActor{AgentID: d.agentID}); err != nil {
		t.Fatal(err)
	}
	if got := d.notices(t, channels.NoticeHelp) + "," + d.one(t, `SELECT state||'/'||COALESCE(drop_reason,'') FROM channel_inbound_events WHERE workspace_id=$1 AND external_event_id='h-7'`) + "," +
		d.one(t, `SELECT count(*)::text FROM conversations WHERE workspace_id=$1`); got != "3,dropped/help,1" {
		t.Fatalf("after opening the kept help = %s, want 3,dropped/help,1", got)
	}
}

// A channel's settings take a help command and its notice; the notice fills no placeholder.
func TestAChannelSetsUpAHelpCommand(t *testing.T) {
	d := newLoopDesk(t, channeltest.NewLoopback("loopback", channels.Async))
	ctx := context.Background()
	ok := json.RawMessage(`{"commands":{"help":{"action":"help","match":"words","phrases":["help"]}},"notices":{"help":"Write report bug."}}`)
	if _, err := d.store.UpdateChannel(ctx, d.workspaceID, d.channel.ID, store.ChannelInput{Settings: ok}, store.ChannelActor{AgentID: d.agentID}); err != nil {
		t.Fatalf("a help command: %v", err)
	}
	wrong := json.RawMessage(`{"notices":{"help":"See {ref}."}}`)
	var field *channels.FieldError
	if _, err := d.store.UpdateChannel(ctx, d.workspaceID, d.channel.ID, store.ChannelInput{Settings: wrong}, store.ChannelActor{AgentID: d.agentID}); !errors.As(err, &field) ||
		field.Fields["settings.notices.help"] == "" {
		t.Fatalf("a help notice with {ref}: %v", err)
	}
}
