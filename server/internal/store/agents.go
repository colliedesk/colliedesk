package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// openAssigned is the work a deactivation moves: conversations still being handled.
const openAssigned = `status IN ('open','pending')`

func (s *Store) ListAgents(ctx context.Context, workspaceID string) ([]domain.TeamMember, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.TeamMember, error) { return listAgents(ctx, tx, workspaceID) })
}

func listAgents(ctx context.Context, tx pgx.Tx, workspaceID string) ([]domain.TeamMember, error) {
	rows, err := tx.Query(ctx, teamMemberSelect+` WHERE a.workspace_id=$1 ORDER BY a.email`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()
	members := make([]domain.TeamMember, 0)
	for rows.Next() {
		member, err := scanTeamMember(rows)
		if err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agents: %w", err)
	}
	return members, nil
}

const teamMemberSelect = `SELECT a.id::text,a.email,a.display_name,a.role,a.timezone_override,a.deactivated_at,
	(SELECT max(s.last_seen_at) FROM sessions s WHERE s.workspace_id=a.workspace_id AND s.agent_id=a.id),
	(SELECT count(*) FROM conversations c WHERE c.workspace_id=a.workspace_id AND c.assignee_id=a.id AND c.` + openAssigned + `)
	FROM agents a`

func scanTeamMember(row pgx.Row) (domain.TeamMember, error) {
	var member domain.TeamMember
	if err := row.Scan(&member.ID, &member.Email, &member.DisplayName, &member.Role, &member.TimezoneOverride,
		&member.DeactivatedAt, &member.LastActiveAt, &member.OpenConversations); err != nil {
		return domain.TeamMember{}, fmt.Errorf("scan agent: %w", err)
	}
	member.DeactivatedAt = domain.UTCOptional(member.DeactivatedAt)
	member.LastActiveAt = domain.UTCOptional(member.LastActiveAt)
	return member, nil
}

// LockActiveAgent reports whether agentID is an active agent of the workspace and, when it is, holds a
// share lock on the agent row until tx ends. The lock conflicts with the row lock UpdateAgent takes, so an
// assignment either finishes before a deactivation starts or sees the deactivation. Every path that makes
// an agent an assignee (one conversation or many) must use it instead of a plain read.
func LockActiveAgent(ctx context.Context, tx pgx.Tx, workspaceID, agentID string) (bool, error) {
	var found int
	err := tx.QueryRow(ctx, `SELECT 1 FROM agents WHERE workspace_id=$1 AND id=$2 AND deactivated_at IS NULL FOR SHARE`,
		workspaceID, agentID).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check active agent: %w", err)
	}
	return true, nil
}

// ValidateAssignee refuses an assignee that is not an active agent of the workspace, holding the share
// lock of LockActiveAgent until tx ends. Lock order for every writer: the agent row first, then the
// conversations (ordered by id), then conversation_tags. Call it before the first conversation row is
// touched; one-conversation and bulk assignment share it.
func ValidateAssignee(ctx context.Context, tx pgx.Tx, workspaceID, agentID string) error {
	active, err := LockActiveAgent(ctx, tx, workspaceID, agentID)
	if err != nil {
		return fmt.Errorf("validate assignee: %w", err)
	}
	if !active {
		return fmt.Errorf("validate assignee: unknown or deactivated agent")
	}
	return nil
}

// AgentUpdate is a change to one agent. Nil fields stay as they are. ReassignTo names the agent that
// receives the open and pending conversations when Active is false; nil leaves them unassigned.
type AgentUpdate struct {
	Role       *string
	Active     *bool
	ReassignTo *string
}

// UpdateAgent changes the role and the active state of an agent of the workspace in one transaction.
// Deactivating also revokes the agent's sessions and unused links and moves the open and pending
// conversations; reactivating takes a seat again on the hosted cloud (a *domain.PlanLimitError when
// none is free). A change that would leave no active admin returns domain.ErrLastAdmin, and a bad
// hand-over target domain.ErrInvalidReassignTarget; either leaves everything as it was.
func (s *Store) UpdateAgent(ctx context.Context, workspaceID, agentID string, update AgentUpdate) (domain.TeamMember, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.TeamMember, error) {
		var email string
		err := tx.QueryRow(ctx, `SELECT email FROM agents WHERE workspace_id=$1 AND id=$2`, workspaceID, agentID).Scan(&email)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.TeamMember{}, fmt.Errorf("update agent: agent not found")
		}
		if err != nil {
			return domain.TeamMember{}, fmt.Errorf("load agent: %w", err)
		}
		// Links go first, before this transaction holds any other lock. A redeem in flight holds its link
		// and then waits for the seat lock or the agent row, so taking those first would deadlock with it.
		if update.Active != nil && !*update.Active {
			if err := s.revokeUnusedLinks(ctx, tx, workspaceID, agentID, email, ""); err != nil {
				return domain.TeamMember{}, err
			}
		}
		// The same lock serializes seat checks and the last-admin rule across concurrent changes.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "agent-seats:"+workspaceID); err != nil {
			return domain.TeamMember{}, fmt.Errorf("lock agent seats: %w", err)
		}
		var role string
		var wasActive bool
		err = tx.QueryRow(ctx, `SELECT role,deactivated_at IS NULL FROM agents WHERE workspace_id=$1 AND id=$2 FOR UPDATE`,
			workspaceID, agentID).Scan(&role, &wasActive)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.TeamMember{}, fmt.Errorf("update agent: agent not found")
		}
		if err != nil {
			return domain.TeamMember{}, fmt.Errorf("load agent: %w", err)
		}
		newRole, nowActive := role, wasActive
		if update.Role != nil {
			newRole = *update.Role
		}
		if update.Active != nil {
			nowActive = *update.Active
		}
		if wasActive && role == "admin" && (!nowActive || newRole != "admin") {
			if err := requireOtherAdmin(ctx, tx, workspaceID, agentID); err != nil {
				return domain.TeamMember{}, err
			}
		}
		if wasActive && !nowActive {
			if err := s.deactivateAgent(ctx, tx, workspaceID, agentID, update.ReassignTo); err != nil {
				return domain.TeamMember{}, err
			}
		}
		if !wasActive && nowActive {
			if s.limits {
				if err := CheckAgentSeat(ctx, tx, workspaceID, s.now(), email, "", s.team25Offered); err != nil {
					return domain.TeamMember{}, err
				}
			}
			if _, err := tx.Exec(ctx, `UPDATE agents SET deactivated_at=NULL WHERE workspace_id=$1 AND id=$2`, workspaceID, agentID); err != nil {
				return domain.TeamMember{}, fmt.Errorf("reactivate agent: %w", err)
			}
		}
		if newRole != role {
			if _, err := tx.Exec(ctx, `UPDATE agents SET role=$3 WHERE workspace_id=$1 AND id=$2`, workspaceID, agentID, newRole); err != nil {
				return domain.TeamMember{}, fmt.Errorf("change agent role: %w", err)
			}
			// An unused admin invite would hand the role back when it is redeemed.
			if newRole == "agent" {
				if err := s.revokeUnusedLinks(ctx, tx, workspaceID, "", email, "admin"); err != nil {
					return domain.TeamMember{}, err
				}
			}
		}
		return scanTeamMember(tx.QueryRow(ctx, teamMemberSelect+` WHERE a.workspace_id=$1 AND a.id=$2`, workspaceID, agentID))
	})
}

func requireOtherAdmin(ctx context.Context, tx pgx.Tx, workspaceID, agentID string) error {
	var others int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM agents WHERE workspace_id=$1 AND id<>$2 AND role='admin' AND deactivated_at IS NULL`,
		workspaceID, agentID).Scan(&others); err != nil {
		return fmt.Errorf("count admins: %w", err)
	}
	if others == 0 {
		return domain.ErrLastAdmin
	}
	return nil
}

// deactivateAgent sets deactivated_at, revokes the agent's sessions, and hands the
// open and pending conversations to reassignTo (unassigned when nil).
func (s *Store) deactivateAgent(ctx context.Context, tx pgx.Tx, workspaceID, agentID string, reassignTo *string) error {
	if reassignTo != nil {
		active, err := LockActiveAgent(ctx, tx, workspaceID, *reassignTo)
		if err != nil {
			return err
		}
		if !active || *reassignTo == agentID {
			return domain.ErrInvalidReassignTarget
		}
	}
	now := s.now()
	statements := []struct {
		name string
		sql  string
		args []any
	}{
		{"deactivate agent", `UPDATE agents SET deactivated_at=$3 WHERE workspace_id=$1 AND id=$2`, []any{workspaceID, agentID, now}},
		{"revoke sessions", `UPDATE sessions SET revoked_at=$3 WHERE workspace_id=$1 AND agent_id=$2 AND revoked_at IS NULL`, []any{workspaceID, agentID, now}},
		// The conversations are locked in id order, after the agent row, so a bulk update that locks them
		// in the same order cannot deadlock with this one.
		{"lock conversations", `SELECT id FROM conversations WHERE workspace_id=$1 AND assignee_id=$2 AND ` + openAssigned + ` ORDER BY id FOR NO KEY UPDATE`, []any{workspaceID, agentID}},
		{"reassign conversations", `UPDATE conversations SET assignee_id=$3::uuid,updated_at=$4
			WHERE workspace_id=$1 AND assignee_id=$2 AND ` + openAssigned, []any{workspaceID, agentID, reassignTo, now}},
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement.sql, statement.args...); err != nil {
			return fmt.Errorf("%s: %w", statement.name, err)
		}
	}
	return nil
}

// revokeUnusedLinks revokes the unused links of one person: the pairing links of agentID (when given)
// and the invites sent to email. A ceiling limits it to invites with that role ceiling.
func (s *Store) revokeUnusedLinks(ctx context.Context, tx pgx.Tx, workspaceID, agentID, email, ceiling string) error {
	if _, err := tx.Exec(ctx, `UPDATE pairing_links SET revoked_at=$5 WHERE workspace_id=$1 AND used_at IS NULL AND revoked_at IS NULL
		AND (($2<>'' AND agent_id::text=$2) OR (kind='invite' AND email=lower($3)))
		AND ($4='' OR role_ceiling=$4)`, workspaceID, agentID, email, ceiling, s.now()); err != nil {
		return fmt.Errorf("revoke links: %w", err)
	}
	return nil
}
