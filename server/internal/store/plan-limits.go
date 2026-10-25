package store

import (
	"context"
	"errors"
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

// WorkspaceAccess is the plan in effect for the workspace at now, read inside a workspace-scoped tx:
// the AI worker reads it before each task, so a workspace locked since the task was queued spends nothing.
func WorkspaceAccess(ctx context.Context, tx pgx.Tx, workspaceID string, now time.Time) (domain.Access, error) {
	return workspaceAccess(ctx, tx, workspaceID, now)
}

// pendingSeatEmails counts the people who hold a seat only through an invite: distinct invited
// addresses that are not active agents. Two invites to one address are one person, and inviting
// someone who is already an active agent (a role change) takes no seat. An invite to a deactivated
// agent's address does: redeeming it would bring that person back.
const pendingSeatEmails = `SELECT count(DISTINCT p.email) FROM pairing_links p
	WHERE p.workspace_id=$1 AND p.kind='invite' AND p.used_at IS NULL AND p.revoked_at IS NULL AND p.expires_at > $2
	AND NOT EXISTS (SELECT 1 FROM agents a WHERE a.workspace_id=p.workspace_id AND a.email=p.email AND a.deactivated_at IS NULL)`

// CheckAgentSeat returns a *domain.PlanLimitError when giving email an agent seat would exceed the
// cap of the workspace's plan. A seat is an agent or a person with a pending invite, so a burst of
// invites cannot pass the cap. Someone who is already an active agent, or who already holds a seat
// through another pending invite, needs no new seat; a deactivated agent holds none, so reactivating
// one takes a seat like a new invite does. A workspace-wide advisory lock makes two concurrent
// invites see each other. The caller runs it only where plan limits apply (the hosted cloud).
// plansOnSale lets the refusal point to the next plan up that the server sells.
// ignoreInvite names an invite that is being redeemed, which is about to become the agent it
// already reserved.
func CheckAgentSeat(ctx context.Context, tx pgx.Tx, workspaceID string, now time.Time, email, ignoreInvite string, plansOnSale []string) error {
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
		return &domain.PlanLimitError{Plan: plan, Agents: limit, Upgrade: domain.UpgradeFor(plan, plansOnSale)}
	}
	return nil
}

// AgentSeatRank is the place of agentID among the workspace's active agents when a smaller plan
// keeps only some of them: admins before agents, so whoever can fix billing keeps access longest and
// the owner (the first admin added) comes first; within each role, by when the agent was first added,
// oldest first. Agents ranked beyond the plan's cap are paused. A reactivated agent keeps its first
// added time, so it ranks where it did, not as the newest.
func AgentSeatRank(ctx context.Context, tx pgx.Tx, workspaceID, agentID string) (int, error) {
	var rank int
	err := tx.QueryRow(ctx, `SELECT r FROM (SELECT id, row_number() OVER (ORDER BY role='admin' DESC, created_at, id) AS r
		FROM agents WHERE workspace_id=$1 AND deactivated_at IS NULL) ranked WHERE id=$2`, workspaceID, agentID).Scan(&rank)
	return rank, err
}

// countedDelivery is a delivery that uses up the trial's daily cap: queued, sent, or failed after it
// was handed to the relay (sent_at set). A delivery report can fail a reply that already reached its
// people, and anyone can post one to the workspace's inbound address, so a later failure never gives
// them back. Only a delivery that failed before leaving does not count.
const countedDelivery = `(status <> 'failed' OR sent_at IS NOT NULL)`

// recipientsInWindow sums the people reached by the counted deliveries in the window: the contact plus
// every Cc address of each reply.
const recipientsInWindow = `SELECT COALESCE(sum(recipients),0) FROM channel_outbox WHERE workspace_id=$1 AND channel_type='email' AND created_at > $2 AND ` + countedDelivery

// customerWroteFirst is true for a conversation that holds a message from the customer: an
// imported one, or inbound mail that came through the platform forwarder and whose authentication
// did not fail. Mail posted to a workspace's own inbound endpoint never counts: its admin writes the
// whole message, Authentication-Results included. Without these rules a trial user could mail
// themselves with a forged From and use the relay to write to the victim.
// A "none" verdict passes for MVP because many small senders publish no DMARC; the daily cap bounds
// that. The real hardening, when needed, is requiring an aligned dmarc=pass, which needs the parser
// to record the DMARC result separately (today a bare spf=pass or dkim=pass of any domain is "pass").
// The mail must come from the address the reply goes to: merging contacts moves a conversation to
// another contact, and a customer who wrote from one address is no proof the other one wants mail.
const customerWroteFirst = `SELECT EXISTS(SELECT 1 FROM messages m
	JOIN conversations c ON c.workspace_id=m.workspace_id AND c.id=m.conversation_id
	JOIN contacts k ON k.workspace_id=c.workspace_id AND k.id=c.contact_id
	WHERE m.workspace_id=$1 AND m.conversation_id=$2 AND m.kind='incoming'
	AND (m.origin IS NOT NULL OR (m.email_meta->>'via'='platform' AND COALESCE(m.email_meta->>'authentication','none') <> 'fail'
		AND lower(m.email_meta->>'from')=lower(k.email))))`

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
// first the customer-wrote-first rule, then the daily cap. recipients is how many people the reply
// reaches (the contact plus its Cc list).
func (s *Store) checkTrialOutbound(ctx context.Context, tx pgx.Tx, workspaceID, conversationID string, domainVerified bool, recipients int) error {
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
			return trialFirstContactRefusal(ctx, tx, workspaceID, conversationID)
		}
	}
	return s.checkTrialCap(ctx, tx, workspaceID, now, recipients)
}

// customerWroteFromAnotherAddress is true for a conversation holding trusted customer mail from an
// address that is not the contact's: a contact merged into this one wrote it.
const customerWroteFromAnotherAddress = `SELECT COALESCE(k.email,''), EXISTS(SELECT 1 FROM messages m WHERE m.workspace_id=c.workspace_id AND m.conversation_id=c.id
	AND m.kind='incoming' AND m.email_meta->>'via'='platform' AND COALESCE(m.email_meta->>'authentication','none') <> 'fail')
	FROM conversations c JOIN contacts k ON k.workspace_id=c.workspace_id AND k.id=c.contact_id WHERE c.workspace_id=$1 AND c.id=$2`

// trialFirstContactRefusal says why the first-contact rule refused a reply: nobody wrote, or the customer
// wrote from another address than the one the reply goes to.
func trialFirstContactRefusal(ctx context.Context, tx pgx.Tx, workspaceID, conversationID string) error {
	var contactEmail string
	var wroteElsewhere bool
	err := tx.QueryRow(ctx, customerWroteFromAnotherAddress, workspaceID, conversationID).Scan(&contactEmail, &wroteElsewhere)
	if errors.Is(err, pgx.ErrNoRows) {
		return &domain.TrialOutboundError{}
	}
	if err != nil {
		return fmt.Errorf("check first contact address: %w", err)
	}
	if wroteElsewhere {
		return &domain.TrialOutboundError{ReplyTo: contactEmail}
	}
	return &domain.TrialOutboundError{}
}

// checkTrialCap refuses with a *domain.TrialOutboundError carrying RetryAfter when sending to
// recipients more people would take the workspace past TrialRecipientsPerDay in the window. Both a new
// reply and the retry of a failed one (which counts again once queued) pass through it. A
// workspace-wide advisory lock makes two concurrent callers see each other's row before the cap is
// compared.
func (s *Store) checkTrialCap(ctx context.Context, tx pgx.Tx, workspaceID string, now time.Time, recipients int) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "outbound-cap:"+workspaceID); err != nil {
		return fmt.Errorf("lock outbound cap: %w", err)
	}
	windowStart := now.Add(-domain.TrialReplyWindow)
	var reached int
	if err := tx.QueryRow(ctx, recipientsInWindow, workspaceID, windowStart).Scan(&reached); err != nil {
		return fmt.Errorf("count recent recipients: %w", err)
	}
	room := domain.TrialRecipientsPerDay - recipients
	if reached <= room {
		return nil
	}
	// Walking from the newest delivery back, the first one at which the running total passes room is
	// the one whose leaving the window frees enough for this reply.
	var frees time.Time
	if err := tx.QueryRow(ctx, `SELECT created_at FROM (
			SELECT created_at, sum(recipients) OVER (ORDER BY created_at DESC, id ROWS UNBOUNDED PRECEDING) AS total
			FROM channel_outbox WHERE workspace_id=$1 AND channel_type='email' AND created_at > $2 AND `+countedDelivery+`) counted
		WHERE total > $3 ORDER BY total LIMIT 1`, workspaceID, windowStart, room).Scan(&frees); err != nil {
		return fmt.Errorf("find next free slot: %w", err)
	}
	return &domain.TrialOutboundError{RetryAfter: frees.Add(domain.TrialReplyWindow).Sub(now)}
}

// repliesLast24h is the trial's cap usage as the admin sees it: the people reached in the window.
func repliesLast24h(ctx context.Context, tx pgx.Tx, workspaceID string, now time.Time) (int, error) {
	var reached int
	if err := tx.QueryRow(ctx, recipientsInWindow, workspaceID, now.Add(-domain.TrialReplyWindow)).Scan(&reached); err != nil {
		return 0, fmt.Errorf("count recent recipients: %w", err)
	}
	return reached, nil
}

// ownStorageBytes is what the trial's storage cap counts: the bytes of every attachment except those of
// imported messages, which are history carried over from another desk and never count there. It is a
// number maintained by statement triggers on attachments (see the storage-counter migration), so reading it
// costs one row however many files the workspace holds.
const ownStorageBytes = `SELECT counted_storage_bytes FROM workspaces WHERE id=$1`

// storedStorageBytes is what a paid plan's cap counts: every attachment, imported ones included
// (see the founding-storage-counter migration).
const storedStorageBytes = `SELECT stored_storage_bytes FROM workspaces WHERE id=$1`

// storageCap is the cap of plan in bytes, 0 for none. A store built with WithPaidStorageBytes
// changes the cap of every plan that counts imported files.
func (s *Store) storageCap(plan string) int64 {
	if domain.StorageCountsImports(plan) && s.paidStorage > 0 {
		return s.paidStorage
	}
	return domain.StorageCap(plan)
}

// accessStorageCap is the cap in effect for access: the largest cap of the paid plans it holds
// (Access.StorageLimit), or the test override of WithPaidStorageBytes.
func (s *Store) accessStorageCap(access domain.Access) int64 {
	if domain.StorageCountsImports(access.Plan) && s.paidStorage > 0 {
		return s.paidStorage
	}
	return access.StorageLimit()
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
	// Upgrade is the plan with more storage this server sells, "" when none (domain.StorageUpgradeFor).
	Upgrade string
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
		room := StorageRoom{Plan: access.Plan, Limit: s.accessStorageCap(access), Upgrade: domain.StorageUpgradeFor(access.Plan, s.plansOnSale)}
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

// CheckInboundStorage is CheckStorage for a customer's email. A paying workspace never
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
	if domain.StorageCountsImports(access.Plan) {
		return nil
	}
	return s.CheckStorage(ctx, tx, workspaceID, addBytes, now)
}

// CheckStorage returns a *domain.StorageLimitError when storing addBytes more would pass the storage
// cap of the workspace's plan. Reaching the cap exactly is allowed. It reads the plan and the usage
// inside tx and takes no lock, so two requests that arrive together can together pass the cap by at
// most one request. Every path that stores an attachment an agent or an import supplies calls it
// before writing the bytes; inbound mail calls CheckInboundStorage. Self-hosted workspaces have no cap.
func (s *Store) CheckStorage(ctx context.Context, tx pgx.Tx, workspaceID string, addBytes int64, now time.Time) error {
	if !s.limits {
		return nil
	}
	access, err := workspaceAccess(ctx, tx, workspaceID, now)
	if err != nil {
		return err
	}
	limit := s.accessStorageCap(access)
	if limit == 0 {
		return nil
	}
	used, err := storageUsed(ctx, tx, workspaceID, access.Plan)
	if err != nil {
		return err
	}
	if used+addBytes > limit {
		return &domain.StorageLimitError{Plan: access.Plan, Used: used, Adding: addBytes, Limit: limit, Upgrade: domain.StorageUpgradeFor(access.Plan, s.plansOnSale)}
	}
	return nil
}
