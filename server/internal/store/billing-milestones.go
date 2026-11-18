package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/milestones"
	"github.com/jackc/pgx/v5"
)

// subscriptionState is what a stored subscription said before an event changed it; found is false
// when the event is the first one stored for that subscription.
type subscriptionState struct {
	found             bool
	status            string
	cancelAtPeriodEnd bool
}

// paid is a subscription Polar charges: active. trialing and past_due are not a payment made.
func (s subscriptionState) paid() bool { return s.found && s.status == "active" }

// cancelled is a subscription set to end at its period's end, or ended.
func (s subscriptionState) cancelled() bool {
	return s.found && (s.status == "canceled" || s.cancelAtPeriodEnd)
}

// subscriptionStateTx reads the stored subscription and locks it until the event's write.
func subscriptionStateTx(ctx context.Context, tx pgx.Tx, workspaceID, subscriptionID string) (subscriptionState, error) {
	var state subscriptionState
	err := tx.QueryRow(ctx, `SELECT status, cancel_at_period_end FROM workspace_billing
		WHERE workspace_id=$1 AND polar_subscription_id=$2 FOR UPDATE`, workspaceID, subscriptionID).Scan(&state.status, &state.cancelAtPeriodEnd)
	if errors.Is(err, pgx.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return state, fmt.Errorf("read stored subscription: %w", err)
	}
	state.found = true
	return state, nil
}

// recordBillingMilestones records paid when an applied event made the subscription active, and cancelled
// when it set the subscription to end, at the time Polar changed it. The webhook and the reconcile both
// apply through ApplySubscription, so a lost webhook still leaves its milestone.
func recordBillingMilestones(ctx context.Context, tx pgx.Tx, workspaceID string, before subscriptionState, incoming domain.Subscription) error {
	after := subscriptionState{found: true, status: incoming.Status, cancelAtPeriodEnd: incoming.CancelAtPeriodEnd}
	detail := map[string]any{"subscription_id": incoming.SubscriptionID, "plan": incoming.Plan}
	if after.paid() && !before.paid() {
		if err := milestones.Record(ctx, tx, workspaceID, milestones.Paid, incoming.ModifiedAt, detail); err != nil {
			return err
		}
	}
	if after.cancelled() && !before.cancelled() {
		return milestones.Record(ctx, tx, workspaceID, milestones.Cancelled, incoming.ModifiedAt, detail)
	}
	return nil
}
