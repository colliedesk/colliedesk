package store

import (
	"context"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store runs every query in a workspace-scoped transaction (row-level security applies) except the
// few reviewed cross-workspace lookups, which use database.DB.Privileged.
type Store struct {
	db       *database.DB
	outbound OutboundPolicy
	// limits turns on the plan limits of the hosted cloud; the zero value (self-hosted) has none.
	limits bool
	clock  func() time.Time
}

func New(pool *pgxpool.Pool) *Store {
	return NewWithDB(database.New(pool))
}

// NewWithDB uses db as is, for example one built with database.NewSplit.
func NewWithDB(db *database.DB) *Store {
	return &Store{db: db}
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

// WithPlanLimits returns a store that enforces the Free plan's monthly conversation quota. Without
// it (self-hosted) no conversation is ever held.
func (s *Store) WithPlanLimits(enabled bool) *Store {
	copy := *s
	copy.limits = enabled
	return &copy
}

// WithClock returns a store that reads now from the given clock; tests move it instead of sleeping.
func (s *Store) WithClock(now func() time.Time) *Store {
	copy := *s
	copy.clock = now
	return &copy
}

func (s *Store) now() time.Time {
	if s.clock != nil {
		return s.clock().UTC()
	}
	return time.Now().UTC()
}
