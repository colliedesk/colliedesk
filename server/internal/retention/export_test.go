package retention

import "context"

// RestoreUnlessDeleted exposes the recovery step of a failed delete to the tests.
func (s *Service) RestoreUnlessDeleted(ctx context.Context, workspaceID string, undo func() error) error {
	return s.restoreUnlessDeleted(ctx, workspaceID, undo)
}
