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
				Messages: []bundle.Message{{ID: "m1", Kind: "incoming", Body: "the printer jams", CreatedAt: at},
					{ID: "m2", Kind: "outgoing", Body: "fixed", CreatedAt: at.Add(time.Hour)}, {ID: "m3", Kind: "note", Body: "checked", CreatedAt: at.Add(2 * time.Hour)}}},
			{ID: "t2", Contact: "u3", Subject: "Scanner", Status: "open", Priority: "normal", ChannelID: d.channel.ID, NumberSeries: "ref", Number: 121, NumberYear: year,
				Place: &bundle.Place{Kind: "group", ExternalID: "group:g1", Name: "Branch one"}, ThreadKeys: []string{"group:g1"}, CreatedAt: at, UpdatedAt: at,
				Messages: []bundle.Message{{ID: "m4", Kind: "incoming", Body: "the scanner is offline", CreatedAt: at}}},
		},
		Counts: bundle.Counts{Contacts: 2, Identities: 2, Conversations: 2, Incoming: 2, Outgoing: 1, Notes: 1, ThreadKeys: 2},
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
		"group:g1:group:customer:the scanner is offline|user:u1:direct:customer:the printer jams|user:u1:direct:staff:fixed" {
		t.Fatalf("chats = %s", got)
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
}
