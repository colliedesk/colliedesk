package bundle_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/bundle"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type desk struct {
	pool      *pgxpool.Pool
	db        *database.DB
	storage   attachments.Storage
	slug      string
	workspace string
	channel   string
}

func newDesk(t *testing.T) desk {
	t.Helper()
	ctx := context.Background()
	pool := databasetest.Pool(t)
	db := database.New(pool)
	storage, err := attachments.NewLocalDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	workspace, _, err := auth.NewWithDB(db, "https://app.colliedesk.example").CreateWorkspace(ctx, "Bundle Desk", "admin@bundle.example.com")
	if err != nil {
		t.Fatal(err)
	}
	d := desk{pool: pool, db: db, storage: storage, slug: workspace.Slug, workspace: workspace.ID}
	d.channel = d.one(t, `INSERT INTO channels(workspace_id,type,name,is_default) VALUES($1,'zalo','Chat',true) RETURNING id::text`, workspace.ID)
	d.one(t, `INSERT INTO agents(workspace_id,email,role) VALUES($1,'ann@bundle.example.com','agent') RETURNING id::text`, workspace.ID)
	d.one(t, `INSERT INTO conversation_number_series(workspace_id,series,format,next) VALUES($1,'ref','REF-{yyyy}-{n:04}',170) RETURNING series`, workspace.ID)
	d.one(t, `INSERT INTO conversation_number_formats(workspace_id,series,format) VALUES($1,'ref','REF-{yyyy}-{n:04}') RETURNING series`, workspace.ID)
	// A customer the workspace already knows on the channel.
	known := d.one(t, `INSERT INTO contacts(workspace_id,name) VALUES($1,'Known') RETURNING id::text`, workspace.ID)
	d.one(t, `INSERT INTO contact_identities(workspace_id,contact_id,channel_id,kind,value) VALUES($1,$2,$3,'zalo_user','100') RETURNING id::text`, workspace.ID, known, d.channel)
	return d
}

func (d desk) one(t *testing.T, sql string, args ...any) string {
	t.Helper()
	var out string
	if err := d.db.Privileged(context.Background(), database.PrivilegeMigrate, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), sql, args...).Scan(&out)
	}); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return out
}

var t0 = time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)

func sample(channel string) (bundle.Bundle, map[string][]byte) {
	file := []byte("\x89PNG\r\n\x1a\nscreen")
	sum := sha256.Sum256(file)
	b := bundle.Bundle{Format: bundle.Format, Source: "sample",
		Contacts: []bundle.Contact{
			{ID: "u100", Name: "Known again", Identities: []bundle.Identity{{ChannelID: channel, Kind: "zalo_user", Value: "100"}}},
			{ID: "u200", Name: "New customer", Identities: []bundle.Identity{{ChannelID: channel, Kind: "zalo_user", Value: "200"}}},
		},
		Conversations: []bundle.Conversation{
			{ID: "t1", Contact: "u100", Subject: "Printer jams", Status: "resolved", Priority: "urgent", ChannelID: channel, NumberSeries: "ref", Number: 123, NumberYear: 2026,
				Tags: []string{"bug"}, ThreadKeys: []string{"user:100"}, Place: &bundle.Place{Kind: "direct", ExternalID: "user:100"}, SLAApplies: true, CreatedAt: t0, UpdatedAt: t0.Add(48 * time.Hour),
				StatusChanges: []bundle.StatusChange{{From: "open", To: "resolved", At: t0.Add(47 * time.Hour)}},
				Messages: []bundle.Message{
					{ID: "m1", Kind: "incoming", Body: "The printer jams", CreatedAt: t0, Files: []bundle.File{{ID: "f1", Name: "screen.png", ContentType: "image/png",
						Path: "files/screen.png", Size: int64(len(file)), SHA256: hex.EncodeToString(sum[:])}}},
					{ID: "m2", Kind: "outgoing", Body: "Fixed", AuthorEmail: "ann@bundle.example.com", CreatedAt: t0.Add(time.Hour)},
					{ID: "m3", Kind: "note", Body: "Checked the tray", AuthorName: "Ann", CreatedAt: t0.Add(2 * time.Hour)},
				}},
			{ID: "t2", Contact: "u200", Subject: "Scanner", Status: "open", Priority: "normal", ChannelID: channel, NumberSeries: "ref", Number: 124, NumberYear: 2026,
				AssigneeEmail: "ANN@bundle.example.com", ThreadKeys: []string{"user:200"}, Place: &bundle.Place{Kind: "direct", ExternalID: "user:200"}, CreatedAt: t0.Add(3 * time.Hour), UpdatedAt: t0.Add(3 * time.Hour),
				Messages: []bundle.Message{{ID: "m4", Kind: "incoming", Body: "The scanner is offline", CreatedAt: t0.Add(3 * time.Hour)}}},
		},
		Counts: bundle.Counts{Contacts: 2, Identities: 2, Conversations: 2, Incoming: 2, Outgoing: 1, Notes: 1, StatusChanges: 1, ThreadKeys: 2, Files: 1, FileBytes: int64(len(file))},
	}
	return b, map[string][]byte{"files/screen.png": file}
}

func pack(t *testing.T, b bundle.Bundle, files map[string][]byte, extra ...string) bundle.Archive {
	t.Helper()
	var buffer bytes.Buffer
	w := zip.NewWriter(&buffer)
	manifest, _ := json.Marshal(b)
	for name, data := range map[string][]byte{"bundle.json": manifest} {
		f, _ := w.Create(name)
		_, _ = f.Write(data)
	}
	for name, data := range files {
		f, _ := w.Create(name)
		_, _ = f.Write(data)
	}
	for _, name := range extra {
		f, _ := w.Create(name)
		_, _ = f.Write([]byte("x"))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	archive, err := bundle.Read(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return archive
}

// A bundle goes in as it was: original times, codes, statuses, files; its customers matched where the
// workspace knows them; nothing queued to go out, no notice, no AI; reconciled OK.
func TestABundleImportsAsItWasAndReconciles(t *testing.T) {
	d := newDesk(t)
	ctx := context.Background()
	// The known customer's chat is there already: the import writes in it, the undo leaves it.
	d.one(t, `INSERT INTO channel_threads(workspace_id,channel_id,kind,external_id,opens_conversations) VALUES($1,$2,'direct','user:100',true) RETURNING id::text`, d.workspace, d.channel)
	b, files := sample(d.channel)
	report, err := bundle.Run(ctx, d.pool, pack(t, b, files), bundle.Options{WorkspaceSlug: d.slug, Storage: d.storage})
	if err != nil || report.Status != bundle.StatusOK {
		t.Fatalf("import = %+v %v", report, err)
	}
	if got := d.one(t, `SELECT string_agg(display_ref||':'||status||':'||priority||':'||to_char(created_at AT TIME ZONE 'UTC','YYYY-MM-DD HH24:MI')||':'||COALESCE(sla_applies::text,''),'|' ORDER BY number) FROM conversations WHERE workspace_id=$1 AND channel='zalo'`, d.workspace); got !=
		"REF-2026-0123:resolved:urgent:2026-03-02 09:00:true|REF-2026-0124:open:normal:2026-03-02 12:00:false" {
		t.Fatalf("conversations = %s", got)
	}
	if got := d.one(t, `SELECT count(DISTINCT contact_id)::text FROM conversations WHERE workspace_id=$1 AND channel='zalo'`, d.workspace) + "," +
		d.one(t, `SELECT (SELECT c.name FROM conversations v JOIN contacts c ON c.id=v.contact_id WHERE v.workspace_id=$1 AND v.number=123)`, d.workspace); got != "2,Known" {
		t.Fatalf("contacts = %s", got)
	}
	if got := d.one(t, `SELECT count(*)::text FROM channel_outbox WHERE workspace_id=$1`, d.workspace) + "," +
		d.one(t, `SELECT count(*)::text FROM messages WHERE workspace_id=$1 AND notice_kind IS NOT NULL`, d.workspace) + "," +
		d.one(t, `SELECT count(*)::text FROM ai_tasks WHERE workspace_id=$1`, d.workspace); got != "0,0,0" {
		t.Fatalf("outbox,notices,ai = %s", got)
	}
	if got := d.one(t, `SELECT a.filename||':'||a.size FROM attachments a WHERE a.workspace_id=$1`, d.workspace); got != "screen.png:14" {
		t.Fatalf("file = %s", got)
	}
	if got := d.one(t, `SELECT COALESCE(a.email,'')||':'||m.kind FROM messages m LEFT JOIN agents a ON a.id=m.author_agent_id WHERE m.workspace_id=$1 AND m.body='Fixed'`, d.workspace); got != "ann@bundle.example.com:outgoing" {
		t.Fatalf("reply author = %s", got)
	}
	if got := d.one(t, `SELECT next::text FROM conversation_number_series WHERE workspace_id=$1 AND series='ref'`, d.workspace); got != "170" {
		t.Fatalf("next = %s: numbers below it must not move it", got)
	}
	// Again: refused, nothing doubled.
	if _, err := bundle.Run(ctx, d.pool, pack(t, b, files), bundle.Options{WorkspaceSlug: d.slug, Storage: d.storage}); err == nil || !strings.Contains(err.Error(), "already imported") {
		t.Fatalf("again = %v", err)
	}
	// Undo removes the import, keeping what the team wrote to since.
	t2 := d.one(t, `SELECT id::text FROM conversations WHERE workspace_id=$1 AND number=124`, d.workspace)
	d.one(t, `INSERT INTO messages(workspace_id,conversation_id,kind,body) VALUES($1,$2,'note','Written after') RETURNING id::text`, d.workspace, t2)
	result, err := freshdesk.Undo(ctx, d.pool, d.storage, freshdesk.UndoOptions{WorkspaceSlug: d.slug, Source: "bundle:sample"})
	if err != nil || result.Conversations != 1 || strings.Join(result.KeptConversationTickets, ",") != "t2" {
		t.Fatalf("undo = %+v %v", result, err)
	}
	if got := d.one(t, `SELECT count(*)::text FROM contact_identities WHERE workspace_id=$1`, d.workspace); got != "2" {
		t.Fatalf("identities after undo = %s (the known one and the kept conversation's)", got)
	}
	if got := d.one(t, `SELECT COALESCE(string_agg(t.external_id||':'||(SELECT count(*) FROM channel_thread_items i WHERE i.workspace_id=t.workspace_id AND i.thread_id=t.id),',' ORDER BY t.external_id),'')
		FROM channel_threads t WHERE t.workspace_id=$1`, d.workspace); got != "user:100:0,user:200:1" {
		t.Fatalf("chats after undo = %s (the one there before, emptied; the kept conversation's, with its message)", got)
	}
	if got := d.one(t, `SELECT COALESCE(string_agg(sender_value,',' ORDER BY sender_value),'') FROM channel_senders WHERE workspace_id=$1`, d.workspace); got != "200" {
		t.Fatalf("senders after undo = %s (the kept conversation's customer)", got)
	}
}

// A dry run reconciles everything and writes nothing.
func TestADryRunWritesNothing(t *testing.T) {
	d := newDesk(t)
	b, files := sample(d.channel)
	report, err := bundle.Run(context.Background(), d.pool, pack(t, b, files), bundle.Options{WorkspaceSlug: d.slug, Storage: d.storage, DryRun: true})
	if err != nil || report.Status != bundle.StatusOK || !report.DryRun {
		t.Fatalf("dry run = %+v %v", report, err)
	}
	if got := d.one(t, `SELECT (SELECT count(*) FROM conversations WHERE workspace_id=$1)||','||(SELECT count(*) FROM import_items WHERE workspace_id=$1)||','||(SELECT count(*) FROM contacts WHERE workspace_id=$1)`, d.workspace); got != "0,0,1" {
		t.Fatalf("after a dry run = %s", got)
	}
}

// One difference with the source's counts fails the import whole: nothing stays, no file either.
func TestADifferenceFailsTheImportWhole(t *testing.T) {
	d := newDesk(t)
	b, files := sample(d.channel)
	b.Counts.Incoming = 3
	report, err := bundle.Run(context.Background(), d.pool, pack(t, b, files), bundle.Options{WorkspaceSlug: d.slug, Storage: d.storage})
	if err != nil || report.Status != bundle.StatusFailed || !strings.Contains(strings.Join(report.Problems, "|"), "incoming messages: the source holds 3") {
		t.Fatalf("report = %+v %v", report, err)
	}
	if got := d.one(t, `SELECT (SELECT count(*) FROM conversations WHERE workspace_id=$1)||','||(SELECT count(*) FROM attachments WHERE workspace_id=$1)`, d.workspace); got != "0,0" {
		t.Fatalf("after a failure = %s", got)
	}
	if !strings.Contains(report.Markdown(), "Nothing was kept") {
		t.Fatalf("markdown = %s", report.Markdown())
	}
}

// What the bundle names that the workspace lacks stops it before anything is written, naming each.
func TestAMissingChannelSeriesOrTeammateStopsTheImport(t *testing.T) {
	d := newDesk(t)
	b, files := sample(d.channel)
	b.Conversations[0].NumberSeries = "other"
	b.Conversations[1].AssigneeEmail = "nobody@bundle.example.com"
	b.Contacts[1].Identities[0].ChannelID = "00000000-0000-4000-8000-000000000000"
	_, err := bundle.Run(context.Background(), d.pool, pack(t, b, files), bundle.Options{WorkspaceSlug: d.slug, Storage: d.storage})
	var missing *bundle.ErrTarget
	if !errors.As(err, &missing) || len(missing.Missing) != 3 {
		t.Fatalf("missing = %v", err)
	}
	if got := d.one(t, `SELECT count(*)::text FROM conversations WHERE workspace_id=$1`, d.workspace); got != "0" {
		t.Fatalf("written = %s", got)
	}
}

// An archive is read within its limits: no entry leaving it, no file not as the bundle says.
func TestABundleArchiveIsReadWithinItsLimits(t *testing.T) {
	b, files := sample("00000000-0000-4000-8000-000000000001")
	read := func(extra string, change func(map[string][]byte)) error {
		var buffer bytes.Buffer
		w := zip.NewWriter(&buffer)
		manifest, _ := json.Marshal(b)
		entries := map[string][]byte{"bundle.json": manifest}
		for name, data := range files {
			entries[name] = data
		}
		if change != nil {
			change(entries)
		}
		if extra != "" {
			entries[extra] = []byte("x")
		}
		for name, data := range entries {
			f, _ := w.Create(name)
			_, _ = io.Copy(f, bytes.NewReader(data))
		}
		_ = w.Close()
		_, err := bundle.Read(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
		return err
	}
	if err := read("", nil); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		extra  string
		change func(map[string][]byte)
		want   string
	}{
		{"../escape", nil, "leaves the archive"},
		{"other.txt", nil, "neither bundle.json nor under files/"},
		{"", func(e map[string][]byte) { e["files/screen.png"] = []byte("changed") }, "holds 7 bytes"},
		{"", func(e map[string][]byte) { delete(e, "bundle.json") }, "no bundle.json"},
	} {
		if err := read(c.extra, c.change); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%q: %v, want %q", c.extra, err, c.want)
		}
	}
}

// A failed import never removes a file the workspace already uses: the store keeps one file for the
// same bytes, and only what nothing references goes.
func TestAFailedImportKeepsAFileTheWorkspaceUses(t *testing.T) {
	d := newDesk(t)
	ctx := context.Background()
	b, files := sample(d.channel)
	if report, err := bundle.Run(ctx, d.pool, pack(t, b, files), bundle.Options{WorkspaceSlug: d.slug, Storage: d.storage}); err != nil || report.Status != bundle.StatusOK {
		t.Fatalf("first = %+v %v", report, err)
	}
	again, _ := sample(d.channel)
	again.Source = "again"
	for index := range again.Conversations {
		again.Conversations[index].ID += "-again"
		again.Conversations[index].Number, again.Conversations[index].NumberYear = 0, 0
		for n := range again.Conversations[index].Messages {
			again.Conversations[index].Messages[n].ID += "-again"
			for f := range again.Conversations[index].Messages[n].Files {
				again.Conversations[index].Messages[n].Files[f].ID += "-again"
			}
		}
	}
	again.Counts.Incoming = 3
	if report, err := bundle.Run(ctx, d.pool, pack(t, again, files), bundle.Options{WorkspaceSlug: d.slug, Storage: d.storage}); err != nil || report.Status != bundle.StatusFailed {
		t.Fatalf("again = %+v %v", report, err)
	}
	key := d.one(t, `SELECT storage_key FROM attachments WHERE workspace_id=$1`, d.workspace)
	if err := attachments.Exists(ctx, d.storage, key); err != nil {
		t.Fatalf("the first import's file after the failed one: %v", err)
	}
}

// A cleanup that runs while the import writes waits for it and keeps its files; one that ran before the
// import locked them makes the import fail whole, never point at a missing file.
func TestACleanupDuringTheImportNeverLosesItsFiles(t *testing.T) {
	d := newDesk(t)
	ctx := context.Background()
	b, files := sample(d.channel)
	discarded := make(chan error, 1)
	bundle.SetBeforeWrite(func(keys []string) {
		go func() { discarded <- store.DiscardObject(ctx, d.db, d.storage, d.workspace, keys[0]) }()
		select {
		case err := <-discarded:
			t.Errorf("the cleanup did not wait for the import: %v", err)
		case <-time.After(300 * time.Millisecond):
		}
	})
	t.Cleanup(func() { bundle.SetBeforeWrite(nil) })
	if report, err := bundle.Run(ctx, d.pool, pack(t, b, files), bundle.Options{WorkspaceSlug: d.slug, Storage: d.storage}); err != nil || report.Status != bundle.StatusOK {
		t.Fatalf("import = %+v %v", report, err)
	}
	select {
	case err := <-discarded:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no cleanup ran while the import wrote")
	}
	key := d.one(t, `SELECT storage_key FROM attachments WHERE workspace_id=$1`, d.workspace)
	if err := attachments.Exists(ctx, d.storage, key); err != nil {
		t.Fatalf("the imported file after the cleanup: %v", err)
	}
	bundle.SetBeforeWrite(nil)

	other := newDesk(t)
	ob, ofiles := sample(other.channel)
	bundle.SetBeforeVerify(func(keys []string) {
		if err := store.DiscardObject(ctx, other.db, other.storage, other.workspace, keys[0]); err != nil {
			t.Error(err)
		}
	})
	t.Cleanup(func() { bundle.SetBeforeVerify(nil) })
	if _, err := bundle.Run(ctx, other.pool, pack(t, ob, ofiles), bundle.Options{WorkspaceSlug: other.slug, Storage: other.storage}); !errors.Is(err, store.ErrObjectMissing) {
		t.Fatalf("a file removed before the lock = %v", err)
	}
	if got := other.one(t, `SELECT (SELECT count(*) FROM conversations WHERE workspace_id=$1)||','||(SELECT count(*) FROM attachments WHERE workspace_id=$1)`, other.workspace); got != "0,0" {
		t.Fatalf("written = %s", got)
	}
}

// The bundle's source names the report files: only letters, digits, dot, dash and underscore.
func TestABundleSourceCannotLeaveTheReportDirectory(t *testing.T) {
	b, files := sample("00000000-0000-4000-8000-000000000001")
	for _, source := range []string{"x../../../escaped", "a/b", "..", "-dash"} {
		b.Source = source
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
		if _, err := bundle.Read(bytes.NewReader(buffer.Bytes()), int64(buffer.Len())); err == nil || !strings.Contains(err.Error(), "source") {
			t.Fatalf("%q: %v", source, err)
		}
	}
	dir := t.TempDir()
	jsonPath, _, err := bundle.Report{Source: "bundle:../escaped"}.WriteFiles(dir)
	if err != nil || !strings.HasPrefix(jsonPath, dir+"/") || strings.Contains(strings.TrimPrefix(jsonPath, dir+"/"), "/") {
		t.Fatalf("report = %s %v", jsonPath, err)
	}
}

// The bundle's own origin is kept as it was; where it came from is under origin.import, a key the bundle
// cannot set.
func TestABundleOriginIsKeptAndTheImportTraceIsApart(t *testing.T) {
	d := newDesk(t)
	b, files := sample(d.channel)
	b.Conversations[0].Origin = json.RawMessage(`{"source":"support_desk","id":"S-1"}`)
	if report, err := bundle.Run(context.Background(), d.pool, pack(t, b, files), bundle.Options{WorkspaceSlug: d.slug, Storage: d.storage}); err != nil || report.Status != bundle.StatusOK {
		t.Fatalf("import = %+v %v", report, err)
	}
	if got := d.one(t, `SELECT (origin->>'source')||','||(origin->>'id')||','||(origin->'import'->>'source')||','||(origin->'import'->>'bundle')||','||(origin->'import'->>'id') FROM conversations WHERE workspace_id=$1 AND number=123`, d.workspace); got != "support_desk,S-1,bundle,sample,t1" {
		t.Fatalf("origin = %s", got)
	}
	// With the frame the API reads: the lists empty, where it came from named.
	if got := d.one(t, `SELECT (origin->>'system')||','||(origin->>'external_id')||','||(origin->'cc')::text||','||(origin->'forwarded_to')::text FROM conversations WHERE workspace_id=$1 AND number=123`, d.workspace); got != "bundle,t1,[],[]" {
		t.Fatalf("conversation frame = %s", got)
	}
	if got := d.one(t, `SELECT string_agg((origin->>'kind')||':'||(origin->'to')::text||':'||(origin->'bcc')::text,',' ORDER BY created_at) FROM messages WHERE workspace_id=$1 AND conversation_id=(SELECT id FROM conversations WHERE workspace_id=$1 AND number=123)`, d.workspace); got != "incoming:[]:[],reply:[]:[],private_note:[]:[]" {
		t.Fatalf("message frames = %s", got)
	}
	b.Conversations[0].Origin = json.RawMessage(`{"cc":"one@example.com"}`)
	var bad bytes.Buffer
	bw := zip.NewWriter(&bad)
	badManifest, _ := json.Marshal(b)
	bf, _ := bw.Create("bundle.json")
	_, _ = bf.Write(badManifest)
	for name, data := range files {
		f, _ := bw.Create(name)
		_, _ = f.Write(data)
	}
	_ = bw.Close()
	if _, err := bundle.Read(bytes.NewReader(bad.Bytes()), int64(bad.Len())); err == nil || !strings.Contains(err.Error(), "conversations.0.origin") {
		t.Fatalf("origin.cc a string = %v", err)
	}
	b.Conversations[0].Origin = json.RawMessage(`{"import":{"source":"elsewhere"}}`)
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
	if _, err := bundle.Read(bytes.NewReader(buffer.Bytes()), int64(buffer.Len())); err == nil || !strings.Contains(err.Error(), "conversations.0.origin.import") {
		t.Fatalf("origin.import = %v", err)
	}
}

// Two bundles may carry the same message ids: each is its own in the chats. A place names its kind's chat.
func TestBundlesOfTwoSourcesShareMessageIDsAndAPlaceNamesItsKind(t *testing.T) {
	d := newDesk(t)
	ctx := context.Background()
	b, files := sample(d.channel)
	if report, err := bundle.Run(ctx, d.pool, pack(t, b, files), bundle.Options{WorkspaceSlug: d.slug, Storage: d.storage}); err != nil || report.Status != bundle.StatusOK {
		t.Fatalf("first = %+v %v", report, err)
	}
	b.Source = "second"
	for index := range b.Conversations {
		b.Conversations[index].Number, b.Conversations[index].NumberYear = 0, 0
	}
	if report, err := bundle.Run(ctx, d.pool, pack(t, b, files), bundle.Options{WorkspaceSlug: d.slug, Storage: d.storage}); err != nil || report.Status != bundle.StatusOK {
		t.Fatalf("second with the same message ids = %+v %v", report, err)
	}
	for _, place := range []bundle.Place{{Kind: "direct", ExternalID: "group:g1"}, {Kind: "group", ExternalID: "user:100"}, {Kind: "direct", ExternalID: "100"}} {
		wrong, _ := sample(d.channel)
		wrong.Conversations[0].Place = &place
		var buffer bytes.Buffer
		w := zip.NewWriter(&buffer)
		manifest, _ := json.Marshal(wrong)
		f, _ := w.Create("bundle.json")
		_, _ = f.Write(manifest)
		for name, data := range files {
			f, _ := w.Create(name)
			_, _ = f.Write(data)
		}
		_ = w.Close()
		if _, err := bundle.Read(bytes.NewReader(buffer.Bytes()), int64(buffer.Len())); err == nil || !strings.Contains(err.Error(), "conversations.0.place") {
			t.Fatalf("place %+v: %v", place, err)
		}
	}
}
