package store

import (
	"context"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// UpdateAgentDisplayName sets the display name of an agent of the workspace; an empty name clears it.
func (s *Store) UpdateAgentDisplayName(ctx context.Context, workspaceID, agentID, name string) (domain.Agent, error) {
	var agent domain.Agent
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `UPDATE agents SET display_name=$3 WHERE workspace_id=$1 AND id=$2
			RETURNING id::text,email,display_name,role,timezone_override`, workspaceID, agentID, name).
			Scan(&agent.ID, &agent.Email, &agent.DisplayName, &agent.Role, &agent.TimezoneOverride)
	})
	if err != nil {
		return domain.Agent{}, fmt.Errorf("update agent display name: %w", err)
	}
	return agent, nil
}
