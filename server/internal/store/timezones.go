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
		// The admin chose it, UTC included: a first sign-in never changes it again.
		if err := tx.QueryRow(ctx, `UPDATE workspaces SET timezone=$2,timezone_chosen_at=now() WHERE id=$1
			RETURNING id::text,slug,name,timezone`, workspaceID, timezone).
			Scan(&workspace.ID, &workspace.Slug, &workspace.Name, &workspace.Timezone); err != nil {
			return err
		}
		return workspaceTimezoneChanged(ctx, tx, workspaceID)
	})
	if err != nil {
		return domain.Workspace{}, fmt.Errorf("update workspace timezone: %w", err)
	}
	return workspace, nil
}

func (s *Store) UpdateAgentTimezone(ctx context.Context, workspaceID, agentID string, timezone *string) (domain.Agent, error) {
	var agent domain.Agent
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `UPDATE agents a SET timezone_override=$3 WHERE a.workspace_id=$1 AND a.id=$2
			RETURNING a.id::text,a.email,a.display_name,a.timezone_override,`+AgentRolesColumn, workspaceID, agentID, timezone).
			Scan(&agent.ID, &agent.Email, &agent.DisplayName, &agent.TimezoneOverride, &agent.Roles)
	})
	if err != nil {
		return domain.Agent{}, fmt.Errorf("update agent timezone: %w", err)
	}
	return agent, nil
}

// workspaceTimezoneChanged makes response targets counted in the workspace's timezone count their clocks again.
func workspaceTimezoneChanged(ctx context.Context, tx pgx.Tx, workspaceID string) error {
	var own bool
	if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT timezone IS NOT NULL FROM response_targets WHERE workspace_id=$1),true)`, workspaceID).Scan(&own); err != nil {
		return err
	}
	if own {
		return nil
	}
	return targetsChanged(ctx, tx, workspaceID)
}

// BrowserTimezoneStart says what a first sign-in started on the browser's timezone: the workspace's
// timezone, the agent's own, or neither.
type BrowserTimezoneStart struct {
	Workspace bool
	Agent     bool
}

// StartOnBrowserTimezone starts a first sign-in on the browser's timezone, only when the session just made
// is the agent's first (agent tokens do not count), so a later sign-in, from any browser, never changes a
// choice.
//
// The person who created the workspace (its earliest agent: the sign-up makes them with the workspace)
// starts the workspace on it while its timezone is still the UTC default nobody chose
// (timezone_chosen_at NULL); they keep following the workspace. Anyone else, or the creator of a
// workspace whose admin chose UTC, gets it as their own timezone while they never chose one and the
// workspace is on UTC; a teammate's sign-in never changes the workspace.
func (s *Store) StartOnBrowserTimezone(ctx context.Context, workspaceID, agentID, timezone string) (BrowserTimezoneStart, error) {
	var start BrowserTimezoneStart
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		const firstSession = `(SELECT count(*) FROM sessions s WHERE s.workspace_id=$1 AND s.agent_id=$2 AND s.scope IS NULL)=1`
		tag, err := tx.Exec(ctx, `UPDATE workspaces w SET timezone=$3 WHERE w.id=$1 AND w.timezone='UTC' AND w.timezone_chosen_at IS NULL
			AND $2::uuid=(SELECT a.id FROM agents a WHERE a.workspace_id=$1 ORDER BY a.created_at,a.id LIMIT 1)
			AND `+firstSession, workspaceID, agentID, timezone)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 1 {
			start.Workspace = true
			return workspaceTimezoneChanged(ctx, tx, workspaceID)
		}
		tag, err = tx.Exec(ctx, `UPDATE agents a SET timezone_override=$3 WHERE a.workspace_id=$1 AND a.id=$2 AND a.timezone_override IS NULL
			AND (SELECT w.timezone FROM workspaces w WHERE w.id=$1)='UTC'
			AND `+firstSession, workspaceID, agentID, timezone)
		start.Agent = err == nil && tag.RowsAffected() == 1
		return err
	})
	if err != nil {
		return BrowserTimezoneStart{}, fmt.Errorf("start on browser timezone: %w", err)
	}
	return start, nil
}
