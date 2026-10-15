package store

import (
	"context"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// workspaceAccess is the plan in effect for the workspace at now, read inside tx.
func workspaceAccess(ctx context.Context, tx pgx.Tx, workspaceID string, now time.Time) (domain.Access, error) {
	var trialEndsAt time.Time
	if err := tx.QueryRow(ctx, `SELECT trial_ends_at FROM workspaces WHERE id=$1`, workspaceID).Scan(&trialEndsAt); err != nil {
		return domain.Access{}, fmt.Errorf("load trial end: %w", err)
	}
	subscriptions, err := LoadSubscriptions(ctx, tx, workspaceID)
	if err != nil {
		return domain.Access{}, fmt.Errorf("load subscriptions: %w", err)
	}
	return domain.NewAccess(subscriptions, trialEndsAt, now), nil
}

// pendingSeatEmails counts the people who hold a seat only through an invite: distinct invited
// addresses that are not agents yet. Two invites to one address are one person, and inviting someone
// who is already an agent (a role change) takes no seat.
const pendingSeatEmails = `SELECT count(DISTINCT p.email) FROM pairing_links p
	WHERE p.workspace_id=$1 AND p.kind='invite' AND p.used_at IS NULL AND p.revoked_at IS NULL AND p.expires_at > $2
	AND NOT EXISTS (SELECT 1 FROM agents a WHERE a.workspace_id=p.workspace_id AND a.email=p.email)`

// CheckAgentSeat returns a *domain.PlanLimitError when giving email an agent seat would exceed the
// cap of the workspace's plan. A seat is an agent or a person with a pending invite, so a burst of
// invites cannot pass the cap. Someone who is already an agent, or who already holds a seat through
// another pending invite, needs no new seat. A workspace-wide advisory lock makes two concurrent
// invites see each other. The caller runs it only where plan limits apply (the hosted cloud).
// ignoreInvite names an invite that is being redeemed, which is about to become the agent it
// already reserved.
func CheckAgentSeat(ctx context.Context, tx pgx.Tx, workspaceID string, now time.Time, email, ignoreInvite string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "agent-seats:"+workspaceID); err != nil {
		return fmt.Errorf("lock agent seats: %w", err)
	}
	// A locked workspace changes nothing, not even the role of someone who already has a seat.
	access, err := workspaceAccess(ctx, tx, workspaceID, now)
	if err != nil {
		return err
	}
	if access.Locked {
		return &domain.LockedError{}
	}
	var known bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agents WHERE workspace_id=$1 AND email=lower($2))`, workspaceID, email).Scan(&known); err != nil {
		return fmt.Errorf("check agent: %w", err)
	}
	if known {
		return nil
	}
	plan := access.Plan
	limit := domain.LimitsFor(plan).Agents
	var seats int
	if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM agents WHERE workspace_id=$1) +
		(SELECT count(DISTINCT p.email) FROM pairing_links p
			WHERE p.workspace_id=$1 AND p.kind='invite' AND p.used_at IS NULL AND p.revoked_at IS NULL AND p.expires_at > $2
			AND p.id::text<>$3 AND p.email<>lower($4)
			AND NOT EXISTS (SELECT 1 FROM agents a WHERE a.workspace_id=p.workspace_id AND a.email=p.email))`,
		workspaceID, now, ignoreInvite, email).Scan(&seats); err != nil {
		return fmt.Errorf("count agent seats: %w", err)
	}
	if seats+1 > limit {
		return &domain.PlanLimitError{Plan: plan, Agents: limit}
	}
	return nil
}
