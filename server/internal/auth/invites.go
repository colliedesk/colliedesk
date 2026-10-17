package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

func (s *Service) CreateInvite(ctx context.Context, principal Principal, email, ceiling string) (domain.Invite, string, error) {
	if !CanDelegate(principal.Agent.Role, ceiling) || !strings.Contains(email, "@") {
		return domain.Invite{}, "", fmt.Errorf("invalid invite grant")
	}
	tx, err := s.db.BeginWorkspace(ctx, principal.Workspace.ID)
	if err != nil {
		return domain.Invite{}, "", fmt.Errorf("begin invite: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := requireActiveAgent(ctx, tx, principal.Workspace.ID, nil, strings.ToLower(email)); err != nil {
		return domain.Invite{}, "", err
	}
	if s.planLimits {
		if err := store.CheckAgentSeat(ctx, tx, principal.Workspace.ID, s.now(), strings.ToLower(email), "", s.team25Offered); err != nil {
			return domain.Invite{}, "", err
		}
	}
	link, err := s.createLinkTx(ctx, tx, principal.Workspace.ID, "", strings.ToLower(email), "invite", ceiling, &principal.Agent.ID)
	if err != nil {
		return domain.Invite{}, "", err
	}
	var invite domain.Invite
	err = tx.QueryRow(ctx, `SELECT p.id::text,p.email,p.role_ceiling,a.email,p.created_at,p.expires_at,p.used_at,p.revoked_at
		FROM pairing_links p JOIN agents a ON a.id=p.created_by_agent_id
		WHERE p.workspace_id=$1 AND p.id=$2`, principal.Workspace.ID, link.ID).
		Scan(&invite.ID, &invite.Email, &invite.RoleCeiling, &invite.CreatedByEmail, &invite.CreatedAt, &invite.ExpiresAt, &invite.UsedAt, &invite.RevokedAt)
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
	rows, err := tx.Query(ctx, `SELECT p.id::text,p.email,p.role_ceiling,a.email,p.created_at,p.expires_at,p.used_at,p.revoked_at
		FROM pairing_links p JOIN agents a ON a.id=p.created_by_agent_id
		WHERE p.workspace_id=$1 AND p.kind='invite' ORDER BY p.created_at DESC`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list invites: %w", err)
	}
	defer rows.Close()
	invites := make([]domain.Invite, 0)
	for rows.Next() {
		var invite domain.Invite
		if err := rows.Scan(&invite.ID, &invite.Email, &invite.RoleCeiling, &invite.CreatedByEmail, &invite.CreatedAt, &invite.ExpiresAt, &invite.UsedAt, &invite.RevokedAt); err != nil {
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
