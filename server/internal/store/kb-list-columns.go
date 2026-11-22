package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// SetKBListColumns stores one agent's columns of the knowledge base article list, in order; nil goes
// back to the default. The caller checks the names (domain.CheckKBListColumns).
func (s *Store) SetKBListColumns(ctx context.Context, workspaceID, agentID string, columns []string) error {
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE agents SET kb_list_columns=$3 WHERE workspace_id=$1 AND id=$2`, workspaceID, agentID, columns)
		return err
	})
	if err != nil {
		return fmt.Errorf("set knowledge base list columns: %w", err)
	}
	return nil
}
