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

// repliesInWindow counts the deliveries that use up the trial's daily cap: failed ones never left.
const repliesInWindow = `SELECT count(*) FROM email_outbox WHERE workspace_id=$1 AND created_at > $2 AND status <> 'failed'`

// customerWroteFirst is true for a conversation that holds a message from the customer: an
// imported one, or inbound mail whose authentication did not fail. Without that rule a trial user
// could mail their own hosted address with a forged From and use the relay to write to the victim.
// A "none" verdict passes because many small senders publish no DMARC; the daily cap bounds that.
const customerWroteFirst = `SELECT EXISTS(SELECT 1 FROM messages WHERE workspace_id=$1 AND conversation_id=$2 AND kind='incoming'
	AND (origin IS NOT NULL OR COALESCE(email_meta->>'authentication','none') <> 'fail'))`

// checkTrialOutbound refuses a reply the trial does not allow with a *domain.TrialOutboundError.
// It only applies to the hosted cloud on the trial plan; paid workspaces, locked ones (their
// replies are refused earlier) and self-hosted servers are untouched. A workspace-wide advisory
// lock makes two concurrent replies see each other's row before the cap is compared.
func (s *Store) checkTrialOutbound(ctx context.Context, tx pgx.Tx, workspaceID, conversationID string, domainVerified bool) error {
	if !s.limits {
		return nil
	}
	now := s.now()
	access, err := workspaceAccess(ctx, tx, workspaceID, now)
	if err != nil {
		return err
	}
	if access.Plan != domain.PlanTrial {
		return nil
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "outbound-cap:"+workspaceID); err != nil {
		return fmt.Errorf("lock outbound cap: %w", err)
	}
	if !domainVerified {
		var wrote bool
		if err := tx.QueryRow(ctx, customerWroteFirst, workspaceID, conversationID).Scan(&wrote); err != nil {
			return fmt.Errorf("check first contact: %w", err)
		}
		if !wrote {
			return &domain.TrialOutboundError{}
		}
	}
	windowStart := now.Add(-domain.TrialReplyWindow)
	var sent int
	if err := tx.QueryRow(ctx, repliesInWindow, workspaceID, windowStart).Scan(&sent); err != nil {
		return fmt.Errorf("count recent replies: %w", err)
	}
	if sent < domain.TrialRepliesPerDay {
		return nil
	}
	// The cap-th newest counted reply is the one whose leaving the window frees a slot.
	var frees time.Time
	if err := tx.QueryRow(ctx, `SELECT created_at FROM email_outbox WHERE workspace_id=$1 AND created_at > $2 AND status <> 'failed'
		ORDER BY created_at DESC OFFSET $3 LIMIT 1`, workspaceID, windowStart, domain.TrialRepliesPerDay-1).Scan(&frees); err != nil {
		return fmt.Errorf("find next free slot: %w", err)
	}
	return &domain.TrialOutboundError{RetryAfter: frees.Add(domain.TrialReplyWindow).Sub(now)}
}

// repliesLast24h is the trial's cap usage as the admin sees it.
func repliesLast24h(ctx context.Context, tx pgx.Tx, workspaceID string, now time.Time) (int, error) {
	var sent int
	if err := tx.QueryRow(ctx, repliesInWindow, workspaceID, now.Add(-domain.TrialReplyWindow)).Scan(&sent); err != nil {
		return 0, fmt.Errorf("count recent replies: %w", err)
	}
	return sent, nil
}
