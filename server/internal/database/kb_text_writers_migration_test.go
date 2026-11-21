package database

import (
	"context"
	"slices"
	"testing"
)

// Going up from a database with history lists as writers of a text the people who wrote it in a
// version after it was last put on the help center, per article and per translation; a review event
// or the publishing version itself does not make a writer.
func TestKBTextWritersStartFromTheVersions(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := rollbackThrough(ctx, pool, "20261121014824-kb-text-writers.up.sql"); err != nil {
		t.Fatal(err)
	}
	ids := seedTwoWorkspaces(t, pool)
	const article = "20000000-0000-4000-8000-000000000001"
	if _, err := pool.Exec(ctx, `INSERT INTO agents(id,workspace_id,email,role) VALUES
		('40000000-0000-4000-8000-00000000000a',$1,'ana@writers.example','admin'),('40000000-0000-4000-8000-00000000000b',$1,'ben@writers.example','admin'),
		('40000000-0000-4000-8000-00000000000c',$1,'cai@writers.example','admin'),('40000000-0000-4000-8000-00000000000d',$1,'dan@writers.example','admin')`, ids.a); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `WITH c AS (INSERT INTO kb_collections(workspace_id,name,slug) VALUES($1,'Docs','docs') RETURNING id),
		a AS (INSERT INTO kb_articles(id,workspace_id,collection_id,number,title,slug,body_md,status,published_at) SELECT $2,$1,id,1,'Returns','returns','text','published',now() FROM c RETURNING id)
		INSERT INTO kb_article_translations(workspace_id,article_id,locale,title,slug,body_md) SELECT $1,id,'de','Rückgabe','rueckgabe','Text' FROM a`, ids.a, article); err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct {
		version             int
		kind, status, agent string
	}{
		{1, "create", "draft", "a"}, {2, "edit", "published", "b"}, {3, "draft", "published", "c"}, {4, "review_request", "published", "d"}, {5, "edit", "draft", "a"},
	} {
		if _, err := pool.Exec(ctx, `INSERT INTO kb_article_versions(workspace_id,article_id,version,kind,title,slug,body_md,status,collection_id,changed_by_agent_id)
			SELECT $1,$2,$3,$4,'Returns','returns','text',$5,collection_id,('40000000-0000-4000-8000-00000000000'||$6)::uuid FROM kb_articles WHERE id=$2`,
			ids.a, article, v.version, v.kind, v.status, v.agent); err != nil {
			t.Fatalf("version %d: %v", v.version, err)
		}
	}
	for _, v := range []struct {
		version             int
		kind, status, agent string
	}{
		{1, "create", "published", "a"}, {2, "edit", "draft", "b"}, {3, "edit", "draft", "c"},
	} {
		if _, err := pool.Exec(ctx, `INSERT INTO kb_article_translation_versions(workspace_id,article_id,locale,version,kind,title,slug,body_md,status,changed_by_agent_id)
			VALUES($1,$2,'de',$3,$4,'Rückgabe','rueckgabe','Text',$5,('40000000-0000-4000-8000-00000000000'||$6)::uuid)`,
			ids.a, article, v.version, v.kind, v.status, v.agent); err != nil {
			t.Fatalf("translation version %d: %v", v.version, err)
		}
	}

	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(ctx, `SELECT COALESCE(locale,'-')||' '||right(agent_id::text,1) FROM kb_text_writers ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for rows.Next() {
		var row string
		if err := rows.Scan(&row); err != nil {
			t.Fatal(err)
		}
		got = append(got, row)
	}
	if want := []string{"- a", "- c", "de b", "de c"}; !slices.Equal(got, want) {
		t.Fatalf("writers after going up = %v, want %v", got, want)
	}
	if err := rollbackThrough(ctx, pool, "20261121014824-kb-text-writers.up.sql"); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
}
