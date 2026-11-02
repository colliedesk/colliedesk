package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// CreateMigrationRequest records that an admin asked Colliedesk to move the workspace's Freshdesk data.
func (s *Store) CreateMigrationRequest(ctx context.Context, workspaceID, agentID, freshdeskAddress, times string, requestedAt time.Time) error {
	_, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (struct{}, error) {
		_, err := tx.Exec(ctx, `INSERT INTO workspace_migration_requests(workspace_id,requested_by,freshdesk_address,times,requested_at)
			VALUES($1,$2,$3,$4,$5)`, workspaceID, agentID, freshdeskAddress, times, requestedAt)
		return struct{}{}, err
	})
	if err != nil {
		return fmt.Errorf("create migration request: %w", err)
	}
	return nil
}
