package store

import (
	"context"
	"errors"
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
// row is written only when the event is not older than what is stored. A workspace that does not
// exist (the scoped transaction cannot see it) stores nothing.
func (s *Store) ApplySubscription(ctx context.Context, workspaceID, webhookID, eventType string, incoming domain.Subscription, now time.Time) (SubscriptionResult, error) {
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
		current, err := scanSubscription(tx.QueryRow(ctx, `SELECT `+subscriptionColumns+` FROM workspace_billing WHERE workspace_id=$1 FOR UPDATE`, workspaceID))
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", err
		}
		if err == nil && keepStored(current, incoming, now) {
			return SubscriptionStale, nil
		}
		_, err = tx.Exec(ctx, `INSERT INTO workspace_billing(workspace_id,`+subscriptionColumns+`) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
			ON CONFLICT(workspace_id) DO UPDATE SET plan=EXCLUDED.plan,polar_subscription_id=EXCLUDED.polar_subscription_id,
				polar_customer_id=EXCLUDED.polar_customer_id,status=EXCLUDED.status,current_period_end=EXCLUDED.current_period_end,
				cancel_at_period_end=EXCLUDED.cancel_at_period_end,ended_at=EXCLUDED.ended_at,
				source_modified_at=EXCLUDED.source_modified_at,updated_at=now()`,
			workspaceID, incoming.Plan, incoming.SubscriptionID, incoming.CustomerID, incoming.Status, incoming.CurrentPeriodEnd,
			incoming.CancelAtPeriodEnd, incoming.EndedAt, incoming.ModifiedAt)
		if err != nil {
			return "", err
		}
		return SubscriptionApplied, nil
	})
	if err != nil {
		return "", fmt.Errorf("apply subscription event: %w", err)
	}
	return result, nil
}

// keepStored decides whether the stored subscription wins over an incoming event. Events of one
// subscription are ordered by modified_at. An event of another subscription never ends a paying
// one (an old subscription's cancellation can arrive after the new one started).
func keepStored(stored, incoming domain.Subscription, now time.Time) bool {
	older := incoming.ModifiedAt.Before(stored.ModifiedAt)
	if stored.SubscriptionID == incoming.SubscriptionID {
		return older
	}
	return stored.Entitled(now) && (!incoming.Entitled(now) || older)
}

// GetBilling returns the workspace's plan, state and current usage. Usage counts agents and the
// conversations created since the start of the current UTC calendar month.
func (s *Store) GetBilling(ctx context.Context, workspaceID string, now time.Time) (domain.Billing, error) {
	billing, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.Billing, error) {
		var subscription *domain.Subscription
		stored, err := scanSubscription(tx.QueryRow(ctx, `SELECT `+subscriptionColumns+` FROM workspace_billing WHERE workspace_id=$1`, workspaceID))
		switch {
		case err == nil:
			subscription = &stored
		case !errors.Is(err, pgx.ErrNoRows):
			return domain.Billing{}, err
		}
		var usage domain.BillingUsage
		err = tx.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM agents WHERE workspace_id=$1),
			(SELECT count(*) FROM conversations WHERE workspace_id=$1
				AND created_at >= date_trunc('month', $2::timestamptz AT TIME ZONE 'UTC') AT TIME ZONE 'UTC')`,
			workspaceID, now).Scan(&usage.Agents, &usage.ConversationsThisMonth)
		if err != nil {
			return domain.Billing{}, err
		}
		return domain.NewBilling(subscription, usage, now), nil
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
