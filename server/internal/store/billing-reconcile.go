package store

import (
	"context"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// BillingSnapshot is the stored side of a comparison with Polar: which workspaces exist, and the
// Polar subscriptions each one holds (complimentary plans are not Polar subscriptions and are left out).
type BillingSnapshot struct {
	Workspaces    map[string]bool
	Subscriptions map[string]map[string]domain.Subscription // workspace id, then Polar subscription id
}

// BillingSnapshot reads every workspace id and every stored Polar subscription in one read-only
// transaction, so the comparison sees one consistent state.
func (s *Store) BillingSnapshot(ctx context.Context) (BillingSnapshot, error) {
	snapshot := BillingSnapshot{Workspaces: map[string]bool{}, Subscriptions: map[string]map[string]domain.Subscription{}}
	err := s.db.Privileged(ctx, database.PrivilegeBillingReconcile, func(tx pgx.Tx) error {
		ids, err := tx.Query(ctx, `SELECT id::text FROM workspaces`)
		if err != nil {
			return err
		}
		workspaceIDs, err := pgx.CollectRows(ids, pgx.RowTo[string])
		if err != nil {
			return err
		}
		for _, id := range workspaceIDs {
			snapshot.Workspaces[id] = true
		}
		rows, err := tx.Query(ctx, `SELECT workspace_id::text,`+subscriptionColumns+` FROM workspace_billing`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var workspaceID string
			var sub domain.Subscription
			if err := rows.Scan(&workspaceID, &sub.Plan, &sub.SubscriptionID, &sub.CustomerID, &sub.Status, &sub.CurrentPeriodEnd, &sub.CancelAtPeriodEnd, &sub.EndedAt, &sub.ModifiedAt); err != nil {
				return err
			}
			if snapshot.Subscriptions[workspaceID] == nil {
				snapshot.Subscriptions[workspaceID] = map[string]domain.Subscription{}
			}
			snapshot.Subscriptions[workspaceID][sub.SubscriptionID] = sub
		}
		return rows.Err()
	}, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return BillingSnapshot{}, fmt.Errorf("read billing snapshot: %w", err)
	}
	return snapshot, nil
}
