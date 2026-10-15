package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// SetTrialEnd sets when the workspace's trial ends and returns the previous end. It never touches
// trial_extended_at: whether an import already extended the trial is a separate fact.
func (s *Store) SetTrialEnd(ctx context.Context, workspaceID string, ends time.Time) (time.Time, error) {
	previous, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (time.Time, error) {
		var previous time.Time
		err := tx.QueryRow(ctx, `UPDATE workspaces w SET trial_ends_at=$2
			FROM (SELECT trial_ends_at FROM workspaces WHERE id=$1 FOR UPDATE) old
			WHERE w.id=$1 RETURNING old.trial_ends_at`, workspaceID, ends).Scan(&previous)
		return previous, err
	})
	if err != nil {
		return time.Time{}, fmt.Errorf("set trial end: %w", err)
	}
	return previous.UTC(), nil
}
