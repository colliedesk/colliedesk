package database

import (
	"context"
	"strings"
	"testing"
)

// A database that already holds attachments gets the second storage counter by migration. The
// backfill must equal the real sum per workspace, count imported files and duplicate files, and leave
// the trial counter alone.
func TestFoundingStorageCounterMigrationBackfillsExistingData(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	for {
		var has bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='workspaces' AND column_name='stored_storage_bytes')`).Scan(&has); err != nil {
			t.Fatal(err)
		}
		if !has {
			break
		}
		if err := rollbackLatest(ctx, pool); err != nil {
			t.Fatalf("roll back to before the founding storage counter: %v", err)
		}
	}

	type fixture struct{ own, imported, duplicate int64 }
	seeds := map[string]fixture{"alpha": {own: 100, imported: 2000, duplicate: 0}, "beta": {own: 7, imported: 0, duplicate: 50}}
	ids := map[string]string{}
	for slug, files := range seeds {
		var workspace, conversation string
		if err := pool.QueryRow(ctx, `WITH w AS (INSERT INTO workspaces(slug,name) VALUES($1,$1) RETURNING id),
			c AS (INSERT INTO contacts(workspace_id,name,email) SELECT id,$1,$1||'@example.com' FROM w RETURNING id,workspace_id),
			v AS (INSERT INTO conversations(workspace_id,contact_id,subject) SELECT workspace_id,id,$1 FROM c RETURNING id,workspace_id)
			SELECT (SELECT id::text FROM w),(SELECT id::text FROM v)`, slug).Scan(&workspace, &conversation); err != nil {
			t.Fatalf("seed %s: %v", slug, err)
		}
		ids[slug] = workspace
		insertMessage := func(origin string) string {
			var id string
			if err := pool.QueryRow(ctx, `INSERT INTO messages(workspace_id,conversation_id,kind,body,origin) VALUES($1,$2,'incoming','x',NULLIF($3,'')::jsonb) RETURNING id::text`,
				workspace, conversation, origin).Scan(&id); err != nil {
				t.Fatal(err)
			}
			return id
		}
		insertFile := func(message string, size int64, sha string) {
			if _, err := pool.Exec(ctx, `INSERT INTO attachments(workspace_id,message_id,filename,content_type,size,sha256,storage_key)
				VALUES($1,$2,'f.bin','application/octet-stream',$3,$4,'k/'||gen_random_uuid())`, workspace, message, size, sha); err != nil {
				t.Fatal(err)
			}
		}
		own := insertMessage("")
		insertFile(own, files.own, strings.Repeat("a", 64))
		if files.imported > 0 {
			insertFile(insertMessage(`{"system":"freshdesk"}`), files.imported, strings.Repeat("b", 64))
		}
		if files.duplicate > 0 {
			insertFile(own, files.duplicate, strings.Repeat("a", 64))
			insertFile(own, files.duplicate, strings.Repeat("a", 64))
		}
	}

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate with data: %v", err)
	}
	// Written out by hand from the fixtures above, not summed by the migration's own query.
	want := map[string]struct{ stored, counted int64 }{
		"alpha": {stored: 2100, counted: 100},
		"beta":  {stored: 107, counted: 107},
	}
	for slug, expected := range want {
		var stored, counted int64
		if err := pool.QueryRow(ctx, `SELECT stored_storage_bytes, counted_storage_bytes FROM workspaces WHERE id=$1`, ids[slug]).Scan(&stored, &counted); err != nil {
			t.Fatal(err)
		}
		if stored != expected.stored || counted != expected.counted {
			t.Errorf("%s: stored=%d counted=%d, want stored=%d counted=%d", slug, stored, counted, expected.stored, expected.counted)
		}
	}
}
