package database

import (
	"context"
	"strings"
	"testing"
)

// A file is a kb_media row of kind file with a document type and its text; an article or one of its
// translations lists files in order, a draft carries its own list, and rolling back refuses while a
// file exists.
func TestKBArticleAttachmentsSchema(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	ids := seedTwoWorkspaces(t, pool)
	if _, err := pool.Exec(ctx, `WITH c AS (INSERT INTO kb_collections(workspace_id,name,slug) VALUES($1,'Docs','docs') RETURNING id),
		a AS (INSERT INTO kb_articles(id,workspace_id,collection_id,number,title,slug,body_md)
			SELECT '20000000-0000-4000-8000-000000000001',$1,id,1,'Returns','returns','text' FROM c RETURNING id)
		INSERT INTO kb_article_translations(workspace_id,article_id,locale,title,slug,body_md)
			SELECT $1,id,'de','Rückgabe','rueckgabe','Text' FROM a`, ids.a); err != nil {
		t.Fatal(err)
	}
	insertFile := `INSERT INTO kb_media(id,workspace_id,kind,storage_key,sha256,size,content_type,name,text)
		VALUES($2::uuid,$1,$3,'k/'||$2::text,repeat($4,64),10,$5,'form.pdf',$6)`
	const fileID = "30000000-0000-4000-8000-000000000001"
	if _, err := pool.Exec(ctx, insertFile, ids.a, fileID, "file", "a", "application/pdf", "Fill in the warranty-form now"); err != nil {
		t.Fatalf("insert a PDF file: %v", err)
	}
	for _, bad := range []struct{ kind, contentType, text string }{
		{"file", "image/png", ""},
		{"image", "application/pdf", ""},
		{"file", "text/html", ""},
		{"file", "application/pdf", strings.Repeat("x", 200*1024+1)},
	} {
		if _, err := pool.Exec(ctx, insertFile, ids.a, "30000000-0000-4000-8000-000000000009", bad.kind, "b", bad.contentType, bad.text); err == nil {
			t.Errorf("kind %s with %s and %d bytes of text was stored, want a refusal", bad.kind, bad.contentType, len(bad.text))
		}
	}
	var found bool
	if err := pool.QueryRow(ctx, `SELECT text_search @@ to_tsquery('simple','warranty') FROM kb_media WHERE id=$1`, fileID).Scan(&found); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Error("text_search does not match a word of the file's text")
	}

	attach := `INSERT INTO kb_article_attachments(workspace_id,article_id,locale,media_id,position) VALUES($1,'20000000-0000-4000-8000-000000000001',$2,$3,$4)`
	if _, err := pool.Exec(ctx, attach, ids.a, nil, fileID, 0); err != nil {
		t.Fatalf("attach to the article: %v", err)
	}
	if _, err := pool.Exec(ctx, attach, ids.a, "de", fileID, 0); err != nil {
		t.Fatalf("attach to the translation: %v", err)
	}
	if _, err := pool.Exec(ctx, attach, ids.a, nil, fileID, 1); err == nil {
		t.Error("the same file was listed twice on the article")
	}
	if _, err := pool.Exec(ctx, attach, ids.a, "fr", fileID, 0); err == nil {
		t.Error("a file was listed on a translation that does not exist")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM kb_article_translations WHERE locale='de'`); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM kb_article_attachments`).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 1 {
		t.Errorf("%d attachments after deleting the translation, want 1 (the article's own)", left)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO kb_article_drafts(workspace_id,article_id,title,slug,body_md,attachment_ids)
		VALUES($1,'20000000-0000-4000-8000-000000000001','Returns','returns','next',ARRAY[$2::uuid])`, ids.a, fileID); err != nil {
		t.Fatalf("save a draft with its own attachment list: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO kb_media_uploads(workspace_id,kind,size,expires_at) VALUES($1,'file',100,now()+interval '1 day')`, ids.a); err != nil {
		t.Fatalf("start a file upload: %v", err)
	}

	err := rollbackThrough(ctx, pool, "20261120141322-kb-article-attachments.up.sql")
	if err == nil || !strings.Contains(err.Error(), "files") {
		t.Fatalf("rollback with a file = %v, want a refusal that names the files", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM kb_media_uploads WHERE kind='file'; UPDATE kb_article_drafts SET attachment_ids=NULL; DELETE FROM kb_media WHERE kind='file'`); err != nil {
		t.Fatal(err)
	}
	if err := rollbackThrough(ctx, pool, "20261120141322-kb-article-attachments.up.sql"); err != nil {
		t.Fatalf("rollback without files: %v", err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
}
