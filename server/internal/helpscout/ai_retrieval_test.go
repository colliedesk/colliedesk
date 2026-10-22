package helpscout_test

import (
	"context"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

// Help Scout conversations are built for AI retrieval after the import like every other.
func TestImportedConversationsAreBuiltForAIRetrieval(t *testing.T) {
	env := newImportEnv(t)
	ctx := context.Background()
	env.mustRun(t)
	if _, err := store.New(env.pool).BuildAIRetrieval(ctx); err != nil {
		t.Fatal(err)
	}
	var resolved, built int
	if err := env.pool.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM conversations c WHERE c.workspace_id=$1 AND c.status IN ('resolved','closed')
				AND EXISTS (SELECT 1 FROM messages m WHERE m.workspace_id=c.workspace_id AND m.conversation_id=c.id AND m.kind='incoming')),
			(SELECT count(*) FROM ai_retrieval_docs WHERE workspace_id=$1 AND conversation_id IS NOT NULL)`, env.workspaceID).Scan(&resolved, &built); err != nil {
		t.Fatal(err)
	}
	if resolved == 0 || built != resolved {
		t.Fatalf("built %d retrieval rows for %d resolved conversations with a customer message", built, resolved)
	}
}
