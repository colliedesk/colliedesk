package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrSlugTaken means another workspace already uses the slug.
var ErrSlugTaken = errors.New("workspace slug is taken")

// UpdateWorkspaceSlug renames the workspace slug, which is also the subdomain of its hosted mail address.
func (s *Store) UpdateWorkspaceSlug(ctx context.Context, workspaceID, slug string) (domain.Workspace, error) {
	var workspace domain.Workspace
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `UPDATE workspaces SET slug=$2 WHERE id=$1 RETURNING id::text,slug,name,timezone`, workspaceID, slug).
			Scan(&workspace.ID, &workspace.Slug, &workspace.Name, &workspace.Timezone)
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return domain.Workspace{}, ErrSlugTaken
	}
	if err != nil {
		return domain.Workspace{}, fmt.Errorf("update workspace slug: %w", err)
	}
	return workspace, nil
}

// UpdateWorkspaceName renames the workspace. Email channels keep their own sender names.
func (s *Store) UpdateWorkspaceName(ctx context.Context, workspaceID, name string) (domain.Workspace, error) {
	var workspace domain.Workspace
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `UPDATE workspaces SET name=$2 WHERE id=$1 RETURNING id::text,slug,name,timezone`, workspaceID, name).
			Scan(&workspace.ID, &workspace.Slug, &workspace.Name, &workspace.Timezone)
	})
	if err != nil {
		return domain.Workspace{}, fmt.Errorf("update workspace name: %w", err)
	}
	return workspace, nil
}

func (s *Store) UpdateWorkspaceTimezone(ctx context.Context, workspaceID, timezone string) (domain.Workspace, error) {
	var workspace domain.Workspace
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `UPDATE workspaces SET timezone=$2 WHERE id=$1
			RETURNING id::text,slug,name,timezone`, workspaceID, timezone).
			Scan(&workspace.ID, &workspace.Slug, &workspace.Name, &workspace.Timezone)
	})
	if err != nil {
		return domain.Workspace{}, fmt.Errorf("update workspace timezone: %w", err)
	}
	return workspace, nil
}

func (s *Store) UpdateAgentTimezone(ctx context.Context, workspaceID, agentID string, timezone *string) (domain.Agent, error) {
	var agent domain.Agent
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `UPDATE agents SET timezone_override=$3 WHERE workspace_id=$1 AND id=$2
			RETURNING id::text,email,display_name,role,timezone_override`, workspaceID, agentID, timezone).
			Scan(&agent.ID, &agent.Email, &agent.DisplayName, &agent.Role, &agent.TimezoneOverride)
	})
	if err != nil {
		return domain.Agent{}, fmt.Errorf("update agent timezone: %w", err)
	}
	return agent, nil
}

// StartOnBrowserTimezone makes timezone the agent's own at their first sign-in: only while they never chose
// one, the workspace is still on its UTC default, and the session just made is their first (agent tokens
// do not count). A later sign-in, from any browser, never changes a choice, including "same as workspace".
// It reports whether the timezone was set.
func (s *Store) StartOnBrowserTimezone(ctx context.Context, workspaceID, agentID, timezone string) (bool, error) {
	var set bool
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE agents a SET timezone_override=$3 WHERE a.workspace_id=$1 AND a.id=$2 AND a.timezone_override IS NULL
			AND (SELECT w.timezone FROM workspaces w WHERE w.id=$1)='UTC'
			AND (SELECT count(*) FROM sessions s WHERE s.workspace_id=$1 AND s.agent_id=$2 AND s.scope IS NULL)=1`, workspaceID, agentID, timezone)
		set = tag.RowsAffected() == 1
		return err
	})
	if err != nil {
		return false, fmt.Errorf("start agent on browser timezone: %w", err)
	}
	return set, nil
}
