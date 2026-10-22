package importjobs

import (
	"context"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OpenKeyForTest opens a job's key as the worker does.
func OpenKeyForTest(k *Keys, workspaceID, jobID string, sealed []byte, keyID string) (string, error) {
	var id *string
	if keyID != "" {
		id = &keyID
	}
	return k.open(workspaceID, jobID, sealed, id)
}

// StubImporter replaces the importer of w.
func StubImporter(w *Worker, run func(context.Context, freshdesk.Options) (*freshdesk.Report, error),
	preview func(context.Context, freshdesk.Options) (*freshdesk.PreviewResult, error),
	undo func(context.Context, freshdesk.UndoOptions) (freshdesk.UndoResult, error)) {
	if run != nil {
		w.runImport = func(ctx context.Context, _ *pgxpool.Pool, o freshdesk.Options) (*freshdesk.Report, error) {
			return run(ctx, o)
		}
	}
	if preview != nil {
		w.preview = preview
	}
	if undo != nil {
		w.undo = func(ctx context.Context, _ *pgxpool.Pool, _ attachments.Storage, o freshdesk.UndoOptions) (freshdesk.UndoResult, error) {
			return undo(ctx, o)
		}
	}
}
