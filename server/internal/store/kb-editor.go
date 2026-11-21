package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// SetKBEditor records the article editor one agent writes in: "visual" or "markdown".
func (s *Store) SetKBEditor(ctx context.Context, workspaceID, agentID, editor string) error {
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE agents SET kb_editor=$3 WHERE workspace_id=$1 AND id=$2`, workspaceID, agentID, editor)
		return err
	})
	if err != nil {
		return fmt.Errorf("set kb editor: %w", err)
	}
	return nil
}
