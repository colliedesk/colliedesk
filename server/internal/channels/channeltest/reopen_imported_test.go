package channeltest_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/bundle"
	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
	"github.com/colliedesk/colliedesk/server/internal/channels/outbox"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// A conversation imported from a bundle with the place it was opened in is reopened by its code from that
// place, one to one and in a group, and from nowhere else; its chat shows its messages.
func TestAnImportedConversationIsReopenedByItsCodeFromItsPlace(t *testing.T) {
	loop := channeltest.NewLoopback("loopback", channels.Async)
	loop.Defaults.Commands = map[string]channels.Command{"reopen": {Action: channels.ActionReopen, Match: channels.MatchWords, Phrases: []string{"not fixed"}}}
	loop.Defaults.Notices.ReopenNotFound = "No request {ref} here."
	loop.Defaults.Notices.ReopenLegacy = "{old_ref} was in the old system: this is {ref} now."
	d := newLoopDesk(t, loop)
	ctx := context.Background()
	year := d.clock.Year()
	next, start, startYear := int64(170), int64(100), year
	if _, err := d.store.SetNumberSeries(ctx, d.workspaceID, "ref", store.NumberSeriesInput{Format: ptr("REF-{yyyy}-{n:04}"), Next: &next, Start: &start, StartYear: &startYear}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.store.UpdateChannel(ctx, d.workspaceID, d.channel.ID, store.ChannelInput{Settings: []byte(`{"number_series":"ref"}`)}, store.ChannelActor{AgentID: d.agentID}); err != nil {
		t.Fatal(err)
	}
	at := d.clock.Add(-48 * time.Hour)
	identity := func(value string) []bundle.Identity {
		return []bundle.Identity{{ChannelID: d.channel.ID, Kind: channeltest.LoopbackIdentity, Value: value}}
	}
	b := bundle.Bundle{Format: bundle.Format, Source: "chat",
		Contacts: []bundle.Contact{{ID: "u1", Name: "One", Identities: identity("u1")}, {ID: "u3", Name: "Three", Identities: identity("u3")}},
		Conversations: []bundle.Conversation{
			{ID: "t1", Contact: "u1", Subject: "Printer", Status: "resolved", Priority: "normal", ChannelID: d.channel.ID, NumberSeries: "REF", Number: 120, NumberYear: year,
				Place: &bundle.Place{Kind: "direct", ExternalID: "user:u1"}, ThreadKeys: []string{"user:u1"}, CreatedAt: at, UpdatedAt: at,
				Messages: []bundle.Message{{ID: "m0", Kind: "note", Body: "moved from the old desk", CreatedAt: at.Add(-time.Minute)},
					{ID: "m1", Kind: "incoming", Body: "the printer jams", CreatedAt: at},
					{ID: "m2", Kind: "outgoing", Body: "fixed", CreatedAt: at.Add(time.Hour)}, {ID: "m3", Kind: "note", Body: "checked", CreatedAt: at.Add(2 * time.Hour)}}},
			{ID: "t2", Contact: "u3", Subject: "Scanner", Status: "open", Priority: "normal", ChannelID: d.channel.ID, NumberSeries: "ref", Number: 121, NumberYear: year,
				Place: &bundle.Place{Kind: "group", ExternalID: "group:g1", Name: "Branch one"}, ThreadKeys: []string{"group:g1"}, CreatedAt: at, UpdatedAt: at,
				Messages: []bundle.Message{{ID: "m4", Kind: "incoming", Body: "the scanner is offline", CreatedAt: at},
					{ID: "m5", Kind: "incoming", Body: "here too", CreatedAt: at.Add(time.Minute),
						Sender: &bundle.Sender{Kind: channeltest.LoopbackIdentity, Value: "u9", Name: "Nine"}}}},
		},
		Counts: bundle.Counts{Contacts: 2, Identities: 2, Conversations: 2, Incoming: 3, Outgoing: 1, Notes: 2, ThreadKeys: 2},
	}
	var buffer bytes.Buffer
	w := zip.NewWriter(&buffer)
	manifest, _ := json.Marshal(b)
	f, _ := w.Create("bundle.json")
	_, _ = f.Write(manifest)
	_ = w.Close()
	archive, err := bundle.Read(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
	if err != nil {
		t.Fatal(err)
	}
	slug := d.one(t, `SELECT slug FROM workspaces WHERE id=$1`)
	if report, err := bundle.Run(ctx, d.pool, archive, bundle.Options{WorkspaceSlug: slug}); err != nil || report.Status != bundle.StatusOK {
		t.Fatalf("import = %+v %v", report, err)
	}
	// Their chats show what was said there; a note is no part of a chat.
	if got := d.one(t, `SELECT string_agg(t.external_id||':'||t.kind||':'||i.role||':'||i.body,'|' ORDER BY t.external_id,i.occurred_at) FROM channel_thread_items i
		JOIN channel_threads t ON t.workspace_id=i.workspace_id AND t.id=i.thread_id WHERE i.workspace_id=$1`); got !=
		"group:g1:group:customer:the scanner is offline|group:g1:group:customer:here too|user:u1:direct:customer:the printer jams|user:u1:direct:staff:fixed" {
		t.Fatalf("chats = %s", got)
	}
	// The people who wrote there are the channel's senders; the items are the conversations', named apart
	// from the channel's own message ids.
	if got := d.one(t, `SELECT string_agg(s.sender_value||':'||s.role||':'||t.external_id,',' ORDER BY s.sender_value) FROM channel_senders s
		JOIN channel_threads t ON t.workspace_id=s.workspace_id AND t.id=s.thread_id WHERE s.workspace_id=$1`); got != "u1:customer:user:u1,u3:customer:group:g1,u9:customer:group:g1" {
		t.Fatalf("senders = %s", got)
	}
	if got := d.one(t, `SELECT string_agg(i.external_id||'>'||c.display_ref,',' ORDER BY i.external_id) FROM channel_thread_items i
		JOIN conversation_thread_items l ON l.workspace_id=i.workspace_id AND l.item_id=i.id AND l.reason='import' JOIN conversations c ON c.id=l.conversation_id WHERE i.workspace_id=$1`); got !=
		fmt.Sprintf("bundle:chat:m1>REF-%[1]d-0120,bundle:chat:m2>REF-%[1]d-0120,bundle:chat:m4>REF-%[1]d-0121,bundle:chat:m5>REF-%[1]d-0121", year) {
		t.Fatalf("linked items = %s", got)
	}
	// A group member who is not the conversation's customer is named as they wrote it, and is no contact.
	if got := d.one(t, `SELECT COALESCE(sender_name,'')||':'||(contact_id IS NULL)::text FROM channel_thread_items WHERE workspace_id=$1 AND external_id='bundle:chat:m5'`); got != "Nine:true" {
		t.Fatalf("another member's item = %s", got)
	}
	// Their message, which names no author, is theirs on the timeline: the sender's name.
	if got := d.one(t, `SELECT COALESCE(author_name,'') FROM messages WHERE workspace_id=$1 AND body='here too'`); got != "Nine" {
		t.Fatalf("another member's message is by %q", got)
	}
	// A reply goes to the chat the conversation is in: the group's to the group, never one to one.
	for _, number := range []int{120, 121} {
		id := d.one(t, `SELECT id::text FROM conversations WHERE workspace_id=$1 AND number=$2`, number)
		if _, err := d.store.CreateMessage(ctx, d.workspaceID, id, store.Actor{AgentID: d.agentID}, "outgoing", fmt.Sprintf("answer %d", number)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := outbox.New(d.store, outbox.Options{Now: func() time.Time { return time.Now().UTC().Add(time.Minute) }}).ProcessDue(ctx); err != nil {
		t.Fatal(err)
	}
	places := map[string]string{}
	for _, sent := range loop.Endpoint.Sent() {
		places[sent.Text] = sent.Place
	}
	if places["answer 120"] != "user:u1" || places["answer 121"] != "group:g1" {
		t.Fatalf("replies went to %v", places)
	}
	code := func(n int) string { return fmt.Sprintf("REF-%d-%04d", year, n) }
	landed := func(id string) string {
		t.Helper()
		return d.one(t, `SELECT COALESCE((SELECT c.display_ref||':'||c.status FROM messages m JOIN conversations c ON c.id=m.conversation_id
			WHERE m.workspace_id=$1 AND m.external_id='loopback:'||$2),'-')`, id)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "r-1", From: "u1", Text: "not fixed " + code(120) + " still jams"})
	d.send(t, channeltest.LoopbackEvent{ID: "r-2", From: "u4", Group: "g1", Text: "not fixed " + code(121)})
	if got := landed("r-1") + "," + landed("r-2"); got != code(120)+":open,"+code(121)+":open" {
		t.Fatalf("reopened = %s", got)
	}
	// From another place: not found, nothing opened.
	d.send(t, channeltest.LoopbackEvent{ID: "r-3", From: "u5", Group: "g2", Text: "not fixed " + code(121)})
	d.send(t, channeltest.LoopbackEvent{ID: "r-4", From: "u3", Text: "not fixed " + code(121)})
	if got := landed("r-3") + "," + landed("r-4") + "," + d.one(t, `SELECT count(*)::text FROM conversations WHERE workspace_id=$1`); got != "-,-,2" {
		t.Fatalf("from elsewhere = %s", got)
	}
	// A conversation opened on the channel whose first message is a note (written earlier) is still the
	// place's: a note is never in a chat.
	d.send(t, channeltest.LoopbackEvent{ID: "n-1", From: "u7", Group: "g7", Text: "the label printer"})
	opened := d.one(t, `SELECT c.id::text FROM messages m JOIN conversations c ON c.id=m.conversation_id WHERE m.workspace_id=$1 AND m.external_id='loopback:n-1'`)
	if _, err := d.pool.Exec(ctx, `INSERT INTO messages(workspace_id,conversation_id,kind,body,created_at) SELECT workspace_id,id,'note','seen before',created_at-interval '1 minute' FROM conversations WHERE id=$1`, opened); err != nil {
		t.Fatal(err)
	}
	if _, err := d.pool.Exec(ctx, `UPDATE conversations SET status='resolved' WHERE id=$1`, opened); err != nil {
		t.Fatal(err)
	}
	ref := d.one(t, `SELECT display_ref FROM conversations WHERE workspace_id=$1 AND id=$2`, opened)
	d.send(t, channeltest.LoopbackEvent{ID: "n-2", From: "u8", Group: "g7", Text: "not fixed " + ref})
	d.send(t, channeltest.LoopbackEvent{ID: "n-3", From: "u7", Text: "not fixed " + ref})
	if got := landed("n-2") + "," + landed("n-3"); got != ref+":open,-" {
		t.Fatalf("a conversation whose first message is a note = %s", got)
	}
}
