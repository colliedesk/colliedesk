package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

const reconcileNow = "2026-10-16T12:00:00Z"

// fakePolar answers the subscription listing the way Polar does: pages of items with a pagination
// block. It is the only fake in these tests; the database is real.
type fakePolar struct {
	mu       sync.Mutex
	pages    [][]map[string]any
	failPage int
	server   *httptest.Server
}

func newFakePolar(t *testing.T, pages ...[]map[string]any) *fakePolar {
	t.Helper()
	fake := &fakePolar{pages: pages}
	fake.server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		if request.URL.Path != "/v1/subscriptions/" || request.Header.Get("Authorization") != "Bearer polar-test-token" {
			http.Error(response, "unexpected request", http.StatusTeapot)
			return
		}
		page := 1
		if value := request.URL.Query().Get("page"); value != "" {
			page, _ = strconv.Atoi(value)
		}
		if page == fake.failPage {
			http.Error(response, "boom", http.StatusInternalServerError)
			return
		}
		items := []map[string]any{}
		if page >= 1 && page <= len(fake.pages) {
			items = fake.pages[page-1]
		}
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{"items": items,
			"pagination": map[string]any{"total_count": 100, "max_page": len(fake.pages)}})
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

func polarSub(id, status, product, externalID, modifiedAt string, extra map[string]any) map[string]any {
	sub := map[string]any{"id": id, "status": status, "product_id": product, "customer_id": "cus_" + id,
		"modified_at": modifiedAt, "created_at": "2026-10-01T00:00:00Z", "current_period_end": "2026-11-16T00:00:00Z",
		"cancel_at_period_end": false, "ended_at": nil,
		"customer": map[string]any{"external_id": externalID, "email": "buyer-" + id + "@customer.example"}}
	for key, value := range extra {
		sub[key] = value
	}
	return sub
}

type recordingMailer struct {
	mu       sync.Mutex
	messages []string
	to       [][]string
	err      error
}

func (m *recordingMailer) Send(_ context.Context, _ string, to []string, message []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.messages, m.to = append(m.messages, string(message)), append(m.to, to)
	return nil
}

type reconcileEnv struct {
	*trialEnv
	polar  *fakePolar
	mailer *recordingMailer
}

func newReconcileEnv(t *testing.T, name string, pages ...[]map[string]any) *reconcileEnv {
	t.Helper()
	env := &reconcileEnv{trialEnv: newTrialEnv(t, name), polar: newFakePolar(t, pages...), mailer: &recordingMailer{}}
	for key, value := range map[string]string{"POLAR_ACCESS_TOKEN": "polar-test-token", "POLAR_WEBHOOK_SECRET": "whsec_test",
		"POLAR_PRODUCT_TEAM_MONTHLY": "prod_team_m", "POLAR_PRODUCT_TEAM_YEARLY": "prod_team_y", "POLAR_PRODUCT_FOUNDING": "prod_founding",
		"POLAR_API_URL": env.polar.server.URL, "SMTP_HOST": "smtp.invalid", "MAIL_FROM_DOMAIN": "mail.example.com",
		"BILLING_ALERT_EMAIL": "ops@example.com"} {
		t.Setenv(key, value)
	}
	previousMailer := reconcileMailer
	reconcileMailer = func(email.SMTPConfig) alertMailer { return env.mailer }
	t.Cleanup(func() { reconcileMailer = previousMailer })
	setClock(t, reconcileNow)
	return env
}

// seed stores a subscription the way a webhook would have.
func (e *reconcileEnv) seed(subscriptionID, status, modifiedAt string) {
	e.t.Helper()
	modified := mustTime(e.t, modifiedAt)
	end := mustTime(e.t, "2026-11-16T00:00:00Z")
	result, err := store.New(e.pool).ApplySubscription(context.Background(), e.workspaceID, "seed-"+subscriptionID+"-"+modifiedAt, "subscription.updated",
		domain.Subscription{Plan: domain.PlanTeam, SubscriptionID: subscriptionID, CustomerID: "cus_" + subscriptionID, Status: status, CurrentPeriodEnd: &end, ModifiedAt: modified})
	if err != nil || result != store.SubscriptionApplied {
		e.t.Fatalf("seed %s: result %q err %v", subscriptionID, result, err)
	}
}

func (e *reconcileEnv) billing() domain.Billing {
	e.t.Helper()
	billing, err := store.New(e.pool).GetBilling(context.Background(), e.workspaceID, mustTime(e.t, reconcileNow))
	if err != nil {
		e.t.Fatal(err)
	}
	return billing
}

func (e *reconcileEnv) mails() []string {
	e.mailer.mu.Lock()
	defer e.mailer.mu.Unlock()
	return append([]string(nil), e.mailer.messages...)
}

func TestReconcileAppliesWhatALostWebhookLeftBehind(t *testing.T) {
	env := newReconcileEnv(t, "Lost Hook")
	env.seed("sub_lost", "incomplete", "2026-10-16T09:00:00Z")
	env.polar.pages = [][]map[string]any{{polarSub("sub_lost", "active", "prod_team_m", env.workspaceID, "2026-10-16T10:00:00Z", nil)}}

	output := env.mustRun("billing", "reconcile")

	if got := env.billing(); got.Plan != domain.PlanTeam || got.Status != "active" {
		t.Fatalf("after reconcile plan=%q status=%q, want team and active", got.Plan, got.Status)
	}
	if !strings.Contains(output, "1 applied") || !strings.Contains(output, "sub_lost") {
		t.Fatalf("summary does not name the applied subscription:\n%s", output)
	}
	mails := env.mails()
	if len(mails) != 1 || !strings.Contains(mails[0], "sub_lost") || !strings.Contains(mails[0], "ops@example.com") {
		t.Fatalf("want one summary mail to ops@example.com naming sub_lost, got %q", mails)
	}
	// A second run finds nothing to do: the same state is not applied or mailed twice.
	second := env.mustRun("billing", "reconcile")
	if !strings.Contains(second, "in sync") || len(env.mails()) != 1 {
		t.Fatalf("second run changed something: mails=%d\n%s", len(env.mails()), second)
	}
}

func TestReconcileStoresASubscriptionWhoseFirstWebhookNeverArrived(t *testing.T) {
	env := newReconcileEnv(t, "Never Arrived")
	env.polar.pages = [][]map[string]any{{polarSub("sub_new", "active", "prod_founding", env.workspaceID, "2026-10-16T10:00:00Z", nil)}}
	env.mustRun("billing", "reconcile")
	if got := env.billing(); got.Plan != domain.PlanFounding || got.Status != "active" {
		t.Fatalf("plan=%q status=%q, want founding and active", got.Plan, got.Status)
	}
}

func TestReconcileInSyncChangesNothingAndSendsNoMail(t *testing.T) {
	env := newReconcileEnv(t, "In Sync")
	env.seed("sub_ok", "active", "2026-10-16T10:00:00Z")
	env.seed("sub_older_at_polar", "active", "2026-10-16T10:00:00Z")
	env.polar.pages = [][]map[string]any{{
		polarSub("sub_ok", "active", "prod_team_m", env.workspaceID, "2026-10-16T10:00:00Z", nil),
		// Polar's copy is older than ours: ours stays.
		polarSub("sub_older_at_polar", "canceled", "prod_team_m", env.workspaceID, "2026-10-16T08:00:00Z", map[string]any{"ended_at": "2026-10-16T08:00:00Z"}),
	}}
	output := env.mustRun("billing", "reconcile")
	if !strings.Contains(output, "in sync") || len(env.mails()) != 0 {
		t.Fatalf("mails=%d output:\n%s", len(env.mails()), output)
	}
	if got := env.billing(); got.Plan != domain.PlanTeam || got.Status != "active" {
		t.Fatalf("plan=%q status=%q, want team and active", got.Plan, got.Status)
	}
}

func TestReconcileReportsPaidSubscriptionsWithoutAWorkspaceAndWritesNothing(t *testing.T) {
	env := newReconcileEnv(t, "Has Workspace")
	env.polar.pages = [][]map[string]any{{
		polarSub("sub_ghost", "active", "prod_team_m", "11111111-2222-3333-4444-555555555555", "2026-10-16T10:00:00Z", nil),
		polarSub("sub_no_external", "past_due", "prod_founding", "", "2026-10-16T10:00:00Z", nil),
		// Not paid: nothing to report.
		polarSub("sub_gone_ghost", "canceled", "prod_team_m", "11111111-2222-3333-4444-555555555556", "2026-10-16T10:00:00Z", nil),
	}}
	output := env.mustRun("billing", "reconcile")
	if !strings.Contains(output, "2 unmatched") {
		t.Fatalf("summary:\n%s", output)
	}
	mails := env.mails()
	if len(mails) != 1 || !strings.Contains(mails[0], "sub_ghost") || !strings.Contains(mails[0], "sub_no_external") || strings.Contains(mails[0], "sub_gone_ghost") {
		t.Fatalf("want one mail naming the two paid unmatched subscriptions only, got %q", mails)
	}
	if !strings.Contains(mails[0], "buyer-sub_ghost@customer.example") {
		t.Fatalf("the mail must name the buyer so the operator can find the workspace:\n%s", mails[0])
	}
	if got := env.billing(); got.Plan == domain.PlanTeam || got.Plan == domain.PlanFounding {
		t.Fatalf("an unmatched subscription changed workspace plan to %q", got.Plan)
	}
}

func TestReconcileDowngradesAWorkspaceWhosePolarSubscriptionEnded(t *testing.T) {
	env := newReconcileEnv(t, "Ended Desk")
	env.seed("sub_end", "active", "2026-10-16T09:00:00Z")
	if got := env.billing(); got.Plan != domain.PlanTeam {
		t.Fatalf("setup: plan %q", got.Plan)
	}
	env.polar.pages = [][]map[string]any{{polarSub("sub_end", "canceled", "prod_team_m", env.workspaceID, "2026-10-16T10:30:00Z",
		map[string]any{"ended_at": "2026-10-16T10:30:00Z", "current_period_end": "2026-10-16T10:30:00Z"})}}
	env.mustRun("billing", "reconcile")
	if got := env.billing(); got.Plan == domain.PlanTeam || got.Status != "canceled" {
		t.Fatalf("plan=%q status=%q, want a downgrade with status canceled", got.Plan, got.Status)
	}
	if len(env.mails()) != 1 {
		t.Fatalf("a downgrade is reported: mails=%d", len(env.mails()))
	}
}

func TestReconcileReportsPaidWorkspacesWhosePolarSubscriptionIsAbsent(t *testing.T) {
	env := newReconcileEnv(t, "Absent Desk")
	env.seed("sub_vanished", "active", "2026-10-16T09:00:00Z")
	env.polar.pages = [][]map[string]any{{}}
	output := env.mustRun("billing", "reconcile")
	mails := env.mails()
	if !strings.Contains(output, "1 absent at Polar") || len(mails) != 1 || !strings.Contains(mails[0], "sub_vanished") {
		t.Fatalf("mails=%q output:\n%s", mails, output)
	}
	if got := env.billing(); got.Plan != domain.PlanTeam {
		t.Fatalf("an absent subscription must be reported, not removed: plan %q", got.Plan)
	}
}

func TestReconcileFailsClosedWhenAnyPolarPageFails(t *testing.T) {
	env := newReconcileEnv(t, "Fail Closed")
	env.seed("sub_a", "incomplete", "2026-10-16T09:00:00Z")
	env.polar.pages = [][]map[string]any{
		{polarSub("sub_a", "active", "prod_team_m", env.workspaceID, "2026-10-16T10:00:00Z", nil)},
		{polarSub("sub_b", "active", "prod_team_m", "11111111-2222-3333-4444-555555555555", "2026-10-16T10:00:00Z", nil)},
	}
	env.polar.failPage = 2
	output, err := env.run("billing", "reconcile")
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("want an error naming HTTP 500, got %v", err)
	}
	if got := env.billing(); got.Status != "incomplete" {
		t.Fatalf("page 1 was applied although page 2 failed: status %q\n%s", got.Status, output)
	}
	if len(env.mails()) != 0 {
		t.Fatalf("mail sent on a failed read: %q", env.mails())
	}
}

func TestReconcileReadsEveryPage(t *testing.T) {
	env := newReconcileEnv(t, "Two Pages")
	env.seed("sub_p2", "incomplete", "2026-10-16T09:00:00Z")
	env.polar.pages = [][]map[string]any{
		{polarSub("sub_other", "canceled", "prod_team_m", "11111111-2222-3333-4444-555555555555", "2026-10-16T10:00:00Z", nil)},
		{polarSub("sub_p2", "active", "prod_team_m", env.workspaceID, "2026-10-16T10:00:00Z", nil)},
	}
	env.mustRun("billing", "reconcile")
	if got := env.billing(); got.Status != "active" {
		t.Fatalf("subscription on page 2 not applied: status %q", got.Status)
	}
}

func TestReconcileDryRunPrintsTheDiffAndChangesNothing(t *testing.T) {
	env := newReconcileEnv(t, "Dry Desk")
	env.seed("sub_dry", "incomplete", "2026-10-16T09:00:00Z")
	env.polar.pages = [][]map[string]any{{
		polarSub("sub_dry", "active", "prod_team_m", env.workspaceID, "2026-10-16T10:00:00Z", nil),
		polarSub("sub_ghost", "active", "prod_team_m", "11111111-2222-3333-4444-555555555555", "2026-10-16T10:00:00Z", nil),
	}}
	output := env.mustRun("billing", "reconcile", "--dry-run")
	if !strings.Contains(output, "dry run") || !strings.Contains(output, "sub_dry") || !strings.Contains(output, "sub_ghost") {
		t.Fatalf("dry run output:\n%s", output)
	}
	if got := env.billing(); got.Status != "incomplete" {
		t.Fatalf("dry run wrote: status %q", got.Status)
	}
	if len(env.mails()) != 0 {
		t.Fatalf("dry run sent mail: %q", env.mails())
	}
}

func TestReconcileWithoutAlertConfigurationStillAppliesAndWarnsOnStderr(t *testing.T) {
	env := newReconcileEnv(t, "No Alert")
	env.seed("sub_n", "incomplete", "2026-10-16T09:00:00Z")
	env.polar.pages = [][]map[string]any{{polarSub("sub_n", "active", "prod_team_m", env.workspaceID, "2026-10-16T10:00:00Z", nil)}}
	t.Setenv("BILLING_ALERT_EMAIL", "")
	var warned bytes.Buffer
	previous := stderr
	stderr = &warned
	t.Cleanup(func() { stderr = previous })
	env.mustRun("billing", "reconcile")
	if got := env.billing(); got.Status != "active" {
		t.Fatalf("status %q, want active", got.Status)
	}
	if !strings.Contains(warned.String(), "BILLING_ALERT_EMAIL") || len(env.mails()) != 0 {
		t.Fatalf("want a stderr warning naming BILLING_ALERT_EMAIL and no mail: stderr=%q mails=%d", warned.String(), len(env.mails()))
	}
}

func TestReconcileFailsWhenTheAlertMailCannotBeSentButKeepsTheAppliedChange(t *testing.T) {
	env := newReconcileEnv(t, "Mail Down")
	env.seed("sub_m", "incomplete", "2026-10-16T09:00:00Z")
	env.polar.pages = [][]map[string]any{{polarSub("sub_m", "active", "prod_team_m", env.workspaceID, "2026-10-16T10:00:00Z", nil)}}
	env.mailer.err = context.DeadlineExceeded
	if _, err := env.run("billing", "reconcile"); err == nil || !strings.Contains(err.Error(), "alert") {
		t.Fatalf("want an error about the alert mail, got %v", err)
	}
	if got := env.billing(); got.Status != "active" {
		t.Fatalf("the change must stay applied when only the mail fails: status %q", got.Status)
	}
}

func TestReconcileNeedsBillingAndRejectsBadArguments(t *testing.T) {
	env := newReconcileEnv(t, "Args Desk")
	for name, args := range map[string][]string{
		"unknown verb":     {"billing", "sync"},
		"no verb":          {"billing"},
		"positional":       {"billing", "reconcile", "now"},
		"unknown flag":     {"billing", "reconcile", "--force"},
		"dry-run with arg": {"billing", "reconcile", "--dry-run=maybe"},
	} {
		var out bytes.Buffer
		if err := run(context.Background(), args, &out, env.databaseURL, trialTestOrigin); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	for _, key := range []string{"POLAR_ACCESS_TOKEN", "POLAR_WEBHOOK_SECRET", "POLAR_PRODUCT_TEAM_MONTHLY", "POLAR_PRODUCT_TEAM_YEARLY", "POLAR_PRODUCT_FOUNDING", "POLAR_API_URL"} {
		t.Setenv(key, "")
	}
	if _, err := env.run("billing", "reconcile"); err == nil || !strings.Contains(err.Error(), "billing is not configured") {
		t.Fatalf("want billing is not configured, got %v", err)
	}
}
