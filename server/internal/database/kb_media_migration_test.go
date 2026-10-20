package database

import (
	"context"
	"strings"
	"testing"
)

// The media migration renames kb_images and kb_article_images and marks as current the links of the
// images the article's text or a translation still names; a link only an older version made stays
// not current. Rolling back keeps the images and refuses once a row holds what the old schema cannot.
func TestKBMediaMigrationMarksCurrentLinksAndRollsBackOnlyWhatItCan(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := rollbackThrough(ctx, pool, "20261020180000-kb-media-library.up.sql"); err != nil {
		t.Fatalf("roll back the media migration: %v", err)
	}
	ids := seedTwoWorkspaces(t, pool)
	image := func(n string) string { return "00000000-0000-4000-8000-00000000000" + n }
	if _, err := pool.Exec(ctx, `WITH c AS (INSERT INTO kb_collections(workspace_id,name,slug) VALUES($1,'Docs','docs') RETURNING id),
		a AS (INSERT INTO kb_articles(id,workspace_id,collection_id,number,title,slug,body_md)
			SELECT '10000000-0000-4000-8000-000000000001',$1,id,1,'A','a','![x](/api/v1/kb/images/'||$2||')' FROM c RETURNING id),
		t AS (INSERT INTO kb_article_translations(workspace_id,article_id,locale,title,body_md) SELECT $1,id,'de','A','![x](/api/v1/kb/images/'||$3||')' FROM a RETURNING 1),
		i AS (INSERT INTO kb_images(id,workspace_id,storage_key,sha256,size,content_type)
			SELECT v.id::uuid,$1,$1::text||'/00/'||v.n,repeat(v.n,64),10,'image/png' FROM (VALUES ($2,'1'),($3,'2'),($4,'3')) v(id,n) RETURNING id)
		INSERT INTO kb_article_images(workspace_id,article_id,image_id) SELECT $1,a.id,i.id FROM a,i`,
		ids.a, image("1"), image("2"), image("3")); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	current := map[string]bool{}
	rows, err := pool.Query(ctx, `SELECT media_id::text,current FROM kb_article_media WHERE workspace_id=$1`, ids.a)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		var is bool
		if err := rows.Scan(&id, &is); err != nil {
			t.Fatal(err)
		}
		current[id] = is
	}
	rows.Close()
	if !current[image("1")] || !current[image("2")] || current[image("3")] || len(current) != 3 {
		t.Fatalf("current links = %v, want the body's and the translation's image only", current)
	}
	var kind string
	if err := pool.QueryRow(ctx, `SELECT kind FROM kb_media WHERE id=$1`, image("1")).Scan(&kind); err != nil || kind != "image" {
		t.Fatalf("kind = %q (%v), want image", kind, err)
	}

	if _, err := pool.Exec(ctx, `UPDATE kb_media SET thumb_storage_key='t', thumb_size=5 WHERE id=$1`, image("1")); err != nil {
		t.Fatal(err)
	}
	var stored int64
	if err := pool.QueryRow(ctx, `SELECT stored_storage_bytes FROM workspaces WHERE id=$1`, ids.a).Scan(&stored); err != nil || stored != 35 {
		t.Fatalf("stored with a thumbnail = %d (%v), want 35", stored, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE kb_media SET name='Logo' WHERE id=$1`, image("1")); err != nil {
		t.Fatal(err)
	}
	if err := rollbackThrough(ctx, pool, "20261020180000-kb-media-library.up.sql"); err == nil || !strings.Contains(err.Error(), "rollback refused") {
		t.Fatalf("rollback with a named image = %v, want refused", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE kb_media SET name='' WHERE id=$1`, image("1")); err != nil {
		t.Fatal(err)
	}
	if err := rollbackThrough(ctx, pool, "20261020180000-kb-media-library.up.sql"); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	var images int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM kb_images`).Scan(&images); err != nil || images != 3 {
		t.Fatalf("images after rollback = %d (%v), want 3", images, err)
	}
	if err := pool.QueryRow(ctx, `SELECT stored_storage_bytes FROM workspaces WHERE id=$1`, ids.a).Scan(&stored); err != nil || stored != 30 {
		t.Fatalf("stored after rollback = %d (%v), want 30 (the thumbnail no longer counts)", stored, err)
	}
}
