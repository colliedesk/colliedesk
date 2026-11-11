package main

import (
	"bytes"
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
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
	t.Setenv("USER", "ops")
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

func TestTrialSetRecordsWhoChangedWhatAndWhen(t *testing.T) {
	env := newTrialEnv(t, "Trial Audit")
	setClock(t, "2026-10-16T08:00:00Z")
	env.mustRun("trial", "set", "--workspace", "trial-audit", "--ends", "2026-10-15T09:00:00Z", "--by", "chinh")
	env.mustRun("trial", "set", "--workspace", "trial-audit", "--ends", "2026-12-01T00:00:00Z")
	rows, err := env.pool.Query(context.Background(), `SELECT changed_by, changed_at, new_ends_at FROM workspace_trial_changes WHERE workspace_id=$1 ORDER BY changed_at, new_ends_at`, env.workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type change struct {
		by       string
		at, ends time.Time
	}
	var got []change
	for rows.Next() {
		var c change
		if err := rows.Scan(&c.by, &c.at, &c.ends); err != nil {
			t.Fatal(err)
		}
		got = append(got, c)
	}
	if len(got) != 2 || got[0].by != "chinh" || got[1].by != "ops" ||
		!got[0].at.Equal(mustTime(t, "2026-10-16T08:00:00Z")) || !got[1].ends.Equal(mustTime(t, "2026-12-01T00:00:00Z")) {
		t.Fatalf("trial changes = %+v, want chinh then the default operator ops", got)
	}
	var previous time.Time
	if err := env.pool.QueryRow(context.Background(), `SELECT previous_ends_at FROM workspace_trial_changes WHERE workspace_id=$1 AND new_ends_at=$2`,
		env.workspaceID, mustTime(t, "2026-12-01T00:00:00Z")).Scan(&previous); err != nil || !previous.Equal(mustTime(t, "2026-10-15T09:00:00Z")) {
		t.Fatalf("previous end = %s, %v; want 2026-10-15T09:00:00Z", previous, err)
	}
}

func TestPlanRevokeRejectsGrantFlags(t *testing.T) {
	env := newTrialEnv(t, "Revoke Flags")
	setClock(t, "2026-10-20T09:00:00Z")
	env.mustRun("plan", "grant", "--workspace", "revoke-flags", "--plan", "team", "--until", "forever", "--by", "chinh")
	for _, flag := range [][]string{{"--plan", "team"}, {"--until", "forever"}} {
		args := append([]string{"plan", "revoke", "--workspace", "revoke-flags"}, flag...)
		if _, err := env.run(args...); err == nil {
			t.Errorf("revoke accepted %v", flag)
		}
	}
	if plan, _ := env.access("2026-10-21T00:00:00Z"); plan != "team" {
		t.Fatalf("a rejected revoke still changed the plan: %s", plan)
	}
}

func TestExpiredGrantIsRecordedAsExpiredNotReplaced(t *testing.T) {
	env := newTrialEnv(t, "Comp Expired")
	setClock(t, "2026-10-20T09:00:00Z")
	env.mustRun("plan", "grant", "--workspace", "comp-expired", "--plan", "team", "--until", "2026-11-01T00:00:00Z", "--by", "chinh")
	setClock(t, "2026-12-01T00:00:00Z")
	output := env.mustRun("plan", "grant", "--workspace", "comp-expired", "--plan", "team", "--until", "forever", "--by", "long")
	if !strings.Contains(output, "expired") || strings.Contains(output, "Replaced") {
		t.Fatalf("grant output should say the previous grant had expired:\n%s", output)
	}
	var reason, by string
	var at time.Time
	if err := env.pool.QueryRow(context.Background(), `SELECT revoke_reason, revoked_by, revoked_at FROM workspace_comp_plans WHERE workspace_id=$1 AND granted_by='chinh'`, env.workspaceID).Scan(&reason, &by, &at); err != nil {
		t.Fatal(err)
	}
	if reason != "expired" || by == "long" || !at.Equal(mustTime(t, "2026-11-01T00:00:00Z")) {
		t.Fatalf("expired row: reason=%q revoked_by=%q revoked_at=%s", reason, by, at)
	}

	setClock(t, "2026-12-02T00:00:00Z")
	output = env.mustRun("plan", "grant", "--workspace", "comp-expired", "--plan", "team", "--until", "2027-06-01T00:00:00Z", "--by", "chinh")
	if !strings.Contains(output, "Replaced") || !strings.Contains(output, "long") {
		t.Fatalf("grant output should name the grant it replaced:\n%s", output)
	}
	if err := env.pool.QueryRow(context.Background(), `SELECT revoke_reason, revoked_by FROM workspace_comp_plans WHERE workspace_id=$1 AND granted_by='long'`, env.workspaceID).Scan(&reason, &by); err != nil || reason != "replaced" || by != "chinh" {
		t.Fatalf("replaced row: reason=%q revoked_by=%q err=%v", reason, by, err)
	}
}

func TestRevokingOnlyAnExpiredGrantSaysItExpired(t *testing.T) {
	env := newTrialEnv(t, "Revoke Expired")
	setClock(t, "2026-10-20T09:00:00Z")
	env.mustRun("plan", "grant", "--workspace", "revoke-expired", "--plan", "team", "--until", "2026-11-01T00:00:00Z", "--by", "chinh")
	setClock(t, "2026-12-01T00:00:00Z")
	_, err := env.run("plan", "revoke", "--workspace", "revoke-expired", "--by", "long")
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("revoke of an expired grant: %v, want an error that says it expired", err)
	}
	var reason string
	if err := env.pool.QueryRow(context.Background(), `SELECT revoke_reason FROM workspace_comp_plans WHERE workspace_id=$1`, env.workspaceID).Scan(&reason); err != nil || reason != "expired" {
		t.Fatalf("revoke_reason=%q err=%v, want expired", reason, err)
	}
}

func TestConcurrentGrantsFailWithAClearErrorNeverARawConstraintViolation(t *testing.T) {
	env := newTrialEnv(t, "Comp Race")
	setClock(t, "2026-10-20T09:00:00Z")
	const workers = 8
	errs := make([]error, workers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, errs[i] = env.run("plan", "grant", "--workspace", "comp-race", "--plan", "team", "--until", "forever", "--by", "chinh")
		}()
	}
	close(start)
	wg.Wait()
	succeeded := 0
	for _, err := range errs {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, store.ErrCompGrantConflict) && !strings.Contains(err.Error(), "SQLSTATE"):
		default:
			t.Errorf("concurrent grant failed with %v, want success or ErrCompGrantConflict", err)
		}
	}
	var inForce int
	if err := env.pool.QueryRow(context.Background(), `SELECT count(*) FROM workspace_comp_plans WHERE workspace_id=$1 AND revoked_at IS NULL`, env.workspaceID).Scan(&inForce); err != nil || inForce != 1 || succeeded == 0 {
		t.Fatalf("in force=%d succeeded=%d err=%v, want exactly one grant in force", inForce, succeeded, err)
	}
}

var offerLinkPattern = regexp.MustCompile(`https://desk\.example\.com/trial-offer\?token=([A-Za-z0-9_-]+)`)
var offerIDPattern = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

func TestTrialOfferPrintsALinkValidForThirtyDaysByDefault(t *testing.T) {
	env := newTrialEnv(t, "Offer Desk")
	setClock(t, "2026-10-20T09:00:00Z")
	output := env.mustRun("trial", "offer", "--workspace", "offer-desk")
	link := offerLinkPattern.FindStringSubmatch(output)
	if link == nil || !strings.Contains(output, "2026-11-19T09:00:00Z") || offerIDPattern.FindString(output) == "" {
		t.Fatalf("offer output lacks the link, its id or its expiry 2026-11-19T09:00:00Z:\n%s", output)
	}
	var stored []byte
	if err := env.pool.QueryRow(context.Background(), `SELECT token_hash FROM trial_offers WHERE workspace_id=$1`, env.workspaceID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored), link[1]) {
		t.Fatal("the raw token is stored; only its hash may be")
	}
	output = env.mustRun("trial", "offer", "--workspace", "offer-desk", "--valid", "7d")
	if !strings.Contains(output, "2026-10-27T09:00:00Z") {
		t.Fatalf("--valid 7d should expire at 2026-10-27T09:00:00Z:\n%s", output)
	}
	output = env.mustRun("trial", "offer", "--workspace", "offer-desk", "--valid", "36h")
	if !strings.Contains(output, "2026-10-21T21:00:00Z") {
		t.Fatalf("--valid 36h should expire at 2026-10-21T21:00:00Z:\n%s", output)
	}
}

func TestTrialOfferRefusesMoreThanThirtyDaysAndBadInput(t *testing.T) {
	env := newTrialEnv(t, "Offer Bad")
	for name, args := range map[string][]string{
		"31 days":           {"trial", "offer", "--workspace", "offer-bad", "--valid", "31d"},
		"721 hours":         {"trial", "offer", "--workspace", "offer-bad", "--valid", "721h"},
		"zero":              {"trial", "offer", "--workspace", "offer-bad", "--valid", "0"},
		"negative":          {"trial", "offer", "--workspace", "offer-bad", "--valid", "-1h"},
		"not a duration":    {"trial", "offer", "--workspace", "offer-bad", "--valid", "a month"},
		"missing workspace": {"trial", "offer"},
		"unknown workspace": {"trial", "offer", "--workspace", "nobody"},
		"revoke without id": {"trial", "offer", "revoke"},
		"revoke unknown":    {"trial", "offer", "revoke", "00000000-0000-0000-0000-000000000001"},
		"revoke not an id":  {"trial", "offer", "revoke", "abc"},
		"offers no ws":      {"trial", "offers"},
	} {
		if _, err := env.run(args...); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	_, err := env.run("trial", "offer", "--workspace", "offer-bad", "--valid", "31d")
	if err == nil || !strings.Contains(err.Error(), "30 days") {
		t.Fatalf("over 30 days: %v, want an error that names the 30-day limit", err)
	}
	var offers int
	if err := env.pool.QueryRow(context.Background(), `SELECT count(*) FROM trial_offers`).Scan(&offers); err != nil || offers != 0 {
		t.Fatalf("offers stored after refusals: %d %v", offers, err)
	}
}

func TestTrialOffersListsEachStateAndRevokeClosesAnOpenOne(t *testing.T) {
	env := newTrialEnv(t, "Offer List")
	other := env.withWorkspace("Offer Other")
	setClock(t, "2026-10-20T09:00:00Z")
	openID := offerIDPattern.FindString(env.mustRun("trial", "offer", "--workspace", "offer-list", "--by", "chinh"))
	revokedID := offerIDPattern.FindString(env.mustRun("trial", "offer", "--workspace", "offer-list"))
	acceptedOutput := env.mustRun("trial", "offer", "--workspace", "offer-list")
	env.mustRun("trial", "offer", "--workspace", "offer-list", "--valid", "1d")
	var agentID string
	if err := env.pool.QueryRow(context.Background(), `SELECT id::text FROM agents WHERE workspace_id=$1`, env.workspaceID).Scan(&agentID); err != nil {
		t.Fatal(err)
	}
	token := offerLinkPattern.FindStringSubmatch(acceptedOutput)[1]
	if _, err := store.New(env.pool).AcceptTrialOffer(context.Background(), env.workspaceID, token, agentID, "admin@offerlist.example.com", mustTime(t, "2026-10-20T10:00:00Z")); err != nil {
		t.Fatal(err)
	}
	setClock(t, "2026-10-22T09:00:00Z")
	output := env.mustRun("trial", "offer", "revoke", revokedID, "--by", "long")
	if !strings.Contains(output, revokedID) || !strings.Contains(output, "long") {
		t.Fatalf("revoke output lacks the id or who:\n%s", output)
	}
	if _, err := env.run("trial", "offer", "revoke", revokedID); err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("revoking a revoked offer: %v, want an error that says it is revoked", err)
	}

	output = env.mustRun("trial", "offers", "--workspace", "offer-list")
	for _, want := range []string{
		openID + "  open", "chinh", revokedID + "  revoked", "by long", "accepted by admin@offerlist.example.com at 2026-10-20T10:00:00Z", "expired",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("offers list lacks %q:\n%s", want, output)
		}
	}
	if output := other.mustRun("trial", "offers", "--workspace", "offer-other"); strings.Contains(output, openID) || !strings.Contains(output, "No trial offers") {
		t.Fatalf("another workspace's list:\n%s", output)
	}
}
