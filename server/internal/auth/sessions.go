package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

func (s *Service) Authenticate(ctx context.Context, raw string, viaCookie bool) (Principal, error) {
	if raw == "" {
		return Principal{}, fmt.Errorf("missing credential")
	}
	now := s.now()
	hash := tokenHash(raw)
	// Only the token-to-workspace lookup crosses workspaces; the session checks run scoped.
	var workspaceID string
	err := s.db.Privileged(ctx, database.PrivilegeSessionToken, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT workspace_id::text FROM sessions WHERE token_hash=$1`, hash).Scan(&workspaceID)
	})
	if err != nil {
		return Principal{}, fmt.Errorf("authenticate session: %w", err)
	}
	principal := Principal{ViaCookie: viaCookie}
	var trialEndsAt time.Time
	err = s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT w.id::text,w.slug,w.name,w.timezone,a.id::text,a.email,a.display_name,
			CASE WHEN a.role='agent' OR s.role_ceiling='agent' THEN 'agent' ELSE 'admin' END,a.timezone_override,s.id::text,w.trial_ends_at
			FROM sessions s JOIN agents a ON a.id=s.agent_id AND a.workspace_id=s.workspace_id JOIN workspaces w ON w.id=s.workspace_id
			WHERE s.workspace_id=$3 AND s.token_hash=$1 AND s.revoked_at IS NULL AND s.expires_at>$2`, hash, now, workspaceID).
			Scan(&principal.Workspace.ID, &principal.Workspace.Slug, &principal.Workspace.Name, &principal.Workspace.Timezone,
				&principal.Agent.ID, &principal.Agent.Email, &principal.Agent.DisplayName, &principal.Agent.Role,
				&principal.Agent.TimezoneOverride, &principal.SessionID, &trialEndsAt)
		if err != nil {
			return fmt.Errorf("authenticate session: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE sessions SET last_seen_at=$2,expires_at=$3 WHERE id=$1 AND workspace_id=$4`,
			principal.SessionID, now, now.Add(sessionLifetime), workspaceID); err != nil {
			return fmt.Errorf("refresh session: %w", err)
		}
		if s.planLimits {
			subscriptions, err := store.LoadSubscriptions(ctx, tx, workspaceID)
			if err != nil {
				return fmt.Errorf("load subscriptions: %w", err)
			}
			principal.Access = domain.NewAccess(subscriptions, trialEndsAt, now)
		}
		return nil
	})
	if err != nil {
		return Principal{}, err
	}
	return principal, nil
}

func (s *Service) ListSessions(ctx context.Context, principal Principal) ([]domain.Session, error) {
	query := `SELECT s.id::text,s.agent_id::text,a.email,s.label,s.created_at,s.last_seen_at,s.expires_at,s.revoked_at
		FROM sessions s JOIN agents a ON a.id=s.agent_id AND a.workspace_id=s.workspace_id WHERE s.workspace_id=$1`
	args := []any{principal.Workspace.ID}
	if principal.Agent.Role != "admin" {
		query += " AND s.agent_id=$2"
		args = append(args, principal.Agent.ID)
	}
	query += " ORDER BY s.last_seen_at DESC"
	var sessions []domain.Session
	err := s.db.InWorkspace(ctx, principal.Workspace.ID, func(tx pgx.Tx) error {
		var err error
		sessions, err = listSessions(ctx, tx, query, args)
		return err
	})
	return sessions, err
}

func listSessions(ctx context.Context, tx pgx.Tx, query string, args []any) ([]domain.Session, error) {
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()
	sessions := make([]domain.Session, 0)
	for rows.Next() {
		var session domain.Session
		if err := rows.Scan(&session.ID, &session.AgentID, &session.AgentEmail, &session.Label, &session.CreatedAt, &session.LastSeenAt, &session.ExpiresAt, &session.RevokedAt); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		session.CreatedAt = domain.UTC(session.CreatedAt)
		session.LastSeenAt = domain.UTC(session.LastSeenAt)
		session.ExpiresAt = domain.UTC(session.ExpiresAt)
		session.RevokedAt = domain.UTCOptional(session.RevokedAt)
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sessions: %w", err)
	}
	return sessions, nil
}

func (s *Service) RevokeSession(ctx context.Context, principal Principal, sessionID string) (bool, error) {
	command := `UPDATE sessions SET revoked_at=COALESCE(revoked_at,$3) WHERE id=$1 AND workspace_id=$2`
	args := []any{sessionID, principal.Workspace.ID, s.now()}
	if principal.Agent.Role != "admin" {
		command += " AND agent_id=$4"
		args = append(args, principal.Agent.ID)
	}
	var revoked bool
	err := s.db.InWorkspace(ctx, principal.Workspace.ID, func(tx pgx.Tx) error {
		result, err := tx.Exec(ctx, command, args...)
		revoked = result.RowsAffected() == 1
		return err
	})
	if err != nil {
		return false, fmt.Errorf("revoke session: %w", err)
	}
	return revoked, nil
}

func (s *Service) Logout(ctx context.Context, principal Principal) error {
	err := s.db.InWorkspace(ctx, principal.Workspace.ID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at=COALESCE(revoked_at,$2) WHERE id=$1 AND workspace_id=$3`,
			principal.SessionID, s.now(), principal.Workspace.ID)
		return err
	})
	if err != nil {
		return fmt.Errorf("logout: %w", err)
	}
	return nil
}
