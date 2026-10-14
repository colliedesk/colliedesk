package freshdesk_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
)

func workspaceRows(t *testing.T, env *importEnv, workspaceID string) string {
	t.Helper()
	var parts []string
	for _, table := range []string{"contacts", "conversations", "messages", "attachments", "time_entries", "tags", "import_items"} {
		var n int
		if err := env.pool.QueryRow(context.Background(), `SELECT count(*) FROM `+table+` WHERE workspace_id=$1`, workspaceID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		parts = append(parts, fmt.Sprintf("%s=%d", table, n))
	}
	return strings.Join(parts, " ")
}

func TestUndoNeverTouchesAnotherWorkspaceOrAnotherSource(t *testing.T) {
	env := newImportEnv(t)
	ctx := context.Background()
	env.mustRun(t) // workspace A, account 777
	env.fake.accountID = 888
	env.mustRun(t) // workspace A, account 888: same data, second source
	env.fake.accountID = 0
	beta, _, err := env.auth.CreateWorkspace(ctx, "Beta Support", "admin@beta.example")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.run(t, ctx, func(o *freshdesk.Options) { o.WorkspaceSlug = "beta-support" }); err != nil {
		t.Fatal(err)
	}
	betaBefore, filesBefore := workspaceRows(t, env, beta.ID), storedFiles(t, env.storageRoot)
	var secondSource string
	if err := env.pool.QueryRow(ctx, `SELECT string_agg(entity||':'||source_id, ',' ORDER BY entity, source_id) FROM import_items
		WHERE workspace_id=$1 AND source='freshdesk:account:888'`, env.workspaceID).Scan(&secondSource); err != nil {
		t.Fatal(err)
	}

	result, err := freshdesk.Undo(ctx, env.pool, env.storage, freshdesk.UndoOptions{WorkspaceSlug: "acme-support", Source: "freshdesk:account:777"})
	if err != nil || result.Conversations != 5 {
		t.Fatalf("undo = %+v, %v", result, err)
	}
	if got := workspaceRows(t, env, beta.ID); got != betaBefore {
		t.Fatalf("other workspace changed: %s -> %s", betaBefore, got)
	}
	var afterSecond string
	if err := env.pool.QueryRow(ctx, `SELECT string_agg(entity||':'||source_id, ',' ORDER BY entity, source_id) FROM import_items
		WHERE workspace_id=$1 AND source='freshdesk:account:888'`, env.workspaceID).Scan(&afterSecond); err != nil || afterSecond != secondSource {
		t.Fatalf("the other import's ledger changed: %v", err)
	}
	if env.count(t, "conversations") != 5 || env.count(t, "attachments") != 3 || result.Files != 0 || storedFiles(t, env.storageRoot) != filesBefore {
		t.Fatalf("the other import lost data: rows=%s files=%d->%d undo=%+v", env.tableCounts(t), filesBefore, storedFiles(t, env.storageRoot), result)
	}
}

func TestImportOrUndoOfAnotherSourceWaitsForTheWorkspaceLock(t *testing.T) {
	env := newImportEnv(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once bool
	env.fake.onRequest = func(path string) {
		if path == "/api/v2/agents" && !once {
			once = true
			close(entered)
			<-release
		}
	}
	done := make(chan error, 1)
	go func() {
		_, err := env.run(t, context.Background(), nil)
		done <- err
	}()
	<-entered
	env.fake.mu.Lock()
	env.fake.accountID = 888
	env.fake.mu.Unlock()
	_, importErr := env.run(t, context.Background(), nil)
	_, undoErr := freshdesk.Undo(context.Background(), env.pool, env.storage, freshdesk.UndoOptions{WorkspaceSlug: "acme-support", Source: "freshdesk:account:888"})
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !errors.Is(importErr, freshdesk.ErrImportRunning) {
		t.Fatalf("import of a second source during an import = %v, want ErrImportRunning", importErr)
	}
	if !errors.Is(undoErr, freshdesk.ErrImportRunning) {
		t.Fatalf("undo during an import = %v", undoErr)
	}
}
