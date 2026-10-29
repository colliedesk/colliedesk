package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// SetSetupGuideHidden hides the setup guide from one agent's inbox, or shows it again.
func (s *Store) SetSetupGuideHidden(ctx context.Context, workspaceID, agentID string, hidden bool) error {
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE agents SET setup_guide_hidden=$3 WHERE workspace_id=$1 AND id=$2`, workspaceID, agentID, hidden)
		return err
	})
	if err != nil {
		return fmt.Errorf("set setup guide hidden: %w", err)
	}
	return nil
}
