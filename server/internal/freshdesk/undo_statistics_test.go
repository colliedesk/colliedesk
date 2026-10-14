package freshdesk_test

import (
	"context"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
)

// Undo usually runs minutes after an import, before autovacuum analyzes the rows it wrote. With
// the planner still believing import_items is empty, the keep query ran as nested loops (37 s for
// 300 tickets on a database holding earlier data). Undo must refresh the statistics first.
func TestUndoRightAfterAnImportPlansWithCurrentStatistics(t *testing.T) {
	env := newImportEnv(t)
	ctx := context.Background()
	env.mustRun(t)
	estimate := func() (float64, int) {
		var estimated float64
		var actual int
		if err := env.pool.QueryRow(ctx, `SELECT (SELECT reltuples FROM pg_class WHERE oid='import_items'::regclass), (SELECT count(*) FROM import_items)`).
			Scan(&estimated, &actual); err != nil {
			t.Fatal(err)
		}
		return estimated, actual
	}
	if estimated, actual := estimate(); estimated == float64(actual) {
		t.Fatalf("import_items statistics already current right after the import (%v rows); the test proves nothing", estimated)
	}
	if _, err := freshdesk.Undo(ctx, env.pool, env.storage, freshdesk.UndoOptions{WorkspaceSlug: "acme-support", DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if estimated, actual := estimate(); estimated != float64(actual) {
		t.Fatalf("planner estimate for import_items after undo = %v rows, table holds %d", estimated, actual)
	}
}
