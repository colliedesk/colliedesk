package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// detachMessages takes chat messages back to the intake queue.
func (s *Store) detachMessages(ctx context.Context, workspaceID, fromID string, ids []string, actor Actor, now time.Time) (MoveResult, error) {
	return MoveResult{}, ErrNoIntakeQueue
}

// undoDetach puts messages taken back to the queue in their conversation again.
func undoDetach(ctx context.Context, tx pgx.Tx, workspaceID, fromID, gatheringID string, ids []string, actor Actor, now time.Time) error {
	return ErrMoveDecided
}
