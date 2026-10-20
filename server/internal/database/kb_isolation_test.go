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
		i AS (INSERT INTO kb_images(workspace_id,storage_key,sha256,size,content_type) VALUES($1,$1::text||'/00/'||$3,$3,10,'image/png') RETURNING id),
		l AS (INSERT INTO kb_article_images(workspace_id,article_id,image_id) SELECT $1,a.id,i.id FROM a,i RETURNING 1)
		INSERT INTO help_centers(workspace_id,title) VALUES($1,$2)`, workspaceID, name, sha)
	if err != nil {
		t.Fatalf("seed knowledge base of %s: %v", name, err)
	}
}

var kbTables = []string{"kb_collections", "kb_articles", "kb_article_versions", "kb_article_translations", "kb_images", "kb_article_images", "help_centers"}

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
	_, err = pool.Exec(ctx, `INSERT INTO kb_article_images(workspace_id,article_id,image_id)
		SELECT $1,a.id,i.id FROM kb_articles a, kb_images i WHERE a.workspace_id=$1 AND i.workspace_id=$2`, ids.a, ids.b)
	if err == nil {
		t.Fatal("an article of alpha was linked to an image of beta")
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

func TestKnowledgeBaseImagesCountTowardStorage(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	ids := seedTwoWorkspaces(t, pool)
	db := New(pool)
	if err := db.InWorkspace(ctx, ids.a, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO kb_images(workspace_id,storage_key,sha256,size,content_type,counted) VALUES
			($1,'k1',repeat('a',64),100,'image/png',true),($1,'k2',repeat('b',64),40,'image/png',false)`, ids.a)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	stored := func() (int64, int64) {
		var counted, all int64
		if err := pool.QueryRow(ctx, `SELECT counted_storage_bytes,stored_storage_bytes FROM workspaces WHERE id=$1`, ids.a).Scan(&counted, &all); err != nil {
			t.Fatal(err)
		}
		return counted, all
	}
	if counted, all := stored(); counted != 100 || all != 140 {
		t.Fatalf("after insert counted=%d stored=%d, want 100 and 140", counted, all)
	}
	if err := db.InWorkspace(ctx, ids.a, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM kb_images WHERE storage_key='k1'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if counted, all := stored(); counted != 0 || all != 40 {
		t.Fatalf("after delete counted=%d stored=%d, want 0 and 40", counted, all)
	}
}
