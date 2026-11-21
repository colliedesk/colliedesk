package retention_test

import (
	"context"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5"
)

const (
	kbArticleID = "20000000-0000-4000-8000-0000000000a1"
	kbFileID    = "30000000-0000-4000-8000-0000000000a1"
)

// kbArticleWithFile gives the workspace a help center article that lists a file of the library, and
// empties the AI retrieval queue the article itself filled.
func (e *env) kbArticleWithFile(t *testing.T, workspaceID string) {
	t.Helper()
	e.exec(t, `WITH c AS (INSERT INTO kb_collections(workspace_id,name,slug) VALUES($1,'Docs','docs') RETURNING id)
		INSERT INTO kb_articles(id,workspace_id,collection_id,number,title,slug,body_md)
			SELECT $2,$1,id,1,'Returns','returns','text' FROM c`, workspaceID, kbArticleID)
	e.exec(t, `INSERT INTO kb_media(id,workspace_id,kind,storage_key,sha256,size,content_type,name,text)
		VALUES($2::uuid,$1,'file','k/'||$2::text,repeat('a',64),10,'application/pdf','form.pdf','Fill in the warranty form')`, workspaceID, kbFileID)
	e.exec(t, `INSERT INTO kb_article_attachments(workspace_id,article_id,locale,media_id,position) VALUES($1,$2,NULL,$3,0)`,
		workspaceID, kbArticleID, kbFileID)
	e.exec(t, `DELETE FROM ai_retrieval_pending WHERE workspace_id=$1`, workspaceID)
}

func (e *env) queuedArticles(t *testing.T, workspaceID string) int {
	t.Helper()
	var n int
	err := e.db.Privileged(context.Background(), database.PrivilegeMigrate, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM ai_retrieval_pending WHERE workspace_id=$1 AND kb_article_id=$2`,
			workspaceID, kbArticleID).Scan(&n)
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAWorkspaceWhoseArticleListsAFileIsDeleted(t *testing.T) {
	e := newEnv(t)
	a := e.workspace(t, "Alpha", "2026-11-01T00:00:00Z", "ann@alpha.example")
	e.kbArticleWithFile(t, a.ID)

	e.run(t, "2027-10-02T00:00:00Z")
	if got := e.run(t, "2027-11-01T00:00:00Z"); got.Deleted != 1 || e.exists(t, a.ID) {
		t.Fatalf("workspace with an article file: %+v, still there %v", got, e.exists(t, a.ID))
	}
	if left := e.dataRows(t, a.ID); len(left) != 0 {
		t.Fatalf("rows left after the delete: %v", left)
	}
}

// restore-workspace.sh removes the live workspace with a plain DELETE before it copies the backup in.
func TestAPlainDeleteOfAWorkspaceWhoseArticleListsAFileSucceeds(t *testing.T) {
	e := newEnv(t)
	a := e.workspace(t, "Alpha", "2026-11-01T00:00:00Z", "ann@alpha.example")
	e.kbArticleWithFile(t, a.ID)

	if _, err := e.pool.Exec(context.Background(), `DELETE FROM workspaces WHERE id=$1`, a.ID); err != nil {
		t.Fatalf("delete the workspace: %v", err)
	}
	if e.exists(t, a.ID) {
		t.Fatal("the workspace is still there")
	}
}

// The queue still learns of a list that changes while the workspace lives.
func TestChangingAnArticlesFilesStillQueuesItForAIRetrieval(t *testing.T) {
	e := newEnv(t)
	a := e.workspace(t, "Alpha", "2026-11-01T00:00:00Z", "ann@alpha.example")
	e.kbArticleWithFile(t, a.ID)
	ctx := context.Background()

	err := e.db.InWorkspace(ctx, a.ID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM kb_article_attachments WHERE article_id=$1`, kbArticleID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := e.queuedArticles(t, a.ID); n != 1 {
		t.Fatalf("after removing the file the article is queued %d times, want 1", n)
	}
	e.exec(t, `DELETE FROM ai_retrieval_pending WHERE workspace_id=$1`, a.ID)
	err = e.db.InWorkspace(ctx, a.ID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO kb_article_attachments(workspace_id,article_id,locale,media_id,position) VALUES($1,$2,NULL,$3,0)`,
			a.ID, kbArticleID, kbFileID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := e.queuedArticles(t, a.ID); n != 1 {
		t.Fatalf("after adding the file the article is queued %d times, want 1", n)
	}
}
