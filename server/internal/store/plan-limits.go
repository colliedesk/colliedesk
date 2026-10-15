package store

import (
	"context"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// pendingInvite selects the pairing_links rows that still hold an agent seat: invites not used,
// not revoked and not expired. It expects the current time as query parameter $2.
const pendingInvite = `kind='invite' AND used_at IS NULL AND revoked_at IS NULL AND expires_at > $2`

// effectivePlan is the plan in effect for the workspace at now.
func effectivePlan(ctx context.Context, tx pgx.Tx, workspaceID string, now time.Time) (string, error) {
	subscriptions, err := loadSubscriptions(ctx, tx, workspaceID)
	if err != nil {
		return "", fmt.Errorf("load subscriptions: %w", err)
	}
	return domain.NewBilling(subscriptions, domain.BillingUsage{}, now).Plan, nil
}

// CheckAgentSeat returns a *domain.PlanLimitError when one more agent would exceed the cap of the
// workspace's plan. Agents and pending invites both hold a seat, so a burst of invites cannot pass
// the cap. A workspace-wide advisory lock makes two concurrent invites see each other. The caller
// runs it only where plan limits apply (the hosted cloud). ignoreInvite names an invite that is
// being redeemed and so is about to become the agent it already reserved.
func CheckAgentSeat(ctx context.Context, tx pgx.Tx, workspaceID string, now time.Time, ignoreInvite string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "agent-seats:"+workspaceID); err != nil {
		return fmt.Errorf("lock agent seats: %w", err)
	}
	plan, err := effectivePlan(ctx, tx, workspaceID, now)
	if err != nil {
		return err
	}
	limit := domain.LimitsFor(plan).Agents
	var seats int
	if err := tx.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM agents WHERE workspace_id=$1) +
		(SELECT count(*) FROM pairing_links WHERE workspace_id=$1 AND `+pendingInvite+` AND id::text<>$3)`,
		workspaceID, now, ignoreInvite).Scan(&seats); err != nil {
		return fmt.Errorf("count agent seats: %w", err)
	}
	if seats+1 > limit {
		return &domain.PlanLimitError{Plan: plan, Agents: limit}
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
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "conversation-quota:"+workspaceID); err != nil {
		return false, fmt.Errorf("lock conversation quota: %w", err)
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
