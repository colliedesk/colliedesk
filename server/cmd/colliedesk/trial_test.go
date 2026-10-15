package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

const trialTestOrigin = "https://desk.example.com"

type trialEnv struct {
	t           *testing.T
	databaseURL string
	pool        *pgxpool.Pool
	workspaceID string
}

func newTrialEnv(t *testing.T, name string) *trialEnv {
	t.Helper()
	databaseURL := cliDatabase(t)
	pool, err := openPool(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return (&trialEnv{t: t, databaseURL: databaseURL, pool: pool}).withWorkspace(name)
}

// withWorkspace creates another workspace in the same database.
func (e *trialEnv) withWorkspace(name string) *trialEnv {
	e.t.Helper()
	var created bytes.Buffer
	email := "admin@" + strings.ReplaceAll(strings.ToLower(name), " ", "") + ".example.com"
	if err := run(context.Background(), []string{"create-workspace", "--name", name, "--admin-email", email}, &created, e.databaseURL, trialTestOrigin); err != nil {
		e.t.Fatal(err)
	}
	next := &trialEnv{t: e.t, databaseURL: e.databaseURL, pool: e.pool}
	slug := strings.ReplaceAll(strings.ToLower(name), " ", "-")
	if err := e.pool.QueryRow(context.Background(), `SELECT id::text FROM workspaces WHERE slug=$1`, slug).Scan(&next.workspaceID); err != nil {
		e.t.Fatal(err)
	}
	return next
}

func (e *trialEnv) run(args ...string) (string, error) {
	e.t.Helper()
	var output bytes.Buffer
	err := run(context.Background(), args, &output, e.databaseURL, trialTestOrigin)
	return output.String(), err
}

func (e *trialEnv) mustRun(args ...string) string {
	e.t.Helper()
	output, err := e.run(args...)
	if err != nil {
		e.t.Fatalf("%v: %v", args, err)
	}
	return output
}

func (e *trialEnv) trialEnd() (ends time.Time, extended bool) {
	e.t.Helper()
	var extendedAt *time.Time
	if err := e.pool.QueryRow(context.Background(), `SELECT trial_ends_at, trial_extended_at FROM workspaces WHERE id=$1`, e.workspaceID).Scan(&ends, &extendedAt); err != nil {
		e.t.Fatal(err)
	}
	return ends.UTC(), extendedAt != nil
}

// access reads the plan in effect through the billing view the app shows, at a fixed instant.
func (e *trialEnv) access(now string) (plan string, locked bool) {
	e.t.Helper()
	billing, err := store.New(e.pool).GetBilling(context.Background(), e.workspaceID, mustTime(e.t, now))
	if err != nil {
		e.t.Fatal(err)
	}
	return billing.Plan, billing.Locked
}

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func setClock(t *testing.T, now string) {
	t.Helper()
	previous := clock
	clock = func() time.Time { return mustTime(t, now) }
	t.Cleanup(func() { clock = previous })
}

func TestTrialSetMovesTheEndAndReportsOldAndNew(t *testing.T) {
	env := newTrialEnv(t, "Trial Desk")
	env.mustRun("trial", "set", "--workspace", "trial-desk", "--ends", "2026-10-15T09:00:00Z")
	output := env.mustRun("trial", "set", "--workspace", "trial-desk", "--ends", "2026-12-01T00:00:00Z")
	ends, extended := env.trialEnd()
	if !ends.Equal(mustTime(t, "2026-12-01T00:00:00Z")) || extended {
		t.Fatalf("trial ends %s extended=%v", ends, extended)
	}
	if !strings.Contains(output, "2026-10-15T09:00:00Z") || !strings.Contains(output, "2026-12-01T00:00:00Z") {
		t.Fatalf("output names neither old nor new end:\n%s", output)
	}
}

func TestTrialSetRejectsBadInputAndLeavesTheRowAlone(t *testing.T) {
	env := newTrialEnv(t, "Trial Bad")
	env.mustRun("trial", "set", "--workspace", "trial-bad", "--ends", "2026-10-15T09:00:00Z")
	for name, args := range map[string][]string{
		"not a date":        {"trial", "set", "--workspace", "trial-bad", "--ends", "tomorrow"},
		"date without zone": {"trial", "set", "--workspace", "trial-bad", "--ends", "2026-12-01 10:00"},
		"missing ends":      {"trial", "set", "--workspace", "trial-bad"},
		"missing workspace": {"trial", "set", "--ends", "2026-12-01T00:00:00Z"},
		"unknown workspace": {"trial", "set", "--workspace", "nobody", "--ends", "2026-12-01T00:00:00Z"},
		"unknown verb":      {"trial", "reset", "--workspace", "trial-bad"},
	} {
		if _, err := env.run(args...); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if ends, _ := env.trialEnd(); !ends.Equal(mustTime(t, "2026-10-15T09:00:00Z")) {
		t.Fatalf("trial end changed to %s", ends)
	}
}

func TestCompPlanUnlocksSurvivesTheTrialEndAndRevokeRelocks(t *testing.T) {
	env := newTrialEnv(t, "Comp Desk")
	env.mustRun("trial", "set", "--workspace", "comp-desk", "--ends", "2026-10-15T09:00:00Z")
	if plan, locked := env.access("2026-10-20T09:00:00Z"); plan != "none" || !locked {
		t.Fatalf("before the grant: plan=%s locked=%v", plan, locked)
	}
	setClock(t, "2026-10-20T09:00:00Z")
	output := env.mustRun("plan", "grant", "--workspace", "comp-desk", "--plan", "team", "--until", "forever", "--by", "chinh")
	if !strings.Contains(output, "chinh") || !strings.Contains(output, "2026-10-20T09:00:00Z") {
		t.Fatalf("grant output lacks who or when:\n%s", output)
	}
	for _, now := range []string{"2026-10-20T09:00:01Z", "2027-06-01T00:00:00Z", "2030-01-01T00:00:00Z"} {
		if plan, locked := env.access(now); plan != "team" || locked {
			t.Fatalf("at %s with a comp plan: plan=%s locked=%v", now, plan, locked)
		}
	}
	var by string
	var at time.Time
	var until *time.Time
	if err := env.pool.QueryRow(context.Background(), `SELECT granted_by, granted_at, until FROM workspace_comp_plans WHERE workspace_id=$1 AND revoked_at IS NULL`, env.workspaceID).Scan(&by, &at, &until); err != nil {
		t.Fatal(err)
	}
	if by != "chinh" || !at.Equal(mustTime(t, "2026-10-20T09:00:00Z")) || until != nil {
		t.Fatalf("audit row: by=%q at=%s until=%v", by, at, until)
	}

	setClock(t, "2026-11-01T00:00:00Z")
	output = env.mustRun("plan", "revoke", "--workspace", "comp-desk", "--by", "long")
	if !strings.Contains(output, "long") {
		t.Fatalf("revoke output lacks who:\n%s", output)
	}
	if plan, locked := env.access("2026-11-01T00:00:01Z"); plan != "none" || !locked {
		t.Fatalf("after revoke: plan=%s locked=%v", plan, locked)
	}
	var revokedBy string
	if err := env.pool.QueryRow(context.Background(), `SELECT revoked_by FROM workspace_comp_plans WHERE workspace_id=$1`, env.workspaceID).Scan(&revokedBy); err != nil || revokedBy != "long" {
		t.Fatalf("revoked_by=%q err=%v", revokedBy, err)
	}
	if _, err := env.run("plan", "revoke", "--workspace", "comp-desk"); err == nil {
		t.Fatal("revoking with no comp plan in force succeeded")
	}
}

func TestCompPlanWithAnEndDateStopsAtThatInstant(t *testing.T) {
	env := newTrialEnv(t, "Comp Until")
	env.mustRun("trial", "set", "--workspace", "comp-until", "--ends", "2026-10-15T09:00:00Z")
	setClock(t, "2026-10-20T09:00:00Z")
	env.mustRun("plan", "grant", "--workspace", "comp-until", "--plan", "team", "--until", "2026-12-01T00:00:00Z", "--by", "chinh")
	if plan, locked := env.access("2026-11-30T23:59:59Z"); plan != "team" || locked {
		t.Fatalf("before the end: plan=%s locked=%v", plan, locked)
	}
	if plan, locked := env.access("2026-12-01T00:00:00Z"); plan != "none" || !locked {
		t.Fatalf("at the end: plan=%s locked=%v", plan, locked)
	}
	// A second grant replaces the first and keeps both rows as history.
	env.mustRun("plan", "grant", "--workspace", "comp-until", "--plan", "team", "--until", "forever", "--by", "chinh")
	if plan, _ := env.access("2027-01-01T00:00:00Z"); plan != "team" {
		t.Fatalf("after re-grant: plan=%s", plan)
	}
	var rows, inForce int
	if err := env.pool.QueryRow(context.Background(), `SELECT count(*), count(*) FILTER (WHERE revoked_at IS NULL) FROM workspace_comp_plans WHERE workspace_id=$1`, env.workspaceID).Scan(&rows, &inForce); err != nil || rows != 2 || inForce != 1 {
		t.Fatalf("rows=%d in force=%d err=%v", rows, inForce, err)
	}
}

func TestCompPlanRejectsBadInputAndStaysWithinOneWorkspace(t *testing.T) {
	env := newTrialEnv(t, "Comp Bad")
	other := env.withWorkspace("Comp Other")
	for name, args := range map[string][]string{
		"plan not allowed": {"plan", "grant", "--workspace", "comp-bad", "--plan", "none", "--until", "forever"},
		"no plan":          {"plan", "grant", "--workspace", "comp-bad", "--until", "forever"},
		"no until":         {"plan", "grant", "--workspace", "comp-bad", "--plan", "team"},
		"bad until":        {"plan", "grant", "--workspace", "comp-bad", "--plan", "team", "--until", "next year"},
		"unknown ws":       {"plan", "grant", "--workspace", "nobody", "--plan", "team", "--until", "forever"},
	} {
		if _, err := env.run(args...); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	env.mustRun("trial", "set", "--workspace", "comp-other", "--ends", "2026-10-15T09:00:00Z")
	setClock(t, "2026-10-20T09:00:00Z")
	env.mustRun("plan", "grant", "--workspace", "comp-bad", "--plan", "team", "--until", "forever", "--by", "chinh")
	if plan, locked := other.access("2026-10-21T00:00:00Z"); plan != "none" || !locked {
		t.Fatalf("other workspace: plan=%s locked=%v", plan, locked)
	}
}
