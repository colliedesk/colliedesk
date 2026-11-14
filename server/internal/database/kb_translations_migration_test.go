package database

import (
	"context"
	"testing"
)

// The translations migration keeps every imported translation, gives it a first version of kind
// import, a slug and the status and search fields Freshdesk sent, and adds its language hidden, so
// the public help center shows what it showed before. Rolling back keeps the translation rows.
func TestKBTranslationsMigrationKeepsImportedTranslationsAndHidesTheirLanguages(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := rollbackThrough(ctx, pool, "20261105100000-kb-translations.up.sql"); err != nil {
		t.Fatalf("roll back the translations migration: %v", err)
	}
	ids := seedTwoWorkspaces(t, pool)
	if _, err := pool.Exec(ctx, `INSERT INTO help_centers(workspace_id,locale) VALUES($1,'de')`, ids.a); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `WITH c AS (INSERT INTO kb_collections(workspace_id,name,slug) VALUES($1,'Docs','docs') RETURNING id),
		a AS (INSERT INTO kb_articles(id,workspace_id,collection_id,number,title,slug,body_md,status,published_at)
			SELECT '10000000-0000-4000-8000-000000000001',$1,id,1,'A','a','text','published',now() FROM c RETURNING id)
		INSERT INTO kb_article_translations(workspace_id,article_id,locale,title,body_md,source_meta,updated_at)
		SELECT $1,id,v.locale,v.title,'body',v.meta::jsonb,'2026-01-02T03:04:05Z' FROM a, (VALUES
			('fr','Réinitialiser le mot de passe','{"status":2,"seo_data":{"meta_title":"  Mot   de passe ","meta_description":"Comment faire"}}'),
			('es','Hola','{"status":1}'),
			('de','Hallo','{"status":2}')) v(locale,title,meta)`, ids.a); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	type row struct {
		slug, status, seoTitle, seoDescription string
		published, outdated                    bool
		versions                               int
		kind                                   string
	}
	got := map[string]row{}
	rows, err := pool.Query(ctx, `SELECT t.locale,t.slug,t.status,t.published_at IS NOT NULL,t.seo_title,t.seo_description,t.outdated,
		(SELECT count(*) FROM kb_article_translation_versions v WHERE v.workspace_id=t.workspace_id AND v.article_id=t.article_id AND v.locale=t.locale),
		(SELECT kind FROM kb_article_translation_versions v WHERE v.workspace_id=t.workspace_id AND v.article_id=t.article_id AND v.locale=t.locale)
		FROM kb_article_translations t WHERE t.workspace_id=$1`, ids.a)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var locale string
		var r row
		if err := rows.Scan(&locale, &r.slug, &r.status, &r.published, &r.seoTitle, &r.seoDescription, &r.outdated, &r.versions, &r.kind); err != nil {
			t.Fatal(err)
		}
		got[locale] = r
	}
	rows.Close()
	want := map[string]row{
		"fr": {slug: "réinitialiser-le-mot-de-passe", status: "published", published: true, seoTitle: "Mot de passe", seoDescription: "Comment faire", versions: 1, kind: "import"},
		"es": {slug: "hola", status: "draft", versions: 1, kind: "import"},
		"de": {slug: "hallo", status: "published", published: true, versions: 1, kind: "import"},
	}
	for locale, w := range want {
		if got[locale] != w {
			t.Errorf("%s = %+v, want %+v", locale, got[locale], w)
		}
	}
	languages := map[string]bool{}
	rows, err = pool.Query(ctx, `SELECT locale, visible FROM help_center_languages WHERE workspace_id=$1`, ids.a)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var locale string
		var visible bool
		if err := rows.Scan(&locale, &visible); err != nil {
			t.Fatal(err)
		}
		languages[locale] = visible
	}
	rows.Close()
	// de is the help center's primary language: no language row, the translation stays.
	if len(languages) != 2 || languages["fr"] || languages["es"] {
		t.Fatalf("languages = %v, want fr and es, both hidden", languages)
	}
	if err := rollbackThrough(ctx, pool, "20261105100000-kb-translations.up.sql"); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM kb_article_translations WHERE workspace_id=$1`, ids.a).Scan(&count); err != nil || count != 3 {
		t.Fatalf("translations after rollback = %d (%v), want 3", count, err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate again: %v", err)
	}
}
