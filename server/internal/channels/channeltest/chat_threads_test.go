package channeltest_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
	"github.com/colliedesk/colliedesk/server/internal/channels/outbox"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// bodies lists a page's item bodies, oldest first, with role and whether linked.
func bodies(page store.ChatPage) string {
	parts := make([]string, 0, len(page.Items))
	for i := len(page.Items) - 1; i >= 0; i-- {
		item := page.Items[i]
		mark := ""
		if item.Linked {
			mark = "*"
		}
		parts = append(parts, item.Role+":"+item.Body+mark)
	}
	return strings.Join(parts, "|")
}

// Every message said in a group is an item of its thread: chatter, the requests and the team's
// replies. Each conversation reads the thread around it and knows which items are its own; two
// conversations of one group never mix their items.
func TestEveryMessageOfAGroupIsAnItemAndConversationsKeepTheirOwn(t *testing.T) {
	d := newLoopDesk(t, channeltest.NewLoopback("loopback", channels.Async))
	ctx := context.Background()
	settings := json.RawMessage(`{"scopes":{"group":{"mode":"commands"}}}`)
	if _, err := d.store.UpdateChannel(ctx, d.workspaceID, d.channel.ID, store.ChannelInput{Settings: settings}, store.ChannelActor{AgentID: d.agentID}); err != nil {
		t.Fatal(err)
	}
	say := func(events ...channeltest.LoopbackEvent) {
		for _, event := range events {
			*d.clock = d.clock.Add(time.Second)
			d.send(t, event)
		}
	}
	say(channeltest.LoopbackEvent{ID: "c-1", From: "m1", Group: "g1", Text: "good morning"},
		channeltest.LoopbackEvent{ID: "c-2", From: "m1", Group: "g1", Text: "report bug: the printer"})
	first := d.one(t, `SELECT conversation_id::text FROM channel_inbound_events WHERE workspace_id=$1 AND external_event_id='c-2'`)
	if _, err := d.pool.Exec(ctx, `UPDATE conversations SET status='resolved',updated_at=now()-interval '30 days' WHERE workspace_id=$1`, d.workspaceID); err != nil {
		t.Fatal(err)
	}
	say(channeltest.LoopbackEvent{ID: "c-3", From: "m2", Group: "g1", Text: "report bug: the scanner"},
		channeltest.LoopbackEvent{ID: "c-4", From: "m2", Group: "g1", Text: "it beeps"})
	second := d.one(t, `SELECT conversation_id::text FROM channel_inbound_events WHERE workspace_id=$1 AND external_event_id='c-3'`)
	if first == second {
		t.Fatal("the two requests share a conversation")
	}
	whole, err := d.store.ConversationChat(ctx, d.workspaceID, first, "", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := bodies(whole); got != "customer:good morning|customer:report bug: the printer*|customer:report bug: the scanner|customer:it beeps" {
		t.Fatalf("the first conversation's chat = %s", got)
	}
	own, err := d.store.ConversationChat(ctx, d.workspaceID, second, "", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := bodies(own); got != "customer:report bug: the scanner*|customer:it beeps*" {
		t.Fatalf("the second conversation's own items = %s", got)
	}
	if !whole.Items[0].IsOtherCustomer || whole.Items[3].IsOtherCustomer {
		t.Fatalf("other customers: %+v", whole.Items)
	}
	// Chatter part of no conversation names no contact: it is another customer's by who wrote it.
	around, err := d.store.ConversationChat(ctx, d.workspaceID, second, "", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if last := around.Items[len(around.Items)-1]; last.Body != "good morning" || last.ContactID != nil || !last.IsOtherCustomer || around.Items[0].IsOtherCustomer {
		t.Fatalf("the second conversation's chat: %+v", around.Items)
	}
	if got := d.one(t, `SELECT string_agg(disposition||':'||COALESCE(disposition_reason,'-'),',' ORDER BY occurred_at,external_id) FROM channel_thread_items WHERE workspace_id=$1`); got != "archived:chat,conversation:-,conversation:-,conversation:-" {
		t.Fatalf("dispositions = %s", got)
	}
	threads, err := d.store.ChannelThreads(ctx, d.workspaceID, d.channel.ID, "group", "", 0)
	if err != nil || len(threads.Items) != 1 || threads.Items[0].LastItem == nil || threads.Items[0].LastItem.Body != "it beeps" {
		t.Fatalf("threads = %+v, %v", threads, err)
	}
	// Another workspace's thread is not found.
	other := newLoopDesk(t, channeltest.NewLoopback("loopback", channels.Async))
	if _, err := other.store.ThreadItems(ctx, other.workspaceID, other.channel.ID, threads.Items[0].ID, "", 0); err == nil {
		t.Fatal("another workspace reads the thread")
	}
}

// A reply the team sends and the channel's own notice are items of the thread too; the copy the channel
// sends back of them adds none, and processing an event again adds none.
func TestRepliesAreItemsAndNothingIsCountedTwice(t *testing.T) {
	loop := channeltest.NewLoopback("loopback", channels.Async)
	loop.Defaults.Notices.Received = "Thanks, we got it."
	d := newLoopDesk(t, loop)
	ctx := context.Background()
	d.send(t, channeltest.LoopbackEvent{ID: "r-1", From: "u1", Text: "help"})
	conversationID := d.one(t, `SELECT id::text FROM conversations WHERE workspace_id=$1`)
	reply, err := d.store.CreateMessage(ctx, d.workspaceID, conversationID, store.Actor{AgentID: d.agentID}, "outgoing", "On it")
	if err != nil {
		t.Fatal(err)
	}
	worker := outbox.New(d.store, outbox.Options{Now: func() time.Time { return time.Now().UTC().Add(time.Minute) }})
	if _, err := worker.ProcessDue(ctx); err != nil {
		t.Fatal(err)
	}
	page, err := d.store.ConversationChat(ctx, d.workspaceID, conversationID, "", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := bodies(page); got != "customer:help*|bot:Thanks, we got it.*|staff:On it*" {
		t.Fatalf("chat = %s", got)
	}
	echo := d.one(t, `SELECT external_id FROM channel_sent_ids WHERE workspace_id=$1 AND message_id=$2 LIMIT 1`, reply.ID)
	d.send(t, channeltest.LoopbackEvent{ID: echo, From: "oa", Text: "On it"})
	d.send(t, channeltest.LoopbackEvent{ID: "r-1", From: "u1", Text: "help"})
	if got := d.one(t, `SELECT count(*)::text FROM channel_thread_items WHERE workspace_id=$1`); got != "3" {
		t.Fatalf("items = %s, want 3", got)
	}
}

// A cancel concludes what was being collected with it as not a request, every item of it; a message an
// admin discards leaves nothing of what it said in its place.
func TestACancelConcludesItsItemsAndADiscardRemovesThem(t *testing.T) {
	d := newLoopDesk(t, channeltest.NewLoopback("loopback", channels.Async))
	ctx := context.Background()
	d.send(t, channeltest.LoopbackEvent{ID: "k-1", From: "u1", Text: "draft of a request", Collect: true},
		channeltest.LoopbackEvent{ID: "k-2", From: "u1", Text: "never mind", Collect: true, Intent: channels.IntentCancel})
	if got := d.one(t, `SELECT string_agg(disposition||':'||disposition_reason,',' ORDER BY external_id) FROM channel_thread_items WHERE workspace_id=$1`); got != "not_request:cancelled,not_request:cancelled" {
		t.Fatalf("items after a cancel = %s", got)
	}
	off := false
	if _, err := d.store.UpdateChannel(ctx, d.workspaceID, d.channel.ID, store.ChannelInput{Enabled: &off}, store.ChannelActor{AgentID: d.agentID}); err != nil {
		t.Fatal(err)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "k-3", From: "u2", Text: "a secret the admin discards"})
	if got := d.one(t, `SELECT count(*)::text FROM channel_thread_items WHERE workspace_id=$1 AND body LIKE '%secret%'`); got != "1" {
		t.Fatalf("held item = %s", got)
	}
	if got := d.one(t, `SELECT string_agg(sender_value,',' ORDER BY sender_value) FROM channel_senders WHERE workspace_id=$1`); got != "u1,u2" {
		t.Fatalf("senders while held = %s", got)
	}
	if _, err := d.store.DiscardHeld(ctx, d.workspaceID, d.channel.ID, nil, store.ChannelActor{AgentID: d.agentID}); err != nil {
		t.Fatal(err)
	}
	if got := d.one(t, `SELECT count(*)::text FROM channel_thread_items WHERE workspace_id=$1 AND body LIKE '%secret%'`); got != "0" {
		t.Fatalf("a discarded message's item stayed: %s", got)
	}
	// Who wrote only what was discarded is no longer listed as having written.
	if got := d.one(t, `SELECT string_agg(sender_value,',' ORDER BY sender_value) FROM channel_senders WHERE workspace_id=$1`); got != "u1" {
		t.Fatalf("senders after the discard = %s", got)
	}
}

// A member whose message joined another's conversation is still another customer there, before and after:
// what they say later in the group, part of no conversation, reads as theirs, not the conversation's.
func TestAMemberWhoJoinedAnotherConversationStaysAnotherCustomer(t *testing.T) {
	d := newLoopDesk(t, channeltest.NewLoopback("loopback", channels.Async))
	ctx := context.Background()
	mode := func(mode string) {
		t.Helper()
		settings := json.RawMessage(`{"scopes":{"group":{"mode":"` + mode + `"}}}`)
		if _, err := d.store.UpdateChannel(ctx, d.workspaceID, d.channel.ID, store.ChannelInput{Settings: settings}, store.ChannelActor{AgentID: d.agentID}); err != nil {
			t.Fatal(err)
		}
	}
	say := func(events ...channeltest.LoopbackEvent) {
		for _, event := range events {
			*d.clock = d.clock.Add(time.Second)
			d.send(t, event)
		}
	}
	mode("all")
	say(channeltest.LoopbackEvent{ID: "j-1", From: "m1", Group: "g1", Text: "report bug: the printer"},
		channeltest.LoopbackEvent{ID: "j-2", From: "m2", Group: "g1", Text: "same here"})
	first := d.one(t, `SELECT conversation_id::text FROM channel_inbound_events WHERE workspace_id=$1 AND external_event_id='j-1'`)
	if _, err := d.pool.Exec(ctx, `UPDATE conversations SET status='resolved',updated_at=now()-interval '30 days' WHERE workspace_id=$1`, d.workspaceID); err != nil {
		t.Fatal(err)
	}
	mode("commands")
	say(channeltest.LoopbackEvent{ID: "j-3", From: "m2", Group: "g1", Text: "lunch?"})
	page, err := d.store.ConversationChat(ctx, d.workspaceID, first, "", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, item := range page.Items {
		got = append(got, fmt.Sprintf("%s=%v", item.Body, item.IsOtherCustomer))
	}
	if fmt.Sprint(got) != "[lunch?=true same here=true report bug: the printer=false]" {
		t.Fatalf("other customers = %v", got)
	}
}
