package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/workspacehost"
	"github.com/jackc/pgx/v5"
)

// Invite link states a preview reports.
const (
	InvitePending = "pending"
	InviteExpired = "expired"
	InviteRevoked = "revoked"
	InviteUsed    = "used"
)

// InvitePreview is what an invite link is, read without using it: the invite page shows it before the
// person joins, and says why a link stopped working.
type InvitePreview struct {
	State         string    `json:"state"`
	WorkspaceName string    `json:"workspace_name"`
	InvitedBy     string    `json:"invited_by"`
	Email         string    `json:"email"`
	Roles         []string  `json:"roles"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// ErrNotAnInvite answers a link a preview does not describe: a pairing or hand-off link, or another
// workspace's invite on a workspace's own host. The API answers it as it answers ErrUnknownLink, so the
// answer tells nobody whether a token exists.
var ErrNotAnInvite = errors.New("the token is not an invite link")

// ErrUnknownLink: no one-time link has the token (mistyped, made up, or deleted). Only these count toward
// a client's limit of refused links: a real link never does.
var ErrUnknownLink = errors.New("no link has this token")

// PreviewInvite describes the invite link token without using it up: it only reads.
func (s *Service) PreviewInvite(ctx context.Context, token string) (InvitePreview, error) {
	linkWorkspace, kind, err := s.linkOf(ctx, token)
	if err != nil {
		return InvitePreview{}, err
	}
	if kind != "invite" {
		return InvitePreview{}, ErrNotAnInvite
	}
	var preview InvitePreview
	var slug string
	var used, revoked bool
	err = s.db.InWorkspace(ctx, linkWorkspace, func(tx pgx.Tx) error {
		// The inviter is named by their display name, else their email; an inviter since removed leaves the
		// workspace's name, so the line still says whom to ask.
		return tx.QueryRow(ctx, `SELECT w.slug, w.name, COALESCE(NULLIF(a.display_name,''), a.email, w.name), p.email,
			COALESCE((SELECT array_agg(r.name ORDER BY CASE r.system WHEN 'admin' THEN 0 WHEN 'agent' THEN 1 ELSE 2 END, lower(r.name), r.id)
				FROM invite_roles i JOIN roles r ON r.workspace_id=i.workspace_id AND r.id=i.role_id
				WHERE i.workspace_id=p.workspace_id AND i.link_id=p.id), '{}'),
			p.expires_at, p.used_at IS NOT NULL, p.revoked_at IS NOT NULL
			FROM pairing_links p JOIN workspaces w ON w.id=p.workspace_id
			LEFT JOIN agents a ON a.workspace_id=p.workspace_id AND a.id=p.created_by_agent_id
			WHERE p.workspace_id=$1 AND p.token_hash=$2 AND p.kind='invite'`, linkWorkspace, tokenHash(token)).
			Scan(&slug, &preview.WorkspaceName, &preview.InvitedBy, &preview.Email, &preview.Roles, &preview.ExpiresAt, &used, &revoked)
	})
	if err != nil {
		return InvitePreview{}, fmt.Errorf("read invite: %w", err)
	}
	// On a workspace's own host only that workspace's links are described, as only they redeem there.
	if hostSlug, onWorkspaceHost := workspacehost.Slug(ctx); onWorkspaceHost && hostSlug != slug {
		return InvitePreview{}, ErrNotAnInvite
	}
	preview.ExpiresAt = domain.UTC(preview.ExpiresAt)
	// The same test as Redeem: a link works while expires_at is still ahead.
	switch {
	case used:
		preview.State = InviteUsed
	case revoked:
		preview.State = InviteRevoked
	case !preview.ExpiresAt.After(s.now()):
		preview.State = InviteExpired
	default:
		preview.State = InvitePending
	}
	return preview, nil
}
