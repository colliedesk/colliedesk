package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// SetTrialEnd sets when the workspace's trial ends and returns the previous end. It never touches
// trial_extended_at: whether an import already extended the trial is a separate fact. Every change
// is recorded with who made it and when.
func (s *Store) SetTrialEnd(ctx context.Context, workspaceID string, ends time.Time, by string, now time.Time) (time.Time, error) {
	if by == "" {
		return time.Time{}, errors.New("changing a trial end needs the name of who does it")
	}
	previous, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (time.Time, error) {
		var previous time.Time
		err := tx.QueryRow(ctx, `UPDATE workspaces w SET trial_ends_at=$2
			FROM (SELECT trial_ends_at FROM workspaces WHERE id=$1 FOR UPDATE) old
			WHERE w.id=$1 RETURNING old.trial_ends_at`, workspaceID, ends).Scan(&previous)
		if err != nil {
			return previous, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO workspace_trial_changes(workspace_id,previous_ends_at,new_ends_at,changed_by,changed_at)
			VALUES($1,$2,$3,$4,$5)`, workspaceID, previous, ends, by, now)
		return previous, err
	})
	if err != nil {
		return time.Time{}, fmt.Errorf("set trial end: %w", err)
	}
	return previous.UTC(), nil
}
