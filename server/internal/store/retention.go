package store

import (
	"context"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// LoadAccessHistory reads what LoadSubscriptions reads plus the complimentary grants that were
// ended (revoked, replaced or expired), each as a canceled subscription that ended when the grant
// did. It never grants access by itself; domain.LockedSince needs it to date the loss of access.
func LoadAccessHistory(ctx context.Context, tx pgx.Tx, workspaceID string) ([]domain.Subscription, error) {
	subscriptions, err := LoadSubscriptions(ctx, tx, workspaceID)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT plan,'`+domain.CompSubscriptionPrefix+`' || id::text,'','canceled',until,false,revoked_at,granted_at,''
		FROM workspace_comp_plans WHERE workspace_id=$1 AND revoked_at IS NOT NULL`, workspaceID)
	if err != nil {
		return nil, err
	}
	ended, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Subscription, error) { return scanSubscription(row) })
	if err != nil {
		return nil, err
	}
	return append(subscriptions, ended...), nil
}
