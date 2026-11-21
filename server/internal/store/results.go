package store

import (
	"context"

	"github.com/colliedesk/colliedesk/server/internal/results"
	"github.com/jackc/pgx/v5"
)

// ReadResults returns the results page's stored numbers and its live help center section
// (docs/internals/results.md); the compute error and the weekly email are for admins.
func (s *Store) ReadResults(ctx context.Context, workspaceID string, viewer results.Viewer) (results.Page, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (results.Page, error) {
		page, err := results.Read(ctx, tx, workspaceID, viewer)
		if err != nil {
			return page, err
		}
		page.HelpCenter, err = helpCenterResults(ctx, tx, workspaceID, viewer.Now)
		return page, err
	})
}
