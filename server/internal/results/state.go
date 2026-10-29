// Package results computes the numbers of the results page (issue #100): a team's numbers in
// Colliedesk next to the baseline of the history it imported. Decisions: docs/internals/results.md.
package results

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// MarkImportFinished records that a full run of an import finished, in the importer's transaction.
// The first source to finish becomes the baseline and its finish time the anchor, which no later run
// moves; any run of that source (a re-run) marks the baseline to compute again with its status. A
// run of another source changes nothing.
func MarkImportFinished(ctx context.Context, tx pgx.Tx, workspaceID, source, status, cause string, finishedAt time.Time) error {
	var importStatus, importCause any
	switch status {
	case "OK", "FAILED", "UNVERIFIED":
		importStatus = status
	}
	if cause != "" && status == "UNVERIFIED" {
		importCause = cause
	}
	if _, err := tx.Exec(ctx, `INSERT INTO results_state(workspace_id,baseline_source,baseline_anchor_at,baseline_import_status,baseline_import_cause,baseline_dirty)
		VALUES($1,$2,$3,$4,$5,true)
		ON CONFLICT (workspace_id) DO UPDATE SET
			baseline_source=COALESCE(results_state.baseline_source,EXCLUDED.baseline_source),
			baseline_anchor_at=COALESCE(results_state.baseline_anchor_at,EXCLUDED.baseline_anchor_at),
			baseline_import_status=CASE WHEN results_state.baseline_source IS NULL OR results_state.baseline_source=EXCLUDED.baseline_source
				THEN EXCLUDED.baseline_import_status ELSE results_state.baseline_import_status END,
			baseline_import_cause=CASE WHEN results_state.baseline_source IS NULL OR results_state.baseline_source=EXCLUDED.baseline_source
				THEN EXCLUDED.baseline_import_cause ELSE results_state.baseline_import_cause END,
			baseline_dirty=results_state.baseline_dirty OR results_state.baseline_source IS NULL OR results_state.baseline_source=EXCLUDED.baseline_source`,
		workspaceID, source, finishedAt.UTC(), importStatus, importCause); err != nil {
		return fmt.Errorf("mark the results baseline: %w", err)
	}
	return nil
}

// MarkImportUndone forgets the baseline when its import is undone, in the undo's transaction: the
// next import to finish becomes the baseline.
func MarkImportUndone(ctx context.Context, tx pgx.Tx, workspaceID, source string) error {
	if _, err := tx.Exec(ctx, `UPDATE results_state SET baseline_source=NULL,baseline_anchor_at=NULL,baseline_import_status=NULL,baseline_import_cause=NULL,baseline_dirty=true
		WHERE workspace_id=$1 AND baseline_source=$2`, workspaceID, source); err != nil {
		return fmt.Errorf("forget the results baseline: %w", err)
	}
	return nil
}

// Anchored says whether source is the baseline and its first full run has finished: from then on
// the stats an import reads are frozen.
func Anchored(ctx context.Context, tx pgx.Tx, workspaceID, source string) (bool, error) {
	var anchored bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM results_state WHERE workspace_id=$1 AND baseline_source=$2)`, workspaceID, source).Scan(&anchored); err != nil {
		return false, fmt.Errorf("read the results baseline: %w", err)
	}
	return anchored, nil
}
