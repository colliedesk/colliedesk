package channeltest_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/autoclose"
	"github.com/colliedesk/colliedesk/server/internal/bundle"
	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// statusLoop tells its customers when the team changes a conversation's status, and closes a resolved
// one after 48 hours, as the desk it replaces did.
func statusLoop() *channeltest.Loopback {
	loop := channeltest.NewLoopback("loopback", channels.Async)
	hours := 48
	loop.Defaults.Notices.StatusPending = "{ref}: we need more from you."
	loop.Defaults.Notices.StatusResolved = "{ref} ({subject}): fixed. Tell us if it is not."
	loop.Defaults.Notices.StatusClosed = "{ref}: closed."
	loop.Defaults.Notices.Reopened = "{ref} is open again."
	loop.Defaults.AutoCloseHours = &hours
	return loop
}

func statusNotices(t *testing.T, d loopDesk) string {
	t.Helper()
	return d.one(t, `SELECT COALESCE(string_agg(notice_kind||'='||body,'|' ORDER BY created_at,id),'') FROM messages WHERE workspace_id=$1 AND notice_kind LIKE 'status_%' OR (workspace_id=$1 AND notice_kind='reopened')`)
}

// The team changing a conversation's status tells its customer in the conversation's chat, quoting what
// they wrote first; each kind its own words, none when the text is empty or the change asks for quiet.
func TestAStatusChangeTellsTheCustomer(t *testing.T) {
	loop := statusLoop()
	d := newLoopDesk(t, loop)
	ctx := context.Background()
	d.send(t, channeltest.LoopbackEvent{ID: "s-1", From: "u1", Text: "the printer jams"})
	id := d.one(t, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND external_id='loopback:s-1'`)
	ref := d.one(t, `SELECT display_ref FROM conversations WHERE workspace_id=$1 AND id=$2`, id)
	subject := d.one(t, `SELECT subject FROM conversations WHERE workspace_id=$1 AND id=$2`, id)
	status := func(to string, quiet bool) {
		t.Helper()
		if _, err := d.store.UpdateConversation(ctx, d.workspaceID, id, store.ConversationUpdate{Status: &to, ActorID: d.agentID, Quiet: quiet}); err != nil {
			t.Fatal(err)
		}
	}
	status("pending", false)
	status("open", false)
	status("resolved", false)
	status("resolved", false) // no change, no word
	if got := statusNotices(t, d); got != "status_pending="+ref+": we need more from you.|reopened="+ref+" is open again.|status_resolved="+ref+" ("+subject+"): fixed. Tell us if it is not." {
		t.Fatalf("notices = %s", got)
	}
	// The notice is the channel's: it leaves the conversation resolved.
	if got := d.one(t, `SELECT status FROM conversations WHERE workspace_id=$1 AND id=$2`, id); got != "resolved" {
		t.Fatalf("status after the notice = %s", got)
	}
	// Delivered in the chat, quoting the customer's first message.
	if got := d.one(t, `SELECT count(*)::text FROM channel_outbox o JOIN messages m ON m.id=o.message_id WHERE m.workspace_id=$1 AND m.notice_kind='status_resolved'`); got != "1" {
		t.Fatalf("outbox = %s", got)
	}
	status("open", true)
	if got := d.one(t, `SELECT count(*)::text FROM messages WHERE workspace_id=$1 AND notice_kind='reopened'`); got != "1" {
		t.Fatalf("a quiet change told the customer: %s", got)
	}
	// Several at once: each its own notice.
	d.send(t, channeltest.LoopbackEvent{ID: "s-2", From: "u2", Text: "the scanner is offline"})
	other := d.one(t, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND external_id='loopback:s-2'`)
	pending := "pending"
	if _, err := d.store.BulkUpdateConversations(ctx, d.workspaceID, []string{id, other}, store.BulkConversationUpdate{Status: &pending, ActorID: d.agentID}); err != nil {
		t.Fatal(err)
	}
	if got := d.one(t, `SELECT count(*)::text FROM messages WHERE workspace_id=$1 AND notice_kind='status_pending'`); got != "3" {
		t.Fatalf("pending notices after the bulk change = %s, want 3", got)
	}
	// A conversation that came through no chat (an email, one started by the team) is told nothing.
	contact := d.one(t, `SELECT contact_id::text FROM conversations WHERE workspace_id=$1 AND id=$2`, id)
	started := d.one(t, `INSERT INTO conversations(workspace_id,contact_id,subject,channel,channel_id) VALUES($1,$2,'By hand','agent',$3) RETURNING id::text`, contact, d.channel.ID)
	resolved := "resolved"
	if _, err := d.store.UpdateConversation(ctx, d.workspaceID, started, store.ConversationUpdate{Status: &resolved, ActorID: d.agentID}); err != nil {
		t.Fatal(err)
	}
	if got := d.one(t, `SELECT count(*)::text FROM messages WHERE workspace_id=$1 AND conversation_id=$2`, started); got != "0" {
		t.Fatalf("a conversation of no chat was told: %s", got)
	}
	if would, err := d.store.StatusNoticeWouldSend(ctx, d.workspaceID, []string{started, other}, "closed"); err != nil || !would {
		t.Fatalf("would send = %v %v", would, err)
	}
	if would, err := d.store.StatusNoticeWouldSend(ctx, d.workspaceID, []string{started}, "closed"); err != nil || would {
		t.Fatalf("would send for no chat = %v %v", would, err)
	}
}

// A resolved conversation closes 48 hours after it was resolved, once, telling its customer; one imported
// resolved and untouched since closes quietly; a channel without auto-close keeps its own.
func TestAResolvedConversationClosesAfter48Hours(t *testing.T) {
	loop := statusLoop()
	d := newLoopDesk(t, loop)
	ctx := context.Background()
	d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "u1", Text: "the printer jams"})
	id := d.one(t, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND external_id='loopback:a-1'`)
	resolved := "resolved"
	if _, err := d.store.UpdateConversation(ctx, d.workspaceID, id, store.ConversationUpdate{Status: &resolved, ActorID: d.agentID}); err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	worker := autoclose.Worker{Store: d.store}
	if _, err := worker.RunOnce(ctx, at.Add(47*time.Hour+59*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got := d.one(t, `SELECT status FROM conversations WHERE workspace_id=$1 AND id=$2`, id); got != "resolved" {
		t.Fatalf("closed before 48 hours: %s", got)
	}
	// Two servers at once: one close, one notice.
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := worker.RunOnce(ctx, at.Add(48*time.Hour+time.Minute)); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if got := d.one(t, `SELECT status FROM conversations WHERE workspace_id=$1 AND id=$2`, id) + "," +
		d.one(t, `SELECT count(*)::text FROM messages WHERE workspace_id=$1 AND notice_kind='status_closed'`); got != "closed,1" {
		t.Fatalf("after 48 hours = %s", got)
	}
	// Imported resolved long ago, untouched since: closed, quietly.
	importInto(t, d, bundle.Bundle{Format: bundle.Format, Source: "old", Contacts: []bundle.Contact{{ID: "x", Name: "Old"}},
		Conversations: []bundle.Conversation{{ID: "o1", Contact: "x", Subject: "Long ago", Status: "resolved", Priority: "normal", ChannelID: d.channel.ID,
			CreatedAt: at.Add(-10 * 24 * time.Hour), UpdatedAt: at.Add(-9 * 24 * time.Hour),
			StatusChanges: []bundle.StatusChange{{From: "open", To: "resolved", At: at.Add(-9 * 24 * time.Hour)}},
			Messages:      []bundle.Message{{ID: "o1m", Kind: "incoming", Body: "old", CreatedAt: at.Add(-10 * 24 * time.Hour)}}}},
		Counts: bundle.Counts{Contacts: 1, Conversations: 1, Incoming: 1, StatusChanges: 1}}, nil)
	if _, err := worker.RunOnce(ctx, at); err != nil {
		t.Fatal(err)
	}
	if got := d.one(t, `SELECT c.status||','||(SELECT count(*) FROM messages m WHERE m.conversation_id=c.id AND m.notice_kind IS NOT NULL) FROM conversations c WHERE c.workspace_id=$1 AND c.subject='Long ago'`); got != "closed,0" {
		t.Fatalf("an imported resolved conversation = %s", got)
	}
	// Another workspace whose channel does not close by itself keeps its resolved conversation.
	plain := newLoopDesk(t, channeltest.NewLoopback("loopback", channels.Async))
	plain.send(t, channeltest.LoopbackEvent{ID: "p-1", From: "u1", Text: "hello"})
	kept := plain.one(t, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND external_id='loopback:p-1'`)
	if _, err := plain.store.UpdateConversation(ctx, plain.workspaceID, kept, store.ConversationUpdate{Status: &resolved, ActorID: plain.agentID}); err != nil {
		t.Fatal(err)
	}
	if _, err := (autoclose.Worker{Store: plain.store}).RunOnce(ctx, at.Add(30*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := plain.one(t, `SELECT status FROM conversations WHERE workspace_id=$1 AND id=$2`, kept); got != "resolved" {
		t.Fatalf("a channel without auto-close = %s", got)
	}
}
