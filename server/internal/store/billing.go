package store

import (
	"context"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// SubscriptionResult says what ApplySubscription did with one webhook delivery.
type SubscriptionResult string

const (
	SubscriptionApplied          SubscriptionResult = "applied"
	SubscriptionDuplicate        SubscriptionResult = "duplicate"         // the webhook id was seen before
	SubscriptionStale            SubscriptionResult = "stale"             // older than, or superseded by, the stored state
	SubscriptionUnknownWorkspace SubscriptionResult = "unknown_workspace" // nothing is stored
)

const subscriptionColumns = `plan,polar_subscription_id,polar_customer_id,status,current_period_end,cancel_at_period_end,ended_at,source_modified_at`

func scanSubscription(row pgx.Row) (domain.Subscription, error) {
	var s domain.Subscription
	err := row.Scan(&s.Plan, &s.SubscriptionID, &s.CustomerID, &s.Status, &s.CurrentPeriodEnd, &s.CancelAtPeriodEnd, &s.EndedAt, &s.ModifiedAt)
	return s, err
}

// ApplySubscription records one Polar subscription event for the workspace in a single
// transaction: the webhook id is stored first, so a redelivery is a no-op, and the subscription
// row is written only when the event is not older than the one stored for the same subscription.
// The upsert itself carries that condition, so two events racing for a subscription that has no row
// yet cannot overwrite each other with the older state. A workspace that does not exist (the scoped
// transaction cannot see it) stores nothing.
func (s *Store) ApplySubscription(ctx context.Context, workspaceID, webhookID, eventType string, incoming domain.Subscription) (SubscriptionResult, error) {
	result, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (SubscriptionResult, error) {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM workspaces WHERE id=$1)`, workspaceID).Scan(&exists); err != nil {
			return "", err
		}
		if !exists {
			return SubscriptionUnknownWorkspace, nil
		}
		recorded, err := tx.Exec(ctx, `INSERT INTO billing_webhook_events(workspace_id,webhook_id,event_type) VALUES($1,$2,$3)
			ON CONFLICT DO NOTHING`, workspaceID, webhookID, eventType)
		if err != nil {
			return "", err
		}
		if recorded.RowsAffected() == 0 {
			return SubscriptionDuplicate, nil
		}
		written, err := tx.Exec(ctx, `INSERT INTO workspace_billing(workspace_id,`+subscriptionColumns+`) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
			ON CONFLICT(workspace_id,polar_subscription_id) DO UPDATE SET plan=EXCLUDED.plan,
				polar_customer_id=EXCLUDED.polar_customer_id,status=EXCLUDED.status,current_period_end=EXCLUDED.current_period_end,
				cancel_at_period_end=EXCLUDED.cancel_at_period_end,ended_at=EXCLUDED.ended_at,
				source_modified_at=EXCLUDED.source_modified_at,updated_at=now()
			WHERE workspace_billing.source_modified_at <= EXCLUDED.source_modified_at`,
			workspaceID, incoming.Plan, incoming.SubscriptionID, incoming.CustomerID, incoming.Status, incoming.CurrentPeriodEnd,
			incoming.CancelAtPeriodEnd, incoming.EndedAt, incoming.ModifiedAt)
		if err != nil {
			return "", err
		}
		if written.RowsAffected() == 0 {
			return SubscriptionStale, nil
		}
		return SubscriptionApplied, nil
	})
	if err != nil {
		return "", fmt.Errorf("apply subscription event: %w", err)
	}
	return result, nil
}

// HasBeenActivated reports whether the workspace ever received a subscription.active webhook, that
// is, whether it already became a paying customer once. billing_webhook_events is never pruned.
func (s *Store) HasBeenActivated(ctx context.Context, workspaceID string) (bool, error) {
	activated, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (bool, error) {
		var activated bool
		err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM billing_webhook_events WHERE workspace_id=$1 AND event_type='subscription.active')`, workspaceID).Scan(&activated)
		return activated, err
	})
	if err != nil {
		return false, fmt.Errorf("read activation: %w", err)
	}
	return activated, nil
}

// LoadSubscriptions reads every subscription of the workspace inside tx, in one statement: the Polar
// rows, plus the complimentary plan in force, which counts as a paid subscription that no payment
// backs (see compSubscriptionColumns).
func LoadSubscriptions(ctx context.Context, tx pgx.Tx, workspaceID string) ([]domain.Subscription, error) {
	rows, err := tx.Query(ctx, `SELECT `+subscriptionColumns+` FROM workspace_billing WHERE workspace_id=$1
		UNION ALL SELECT `+compSubscriptionColumns+` FROM workspace_comp_plans WHERE workspace_id=$1 AND revoked_at IS NULL`, workspaceID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Subscription, error) { return scanSubscription(row) })
}

// GetBilling returns the workspace's plan, state and current usage at now.
func (s *Store) GetBilling(ctx context.Context, workspaceID string, now time.Time) (domain.Billing, error) {
	billing, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.Billing, error) {
		subscriptions, err := LoadSubscriptions(ctx, tx, workspaceID)
		if err != nil {
			return domain.Billing{}, err
		}
		var usage domain.BillingUsage
		var trialEndsAt time.Time
		var storedBytes int64
		err = tx.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM agents WHERE workspace_id=$1 AND deactivated_at IS NULL),
			(`+pendingSeatEmails+`),
			(SELECT trial_ends_at FROM workspaces WHERE id=$1),
			(`+ownStorageBytes+`),
			(`+storedStorageBytes+`)`,
			workspaceID, now).
			Scan(&usage.Agents, &usage.PendingInvites, &trialEndsAt, &usage.StorageBytes, &storedBytes)
		if err != nil {
			return domain.Billing{}, err
		}
		billing := domain.NewBilling(subscriptions, trialEndsAt, usage, now)
		if domain.StorageCountsImports(billing.Plan) {
			billing.Usage.StorageBytes = storedBytes
			billing.Limits = domain.LimitsForStorage(billing.Plan, s.storageCap(billing.Plan))
		}
		if billing.Plan == domain.PlanTrial {
			if billing.Usage.RepliesLast24h, err = repliesLast24h(ctx, tx, workspaceID, now); err != nil {
				return domain.Billing{}, err
			}
		}
		return billing, nil
	})
	if err != nil {
		return domain.Billing{}, fmt.Errorf("get billing: %w", err)
	}
	return billing, nil
}

// HasBillingCustomer reports whether the workspace ever had a subscription, that is, whether Polar
// knows it as a customer.
func (s *Store) HasBillingCustomer(ctx context.Context, workspaceID string) (bool, error) {
	exists, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (bool, error) {
		var exists bool
		err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM workspace_billing WHERE workspace_id=$1)`, workspaceID).Scan(&exists)
		return exists, err
	})
	if err != nil {
		return false, fmt.Errorf("check billing customer: %w", err)
	}
	return exists, nil
}
