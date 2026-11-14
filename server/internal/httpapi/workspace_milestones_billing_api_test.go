package httpapi_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5"
)

func TestTheFirstInviteOfAnotherPersonIsRecordedOnce(t *testing.T) {
	f := newLimitsFixture(t, true)
	first := invite(t, f, f.adminA, 1)
	if first.Code != http.StatusCreated {
		t.Fatalf("invite: status=%d body=%s", first.Code, first.Body)
	}
	if second := invite(t, f, f.adminA, 2); second.Code != http.StatusCreated {
		t.Fatalf("second invite: status=%d body=%s", second.Code, second.Body)
	}
	firstID := queryString(t, f.pool, `SELECT id::text FROM pairing_links WHERE workspace_id=$1 AND email='teammate1@limits-alpha.example.com'`, f.workspaceA)
	got := onlyKind(milestonesOf(t, f.pool, f.workspaceA), "teammate_invited")
	if len(got) != 1 || got[0].Detail["invite_id"] != firstID {
		t.Fatalf("teammate_invited = %+v, want one naming the first invite %s", got, firstID)
	}
	if got := onlyKind(milestonesOf(t, f.pool, f.workspaceB), "teammate_invited"); len(got) != 0 {
		t.Fatalf("workspace B got A's invite: %+v", got)
	}
}

func TestPaymentsAndCancellationsAreRecordedEachTimeBillingChanges(t *testing.T) {
	f := newLimitsFixture(t, true)
	paidAt := time.Date(2026, time.October, 5, 12, 0, 0, 0, time.UTC)
	active := activeTeam(f.workspaceA, "sub_first", paidAt)
	f.billing.mustDeliver(t, "evt_active", active.event("subscription.active"))
	// The same state again (Polar's subscription.updated) is no new payment.
	again := active
	again.ModifiedAt = paidAt.Add(time.Minute)
	f.billing.mustDeliver(t, "evt_updated", again.event("subscription.updated"))

	cancelledAt := time.Date(2026, time.October, 20, 8, 30, 0, 0, time.UTC)
	cancelling := active
	cancelling.CancelAtPeriodEnd, cancelling.ModifiedAt = true, cancelledAt
	f.billing.mustDeliver(t, "evt_canceled", cancelling.event("subscription.canceled"))
	ended := cancelling
	ended.Status, ended.ModifiedAt, ended.EndedAt = "canceled", time.Date(2026, time.November, 4, 12, 0, 0, 0, time.UTC), &cancelledAt
	f.billing.mustDeliver(t, "evt_revoked", ended.event("subscription.revoked"))

	paidAgainAt := time.Date(2026, time.November, 10, 9, 0, 0, 0, time.UTC)
	f.billing.mustDeliver(t, "evt_active_2", activeTeam(f.workspaceA, "sub_second", paidAgainAt).event("subscription.active"))

	got := milestonesOf(t, f.pool, f.workspaceA)
	paid, cancelled := onlyKind(got, "paid"), onlyKind(got, "cancelled")
	if len(paid) != 2 || !paid[0].At.Equal(paidAt) || !paid[1].At.Equal(paidAgainAt) {
		t.Fatalf("paid = %+v, want 2026-10-05T12:00:00Z and 2026-11-10T09:00:00Z", paid)
	}
	if len(cancelled) != 1 || !cancelled[0].At.Equal(cancelledAt) {
		t.Fatalf("cancelled = %+v, want one at 2026-10-20T08:30:00Z", cancelled)
	}
	view := queryString(t, f.pool, `SELECT paid_at::text||' '||cancelled_at::text FROM workspace_milestones WHERE workspace_id=$1`, f.workspaceA)
	if view != "2026-11-10 09:00:00+00 2026-10-20 08:30:00+00" {
		t.Fatalf("view paid_at, cancelled_at = %q, want the latest payment and the cancellation", view)
	}
	if got := milestonesOf(t, f.pool, f.workspaceB); kindsOf(got) != "signed_up" {
		t.Fatalf("workspace B milestones = %q, want only its own sign-up", kindsOf(got))
	}
}

// The app role in a workspace's transaction sees that workspace's log and view row only.
func TestTheLogAndTheViewShowAWorkspaceOnlyItsOwnRows(t *testing.T) {
	f := newLimitsFixture(t, true)
	f.receive(t, false, 1, "Order missing")
	ctx := context.Background()
	var others, rows, ownRows int
	err := database.New(f.pool).InWorkspace(ctx, f.workspaceB, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM workspace_events WHERE workspace_id=$1`, f.workspaceA).Scan(&others); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE workspace_id=$1) FROM workspace_milestones`, f.workspaceB).Scan(&rows, &ownRows)
	})
	if err != nil {
		t.Fatal(err)
	}
	if others != 0 || rows != 1 || ownRows != 1 {
		t.Fatalf("in workspace B: %d of A's events, %d view rows (%d its own); want 0, 1, 1", others, rows, ownRows)
	}
}
