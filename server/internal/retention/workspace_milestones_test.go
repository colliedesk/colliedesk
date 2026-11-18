package retention_test

import (
	"context"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// milestones lists a workspace's log as "kind at", oldest first, read with the owner's privileges.
func (e *env) milestones(t *testing.T, id string) string {
	t.Helper()
	var list []string
	err := e.db.Privileged(context.Background(), database.PrivilegeMigrate, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), `SELECT kind||' '||to_char(at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS"Z"')
			FROM workspace_events WHERE workspace_id=$1 ORDER BY at, id`, id)
		if err != nil {
			return err
		}
		list, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(list, ",")
}

// dataRows is rows without the lifecycle log and its view: the log outlives the workspace on purpose and
// grows while a pass runs (trial_ended), so it is checked on its own.
func (e *env) dataRows(t *testing.T, id string) map[string]int {
	t.Helper()
	rows := e.rows(t, id)
	delete(rows, "workspace_events")
	delete(rows, "workspace_milestones")
	return rows
}

func (e *env) trialEnded(t *testing.T, id string) string {
	t.Helper()
	for _, item := range strings.Split(e.milestones(t, id), ",") {
		if strings.HasPrefix(item, "trial_ended ") {
			return item
		}
	}
	return ""
}

// webhook stores one billing event for a workspace, as the server stores it when it applies a delivery, and sets
// the moment it arrived, which the tests choose.
func (e *env) webhook(t *testing.T, id, subscription, status, eventType, received, modified string) {
	t.Helper()
	if _, err := e.store.ApplySubscription(context.Background(), id, "hook-"+subscription+"-"+eventType, eventType, domain.Subscription{
		Plan: domain.PlanTeam, SubscriptionID: subscription, CustomerID: "cus_" + subscription, Status: status, ModifiedAt: at(modified)}); err != nil {
		t.Fatal(err)
	}
	err := e.db.Privileged(context.Background(), database.PrivilegeMigrate, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE billing_webhook_events SET received_at=$3 WHERE workspace_id=$1 AND webhook_id=$2`, id, "hook-"+subscription+"-"+eventType, at(received))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The trial's end is computed when the workspace is read; the retention pass writes it down, at the moment
// the trial ended, only when it is proven that nothing paid or complimentary was in force before that moment.
// Paying after the end does not undo it; what cannot be proven is left unwritten.
func TestTheRetentionPassRecordsATrialThatEndedOnceAtItsEnd(t *testing.T) {
	e := newEnv(t)
	ended := e.workspace(t, "Alpha", "2026-11-01T00:00:00Z", "ann@alpha.example")
	// Beta locked, then its subscription was created on 10 November: the first billing event the server saw is a
	// subscription.created that arrived after the end.
	paysLater := e.workspace(t, "Beta", "2026-11-01T00:00:00Z", "carl@beta.example")
	e.webhook(t, paysLater.ID, "sub_beta", "incomplete", "subscription.created", "2026-11-10T09:00:00Z", "2026-11-10T09:00:00Z")
	e.webhook(t, paysLater.ID, "sub_beta", "active", "subscription.active", "2026-11-10T09:01:00Z", "2026-11-10T09:01:00Z")
	// Gamma paid during its trial and cancelled later: its trial did not end unpaid.
	converted := e.workspace(t, "Gamma", "2026-11-01T00:00:00Z", "dana@gamma.example")
	e.webhook(t, converted.ID, "sub_gamma", "active", "subscription.active", "2026-10-20T00:00:00Z", "2026-10-20T00:00:00Z")
	e.webhook(t, converted.ID, "sub_gamma", "canceled", "subscription.canceled", "2026-11-20T00:00:00Z", "2026-11-20T00:00:00Z")
	// Delta had a complimentary plan from before its trial ended.
	comp := e.workspace(t, "Delta", "2026-11-01T00:00:00Z", "dev@delta.example")
	// Epsilon's subscription was active before the end but reached the server only through the reconcile, after
	// the end, and was last modified after it too: the stored rows cannot show when it started.
	reconciled := e.workspace(t, "Epsilon", "2026-11-01T00:00:00Z", "eve@epsilon.example")
	e.webhook(t, reconciled.ID, "sub_eps", "active", "reconcile", "2026-11-05T00:00:00Z", "2026-11-05T00:00:00Z")
	err := e.db.Privileged(context.Background(), database.PrivilegeMigrate, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO workspace_comp_plans(workspace_id,plan,granted_by,granted_at) VALUES($1,'team','owner','2026-10-10T00:00:00Z')`, comp.ID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	e.run(t, "2026-10-31T23:59:59Z")
	if got := e.trialEnded(t, ended.ID); got != "" {
		t.Fatalf("before the end: %q, want nothing", got)
	}
	e.run(t, "2026-12-02T00:00:00Z")
	e.run(t, "2026-12-03T00:00:00Z")
	if got := e.milestones(t, ended.ID); strings.Count(got, "trial_ended") != 1 || e.trialEnded(t, ended.ID) != "trial_ended 2026-11-01T00:00:00Z" {
		t.Fatalf("after the end: %q, want one trial_ended at 2026-11-01T00:00:00Z", got)
	}
	if got := e.trialEnded(t, paysLater.ID); got != "trial_ended 2026-11-01T00:00:00Z" {
		t.Fatalf("a workspace that locked and paid later: %q, want trial_ended at 2026-11-01T00:00:00Z", got)
	}
	for name, id := range map[string]string{
		"a subscription paid during the trial":               converted.ID,
		"a complimentary plan from before the end":           comp.ID,
		"a reconciled subscription that may predate the end": reconciled.ID,
	} {
		if got := e.trialEnded(t, id); got != "" {
			t.Fatalf("%s: %q, want no trial_ended", name, got)
		}
	}
}

// A payment or a reconcile may still be on its way when a trial ends: only a trial that ended at least an hour
// ago is decided.
func TestATrialThatEndedMinutesAgoIsNotDecidedYet(t *testing.T) {
	e := newEnv(t)
	w := e.workspace(t, "Alpha", "2026-11-01T00:00:00Z", "ann@alpha.example")
	e.run(t, "2026-11-01T00:30:00Z")
	if got := e.trialEnded(t, w.ID); got != "" {
		t.Fatalf("30 minutes after the end: %q, want nothing yet", got)
	}
	e.run(t, "2026-11-01T01:01:00Z")
	if got := e.trialEnded(t, w.ID); got != "trial_ended 2026-11-01T00:00:00Z" {
		t.Fatalf("an hour after the end: %q, want trial_ended at 2026-11-01T00:00:00Z", got)
	}
}

func TestRetentionDeletionLeavesTheLogAndTheViewRow(t *testing.T) {
	e := newEnv(t)
	a := e.workspace(t, "Alpha", "2026-11-01T00:00:00Z", "ann@alpha.example")
	e.run(t, "2026-11-02T00:00:00Z")
	before := e.milestones(t, a.ID)
	if !strings.Contains(before, "signed_up") || !strings.Contains(before, "trial_ended 2026-11-01T00:00:00Z") {
		t.Fatalf("log before deletion = %q", before)
	}
	e.run(t, "2027-10-02T00:00:00Z")
	if got := e.run(t, "2027-11-01T00:00:01Z"); got.Deleted != 1 || e.exists(t, a.ID) {
		t.Fatalf("deletion day: %+v, want the workspace deleted", got)
	}
	if after := e.milestones(t, a.ID); after != before {
		t.Fatalf("log after deletion = %q, want %q", after, before)
	}
	if rows := e.rows(t, a.ID); len(rows) != 2 || rows["workspace_events"] == 0 || rows["workspace_milestones"] != 1 {
		t.Fatalf("rows left of the deleted workspace = %v, want only its log and its view row", rows)
	}
}
