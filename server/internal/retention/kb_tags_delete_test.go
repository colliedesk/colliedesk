package retention_test

import (
	"context"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5"
)

const taggedArticleID = "20000000-0000-4000-8000-0000000000b1"

// kbTaggedArticle gives the workspace a help center article with a tag, and empties the AI retrieval
// queue the article and its tag filled.
func (e *env) kbTaggedArticle(t *testing.T, workspaceID string) {
	t.Helper()
	e.exec(t, `WITH c AS (INSERT INTO kb_collections(workspace_id,name,slug) VALUES($1,'Guides','guides') RETURNING id)
		INSERT INTO kb_articles(id,workspace_id,collection_id,number,title,slug,body_md)
			SELECT $2,$1,id,1,'Returns','returns','text' FROM c`, workspaceID, taggedArticleID)
	e.exec(t, `INSERT INTO kb_article_tags(workspace_id,article_id,tag) VALUES($1,$2,'billing')`, workspaceID, taggedArticleID)
	e.exec(t, `DELETE FROM ai_retrieval_pending WHERE workspace_id=$1`, workspaceID)
}

func TestAWorkspaceWhoseArticleHasATagIsDeleted(t *testing.T) {
	e := newEnv(t)
	a := e.workspace(t, "Alpha", "2026-11-01T00:00:00Z", "ann@alpha.example")
	e.kbTaggedArticle(t, a.ID)

	e.run(t, "2027-10-02T00:00:00Z")
	if got := e.run(t, "2027-11-01T00:00:00Z"); got.Deleted != 1 || e.exists(t, a.ID) {
		t.Fatalf("workspace with a tagged article: %+v, still there %v", got, e.exists(t, a.ID))
	}
	if left := e.dataRows(t, a.ID); len(left) != 0 {
		t.Fatalf("rows left after the delete: %v", left)
	}
}

// restore-workspace.sh removes the live workspace with a plain DELETE before it copies the backup in.
func TestAPlainDeleteOfAWorkspaceWhoseArticleHasATagSucceeds(t *testing.T) {
	e := newEnv(t)
	a := e.workspace(t, "Alpha", "2026-11-01T00:00:00Z", "ann@alpha.example")
	e.kbTaggedArticle(t, a.ID)

	if _, err := e.pool.Exec(context.Background(), `DELETE FROM workspaces WHERE id=$1`, a.ID); err != nil {
		t.Fatalf("delete the workspace: %v", err)
	}
	if e.exists(t, a.ID) {
		t.Fatal("the workspace is still there")
	}
}

// The queue still learns of a tag removed while the workspace lives.
func TestRemovingAnArticlesTagStillQueuesItForAIRetrieval(t *testing.T) {
	e := newEnv(t)
	a := e.workspace(t, "Alpha", "2026-11-01T00:00:00Z", "ann@alpha.example")
	e.kbTaggedArticle(t, a.ID)
	ctx := context.Background()

	err := e.db.InWorkspace(ctx, a.ID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM kb_article_tags WHERE article_id=$1`, taggedArticleID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	var n int
	err = e.db.Privileged(ctx, database.PrivilegeMigrate, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM ai_retrieval_pending WHERE workspace_id=$1 AND kb_article_id=$2`,
			a.ID, taggedArticleID).Scan(&n)
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("after removing the tag the article is queued %d times, want 1", n)
	}
}
