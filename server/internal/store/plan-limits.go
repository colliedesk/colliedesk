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
// invites cannot pass the cap. Someone who is already an active agent, or who already holds a seat
// through another pending invite, needs no new seat; a deactivated agent holds none, so reactivating
// one takes a seat like a new invite does. A workspace-wide advisory lock makes two concurrent
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
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agents WHERE workspace_id=$1 AND email=lower($2) AND deactivated_at IS NULL)`, workspaceID, email).Scan(&known); err != nil {
		return fmt.Errorf("check agent: %w", err)
	}
	if known {
		return nil
	}
	plan := access.Plan
	limit := domain.LimitsFor(plan).Agents
	var seats int
	if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM agents WHERE workspace_id=$1 AND deactivated_at IS NULL) +
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
// imported one, or inbound mail that came through the platform forwarder and whose authentication
// did not fail. Mail posted to a workspace's own inbound endpoint never counts: its admin writes the
// whole message, Authentication-Results included. Without these rules a trial user could mail
// themselves with a forged From and use the relay to write to the victim.
// A "none" verdict passes for MVP because many small senders publish no DMARC; the daily cap bounds
// that. The real hardening, when needed, is requiring an aligned dmarc=pass, which needs the parser
// to record the DMARC result separately (today a bare spf=pass or dkim=pass of any domain is "pass").
const customerWroteFirst = `SELECT EXISTS(SELECT 1 FROM messages WHERE workspace_id=$1 AND conversation_id=$2 AND kind='incoming'
	AND (origin IS NOT NULL OR (email_meta->>'via'='platform' AND COALESCE(email_meta->>'authentication','none') <> 'fail')))`

// onTrial reports whether the trial outbound rules apply to the workspace now: plan limits are on
// and the plan in effect is the trial. Paid, locked and self-hosted workspaces are untouched.
func (s *Store) onTrial(ctx context.Context, tx pgx.Tx, workspaceID string, now time.Time) (bool, error) {
	if !s.limits {
		return false, nil
	}
	access, err := workspaceAccess(ctx, tx, workspaceID, now)
	if err != nil {
		return false, err
	}
	return access.Plan == domain.PlanTrial, nil
}

// checkTrialOutbound refuses a new reply the trial does not allow with a *domain.TrialOutboundError:
// first the customer-wrote-first rule, then the daily cap.
func (s *Store) checkTrialOutbound(ctx context.Context, tx pgx.Tx, workspaceID, conversationID string, domainVerified bool) error {
	now := s.now()
	if trial, err := s.onTrial(ctx, tx, workspaceID, now); err != nil || !trial {
		return err
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
	return s.checkTrialCap(ctx, tx, workspaceID, now)
}

// checkTrialCap refuses with a *domain.TrialOutboundError carrying RetryAfter when the workspace
// already has TrialRepliesPerDay deliveries in the window. Both a new reply and the retry of a
// failed one (which counts again once queued) pass through it. A workspace-wide advisory lock makes
// two concurrent callers see each other's row before the cap is compared.
func (s *Store) checkTrialCap(ctx context.Context, tx pgx.Tx, workspaceID string, now time.Time) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "outbound-cap:"+workspaceID); err != nil {
		return fmt.Errorf("lock outbound cap: %w", err)
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

// ownStorageBytes is what the trial's storage cap counts: the bytes of every attachment except those of
// imported messages, which are history carried over from another desk and never count there. It is a
// number maintained by statement triggers on attachments (see the storage-counter migration), so reading it
// costs one row however many files the workspace holds.
const ownStorageBytes = `SELECT counted_storage_bytes FROM workspaces WHERE id=$1`

// storedStorageBytes is what the Founding plan's cap counts: every attachment, imported ones included
// (see the founding-storage-counter migration).
const storedStorageBytes = `SELECT stored_storage_bytes FROM workspaces WHERE id=$1`

// storageCap is the cap of plan in bytes, 0 for none. A store built with WithFoundingStorageBytes
// changes the Founding cap.
func (s *Store) storageCap(plan string) int64 {
	if plan == domain.PlanFounding && s.foundingStorage > 0 {
		return s.foundingStorage
	}
	return domain.StorageCap(plan)
}

// storageUsed reads the bytes plan's cap counts for the workspace.
func storageUsed(ctx context.Context, tx pgx.Tx, workspaceID, plan string) (int64, error) {
	query := ownStorageBytes
	if domain.StorageCountsImports(plan) {
		query = storedStorageBytes
	}
	var used int64
	if err := tx.QueryRow(ctx, query, workspaceID).Scan(&used); err != nil {
		return 0, fmt.Errorf("sum attachment storage: %w", err)
	}
	return used, nil
}

// StorageRoom is where a workspace stands against its plan's storage cap.
type StorageRoom struct {
	Plan string
	// Capped is false for plans and installs without a cap; Limit and Used are then zero.
	Capped      bool
	Limit, Used int64
}

// GetStorageRoom reads the plan's storage cap and the bytes it counts, for the importer to check
// before it writes. A self-hosted store (no plan limits) is never capped.
func (s *Store) GetStorageRoom(ctx context.Context, workspaceID string) (StorageRoom, error) {
	room, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (StorageRoom, error) {
		if !s.limits {
			return StorageRoom{}, nil
		}
		access, err := workspaceAccess(ctx, tx, workspaceID, s.now())
		if err != nil {
			return StorageRoom{}, err
		}
		room := StorageRoom{Plan: access.Plan, Limit: s.storageCap(access.Plan)}
		if room.Limit == 0 {
			return room, nil
		}
		room.Capped = true
		room.Used, err = storageUsed(ctx, tx, workspaceID, access.Plan)
		return room, err
	})
	if err != nil {
		return StorageRoom{}, fmt.Errorf("get storage room: %w", err)
	}
	return room, nil
}

// CheckInboundStorage is CheckStorage for a customer's email. A paying Founding workspace never
// bounces its customers' mail for storage: the mail is stored in full and its bytes still count, so
// billing shows the usage past the cap. The cap holds for uploads, reply attachments and imports. The
// trial and a locked workspace keep refusing inbound mail at their 2 GiB, as before.
func (s *Store) CheckInboundStorage(ctx context.Context, tx pgx.Tx, workspaceID string, addBytes int64, now time.Time) error {
	if !s.limits {
		return nil
	}
	access, err := workspaceAccess(ctx, tx, workspaceID, now)
	if err != nil {
		return err
	}
	if access.Plan == domain.PlanFounding {
		return nil
	}
	return s.CheckStorage(ctx, tx, workspaceID, addBytes, now)
}

// CheckStorage returns a *domain.StorageLimitError when storing addBytes more would pass the storage
// cap of the workspace's plan. Reaching the cap exactly is allowed. It reads the plan and the usage
// inside tx and takes no lock, so two requests that arrive together can together pass the cap by at
// most one request. Every path that stores an attachment an agent or an import supplies calls it
// before writing the bytes; inbound mail calls CheckInboundStorage. Team and self-hosted workspaces
// have no cap.
func (s *Store) CheckStorage(ctx context.Context, tx pgx.Tx, workspaceID string, addBytes int64, now time.Time) error {
	if !s.limits {
		return nil
	}
	access, err := workspaceAccess(ctx, tx, workspaceID, now)
	if err != nil {
		return err
	}
	limit := s.storageCap(access.Plan)
	if limit == 0 {
		return nil
	}
	used, err := storageUsed(ctx, tx, workspaceID, access.Plan)
	if err != nil {
		return err
	}
	if used+addBytes > limit {
		return &domain.StorageLimitError{Plan: access.Plan, Used: used, Adding: addBytes, Limit: limit}
	}
	return nil
}
