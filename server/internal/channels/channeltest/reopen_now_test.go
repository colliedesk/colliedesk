package channeltest_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/bundle"
	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// gatheringLoop collects a customer's messages before it opens a request, as a chat channel set up to
// replace a desk does: one to one quiet 10 s then a question, groups quiet 300 s.
func gatheringLoop() *channeltest.Loopback {
	loop := channeltest.NewLoopback("loopback", channels.Async)
	loop.Defaults.Commands = map[string]channels.Command{
		"bug":     {Match: channels.MatchWords, Phrases: []string{"report bug"}},
		"confirm": {Action: channels.ActionConfirm, Match: channels.MatchExact, Phrases: []string{"ok"}},
		"reopen":  {Action: channels.ActionReopen, Match: channels.MatchWords, Phrases: []string{"not fixed"}},
	}
	loop.Defaults.Notices.AskConfirm = "Is that all? Send OK."
	loop.Defaults.Notices.ReopenNotFound = "No request {ref} here."
	loop.Defaults.Notices.ReopenLegacy = "{old_ref} was in the old system: this is {ref} now."
	loop.Defaults.Notices.Reopened = "{ref} is open again."
	loop.Defaults.Notices.ReopenNoted = "Added to {ref}."
	loop.Defaults.Scopes = map[string]channels.Scope{
		"user":  {Mode: channels.ModeCommands, Unmatched: channels.UnmatchedNotice, Gather: &channels.Gather{QuietSeconds: 10, MaxSeconds: 1800, ConfirmSeconds: 60}},
		"group": {Mode: channels.ModeCommands, Gather: &channels.Gather{QuietSeconds: 300, MaxSeconds: 1200}},
	}
	loop.SharedScopes = []string{"group"}
	return loop
}

func importInto(t *testing.T, d loopDesk, b bundle.Bundle, files map[string][]byte) {
	t.Helper()
	var buffer bytes.Buffer
	w := zip.NewWriter(&buffer)
	manifest, _ := json.Marshal(b)
	f, _ := w.Create("bundle.json")
	_, _ = f.Write(manifest)
	for name, data := range files {
		f, _ := w.Create(name)
		_, _ = f.Write(data)
	}
	_ = w.Close()
	archive, err := bundle.Read(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
	if err != nil {
		t.Fatal(err)
	}
	disk, err := attachments.NewLocalDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if report, err := bundle.Run(context.Background(), d.pool, archive, bundle.Options{WorkspaceSlug: d.one(t, `SELECT slug FROM workspaces WHERE id=$1`), Storage: disk}); err != nil || report.Status != bundle.StatusOK {
		t.Fatalf("import = %+v %v", report, err)
	}
}

// A reopen command is answered as it comes, one to one and in a group, whatever the scope collects: it
// is no request being written, asks nothing, and two of them are two. A code the workspace has (an
// imported conversation of a year before) is never taken for the old system's.
func TestAReopenCommandIsAnsweredAsItComes(t *testing.T) {
	loop := gatheringLoop()
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
	file := []byte("\x89PNG\r\n\x1a\nphoto")
	sum := sha256.Sum256(file)
	identity := func(value string) []bundle.Identity {
		return []bundle.Identity{{ChannelID: d.channel.ID, Kind: channeltest.LoopbackIdentity, Value: value}}
	}
	importInto(t, d, bundle.Bundle{Format: bundle.Format, Source: "desk",
		Contacts: []bundle.Contact{{ID: "u1", Name: "One", Identities: identity("u1")}, {ID: "u3", Name: "Three", Identities: identity("u3")}},
		Conversations: []bundle.Conversation{
			{ID: "t1", Contact: "u1", Subject: "Printer", Status: "resolved", Priority: "normal", ChannelID: d.channel.ID, NumberSeries: "ref", Number: 120, NumberYear: year,
				Place: &bundle.Place{Kind: "direct", ExternalID: "user:u1"}, CreatedAt: at, UpdatedAt: at,
				Messages: []bundle.Message{{ID: "m1", Kind: "incoming", Body: "the printer jams", CreatedAt: at}}},
			{ID: "t2", Contact: "u3", Subject: "Scanner", Status: "resolved", Priority: "normal", ChannelID: d.channel.ID, NumberSeries: "ref", Number: 121, NumberYear: year,
				Place: &bundle.Place{Kind: "group", ExternalID: "group:g1"}, CreatedAt: at, UpdatedAt: at,
				Messages: []bundle.Message{{ID: "m2", Kind: "incoming", Body: "the scanner is offline", CreatedAt: at},
					{ID: "m3", Kind: "outgoing", Body: "on it", AuthorName: "Staff One", CreatedAt: at.Add(time.Minute),
						Sender: &bundle.Sender{Kind: channeltest.LoopbackIdentity, Value: "s1", Name: "Staff One"}}}},
			{ID: "t3", Contact: "u1", Subject: "Old", Status: "open", Priority: "normal", ChannelID: d.channel.ID, NumberSeries: "ref", Number: 99, NumberYear: year - 1,
				Place: &bundle.Place{Kind: "direct", ExternalID: "user:u1"}, CreatedAt: at.Add(-time.Hour), UpdatedAt: at.Add(-time.Hour),
				Messages: []bundle.Message{{ID: "m4", Kind: "incoming", Body: "last year's", CreatedAt: at.Add(-time.Hour),
					Files: []bundle.File{{ID: "f4", Name: "photo.png", ContentType: "image/png", Path: "files/photo.png", Size: int64(len(file)), SHA256: hex.EncodeToString(sum[:])}}}}},
		},
		Counts: bundle.Counts{Contacts: 2, Identities: 2, Conversations: 3, Incoming: 3, Outgoing: 1, Files: 1, FileBytes: int64(len(file))},
	}, map[string][]byte{"files/photo.png": file})
	code := func(y, n int) string { return fmt.Sprintf("REF-%d-%04d", y, n) }
	landed := func(id string) string {
		t.Helper()
		return d.one(t, `SELECT COALESCE((SELECT c.display_ref||':'||c.status FROM messages m JOIN conversations c ON c.id=m.conversation_id
			WHERE m.workspace_id=$1 AND m.external_id='loopback:'||$2),'-')`, id)
	}
	// One to one and in a group: at once, nothing asked, nothing waiting.
	d.send(t, channeltest.LoopbackEvent{ID: "r-1", From: "u1", Text: "not fixed " + code(year, 120) + " still jams"})
	d.send(t, channeltest.LoopbackEvent{ID: "r-2", From: "u4", Group: "g1", Text: "not fixed " + code(year, 121)})
	if got := landed("r-1") + "," + landed("r-2") + "," + d.notices(t, channels.NoticeAskConfirm); got != code(year, 120)+":open,"+code(year, 121)+":open,0" {
		t.Fatalf("reopened at once = %s", got)
	}
	// Two in a row: each its own. The group's code from one to one is not found; last year's imported
	// conversation is found from its own chat, not taken for the old system's.
	d.send(t, channeltest.LoopbackEvent{ID: "r-3", From: "u1", Text: "not fixed " + code(year, 121)})
	d.send(t, channeltest.LoopbackEvent{ID: "r-4", From: "u1", Text: "not fixed " + code(year-1, 99)})
	if got := landed("r-3") + "," + landed("r-4") + "," + d.notices(t, channels.NoticeReopenNotFound) + "," + d.one(t, `SELECT count(*)::text FROM conversations WHERE workspace_id=$1`); got !=
		"-,"+code(year-1, 99)+":open,1,3" {
		t.Fatalf("two in a row = %s", got)
	}
	// Someone else typing it: not found, and no new conversation for the old system.
	d.send(t, channeltest.LoopbackEvent{ID: "r-5", From: "u5", Text: "not fixed " + code(year-1, 99)})
	if got := landed("r-5") + "," + d.notices(t, channels.NoticeReopenNotFound) + "," + d.one(t, `SELECT count(*)::text FROM conversations WHERE workspace_id=$1`); got != "-,2,3" {
		t.Fatalf("a code of the workspace from elsewhere = %s", got)
	}
	// Without a code it is no reopen of anything: collected as before, nothing opened at once.
	d.send(t, channeltest.LoopbackEvent{ID: "r-6", From: "u6", Text: "not fixed"})
	if got := landed("r-6") + "," + d.one(t, `SELECT count(*)::text FROM conversations WHERE workspace_id=$1`) + "," +
		d.one(t, `SELECT state FROM channel_inbound_events WHERE workspace_id=$1 AND external_event_id='r-6'`); got != "-,3,pending" {
		t.Fatalf("a reopen without a code = %s", got)
	}
	// The same code in another series with the same format is not this channel's: still the old system's.
	oldNext, oldStart := int64(1), int64(1)
	if _, err := d.store.SetNumberSeries(ctx, d.workspaceID, "other", store.NumberSeriesInput{Format: ptr("REF-{yyyy}-{n:04}"), Next: &oldNext, Start: &oldStart, StartYear: &startYear}); err != nil {
		t.Fatal(err)
	}
	importInto(t, d, bundle.Bundle{Format: bundle.Format, Source: "other", Contacts: []bundle.Contact{{ID: "x", Name: "Other"}},
		Conversations: []bundle.Conversation{{ID: "o1", Contact: "x", Subject: "Elsewhere", Status: "open", Priority: "normal", NumberSeries: "other", Number: 98, NumberYear: year - 1,
			CreatedAt: at, UpdatedAt: at, Messages: []bundle.Message{{ID: "o1m", Kind: "incoming", Body: "elsewhere", CreatedAt: at}}}},
		Counts: bundle.Counts{Contacts: 1, Conversations: 1, Incoming: 1}}, nil)
	d.send(t, channeltest.LoopbackEvent{ID: "r-7", From: "u1", Text: "not fixed " + code(year-1, 98)})
	if got := d.one(t, `SELECT count(*)::text FROM conversations c JOIN conversation_tags ct ON ct.conversation_id=c.id JOIN tags g ON g.id=ct.tag_id WHERE c.workspace_id=$1 AND g.name='legacy-ref'`); got != "1" {
		t.Fatalf("another series' code = %s legacy conversations, want 1", got)
	}
	// The chat names the files of what was said, and a team member's group message is the team's.
	t3 := d.one(t, `SELECT id::text FROM conversations WHERE workspace_id=$1 AND number=99`)
	page, err := d.store.ConversationChat(ctx, d.workspaceID, t3, "", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, item := range page.Items {
		for _, f := range item.Files {
			files = append(files, item.Body+":"+f.Name+":"+f.Status)
		}
	}
	if fmt.Sprint(files) != "[last year's:photo.png:stored]" {
		t.Fatalf("chat files = %v", files)
	}
	if got := d.one(t, `SELECT i.direction||':'||i.role||':'||COALESCE(i.sender_value,'')||':'||(SELECT s.role FROM channel_senders s WHERE s.workspace_id=i.workspace_id AND s.sender_value='s1')
		FROM channel_thread_items i WHERE i.workspace_id=$1 AND i.external_id='bundle:desk:m3'`); got != "in:staff:s1:staff" {
		t.Fatalf("the team member's group message = %s", got)
	}
}
