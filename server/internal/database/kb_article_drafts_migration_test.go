package database

import (
	"context"
	"strings"
	"testing"
)

// The drafts migration changes no article: a draft stays a draft, a published article stays published
// with its date, and no unpublished draft is invented for any of them. Rolling back refuses while a
// draft exists, because dropping the table would lose its text.
func TestKBArticleDraftsMigrationLeavesArticlesAsTheyWere(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := rollbackThrough(ctx, pool, "20261120073409-kb-article-drafts.up.sql"); err != nil {
		t.Fatalf("roll back the drafts migration: %v", err)
	}
	ids := seedTwoWorkspaces(t, pool)
	if _, err := pool.Exec(ctx, `WITH c AS (INSERT INTO kb_collections(workspace_id,name,slug) VALUES($1,'Docs','docs') RETURNING id),
		a AS (INSERT INTO kb_articles(id,workspace_id,collection_id,number,title,slug,body_md,status,published_at)
			SELECT v.id::uuid,$1,c.id,v.number,v.title,v.slug,'text',v.status,v.published::timestamptz FROM c, (VALUES
				('20000000-0000-4000-8000-000000000001',1,'Live','live','published','2026-01-02T03:04:05Z'),
				('20000000-0000-4000-8000-000000000002',2,'Unsent','unsent','draft',NULL)) v(id,number,title,slug,status,published)
			RETURNING id)
		INSERT INTO kb_article_translations(workspace_id,article_id,locale,title,slug,body_md,status,published_at)
		SELECT $1,id,'fr','Vivant','vivant','texte','published','2026-01-02T03:04:05Z' FROM a WHERE id='20000000-0000-4000-8000-000000000001'`, ids.a); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	got := map[string]string{}
	rows, err := pool.Query(ctx, `SELECT slug, status || ' ' || COALESCE(to_char(published_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS'),'-')
		FROM kb_articles WHERE workspace_id=$1
		UNION ALL SELECT 'fr:' || slug, status || ' ' || COALESCE(to_char(published_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS'),'-')
		FROM kb_article_translations WHERE workspace_id=$1`, ids.a)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var slug, state string
		if err := rows.Scan(&slug, &state); err != nil {
			t.Fatal(err)
		}
		got[slug] = state
	}
	rows.Close()
	want := map[string]string{"live": "published 2026-01-02T03:04:05", "unsent": "draft -", "fr:vivant": "published 2026-01-02T03:04:05"}
	for slug, w := range want {
		if got[slug] != w {
			t.Errorf("%s = %q, want %q", slug, got[slug], w)
		}
	}
	var drafts int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM kb_article_drafts`).Scan(&drafts); err != nil {
		t.Fatal(err)
	}
	if drafts != 0 {
		t.Errorf("kb_article_drafts has %d rows after the upgrade, want 0", drafts)
	}

	if _, err := pool.Exec(ctx, `INSERT INTO kb_article_drafts(workspace_id,article_id,title,slug,body_md)
		VALUES($1,'20000000-0000-4000-8000-000000000001','Next','next','new text')`, ids.a); err != nil {
		t.Fatalf("save a draft: %v", err)
	}
	err = rollbackThrough(ctx, pool, "20261120073409-kb-article-drafts.up.sql")
	if err == nil || !strings.Contains(err.Error(), "unpublished changes") {
		t.Fatalf("rollback with a draft = %v, want a refusal that names the unpublished changes", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM kb_article_drafts`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
}
