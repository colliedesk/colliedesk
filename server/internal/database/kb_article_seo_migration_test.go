package database

import (
	"context"
	"strings"
	"testing"
)

// An article imported before the import read Freshdesk's search fields gets them from the seo_data it
// kept, as the import cleans them: spaces collapsed, a field over the limit left empty, never cut. An
// article changed in Colliedesk after its import, or a field a person already filled, stays as it is.
// The newest import version gets the same fields, so restoring it keeps them.
func TestKBArticleSEOFromFreshdeskFillsOnlyArticlesUnchangedSinceImport(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := rollbackThrough(ctx, pool, "20261114221049-kb-article-seo-from-freshdesk.up.sql"); err != nil {
		t.Fatalf("roll back the backfill: %v", err)
	}
	ids := seedTwoWorkspaces(t, pool)
	long := strings.Repeat("x", 121)
	seed := func(workspace string, articles [][4]string) {
		t.Helper()
		// Each article: id, seo_data JSON, kinds of its versions (comma-separated), seo_title already set.
		for i, a := range articles {
			if _, err := pool.Exec(ctx, `WITH c AS (
					INSERT INTO kb_collections(workspace_id,name,slug) VALUES($1,'Docs','docs-'||$2::bigint) RETURNING id),
				a AS (INSERT INTO kb_articles(id,workspace_id,collection_id,number,title,slug,body_md,status,published_at,source_meta,seo_title)
					SELECT $3::uuid,$1,id,$2::bigint,'Title','title','text','published',now(),$4::jsonb,$6 FROM c RETURNING id)
				INSERT INTO kb_article_versions(workspace_id,article_id,version,kind,title,slug,body_md,status,seo_title)
				SELECT $1,a.id,k.n,k.kind,'Title','title','text','published',$6
				FROM a, unnest(string_to_array($5,',')) WITH ORDINALITY k(kind,n)`,
				workspace, i+1, a[0], a[1], a[2], a[3]); err != nil {
				t.Fatalf("seed %s: %v", a[0], err)
			}
		}
	}
	seed(ids.a, [][4]string{
		{"20000000-0000-4000-8000-000000000001", `{"freshdesk_id":1,"seo_data":{"meta_title":"  Reset   your password ","meta_description":"How to reset it\nin two\u00a0 steps"}}`, "import", ""},
		{"20000000-0000-4000-8000-000000000002", `{"freshdesk_id":2,"seo_data":{"meta_title":"Edited","meta_description":"Edited"}}`, "import,edit", ""},
		{"20000000-0000-4000-8000-000000000003", `{"freshdesk_id":3,"seo_data":{"meta_title":"` + long + `","meta_description":42}}`, "import", ""},
		{"20000000-0000-4000-8000-000000000004", `{"freshdesk_id":4,"seo_data":{"meta_title":"From Freshdesk","meta_description":"Filled"}}`, "import", "Set by a person"},
		{"20000000-0000-4000-8000-000000000005", `{"freshdesk_id":5}`, "import", ""},
		{"20000000-0000-4000-8000-000000000006", `null`, "create", ""},
	})
	seed(ids.b, [][4]string{
		{"20000000-0000-4000-8000-000000000011", `{"freshdesk_id":1,"seo_data":{"meta_title":"Beta title","meta_description":"Beta description"}}`, "import", ""},
	})
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	type row struct{ workspace, title, description, versionTitle, versionDescription string }
	got := map[string]row{}
	rows, err := pool.Query(ctx, `SELECT a.id::text, a.workspace_id::text, a.seo_title, a.seo_description, v.seo_title, v.seo_description
		FROM kb_articles a JOIN LATERAL (SELECT seo_title, seo_description FROM kb_article_versions v
			WHERE v.workspace_id=a.workspace_id AND v.article_id=a.id ORDER BY version DESC LIMIT 1) v ON true`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		var r row
		if err := rows.Scan(&id, &r.workspace, &r.title, &r.description, &r.versionTitle, &r.versionDescription); err != nil {
			t.Fatal(err)
		}
		got[id] = r
	}
	rows.Close()
	want := map[string]row{
		"20000000-0000-4000-8000-000000000001": {ids.a, "Reset your password", "How to reset it in two steps", "Reset your password", "How to reset it in two steps"},
		"20000000-0000-4000-8000-000000000002": {ids.a, "", "", "", ""},
		"20000000-0000-4000-8000-000000000003": {ids.a, "", "", "", ""},
		"20000000-0000-4000-8000-000000000004": {ids.a, "Set by a person", "Filled", "Set by a person", "Filled"},
		"20000000-0000-4000-8000-000000000005": {ids.a, "", "", "", ""},
		"20000000-0000-4000-8000-000000000006": {ids.a, "", "", "", ""},
		"20000000-0000-4000-8000-000000000011": {ids.b, "Beta title", "Beta description", "Beta title", "Beta description"},
	}
	if len(got) != len(want) {
		t.Fatalf("articles after upgrade = %d, want %d: %+v", len(got), len(want), got)
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("%s = %+v, want %+v", id, got[id], w)
		}
	}
	var versions int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM kb_article_versions`).Scan(&versions); err != nil || versions != 8 {
		t.Fatalf("versions after upgrade = %d (%v), want 8", versions, err)
	}
	// Rolling back keeps the filled fields: they are what Freshdesk sent, and the columns go only with
	// the migration that added them.
	if err := rollbackThrough(ctx, pool, "20261114221049-kb-article-seo-from-freshdesk.up.sql"); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	var title string
	if err := pool.QueryRow(ctx, `SELECT seo_title FROM kb_articles WHERE id='20000000-0000-4000-8000-000000000001'`).Scan(&title); err != nil || title != "Reset your password" {
		t.Fatalf("seo_title after rollback = %q (%v), want kept", title, err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate again: %v", err)
	}
}
