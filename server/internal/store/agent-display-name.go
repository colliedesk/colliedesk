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
		return tx.QueryRow(ctx, `UPDATE agents a SET display_name=$3 WHERE a.workspace_id=$1 AND a.id=$2
			RETURNING a.id::text,a.email,a.display_name,a.timezone_override,`+AgentRolesColumn, workspaceID, agentID, name).
			Scan(&agent.ID, &agent.Email, &agent.DisplayName, &agent.TimezoneOverride, &agent.Roles)
	})
	if err != nil {
		return domain.Agent{}, fmt.Errorf("update agent display name: %w", err)
	}
	return agent, nil
}
