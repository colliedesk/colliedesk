package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// OnboardingGoal returns the goal the workspace gave at cloud sign-up, or nil when it gave none or was
// not created by cloud sign-up.
func (s *Store) OnboardingGoal(ctx context.Context, workspaceID string) (*string, error) {
	var goal *string
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT goal FROM workspace_onboarding WHERE workspace_id=$1`, workspaceID).Scan(&goal)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("read onboarding goal: %w", err)
	}
	return goal, nil
}
