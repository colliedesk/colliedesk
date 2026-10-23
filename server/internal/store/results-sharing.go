package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/results"
	"github.com/jackc/pgx/v5"
)

// ErrShareLinkOff: a results share link that was turned off, replaced, never existed, or belongs to a
// locked workspace. Every case answers the same, so the answer says nothing about the workspace.
var ErrShareLinkOff = errors.New("this link was turned off")

// ResultsShareLink is the active share link of a workspace; never the token, which is stored as a hash.
type ResultsShareLink struct {
	CreatedAt     time.Time  `json:"created_at"`
	CreatedByName *string    `json:"created_by_name"`
	LastViewedAt  *time.Time `json:"last_viewed_at"`
}

func shareTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// ResultsShareLink returns the active link, or nil.
func (s *Store) ResultsShareLink(ctx context.Context, workspaceID string) (*ResultsShareLink, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (*ResultsShareLink, error) {
		return activeShareLink(ctx, tx, workspaceID)
	})
}

func activeShareLink(ctx context.Context, tx pgx.Tx, workspaceID string) (*ResultsShareLink, error) {
	var link ResultsShareLink
	err := tx.QueryRow(ctx, `SELECT l.created_at,NULLIF(COALESCE(NULLIF(a.display_name,''),a.email),''),l.last_viewed_at
		FROM results_share_links l LEFT JOIN agents a ON a.workspace_id=l.workspace_id AND a.id=l.created_by
		WHERE l.workspace_id=$1 AND l.revoked_at IS NULL`, workspaceID).Scan(&link.CreatedAt, &link.CreatedByName, &link.LastViewedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the share link: %w", err)
	}
	link.CreatedAt = link.CreatedAt.UTC()
	if link.LastViewedAt != nil {
		viewed := link.LastViewedAt.UTC()
		link.LastViewedAt = &viewed
	}
	return &link, nil
}

// CreateResultsShareLink turns the old link off and makes a new one; the token is returned only now.
func (s *Store) CreateResultsShareLink(ctx context.Context, workspaceID, actorID string) (string, ResultsShareLink, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", ResultsShareLink{}, fmt.Errorf("generate share link: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	link, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (ResultsShareLink, error) {
		if _, err := tx.Exec(ctx, `UPDATE results_share_links SET revoked_at=now(),revoked_by=NULLIF($2,'')::uuid WHERE workspace_id=$1 AND revoked_at IS NULL`,
			workspaceID, actorID); err != nil {
			return ResultsShareLink{}, fmt.Errorf("turn off the old share link: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO results_share_links(workspace_id,token_hash,created_by) VALUES($1,$2,NULLIF($3,'')::uuid)`,
			workspaceID, shareTokenHash(token), actorID); err != nil {
			return ResultsShareLink{}, fmt.Errorf("create the share link: %w", err)
		}
		link, err := activeShareLink(ctx, tx, workspaceID)
		if err != nil || link == nil {
			return ResultsShareLink{}, fmt.Errorf("read the new share link: %w", err)
		}
		return *link, nil
	})
	return token, link, err
}

// RevokeResultsShareLink turns the active link off; with none it changes nothing.
func (s *Store) RevokeResultsShareLink(ctx context.Context, workspaceID, actorID string) error {
	_, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (struct{}, error) {
		_, err := tx.Exec(ctx, `UPDATE results_share_links SET revoked_at=now(),revoked_by=NULLIF($2,'')::uuid WHERE workspace_id=$1 AND revoked_at IS NULL`,
			workspaceID, actorID)
		return struct{}{}, err
	})
	return err
}

// SharedResults reads what a share link shows. The token's workspace is found by its hash (the only
// cross-workspace read), then everything is read in that workspace. The visit is recorded at most once
// an hour, so a public page cannot make the database write without limit.
func (s *Store) SharedResults(ctx context.Context, token string, now time.Time) (results.Shared, error) {
	var workspaceID string
	err := s.db.Privileged(ctx, database.PrivilegeResultsShareToken, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT workspace_id::text FROM results_share_links WHERE token_hash=$1 AND revoked_at IS NULL`, shareTokenHash(token)).Scan(&workspaceID)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return results.Shared{}, ErrShareLinkOff
	}
	if err != nil {
		return results.Shared{}, fmt.Errorf("find the share link: %w", err)
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (results.Shared, error) {
		// On the hosted cloud, a locked workspace (trial or plan ended) shares nothing.
		if s.limits {
			access, err := workspaceAccess(ctx, tx, workspaceID, now)
			if err != nil {
				return results.Shared{}, err
			}
			if access.Locked {
				return results.Shared{}, ErrShareLinkOff
			}
		}
		var name string
		if err := tx.QueryRow(ctx, `SELECT name FROM workspaces WHERE id=$1`, workspaceID).Scan(&name); err != nil {
			return results.Shared{}, fmt.Errorf("read the workspace: %w", err)
		}
		page, err := results.Read(ctx, tx, workspaceID, results.Viewer{Now: now})
		if err != nil {
			return results.Shared{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE results_share_links SET last_viewed_at=$3 WHERE workspace_id=$1 AND token_hash=$2
			AND (last_viewed_at IS NULL OR last_viewed_at<$3::timestamptz - interval '1 hour')`, workspaceID, shareTokenHash(token), now); err != nil {
			return results.Shared{}, fmt.Errorf("record the visit: %w", err)
		}
		return results.SharedFrom(page, name), nil
	})
}

// SetResultsEmail turns the agent's weekly results email on or off.
func (s *Store) SetResultsEmail(ctx context.Context, workspaceID, agentID string, weekly bool) error {
	_, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (struct{}, error) {
		_, err := tx.Exec(ctx, `UPDATE agents SET results_email=$3 WHERE workspace_id=$1 AND id=$2`, workspaceID, agentID, weekly)
		return struct{}{}, err
	})
	return err
}
