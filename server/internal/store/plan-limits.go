package store

import (
	"context"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// effectivePlan is the plan in effect for the workspace at now.
func effectivePlan(ctx context.Context, tx pgx.Tx, workspaceID string, now time.Time) (string, error) {
	subscriptions, err := loadSubscriptions(ctx, tx, workspaceID)
	if err != nil {
		return "", fmt.Errorf("load subscriptions: %w", err)
	}
	return domain.NewBilling(subscriptions, domain.BillingUsage{}, now).Plan, nil
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
	var known bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agents WHERE workspace_id=$1 AND email=lower($2))`, workspaceID, email).Scan(&known); err != nil {
		return fmt.Errorf("check agent: %w", err)
	}
	if known {
		return nil
	}
	plan, err := effectivePlan(ctx, tx, workspaceID, now)
	if err != nil {
		return err
	}
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

// lockConversationQuota serializes the writers that change a workspace's monthly conversation
// count until the end of the caller's transaction. Take it as late as possible, after any slow work
// such as uploads, and always after the inbound sender lock.
func lockConversationQuota(ctx context.Context, tx pgx.Tx, workspaceID string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "conversation-quota:"+workspaceID); err != nil {
		return fmt.Errorf("lock conversation quota: %w", err)
	}
	return nil
}

// overConversationQuota reports whether a conversation received now would exceed the monthly quota
// of the workspace's plan. It is false without plan limits (self-hosted) and for plans with no cap.
// The workspace-wide advisory lock lasts to the end of the caller's transaction, so concurrent
// deliveries count each other and the quota holds exactly.
func (s *Store) overConversationQuota(ctx context.Context, tx pgx.Tx, workspaceID string, now time.Time) (bool, error) {
	if !s.limits {
		return false, nil
	}
	if err := lockConversationQuota(ctx, tx, workspaceID); err != nil {
		return false, err
	}
	plan, err := effectivePlan(ctx, tx, workspaceID, now)
	if err != nil {
		return false, err
	}
	quota := domain.LimitsFor(plan).ConversationsPerMonth
	if quota == nil {
		return false, nil
	}
	var received int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM conversations WHERE workspace_id=$1 AND origin IS NULL AND created_at >= $2`,
		workspaceID, domain.MonthStart(now)).Scan(&received); err != nil {
		return false, fmt.Errorf("count conversations this month: %w", err)
	}
	return received >= *quota, nil
}

// releaseHeldIfPaid makes the workspace's held conversations visible once its plan in effect is
// paid. It runs in the transaction that records the subscription, so an upgrade and the release
// commit together.
func releaseHeldIfPaid(ctx context.Context, tx pgx.Tx, workspaceID string, now time.Time) error {
	plan, err := effectivePlan(ctx, tx, workspaceID, now)
	if err != nil {
		return err
	}
	if plan == domain.PlanFree {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE conversations SET held_at=NULL WHERE workspace_id=$1 AND held_at IS NOT NULL`, workspaceID); err != nil {
		return fmt.Errorf("release held conversations: %w", err)
	}
	return nil
}
