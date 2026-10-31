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

// noticeLoop is a chat whose one-to-one scope opens a conversation only on a command and answers any
// other message with the channel's unmatched notice.
func noticeLoop() *channeltest.Loopback {
	loop := channeltest.NewLoopback("loopback", channels.Async)
	loop.Defaults.Commands = map[string]channels.Command{"bug": {Match: channels.MatchWords, Phrases: []string{"report bug"}}}
	loop.Defaults.Notices.Unmatched = "Write report bug and what happened."
	loop.Defaults.Scopes = map[string]channels.Scope{"user": {Mode: channels.ModeCommands, Unmatched: channels.UnmatchedNotice}}
	loop.SharedScopes = []string{"group"}
	return loop
}

// A one-to-one message that names no command opens nothing and is answered with the unmatched notice, in
// its chat: the notice goes out through the outbox, says what it answers, and is in the chat with the id
// the channel gave it. Every such message is answered; a command is not, and a second delivery of one
// message is answered once.
func TestAMessageNamingNoCommandIsAnsweredWithTheUnmatchedNotice(t *testing.T) {
	loop := noticeLoop()
	d := newLoopDesk(t, loop)
	ctx := context.Background()
	d.send(t, channeltest.LoopbackEvent{ID: "u-1", From: "u1", Text: "hello, are you there?"})
	if got := d.one(t, `SELECT count(*)::text FROM conversations WHERE workspace_id=$1`); got != "0" {
		t.Fatalf("conversations = %s, want none", got)
	}
	if got := d.one(t, `SELECT disposition||'/'||COALESCE(disposition_reason,'') FROM channel_thread_items WHERE workspace_id=$1 AND external_id='u-1'`); got != "archived/unmatched" {
		t.Fatalf("the customer's item = %s", got)
	}
	if got := d.one(t, `SELECT i.role||':'||i.body||':'||COALESCE(i.quote_external_id,'')||':'||o.status FROM channel_thread_items i
		JOIN channel_outbox o ON o.workspace_id=i.workspace_id AND o.thread_item_id=i.id WHERE i.workspace_id=$1 AND i.direction='out'`); got != "bot:Write report bug and what happened.:u-1:queued" {
		t.Fatalf("the notice = %s", got)
	}
	worker := outbox.New(d.store, outbox.Options{Now: func() time.Time { return time.Now().UTC().Add(time.Minute) }})
	if _, err := worker.ProcessDue(ctx); err != nil {
		t.Fatal(err)
	}
	sent := loop.Endpoint.Sent()
	if len(sent) != 1 || sent[0].To != "u1" || sent[0].Text != "Write report bug and what happened." {
		t.Fatalf("sent = %+v", sent)
	}
	if got := d.one(t, `SELECT COALESCE(i.external_id,'none')||':'||o.status FROM channel_thread_items i JOIN channel_outbox o ON o.workspace_id=i.workspace_id AND o.thread_item_id=i.id
		WHERE i.workspace_id=$1 AND i.direction='out'`); got != sent[0].ExternalID+":sent" {
		t.Fatalf("after sending: %s, want %s:sent", got, sent[0].ExternalID)
	}
	thread := d.one(t, `SELECT id::text FROM channel_threads WHERE workspace_id=$1`)
	page, err := d.store.ThreadItems(ctx, d.workspaceID, d.channel.ID, thread, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range page.Items {
		if want := map[string]string{"bot": "sent", "customer": ""}[item.Role]; (item.DeliveryStatus == nil) != (want == "") || (want != "" && *item.DeliveryStatus != want) {
			t.Fatalf("%s item: delivery %v, want %q", item.Role, item.DeliveryStatus, want)
		}
	}
	// The channel's copy of what it sent is its own, not a customer's message.
	d.send(t, channeltest.LoopbackEvent{ID: sent[0].ExternalID, From: "u1", Text: sent[0].Text, Echo: true})
	if got := d.one(t, `SELECT count(*)::text FROM channel_thread_items WHERE workspace_id=$1`); got != "2" {
		t.Fatalf("items after the echo = %s, want the two", got)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "u-1", From: "u1", Text: "hello, are you there?"})
	d.send(t, channeltest.LoopbackEvent{ID: "u-2", From: "u1", Text: "anyone?"})
	d.send(t, channeltest.LoopbackEvent{ID: "u-3", From: "u1", Text: "report bug the invoice is wrong"})
	if got := d.one(t, `SELECT count(*)::text FROM channel_outbox WHERE workspace_id=$1 AND thread_item_id IS NOT NULL`); got != "2" {
		t.Fatalf("notices = %s, want one for each message naming no command", got)
	}
	if got := d.one(t, `SELECT count(*)::text FROM conversations WHERE workspace_id=$1`); got != "1" {
		t.Fatalf("conversations = %s, want the command's", got)
	}
}

// Without a notice text the scope keeps the message and sends nothing; a notice may name no placeholder
// the channel does not fill.
func TestTheUnmatchedNoticeNeedsItsTextAndKnownPlaceholders(t *testing.T) {
	loop := noticeLoop()
	loop.Defaults.Notices.Unmatched = ""
	d := newLoopDesk(t, loop)
	d.send(t, channeltest.LoopbackEvent{ID: "q-1", From: "u1", Text: "hello"})
	if got := d.one(t, `SELECT count(*)::text FROM channel_thread_items WHERE workspace_id=$1 AND direction='out'`); got != "0" {
		t.Fatalf("notices without a text = %s", got)
	}
	for _, settings := range []string{`{"notices":{"unmatched":"Your ticket {ref} is open"}}`, `{"notices":{"received":"Thanks {name}"}}`,
		`{"scopes":{"user":{"mode":"commands","unmatched":"answer"}}}`, `{"scopes":{"group":{"mode":"commands","unmatched":"notice"}}}`} {
		if _, err := d.updateSettings(settings); err == nil {
			t.Errorf("%s was kept, want it refused", settings)
		}
	}
	if _, err := d.updateSettings(`{"notices":{"unmatched":"Write {not a placeholder} or report bug"}}`); err != nil {
		t.Errorf("braces that name no placeholder: %v", err)
	}
}

func (d loopDesk) updateSettings(settings string) (channels.Channel, error) {
	return d.store.UpdateChannel(context.Background(), d.workspaceID, d.channel.ID, store.ChannelInput{Settings: []byte(settings)}, store.ChannelActor{AgentID: d.agentID})
}

// What the team writes on the chat app, and the copy the channel sends back of its own messages, are
// never answered: the channel never answers itself.
func TestTheUnmatchedNoticeNeverAnswersTheTeamOrTheChannel(t *testing.T) {
	d := newLoopDesk(t, noticeLoop())
	d.send(t, channeltest.LoopbackEvent{ID: "s-1", From: "nv1", Text: "checking the printer", Staff: true})
	d.send(t, channeltest.LoopbackEvent{ID: "e-1", From: "u1", Text: "Write report bug and what happened.", Echo: true})
	if got := d.one(t, `SELECT count(*)::text FROM channel_outbox WHERE workspace_id=$1 AND thread_item_id IS NOT NULL`); got != "0" {
		t.Fatalf("notices = %s, want none", got)
	}
}
