package store

import (
	"context"

	"github.com/colliedesk/colliedesk/server/internal/results"
	"github.com/jackc/pgx/v5"
)

// ReadResults returns the results page's stored numbers (docs/internals/results.md); the compute error
// and the weekly email are for admins.
func (s *Store) ReadResults(ctx context.Context, workspaceID string, viewer results.Viewer) (results.Page, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (results.Page, error) {
		return results.Read(ctx, tx, workspaceID, viewer)
	})
}
