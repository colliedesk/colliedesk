package channeltest_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/autoclose"
	"github.com/colliedesk/colliedesk/server/internal/bundle"
	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
	"github.com/colliedesk/colliedesk/server/internal/channels/outbox"
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
	if result, err := d.store.BulkUpdateConversationsTelling(ctx, d.workspaceID, []string{id, other}, store.BulkConversationUpdate{Status: &pending, ActorID: d.agentID}); err != nil || result.Told != 2 {
		t.Fatalf("bulk change = %+v %v, want 2 told", result, err)
	}
	// A bulk change tells after it commits: the server sends what it marked, once, however many run.
	if got := d.one(t, `SELECT count(*)::text FROM messages WHERE workspace_id=$1 AND notice_kind='status_pending'`); got != "1" {
		t.Fatalf("pending notices inside the bulk change = %s, want 1", got)
	}
	notices := autoclose.Notices{Store: d.store}
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := notices.RunOnce(ctx); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if taken, err := notices.RunOnce(ctx); err != nil || taken != 0 {
		t.Fatalf("a second round took %d (%v)", taken, err)
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
	// The conversation says who changed its status last, and which statuses would tell its customer.
	item, err := d.store.GetConversation(ctx, d.workspaceID, id)
	if err != nil {
		t.Fatal(err)
	}
	if item.LastStatusChange == nil || item.LastStatusChange.To != "pending" || item.LastStatusChange.By != "agent" || fmt.Sprint(item.StatusNotices) != "[open resolved closed]" {
		t.Fatalf("last change = %+v, notices = %v", item.LastStatusChange, item.StatusNotices)
	}
	// A team member with no name set is named by their email.
	email := d.one(t, `UPDATE agents SET display_name='' WHERE workspace_id=$1 AND id=$2 RETURNING email`, d.agentID)
	if named, err := d.store.GetConversation(ctx, d.workspaceID, id); err != nil || named.LastStatusChange == nil || named.LastStatusChange.Name == nil || *named.LastStatusChange.Name != email {
		t.Fatalf("named = %+v %v, want %s", named.LastStatusChange, err, email)
	}
	if none, err := d.store.GetConversation(ctx, d.workspaceID, started); err != nil || len(none.StatusNotices) != 0 || none.MayTellCustomer || !item.MayTellCustomer {
		t.Fatalf("a conversation of no chat would tell %v (%v)", none.StatusNotices, err)
	}
	// The list says it too, for each row: the inbox offers to tell the customers before a bulk change.
	mayTell := func() string {
		t.Helper()
		page, err := d.store.ListConversations(ctx, d.workspaceID, store.ConversationFilter{Page: 1, PageSize: 50})
		if err != nil {
			t.Fatal(err)
		}
		told := map[string]bool{}
		for _, row := range page.Items {
			told[row.ID] = row.MayTellCustomer
		}
		return fmt.Sprintf("chat=%v other=%v none=%v", told[id], told[other], told[started])
	}
	if got := mayTell(); got != "chat=true other=true none=false" {
		t.Fatalf("may tell = %s", got)
	}
	// A chat message moved into a conversation that came by no chat puts it in that chat.
	d.one(t, `INSERT INTO conversation_thread_items(workspace_id,conversation_id,item_id,reason)
		SELECT workspace_id,$2::uuid,item_id,'agent' FROM conversation_thread_items WHERE workspace_id=$1 AND conversation_id=$3 LIMIT 1 RETURNING 'ok'`, started, other)
	if got := mayTell(); got != "chat=true other=true none=true" {
		t.Fatalf("may tell after a chat message moved in = %s", got)
	}
	// The customer writing opens a resolved conversation again: theirs.
	if _, err := d.store.UpdateConversation(ctx, d.workspaceID, other, store.ConversationUpdate{Status: &resolved, ActorID: d.agentID, Quiet: true}); err != nil {
		t.Fatal(err)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "s-3", From: "u2", Text: "still offline"})
	if item, err := d.store.GetConversation(ctx, d.workspaceID, other); err != nil || item.LastStatusChange == nil || item.LastStatusChange.By != "customer" || item.LastStatusChange.To != "open" {
		t.Fatalf("after the customer wrote = %+v %v", item.LastStatusChange, err)
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
	if item, err := d.store.GetConversation(ctx, d.workspaceID, id); err != nil || item.LastStatusChange == nil || item.LastStatusChange.By != "auto_close" {
		t.Fatalf("closed by = %+v %v", item.LastStatusChange, err)
	}
	// Imported resolved, after the channel began closing by itself, and untouched since: closed, quietly.
	d.one(t, `UPDATE channels SET auto_close_since=$2 WHERE workspace_id=$1 AND id=$3 RETURNING 'ok'`, at.Add(-10*24*time.Hour), d.channel.ID)
	importInto(t, d, bundle.Bundle{Format: bundle.Format, Source: "old", Contacts: []bundle.Contact{{ID: "x", Name: "Old"}},
		Conversations: []bundle.Conversation{{ID: "o1", Contact: "x", Subject: "Long ago", Status: "resolved", Priority: "normal", ChannelID: d.channel.ID,
			CreatedAt: at.Add(-10 * 24 * time.Hour), UpdatedAt: at.Add(-9 * 24 * time.Hour),
			StatusChanges: []bundle.StatusChange{{From: "open", To: "resolved", At: at.Add(-9 * 24 * time.Hour)}},
			Messages:      []bundle.Message{{ID: "o1m", Kind: "incoming", Body: "old", CreatedAt: at.Add(-10 * 24 * time.Hour)}}}},
		Counts: bundle.Counts{Contacts: 1, Conversations: 1, Incoming: 1, StatusChanges: 1}}, nil)
	if _, err := worker.RunOnce(ctx, at.Add(48*time.Hour+time.Minute)); err != nil {
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

// Turning auto-close on closes only what is resolved after it: a conversation resolved before never closes
// by itself (the team closes it), one resolved after closes its hours later. Shortening the hours closes
// what they already passed; turning it off and on again begins again.
func TestTurningAutoCloseOnClosesOnlyWhatIsResolvedAfter(t *testing.T) {
	d := newLoopDesk(t, channeltest.NewLoopback("loopback", channels.Async))
	ctx := context.Background()
	resolved := "resolved"
	conversation := func(event string, ago time.Duration) string {
		t.Helper()
		d.send(t, channeltest.LoopbackEvent{ID: event, From: event, Text: "the printer jams"})
		id := d.one(t, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND external_id=$2`, "loopback:"+event)
		if _, err := d.store.UpdateConversation(ctx, d.workspaceID, id, store.ConversationUpdate{Status: &resolved, ActorID: d.agentID}); err != nil {
			t.Fatal(err)
		}
		d.one(t, `UPDATE conversation_status_changes SET changed_at=changed_at-$3::interval WHERE workspace_id=$1 AND conversation_id=$2 RETURNING 'ok'`, id, ago.String())
		d.one(t, `UPDATE messages SET created_at=created_at-$3::interval WHERE workspace_id=$1 AND conversation_id=$2 RETURNING 'ok'`, id, ago.String())
		return id
	}
	set := func(hours int) {
		t.Helper()
		settings := fmt.Sprintf(`{"auto_close_hours":%d,"notices":{"status_closed":"{ref}: closed."}}`, hours)
		if _, err := d.store.UpdateChannel(ctx, d.workspaceID, d.channel.ID, store.ChannelInput{Settings: []byte(settings)}, store.ChannelActor{AgentID: d.agentID}); err != nil {
			t.Fatal(err)
		}
	}
	statuses := func(at time.Time, ids ...string) string {
		t.Helper()
		if _, err := (autoclose.Worker{Store: d.store}).RunOnce(ctx, at); err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, id := range ids {
			got = append(got, d.one(t, `SELECT status FROM conversations WHERE workspace_id=$1 AND id=$2`, id))
		}
		return fmt.Sprint(got)
	}
	since := func() string {
		t.Helper()
		return d.one(t, `SELECT COALESCE(auto_close_since::text,'') FROM channels WHERE workspace_id=$1 AND id=$2`, d.channel.ID)
	}
	old := conversation("n-1", 3*24*time.Hour)
	set(48)
	if got := statuses(time.Now().Add(30*24*time.Hour), old); got != "[resolved]" {
		t.Fatalf("a conversation resolved before auto-close was turned on = %s", got)
	}
	// Turned on two days ago; one resolved 30 hours ago waits its 48, then 24 when shortened.
	d.one(t, `UPDATE channels SET auto_close_since=now()-interval '2 days' WHERE workspace_id=$1 AND id=$2 RETURNING 'ok'`, d.channel.ID)
	turnedOn := since()
	recent := conversation("n-2", 30*time.Hour)
	if got := statuses(time.Now(), old, recent); got != "[resolved resolved]" {
		t.Fatalf("before 48 hours = %s", got)
	}
	set(24)
	if since() != turnedOn {
		t.Fatalf("shortening the hours moved when it was turned on: %s, was %s", since(), turnedOn)
	}
	if got := statuses(time.Now(), old, recent); got != "[resolved closed]" {
		t.Fatalf("after shortening to 24 hours = %s", got)
	}
	// Off clears it; on again begins now.
	set(0)
	if since() != "" {
		t.Fatalf("turned off, still since %s", since())
	}
	set(24)
	if since() <= turnedOn {
		t.Fatalf("turned on again since %s, was %s", since(), turnedOn)
	}
	later := conversation("n-3", 0)
	if got := statuses(time.Now().Add(24*time.Hour+time.Minute), old, later); got != "[resolved closed]" {
		t.Fatalf("24 hours after a resolve once on again = %s", got)
	}
}

// A conversation the team resolved without telling the customer closes without telling them too, and
// one resolved again (or written to) after the worker listed it waits its new time.
func TestAQuietResolveClosesQuietlyAndAResolveAgainWaits(t *testing.T) {
	d := newLoopDesk(t, statusLoop())
	ctx := context.Background()
	d.send(t, channeltest.LoopbackEvent{ID: "q-1", From: "u1", Text: "the printer jams"})
	quiet := d.one(t, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND external_id='loopback:q-1'`)
	d.send(t, channeltest.LoopbackEvent{ID: "q-2", From: "u2", Text: "the scanner is offline"})
	again := d.one(t, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND external_id='loopback:q-2'`)
	resolved, open := "resolved", "open"
	if _, err := d.store.UpdateConversation(ctx, d.workspaceID, quiet, store.ConversationUpdate{Status: &resolved, ActorID: d.agentID, Quiet: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.store.UpdateConversation(ctx, d.workspaceID, again, store.ConversationUpdate{Status: &resolved, ActorID: d.agentID}); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(48*time.Hour + time.Minute)
	due, err := d.store.AutoCloseDue(ctx, at, 10)
	if err != nil || len(due) != 2 {
		t.Fatalf("due = %+v %v", due, err)
	}
	// Opened and resolved again after it was listed: its 48 hours start again.
	for _, to := range []*string{&open, &resolved} {
		if _, err := d.store.UpdateConversation(ctx, d.workspaceID, again, store.ConversationUpdate{Status: to, ActorID: d.agentID, Quiet: true}); err != nil {
			t.Fatal(err)
		}
	}
	d.one(t, `UPDATE conversation_status_changes SET changed_at=$3 WHERE workspace_id=$1 AND conversation_id=$2 AND to_status='resolved' AND changed_at=(SELECT max(changed_at) FROM conversation_status_changes WHERE workspace_id=$1 AND conversation_id=$2) RETURNING 'ok'`,
		again, at.Add(-time.Hour))
	for _, conversation := range due {
		if _, err := d.store.AutoClose(ctx, conversation, at); err != nil {
			t.Fatal(err)
		}
	}
	if got := d.one(t, `SELECT string_agg(status,',' ORDER BY id=$2 DESC) FROM conversations WHERE workspace_id=$1 AND id IN ($2,$3)`, quiet, again); got != "closed,resolved" {
		t.Fatalf("statuses = %s", got)
	}
	if got := d.one(t, `SELECT count(*)::text FROM messages WHERE workspace_id=$1 AND notice_kind='status_closed'`); got != "0" {
		t.Fatalf("a quiet resolve was told closed: %s", got)
	}
}

// A bulk change marks what it will tell; any later change of the status takes the mark away, so the
// customer hears the status the conversation has, once: resolved in bulk, opened by the customer and
// resolved by a person before the server sends is told resolved once, by the person's change.
func TestABulkNoticeNeverTellsAStatusTheConversationLeft(t *testing.T) {
	d := newLoopDesk(t, statusLoop())
	ctx := context.Background()
	d.send(t, channeltest.LoopbackEvent{ID: "b-1", From: "u1", Text: "the printer jams"})
	id := d.one(t, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND external_id='loopback:b-1'`)
	resolved := "resolved"
	if _, err := d.store.BulkUpdateConversations(ctx, d.workspaceID, []string{id}, store.BulkConversationUpdate{Status: &resolved, ActorID: d.agentID}); err != nil {
		t.Fatal(err)
	}
	if got := d.one(t, `SELECT COALESCE(status_notice_due,'') FROM conversations WHERE workspace_id=$1 AND id=$2`, id); got != "resolved" {
		t.Fatalf("mark = %q", got)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "b-2", From: "u1", Text: "still jams"})
	if _, err := d.store.UpdateConversation(ctx, d.workspaceID, id, store.ConversationUpdate{Status: &resolved, ActorID: d.agentID}); err != nil {
		t.Fatal(err)
	}
	if _, err := (autoclose.Notices{Store: d.store}).RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if got := d.one(t, `SELECT count(*)::text FROM messages WHERE workspace_id=$1 AND notice_kind='status_resolved'`); got != "1" {
		t.Fatalf("resolved notices = %s, want 1", got)
	}
	// A quiet bulk change marks nothing.
	pending := "pending"
	if _, err := d.store.BulkUpdateConversations(ctx, d.workspaceID, []string{id}, store.BulkConversationUpdate{Status: &pending, ActorID: d.agentID, Quiet: true}); err != nil {
		t.Fatal(err)
	}
	if got := d.one(t, `SELECT COALESCE(status_notice_due,'') FROM conversations WHERE workspace_id=$1 AND id=$2`, id); got != "" {
		t.Fatalf("a quiet bulk change marked %q", got)
	}
}

// A removed channel cannot send: a status change of one of its conversations tells nothing, says it would
// tell nothing (a token is not sent to ask a person), and lists no status that would.
func TestARemovedChannelTellsNothing(t *testing.T) {
	d := newLoopDesk(t, statusLoop())
	ctx := context.Background()
	d.send(t, channeltest.LoopbackEvent{ID: "r-1", From: "u1", Text: "the printer jams"})
	id := d.one(t, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND external_id='loopback:r-1'`)
	if err := d.store.RemoveChannel(ctx, d.workspaceID, d.channel.ID, store.ChannelActor{AgentID: d.agentID}); err != nil {
		t.Fatal(err)
	}
	if would, err := d.store.StatusNoticeWouldSend(ctx, d.workspaceID, []string{id}, "resolved"); err != nil || would {
		t.Fatalf("would send = %v %v", would, err)
	}
	resolved := "resolved"
	item, err := d.store.UpdateConversation(ctx, d.workspaceID, id, store.ConversationUpdate{Status: &resolved, ActorID: d.agentID})
	if err != nil {
		t.Fatal(err)
	}
	if got := d.one(t, `SELECT count(*)::text FROM messages WHERE workspace_id=$1 AND notice_kind LIKE 'status_%'`); got != "0" {
		t.Fatalf("a removed channel told %s", got)
	}
	if read, err := d.store.GetConversation(ctx, d.workspaceID, item.ID); err != nil || len(read.StatusNotices) != 0 || read.MayTellCustomer {
		t.Fatalf("status notices = %v %v", read.StatusNotices, err)
	}
}

// A conversation imported resolved, untouched here since, closes by itself as the desk it came from would
// have: its hours after its resolve there and the customer's last message, whenever auto-close was turned
// on, and quietly (that desk told the customer). Resolved three days before, it closes on the next round;
// resolved an hour before, 47 hours later.
func TestAnImportedResolvedConversationClosesAsItsDeskWould(t *testing.T) {
	d := newLoopDesk(t, statusLoop())
	ctx := context.Background()
	at := time.Now()
	imported := func(id string, resolved time.Time) bundle.Conversation {
		return bundle.Conversation{ID: id, Contact: "x", Subject: "Imported " + id, Status: "resolved", Priority: "normal", ChannelID: d.channel.ID,
			CreatedAt: resolved.Add(-time.Hour), UpdatedAt: resolved,
			StatusChanges: []bundle.StatusChange{{From: "open", To: "resolved", At: resolved}},
			Messages:      []bundle.Message{{ID: id + "m", Kind: "incoming", Body: "old", CreatedAt: resolved.Add(-time.Hour)}}}
	}
	importInto(t, d, bundle.Bundle{Format: bundle.Format, Source: "old", Contacts: []bundle.Contact{{ID: "x", Name: "Old"}},
		Conversations: []bundle.Conversation{imported("long", at.Add(-72*time.Hour)), imported("recent", at.Add(-time.Hour))},
		Counts:        bundle.Counts{Contacts: 1, Conversations: 2, Incoming: 2, StatusChanges: 2}}, nil)
	statuses := func(when time.Time) string {
		t.Helper()
		if _, err := (autoclose.Worker{Store: d.store}).RunOnce(ctx, when); err != nil {
			t.Fatal(err)
		}
		return d.one(t, `SELECT string_agg(subject||'='||status,',' ORDER BY subject) FROM conversations WHERE workspace_id=$1`)
	}
	if got := statuses(at.Add(time.Minute)); got != "Imported long=closed,Imported recent=resolved" {
		t.Fatalf("next round = %s", got)
	}
	if got := statuses(at.Add(46 * time.Hour)); got != "Imported long=closed,Imported recent=resolved" {
		t.Fatalf("46 hours on = %s", got)
	}
	if got := statuses(at.Add(47*time.Hour + time.Minute)); got != "Imported long=closed,Imported recent=closed" {
		t.Fatalf("47 hours on = %s", got)
	}
	if got := d.one(t, `SELECT count(*)::text FROM messages WHERE workspace_id=$1 AND notice_kind IS NOT NULL`); got != "0" {
		t.Fatalf("imported conversations told the customer: %s", got)
	}
}

// A channel turned off tells nothing either: no notice, no token asking a person, nothing a status
// would tell. One whose processing is paused keeps the notice and sends it once when it resumes.
func TestAChannelTurnedOffTellsNothingAndAPausedOneLater(t *testing.T) {
	loop := statusLoop()
	d := newLoopDesk(t, loop)
	ctx := context.Background()
	d.send(t, channeltest.LoopbackEvent{ID: "o-1", From: "u1", Text: "the printer jams"})
	id := d.one(t, `SELECT conversation_id::text FROM messages WHERE workspace_id=$1 AND external_id='loopback:o-1'`)
	set := func(input store.ChannelInput) {
		t.Helper()
		if _, err := d.store.UpdateChannel(ctx, d.workspaceID, d.channel.ID, input, store.ChannelActor{AgentID: d.agentID}); err != nil {
			t.Fatal(err)
		}
	}
	status := func(to string) {
		t.Helper()
		if _, err := d.store.UpdateConversation(ctx, d.workspaceID, id, store.ConversationUpdate{Status: &to, ActorID: d.agentID}); err != nil {
			t.Fatal(err)
		}
	}
	off, on, paused, resumed := false, true, true, false
	set(store.ChannelInput{Enabled: &off})
	if would, err := d.store.StatusNoticeWouldSend(ctx, d.workspaceID, []string{id}, "resolved"); err != nil || would {
		t.Fatalf("would send on a channel turned off = %v %v", would, err)
	}
	if item, err := d.store.GetConversation(ctx, d.workspaceID, id); err != nil || item.MayTellCustomer || len(item.StatusNotices) != 0 {
		t.Fatalf("a channel turned off would tell: %+v %v", item.StatusNotices, err)
	}
	status("resolved")
	if got := d.one(t, `SELECT count(*)::text FROM messages WHERE workspace_id=$1 AND notice_kind LIKE 'status_%'`); got != "0" {
		t.Fatalf("a channel turned off told %s", got)
	}
	set(store.ChannelInput{Enabled: &on, ProcessingPaused: &paused})
	status("pending")
	worker := outbox.New(d.store, outbox.Options{Now: func() time.Time { return time.Now().UTC().Add(time.Minute) }})
	sent := func() int {
		t.Helper()
		if _, err := worker.ProcessDue(ctx); err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, message := range loop.Endpoint.Sent() {
			if strings.Contains(message.Text, "we need more from you") {
				count++
			}
		}
		return count
	}
	if got := sent(); got != 0 {
		t.Fatalf("a paused channel sent %d", got)
	}
	set(store.ChannelInput{ProcessingPaused: &resumed})
	if first, again := sent(), sent(); first != 1 || again != 1 {
		t.Fatalf("after resuming sent %d, then %d", first, again)
	}
}
