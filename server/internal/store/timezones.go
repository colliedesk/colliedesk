package store

import (
	"context"
	"fmt"

	"github.com/hclabsco/colliedesk/server/internal/domain"
)

func (s *Store) UpdateWorkspaceTimezone(ctx context.Context, workspaceID, timezone string) (domain.Workspace, error) {
	var workspace domain.Workspace
	err := s.pool.QueryRow(ctx, `UPDATE workspaces SET timezone=$2 WHERE id=$1
		RETURNING id::text,slug,name,timezone`, workspaceID, timezone).
		Scan(&workspace.ID, &workspace.Slug, &workspace.Name, &workspace.Timezone)
	if err != nil {
		return domain.Workspace{}, fmt.Errorf("update workspace timezone: %w", err)
	}
	return workspace, nil
}

func (s *Store) UpdateAgentTimezone(ctx context.Context, workspaceID, agentID string, timezone *string) (domain.Agent, error) {
	var agent domain.Agent
	err := s.pool.QueryRow(ctx, `UPDATE agents SET timezone_override=$3 WHERE workspace_id=$1 AND id=$2
		RETURNING id::text,email,display_name,role,timezone_override`, workspaceID, agentID, timezone).
		Scan(&agent.ID, &agent.Email, &agent.DisplayName, &agent.Role, &agent.TimezoneOverride)
	if err != nil {
		return domain.Agent{}, fmt.Errorf("update agent timezone: %w", err)
	}
	return agent, nil
}
