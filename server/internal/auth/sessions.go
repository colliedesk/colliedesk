package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

// ErrInvalidCredential wraps every refusal of a credential: missing, unknown, revoked, expired, an agent
// token past its maximum age, or an agent token sent as a cookie. Any other error from Authenticate is a
// failure of the system, and a caller must not tell the client its credential is bad.
var ErrInvalidCredential = errors.New("invalid credential")

// lastSeenInterval is how old last_seen_at must be before a request writes it again. One agent tool call
// authenticates several times in a moment; writing the row each time made those requests queue on it.
const lastSeenInterval = time.Minute

func (s *Service) Authenticate(ctx context.Context, raw string, viaCookie bool) (Principal, error) {
	if raw == "" {
		return Principal{}, fmt.Errorf("%w: missing credential", ErrInvalidCredential)
	}
	now := s.now()
	hash := credentialHash(raw)
	// Only the token-to-workspace lookup crosses workspaces; the session checks run scoped.
	var workspaceID string
	err := s.db.Privileged(ctx, database.PrivilegeSessionToken, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT workspace_id::text FROM sessions WHERE token_hash=$1`, hash).Scan(&workspaceID)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, fmt.Errorf("%w: no such session", ErrInvalidCredential)
	}
	if err != nil {
		return Principal{}, fmt.Errorf("authenticate session: %w", err)
	}
	principal := Principal{ViaCookie: viaCookie}
	var trialEndsAt, createdAt, lastSeenAt time.Time
	var trialExtended bool
	var activeAgents int
	// The active agents are counted only where plan limits apply, for the seat pause below.
	countAgents := "0"
	if s.planLimits {
		countAgents = "(SELECT count(*) FROM agents x WHERE x.workspace_id=s.workspace_id AND x.deactivated_at IS NULL)"
	}
	err = s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT w.id::text,w.slug,w.name,w.timezone,a.id::text,a.email,a.display_name,
			CASE WHEN a.role='agent' OR s.role_ceiling='agent' THEN 'agent' ELSE 'admin' END,a.timezone_override,s.id::text,w.trial_ends_at,COALESCE(s.scope,''),w.trial_extended_at IS NOT NULL,s.created_at,s.last_seen_at,`+countAgents+`
			FROM sessions s JOIN agents a ON a.id=s.agent_id AND a.workspace_id=s.workspace_id JOIN workspaces w ON w.id=s.workspace_id
			WHERE s.workspace_id=$3 AND s.token_hash=$1 AND s.revoked_at IS NULL AND s.expires_at>$2 AND a.deactivated_at IS NULL`, hash, now, workspaceID).
			Scan(&principal.Workspace.ID, &principal.Workspace.Slug, &principal.Workspace.Name, &principal.Workspace.Timezone,
				&principal.Agent.ID, &principal.Agent.Email, &principal.Agent.DisplayName, &principal.Agent.Role,
				&principal.Agent.TimezoneOverride, &principal.SessionID, &trialEndsAt, &principal.Scope, &trialExtended, &createdAt, &lastSeenAt, &activeAgents)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: session revoked, expired or not found", ErrInvalidCredential)
		}
		if err != nil {
			return fmt.Errorf("authenticate session: %w", err)
		}
		// A browser attaches cookies by itself; an agent token is only ever sent on purpose, as a bearer.
		if viaCookie && principal.Scope != "" {
			return fmt.Errorf("%w: agent tokens are not accepted as cookies", ErrInvalidCredential)
		}
		expires := now.Add(sessionLifetime)
		if principal.Scope != "" {
			// A token that is used every day still ends one year after its creation.
			limit := createdAt.Add(agentTokenMaxAge)
			if !now.Before(limit) {
				return fmt.Errorf("%w: agent token is older than its maximum age", ErrInvalidCredential)
			}
			if limit.Before(expires) {
				expires = limit
			}
		}
		if sinceSeen := now.Sub(lastSeenAt); sinceSeen < 0 || sinceSeen >= lastSeenInterval {
			if _, err := tx.Exec(ctx, `UPDATE sessions SET last_seen_at=$2,expires_at=$3 WHERE id=$1 AND workspace_id=$4`,
				principal.SessionID, now, expires, workspaceID); err != nil {
				return fmt.Errorf("refresh session: %w", err)
			}
		}
		if s.planLimits {
			subscriptions, err := store.LoadSubscriptions(ctx, tx, workspaceID)
			if err != nil {
				return fmt.Errorf("load subscriptions: %w", err)
			}
			principal.Access = domain.NewAccess(subscriptions, trialEndsAt, now)
			principal.Access.TrialExtended = trialExtended
			// After a move to a smaller plan the newest agents beyond the cap are paused. The rank is
			// read only then, so a workspace that fits pays one count in the query above.
			if limit := domain.LimitsFor(principal.Access.Plan).Agents; !principal.Access.Locked && activeAgents > limit {
				principal.Access.AgentsOver = activeAgents - limit
				rank, err := store.AgentSeatRank(ctx, tx, workspaceID, principal.Agent.ID)
				if err != nil {
					return fmt.Errorf("rank agent seat: %w", err)
				}
				principal.Access.SeatPaused = rank > limit
			}
		}
		return nil
	})
	if err != nil {
		return Principal{}, err
	}
	return principal, nil
}

func (s *Service) ListSessions(ctx context.Context, principal Principal) ([]domain.Session, error) {
	query := `SELECT ` + sessionColumns + `
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
	for index := range sessions {
		sessions[index].Current = sessions[index].ID == principal.SessionID
	}
	return sessions, err
}

// sessionColumns is the select list scanSession reads; the query joins sessions s to agents a.
const sessionColumns = `s.id::text,s.agent_id::text,a.email,s.label,s.scope,s.created_at,s.last_seen_at,s.expires_at,s.revoked_at`

func scanSession(row pgx.Row) (domain.Session, error) {
	var session domain.Session
	if err := row.Scan(&session.ID, &session.AgentID, &session.AgentEmail, &session.Label, &session.Scope, &session.CreatedAt, &session.LastSeenAt, &session.ExpiresAt, &session.RevokedAt); err != nil {
		return domain.Session{}, fmt.Errorf("scan session: %w", err)
	}
	session.CreatedAt = domain.UTC(session.CreatedAt)
	session.LastSeenAt = domain.UTC(session.LastSeenAt)
	session.ExpiresAt = domain.UTC(session.ExpiresAt)
	session.RevokedAt = domain.UTCOptional(session.RevokedAt)
	return session, nil
}

func listSessions(ctx context.Context, tx pgx.Tx, query string, args []any) ([]domain.Session, error) {
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()
	sessions := make([]domain.Session, 0)
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
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
