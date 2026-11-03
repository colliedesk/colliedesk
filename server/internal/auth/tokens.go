package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// AgentToken is a created token. Token is the raw bearer value; it exists only in this result.
type AgentToken struct {
	Token   string         `json:"token"`
	Session domain.Session `json:"session"`
}

// CreateAgentToken gives the signed-in person a token that follows a role whose permissions they all
// hold (any role of the workspace for an admin) or holds a fixed list of permissions they hold. Only a person's own session makes tokens (the route is person-only).
func (s *Service) CreateAgentToken(ctx context.Context, principal Principal, request TokenRequest) (AgentToken, error) {
	request, err := checkTokenRequest(request)
	if err != nil {
		return AgentToken{}, err
	}
	var created AgentToken
	err = s.db.InWorkspace(ctx, principal.Workspace.ID, func(tx pgx.Tx) error {
		var err error
		created, err = s.insertAgentToken(ctx, tx, principal.Workspace.ID, principal.Agent.ID, principal.effective(), request)
		return err
	})
	return created, err
}

// IssueAgentToken is the operator path of CreateAgentToken (collie token create): it names the
// workspace by slug and the person by email, for installs where nobody can sign in yet.
func (s *Service) IssueAgentToken(ctx context.Context, workspaceSlug, email string, request TokenRequest) (AgentToken, error) {
	request, err := checkTokenRequest(request)
	if err != nil {
		return AgentToken{}, err
	}
	workspaceID, err := s.db.WorkspaceIDBySlug(ctx, workspaceSlug)
	if err != nil {
		return AgentToken{}, err
	}
	var created AgentToken
	err = s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		var agentID string
		var roles []roleRow
		err := tx.QueryRow(ctx, `SELECT a.id::text,`+personRolesColumn+` FROM agents a WHERE a.workspace_id=$1 AND a.email=lower($2) AND a.deactivated_at IS NULL`,
			workspaceID, strings.TrimSpace(email)).Scan(&agentID, &roles)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("no active agent %q in workspace %q", email, workspaceSlug)
		}
		if err != nil {
			return fmt.Errorf("find agent: %w", err)
		}
		created, err = s.insertAgentToken(ctx, tx, workspaceID, agentID, rolesPermissions(roles), request)
		return err
	})
	return created, err
}

// insertAgentToken stores the token of agentID, who holds held. A role may be any of the workspace's
// whose permissions are all within held, so an admin may pick every role and anyone may pick their own
// (domain.ErrRoleNotFound, or *domain.ExceedsYoursError naming what they lack); a list must be within
// held (*domain.ExceedsYoursError).
func (s *Service) insertAgentToken(ctx context.Context, tx pgx.Tx, workspaceID, agentID string, held PermissionSet, request TokenRequest) (AgentToken, error) {
	scope, roleID, given := ScopeCustom, "", request.Permissions
	if request.Permissions == nil {
		var role roleRow
		err := tx.QueryRow(ctx, `SELECT r.id::text,r.name,r.system,r.permissions FROM roles r
			WHERE r.workspace_id=$1 AND r.deleted_at IS NULL AND (r.id::text=$2 OR ($2='' AND lower(r.name)=lower($3)))`,
			workspaceID, request.RoleID, strings.TrimSpace(request.RoleName)).Scan(&role.ID, &role.Name, &role.System, &role.Permissions)
		if errors.Is(err, pgx.ErrNoRows) {
			return AgentToken{}, domain.ErrRoleNotFound
		}
		if err != nil {
			return AgentToken{}, fmt.Errorf("find role: %w", err)
		}
		given = role.permissions().Sorted()
		if missing := held.Missing(given); len(missing) > 0 {
			return AgentToken{}, &domain.ExceedsYoursError{Missing: missing}
		}
		scope, roleID = ScopeRole, role.ID
	} else if missing := held.Missing(request.Permissions); len(missing) > 0 {
		return AgentToken{}, &domain.ExceedsYoursError{Missing: missing}
	}
	raw, hash, err := newAgentToken()
	if err != nil {
		return AgentToken{}, err
	}
	// role_ceiling is kept for a rollback: admin when the token holds what only the old admin tier held.
	ceiling := "agent"
	if legacyAdminTier(given) {
		ceiling = "admin"
	}
	var permissions []string
	if scope == ScopeCustom {
		permissions = request.Permissions
	}
	// created_at comes from the service clock: the one-year limit is counted from it.
	now := s.now()
	session, err := scanSession(tx.QueryRow(ctx, `WITH s AS (
			INSERT INTO sessions(workspace_id,agent_id,token_hash,role_ceiling,scope,permissions,role_id,label,created_at,last_seen_at,expires_at)
			VALUES($1,$2,$3,$4,$5,$6,NULLIF($7,'')::uuid,$8,$9,$9,$10) RETURNING *)
		SELECT `+sessionColumns+` FROM s`+sessionJoins,
		workspaceID, agentID, hash, ceiling, scope, permissions, roleID, request.Label, now, now.Add(sessionLifetime)))
	if err != nil {
		return AgentToken{}, fmt.Errorf("create agent token: %w", err)
	}
	return AgentToken{Token: raw, Session: session}, nil
}
