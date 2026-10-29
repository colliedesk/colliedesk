package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5"
)

// The fixed callback addresses registered with providers (Google, Microsoft, GitHub) are on APP_ORIGIN,
// while the session of the person who started a consent lives on the workspace's own host. These
// lookups find only the slug of the workspace a live state (or an email channel) belongs to, so the
// callback can send the browser there; the state is spent, and the person checked, by the callback on
// that host, inside the workspace.

// CallbackStateSlug is the slug of the workspace that started the live state at provider: a mailbox
// consent ("gmail", "microsoft") or, for provider "github", a GitHub authorization. "" when no live
// state matches.
func (s *Store) CallbackStateSlug(ctx context.Context, provider, state string, now time.Time) (string, error) {
	if state == "" {
		return "", nil
	}
	query := `SELECT w.slug FROM mailbox_oauth_states o JOIN workspaces w ON w.id=o.workspace_id
		WHERE o.state_hash=$1 AND o.provider=$2 AND o.expires_at>$3`
	args := []any{oauthStateHash(state), provider, now}
	if provider == "github" {
		query = `SELECT w.slug FROM autofix_oauth_states o JOIN workspaces w ON w.id=o.workspace_id
			WHERE o.state_hash=$1 AND o.expires_at>$2`
		args = []any{oauthStateHash(state), now}
	}
	return s.callbackSlug(ctx, query, args...)
}

// ChannelSlug is the slug of the workspace whose email channel has channelID, or "".
func (s *Store) ChannelSlug(ctx context.Context, channelID string) (string, error) {
	return s.callbackSlug(ctx, `SELECT w.slug FROM email_channels c JOIN workspaces w ON w.id=c.workspace_id WHERE c.channel_id::text=$1`, channelID)
}

func (s *Store) callbackSlug(ctx context.Context, query string, args ...any) (string, error) {
	var slug string
	err := s.db.Privileged(ctx, database.PrivilegeCallbackRouting, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, query, args...).Scan(&slug)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("find the workspace of a callback: %w", err)
	}
	return slug, nil
}
