package database

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"
)

// seedKB gives the workspace a collection, an article with a version, a translation, an image linked
// to the article, and help center settings, each named after the workspace.
func seedKB(t *testing.T, ctx context.Context, tx pgx.Tx, workspaceID, name string) {
	t.Helper()
	sha := fmt.Sprintf("%064x", len(name))
	_, err := tx.Exec(ctx, `WITH c AS (INSERT INTO kb_collections(workspace_id,name,slug) VALUES($1,$2,'docs') RETURNING id),
		a AS (INSERT INTO kb_articles(workspace_id,collection_id,number,title,slug,body_md) SELECT $1,id,1,$2,'a','text' FROM c RETURNING id,collection_id),
		v AS (INSERT INTO kb_article_versions(workspace_id,article_id,version,kind,title,slug,body_md,status,collection_id)
			SELECT $1,id,1,'create',$2,'a','text','draft',collection_id FROM a RETURNING 1),
		tr AS (INSERT INTO kb_article_translations(workspace_id,article_id,locale,title,body_md) SELECT $1,id,'de',$2,'text' FROM a RETURNING 1),
		i AS (INSERT INTO kb_media(workspace_id,storage_key,sha256,size,content_type) VALUES($1,$1::text||'/00/'||$3,$3,10,'image/png') RETURNING id),
		l AS (INSERT INTO kb_article_media(workspace_id,article_id,media_id,current) SELECT $1,a.id,i.id,true FROM a,i RETURNING 1)
		INSERT INTO help_centers(workspace_id,title) VALUES($1,$2)`, workspaceID, name, sha)
	if err != nil {
		t.Fatalf("seed knowledge base of %s: %v", name, err)
	}
}

var kbTables = []string{"kb_collections", "kb_articles", "kb_article_versions", "kb_article_translations", "kb_media", "kb_article_media", "help_centers"}

func TestKnowledgeBaseTablesAreScopedByRowLevelSecurity(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	ids := seedTwoWorkspaces(t, pool)
	db := New(pool)
	for workspace, name := range map[string]string{ids.a: "Alpha", ids.b: "Beta"} {
		if err := db.InWorkspace(ctx, workspace, func(tx pgx.Tx) error { seedKB(t, ctx, tx, workspace, name); return nil }); err != nil {
			t.Fatal(err)
		}
	}
	for _, workspace := range []string{ids.a, ids.b} {
		err := db.InWorkspace(ctx, workspace, func(tx pgx.Tx) error {
			for _, table := range kbTables {
				// Deliberately unfiltered: row-level security alone must scope it.
				rows, err := tx.Query(ctx, `SELECT DISTINCT workspace_id::text FROM `+table)
				if err != nil {
					return err
				}
				got, err := pgx.CollectRows(rows, pgx.RowTo[string])
				if err != nil {
					return err
				}
				if !slices.Equal(got, []string{workspace}) {
					t.Errorf("%s in workspace %s sees rows of %v", table, workspace, got)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// Writing into another workspace is refused, and a link cannot join two workspaces.
	err := db.InWorkspace(ctx, ids.a, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO kb_collections(workspace_id,name,slug) VALUES($1,'Smuggled','smuggled')`, ids.b)
		return err
	})
	if err == nil {
		t.Fatal("workspace alpha inserted a collection into workspace beta")
	}
	_, err = pool.Exec(ctx, `INSERT INTO kb_article_media(workspace_id,article_id,media_id)
		SELECT $1,a.id,i.id FROM kb_articles a, kb_media i WHERE a.workspace_id=$1 AND i.workspace_id=$2`, ids.a, ids.b)
	if err == nil {
		t.Fatal("an article of alpha was linked to media of beta")
	}
	single := singleConnection(t, pool)
	role := pgx.Identifier{policyRole(t, pool)}.Sanitize()
	tx, err := single.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `SET LOCAL ROLE `+role); err != nil {
		t.Fatal(err)
	}
	for _, table := range kbTables {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s without workspace context = %d (%v), want 0", table, count, err)
		}
	}
}

func TestKnowledgeBaseVersionsAreAppendOnlyForTheAppRole(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	ids := seedTwoWorkspaces(t, pool)
	db := New(pool)
	if err := db.InWorkspace(ctx, ids.a, func(tx pgx.Tx) error { seedKB(t, ctx, tx, ids.a, "Alpha"); return nil }); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{`UPDATE kb_article_versions SET title='rewritten'`, `DELETE FROM kb_article_versions`} {
		err := db.InWorkspace(ctx, ids.a, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, statement)
			return err
		})
		if err == nil {
			t.Errorf("%s ran as the app role", statement)
		}
	}
}

// Media count toward the plan with their original bytes and a video's poster; a thumbnail only takes
// disk, so it counts in stored_storage_bytes alone. Setting, changing and dropping a poster or a
// thumbnail moves the counters by the difference, and deleting every row brings them back to zero.
func TestKnowledgeBaseMediaCountTowardStorage(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	ids := seedTwoWorkspaces(t, pool)
	db := New(pool)
	exec := func(statement string) {
		t.Helper()
		if err := db.InWorkspace(ctx, ids.a, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, statement, ids.a)
			return err
		}); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	stored := func(step string, wantCounted, wantAll int64) {
		t.Helper()
		var counted, all int64
		if err := pool.QueryRow(ctx, `SELECT counted_storage_bytes,stored_storage_bytes FROM workspaces WHERE id=$1`, ids.a).Scan(&counted, &all); err != nil {
			t.Fatal(err)
		}
		if counted != wantCounted || all != wantAll {
			t.Fatalf("%s: counted=%d stored=%d, want %d and %d", step, counted, all, wantCounted, wantAll)
		}
	}
	exec(`INSERT INTO kb_media(workspace_id,storage_key,sha256,size,content_type,counted) VALUES
		($1,'k1',repeat('a',64),100,'image/png',true),($1,'k2',repeat('b',64),40,'image/png',false)`)
	stored("after insert", 100, 140)
	exec(`UPDATE kb_media SET thumb_storage_key='t1', thumb_size=7 WHERE storage_key IN ('k1','k2') AND workspace_id=$1`)
	stored("after thumbnails", 100, 154)
	exec(`INSERT INTO kb_media(workspace_id,kind,storage_key,sha256,size,content_type) VALUES ($1,'video','v1',repeat('c',64),1000,'video/mp4')`)
	stored("after video", 1100, 1154)
	exec(`UPDATE kb_media SET poster_storage_key='p1', poster_size=50, thumb_storage_key='pt1', thumb_size=5 WHERE storage_key='v1' AND workspace_id=$1`)
	stored("after poster", 1150, 1209)
	exec(`UPDATE kb_media SET poster_storage_key='p2', poster_size=30 WHERE storage_key='v1' AND workspace_id=$1`)
	stored("after poster change", 1130, 1189)
	exec(`UPDATE kb_media SET counted=false WHERE storage_key='k1' AND workspace_id=$1`)
	stored("after uncounting", 1030, 1189)
	exec(`DELETE FROM kb_media WHERE storage_key='k1' AND workspace_id=$1`)
	stored("after delete", 1030, 1082)
	exec(`DELETE FROM kb_media WHERE workspace_id=$1`)
	stored("after deleting all", 0, 0)
}
