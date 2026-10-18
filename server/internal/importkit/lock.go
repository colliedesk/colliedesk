package importkit

import (
	"context"
	"errors"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/database"
)

// ErrImportRunning means another import or undo holds the workspace lock.
var ErrImportRunning = errors.New("an import or undo is already running in this workspace")

// AcquireRunLock holds a PostgreSQL advisory lock on the workspace for a whole import or undo. It is
// per workspace, not per source or importer: imports of different accounts share content-addressed
// files, so an undo deleting unreferenced files must never overlap another import into the same
// workspace.
func AcquireRunLock(ctx context.Context, db *database.DB, workspaceID string) (func(), error) {
	release, err := db.TryAdvisoryLock(ctx, "colliedesk-import|"+workspaceID)
	if errors.Is(err, database.ErrLockHeld) {
		return nil, fmt.Errorf("%w; wait for it to finish, then run again", ErrImportRunning)
	}
	if err != nil {
		return nil, fmt.Errorf("take import lock: %w", err)
	}
	return release, nil
}
