package freshdesk_test

import (
	"context"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// An import writes conversations before their messages; once it is done, the AI retrieval worker
// builds every resolved ticket with its customer message and never a private note, and undoing the
// import takes those rows with the tickets.
func TestImportedTicketsAreBuiltForAIRetrievalAndUndoTakesThemAway(t *testing.T) {
	env := newImportEnv(t)
	ctx := context.Background()
	env.mustRun(t)
	if _, err := store.New(env.pool).BuildAIRetrieval(ctx); err != nil {
		t.Fatal(err)
	}
	var resolved, built, leaked int
	if err := env.pool.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM conversations c WHERE c.workspace_id=$1 AND c.status IN ('resolved','closed')
				AND EXISTS (SELECT 1 FROM messages m WHERE m.workspace_id=c.workspace_id AND m.conversation_id=c.id AND m.kind='incoming')),
			(SELECT count(*) FROM ai_retrieval_docs WHERE workspace_id=$1 AND conversation_id IS NOT NULL),
			(SELECT count(*) FROM ai_retrieval_docs d JOIN messages m ON m.workspace_id=d.workspace_id AND m.conversation_id=d.conversation_id AND m.kind='note'
				WHERE d.workspace_id=$1 AND strpos(d.body, left(m.body, 40)) > 0)`, env.workspaceID).Scan(&resolved, &built, &leaked); err != nil {
		t.Fatal(err)
	}
	if resolved == 0 || built != resolved {
		t.Fatalf("built %d retrieval rows for %d resolved tickets with a customer message", built, resolved)
	}
	if leaked != 0 {
		t.Errorf("%d retrieval rows hold a private note", leaked)
	}
	if _, err := freshdesk.Undo(ctx, env.pool, env.storage, freshdesk.UndoOptions{WorkspaceSlug: "acme-support"}); err != nil {
		t.Fatal(err)
	}
	if left := env.count(t, "ai_retrieval_docs"); left != 0 {
		t.Errorf("%d retrieval rows outlive the undone import", left)
	}
}
