package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// maxAdminNames bounds the admins listed to a person who must ask one of them for a plan.
const maxAdminNames = 5

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

// AdminNames lists the active admins of a workspace by display name, else by email address, ordered by
// display name then address (admins with a name first), at most five.
func (s *Store) AdminNames(ctx context.Context, workspaceID string) ([]string, error) {
	names, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]string, error) {
		rows, err := tx.Query(ctx, `SELECT COALESCE(NULLIF(display_name,''), email) FROM agents
			WHERE workspace_id=$1 AND role='admin' AND deactivated_at IS NULL
			ORDER BY (COALESCE(display_name,'')=''), lower(display_name), email LIMIT $2`, workspaceID, maxAdminNames)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, pgx.RowTo[string])
	})
	if err != nil {
		return nil, fmt.Errorf("list admin names: %w", err)
	}
	return names, nil
}
