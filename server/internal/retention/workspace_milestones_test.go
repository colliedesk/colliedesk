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

// The trial's end is computed when the workspace is read; the retention pass writes it down as soon as
// it sees a trial that ran out with nothing paying, at the moment the trial ended.
func TestTheRetentionPassRecordsATrialThatEndedOnceAtItsEnd(t *testing.T) {
	e := newEnv(t)
	ended := e.workspace(t, "Alpha", "2026-11-01T00:00:00Z", "ann@alpha.example")
	paying := e.workspace(t, "Beta", "2026-11-01T00:00:00Z", "carl@beta.example")
	periodEnd := at("2026-12-01T00:00:00Z")
	e.pay(t, paying.ID, "sub_beta", "active", &periodEnd)
	// Gamma paid during its trial and cancelled later: its trial did not end unpaid.
	converted := e.workspace(t, "Gamma", "2026-11-01T00:00:00Z", "dana@gamma.example")
	for _, change := range []struct{ status, modified string }{{"active", "2026-10-20T00:00:00Z"}, {"canceled", "2026-11-20T00:00:00Z"}} {
		if _, err := e.store.ApplySubscription(context.Background(), converted.ID, "hook-gamma-"+change.status, "subscription.updated", domain.Subscription{
			Plan: domain.PlanTeam, SubscriptionID: "sub_gamma", CustomerID: "cus_gamma", Status: change.status, ModifiedAt: at(change.modified)}); err != nil {
			t.Fatal(err)
		}
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
	if got := e.trialEnded(t, paying.ID); got != "" {
		t.Fatalf("a paying workspace: %q, want no trial_ended", got)
	}
	if got := e.trialEnded(t, converted.ID); got != "" {
		t.Fatalf("a workspace that paid during its trial: %q, want no trial_ended", got)
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
