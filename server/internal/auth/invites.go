package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/hclabsco/colliedesk/server/internal/domain"
)

func (s *Service) CreateInvite(ctx context.Context, principal Principal, email, ceiling string) (domain.Invite, string, error) {
	if !CanDelegate(principal.Agent.Role, ceiling) || !strings.Contains(email, "@") {
		return domain.Invite{}, "", fmt.Errorf("invalid invite grant")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Invite{}, "", fmt.Errorf("begin invite: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	link, linkID, err := s.createLinkTx(ctx, tx, principal.Workspace.ID, "", strings.ToLower(email), "invite", ceiling, &principal.Agent.ID)
	if err != nil {
		return domain.Invite{}, "", err
	}
	var invite domain.Invite
	err = tx.QueryRow(ctx, `SELECT p.id::text,p.email,p.role_ceiling,a.email,p.created_at,p.expires_at,p.used_at,p.revoked_at
		FROM pairing_links p JOIN agents a ON a.id=p.created_by_agent_id
		WHERE p.workspace_id=$1 AND p.id=$2`, principal.Workspace.ID, linkID).
		Scan(&invite.ID, &invite.Email, &invite.RoleCeiling, &invite.CreatedByEmail, &invite.CreatedAt, &invite.ExpiresAt, &invite.UsedAt, &invite.RevokedAt)
	if err != nil {
		return domain.Invite{}, "", fmt.Errorf("load invite: %w", err)
	}
	normalizeInvite(&invite)
	if err := tx.Commit(ctx); err != nil {
		return domain.Invite{}, "", fmt.Errorf("commit invite: %w", err)
	}
	return invite, link, nil
}

func (s *Service) ListInvites(ctx context.Context, workspaceID string) ([]domain.Invite, error) {
	rows, err := s.pool.Query(ctx, `SELECT p.id::text,p.email,p.role_ceiling,a.email,p.created_at,p.expires_at,p.used_at,p.revoked_at
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
	result, err := s.pool.Exec(ctx, `UPDATE pairing_links SET revoked_at=COALESCE(revoked_at,$3) WHERE id=$1 AND workspace_id=$2 AND kind='invite'`, inviteID, workspaceID, s.now())
	if err != nil {
		return false, fmt.Errorf("revoke invite: %w", err)
	}
	return result.RowsAffected() == 1, nil
}
