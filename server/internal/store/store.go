package store

import (
	"context"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store runs every query in a workspace-scoped transaction (row-level security applies) except the
// few reviewed cross-workspace lookups, which use database.DB.Privileged.
type Store struct {
	db       *database.DB
	outbound OutboundPolicy
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{db: database.New(pool)}
}

// scoped runs fn in one transaction scoped to the workspace and returns its result.
func scoped[T any](ctx context.Context, s *Store, workspaceID string, fn func(pgx.Tx) (T, error)) (T, error) {
	var result T
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		var err error
		result, err = fn(tx)
		return err
	})
	if err != nil {
		var zero T
		return zero, err
	}
	return result, nil
}
