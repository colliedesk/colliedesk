package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

// ErrInvalidInviteEmail: an invite needs an email address.
var ErrInvalidInviteEmail = errors.New("an invite needs an email address")

// CreateInvite invites email with roles (by ids or by names; the Agent role when neither is given). The
// caller must hold every permission of those roles (*domain.ExceedsYoursError).
func (s *Service) CreateInvite(ctx context.Context, principal Principal, email string, roleIDs, roleNames []string) (domain.Invite, string, error) {
	if !strings.Contains(email, "@") {
		return domain.Invite{}, "", ErrInvalidInviteEmail
	}
	tx, err := s.db.BeginWorkspace(ctx, principal.Workspace.ID)
	if err != nil {
		return domain.Invite{}, "", fmt.Errorf("begin invite: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := requireActiveAgent(ctx, tx, principal.Workspace.ID, nil, strings.ToLower(email)); err != nil {
		return domain.Invite{}, "", err
	}
	if len(roleIDs) == 0 && len(roleNames) == 0 {
		roleNames = []string{"Agent"}
		if err := tx.QueryRow(ctx, `SELECT name FROM roles WHERE workspace_id=$1 AND system='agent'`, principal.Workspace.ID).Scan(&roleNames[0]); err != nil {
			return domain.Invite{}, "", fmt.Errorf("find the Agent role: %w", err)
		}
	}
	roles, err := store.ResolveRoles(ctx, tx, principal.Workspace.ID, roleIDs, roleNames)
	if err != nil {
		return domain.Invite{}, "", err
	}
	ceiling, ids, err := checkRolesWithin(ctx, tx, principal, roles)
	if err != nil {
		return domain.Invite{}, "", err
	}
	if s.planLimits {
		if err := store.CheckAgentSeat(ctx, tx, principal.Workspace.ID, s.now(), strings.ToLower(email), "", s.plansOnSale); err != nil {
			return domain.Invite{}, "", err
		}
	}
	link, err := s.createLinkTx(ctx, tx, principal.Workspace.ID, "", strings.ToLower(email), "invite", ceiling, &principal.Agent.ID)
	if err != nil {
		return domain.Invite{}, "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO invite_roles(workspace_id,link_id,role_id) SELECT $1,$2,unnest($3::uuid[])`, principal.Workspace.ID, link.ID, ids); err != nil {
		return domain.Invite{}, "", fmt.Errorf("invite roles: %w", err)
	}
	var invite domain.Invite
	err = tx.QueryRow(ctx, `SELECT `+inviteColumns+`
		FROM pairing_links p JOIN agents a ON a.id=p.created_by_agent_id AND a.workspace_id=p.workspace_id
		WHERE p.workspace_id=$1 AND p.id=$2`, principal.Workspace.ID, link.ID).
		Scan(&invite.ID, &invite.Email, &invite.Roles, &invite.CreatedByEmail, &invite.CreatedAt, &invite.ExpiresAt, &invite.UsedAt, &invite.RevokedAt)
	if err != nil {
		return domain.Invite{}, "", fmt.Errorf("load invite: %w", err)
	}
	normalizeInvite(&invite)
	if err := tx.Commit(ctx); err != nil {
		return domain.Invite{}, "", fmt.Errorf("commit invite: %w", err)
	}
	return invite, link.URL, nil
}

func (s *Service) ListInvites(ctx context.Context, workspaceID string) ([]domain.Invite, error) {
	var invites []domain.Invite
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		var err error
		invites, err = listInvites(ctx, tx, workspaceID)
		return err
	})
	return invites, err
}

func listInvites(ctx context.Context, tx pgx.Tx, workspaceID string) ([]domain.Invite, error) {
	rows, err := tx.Query(ctx, `SELECT `+inviteColumns+`
		FROM pairing_links p JOIN agents a ON a.id=p.created_by_agent_id AND a.workspace_id=p.workspace_id
		WHERE p.workspace_id=$1 AND p.kind='invite' ORDER BY p.created_at DESC`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list invites: %w", err)
	}
	defer rows.Close()
	invites := make([]domain.Invite, 0)
	for rows.Next() {
		var invite domain.Invite
		if err := rows.Scan(&invite.ID, &invite.Email, &invite.Roles, &invite.CreatedByEmail, &invite.CreatedAt, &invite.ExpiresAt, &invite.UsedAt, &invite.RevokedAt); err != nil {
			return nil, fmt.Errorf("scan invite: %w", err)
		}
		normalizeInvite(&invite)
		invites = append(invites, invite)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate invites: %w", err)
	}
	return invites, nil
}

// inviteColumns reads an invite (pairing_links p, its creator a) with the roles it gives.
const inviteColumns = `p.id::text,p.email,COALESCE((SELECT json_agg(json_build_object('id',r.id,'name',r.name,'system',r.system)
		ORDER BY CASE r.system WHEN 'admin' THEN 0 WHEN 'agent' THEN 1 ELSE 2 END, lower(r.name), r.id)
	FROM invite_roles i JOIN roles r ON r.workspace_id=i.workspace_id AND r.id=i.role_id
	WHERE i.workspace_id=p.workspace_id AND i.link_id=p.id),'[]'::json),a.email,p.created_at,p.expires_at,p.used_at,p.revoked_at`

// checkRolesWithin refuses roles holding a permission the principal lacks; it returns the legacy role
// ceiling to record (admin when the Admin role is among them) and the role ids.
func checkRolesWithin(ctx context.Context, tx pgx.Tx, principal Principal, roles []domain.RoleRef) (string, []string, error) {
	ceiling := "agent"
	ids := make([]string, len(roles))
	var wanted []string
	for index, role := range roles {
		ids[index] = role.ID
		if role.IsAdmin() {
			ceiling = "admin"
			wanted = append(wanted, AllPermissions()...)
			continue
		}
		var permissions []string
		if err := tx.QueryRow(ctx, `SELECT COALESCE(permissions,'{}') FROM roles WHERE workspace_id=$1 AND id=$2`, principal.Workspace.ID, role.ID).Scan(&permissions); err != nil {
			return "", nil, fmt.Errorf("role permissions: %w", err)
		}
		wanted = append(wanted, permissions...)
	}
	if missing := principal.Missing(wanted); len(missing) > 0 {
		return "", nil, &domain.ExceedsYoursError{Missing: missing}
	}
	return ceiling, ids, nil
}

func normalizeInvite(invite *domain.Invite) {
	invite.CreatedAt = domain.UTC(invite.CreatedAt)
	invite.ExpiresAt = domain.UTC(invite.ExpiresAt)
	invite.UsedAt = domain.UTCOptional(invite.UsedAt)
	invite.RevokedAt = domain.UTCOptional(invite.RevokedAt)
}

func (s *Service) RevokeInvite(ctx context.Context, workspaceID, inviteID string) (bool, error) {
	var revoked bool
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		result, err := tx.Exec(ctx, `UPDATE pairing_links SET revoked_at=COALESCE(revoked_at,$3) WHERE id=$1 AND workspace_id=$2 AND kind='invite'`, inviteID, workspaceID, s.now())
		revoked = result.RowsAffected() == 1
		return err
	})
	if err != nil {
		return false, fmt.Errorf("revoke invite: %w", err)
	}
	return revoked, nil
}
