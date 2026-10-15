package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestBillingIsAdminOnly(t *testing.T) {
	f := newBillingFixture(t)
	for _, route := range [][2]string{{http.MethodGet, "/api/v1/billing"}, {http.MethodPost, "/api/v1/billing/checkout"}, {http.MethodPost, "/api/v1/billing/portal"}} {
		var body any
		if route[1] == "/api/v1/billing/checkout" {
			body = map[string]any{"plan": "team_monthly"}
		}
		if got := request(t, f.api, route[0], route[1], body, "", nil).Code; got != http.StatusUnauthorized {
			t.Errorf("%s %s without a session: status=%d, want 401", route[0], route[1], got)
		}
		if got := request(t, f.api, route[0], route[1], body, f.agentTokenA, nil).Code; got != http.StatusForbidden {
			t.Errorf("%s %s as an agent: status=%d, want 403", route[0], route[1], got)
		}
	}
	if calls := f.fake.recorded(); len(calls) != 0 {
		t.Fatalf("forbidden requests reached Polar: %v", calls)
	}
}

func TestFreeWorkspaceBillingShowsLimitsAndThisMonthsUsage(t *testing.T) {
	f := newBillingFixture(t)
	contact := create(t, f.api, f.adminA, "/api/v1/contacts", map[string]any{"name": "Usage", "email": "usage@customer.example"})
	ids := make([]string, 4)
	for index := range ids {
		ids[index] = create(t, f.api, f.adminA, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Usage"})
	}
	// Hand-written calendar: 40 days ago and one second before the UTC month starts are last month's;
	// the exact start of the UTC month and now are this month's. Expected usage: 2.
	for index, moment := range []string{
		`now() - interval '40 days'`,
		`date_trunc('month', now() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' - interval '1 second'`,
		`date_trunc('month', now() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'`,
	} {
		if _, err := f.pool.Exec(context.Background(), `UPDATE conversations SET created_at=`+moment+` WHERE id=$1`, ids[index]); err != nil {
			t.Fatal(err)
		}
	}
	got := getBilling(t, f.api, f.adminA)
	if got.Plan != "free" || got.Status != "none" || got.CurrentPeriodEnd != nil || got.CancelAtPeriodEnd {
		t.Fatalf("billing=%+v, want a free workspace with no subscription", got)
	}
	if got.Limits.Agents != 3 || got.Limits.ConversationsPerMonth == nil || *got.Limits.ConversationsPerMonth != 100 {
		t.Fatalf("limits=%+v, want 3 agents and 100 conversations a month", got.Limits)
	}
	if got.Usage.Agents != 2 || got.Usage.ConversationsThisMonth != 2 {
		t.Fatalf("usage=%+v, want 2 agents (admin + invited agent) and 2 conversations this month", got.Usage)
	}
}

func TestBillingFromOneWorkspaceNeverShowsAnothers(t *testing.T) {
	f := newBillingFixture(t)
	contact := create(t, f.api, f.adminA, "/api/v1/contacts", map[string]any{"name": "Only A", "email": "a@customer.example"})
	create(t, f.api, f.adminA, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "A only"})
	f.mustDeliver(t, "msg_iso", activeTeam(f.workspaceA, "sub_iso", time.Now()).event("subscription.active"))

	b := getBilling(t, f.api, f.adminB)
	if b.Plan != "free" || b.Usage.ConversationsThisMonth != 0 || b.Usage.Agents != 1 {
		t.Fatalf("workspace B sees billing=%+v, want its own free state with 1 agent and 0 conversations", b)
	}
	if a := getBilling(t, f.api, f.adminA); a.Plan != "team" || a.Usage.ConversationsThisMonth != 1 {
		t.Fatalf("workspace A billing=%+v", a)
	}
}

func TestCheckoutUsesTheSessionsWorkspaceAndThePlansProduct(t *testing.T) {
	for plan, product := range map[string]string{"team_monthly": productMonthly, "team_yearly": productYearly, "founding": productFounding} {
		t.Run(plan, func(t *testing.T) {
			f := newBillingFixture(t)
			response := request(t, f.api, http.MethodPost, "/api/v1/billing/checkout", map[string]any{"plan": plan}, f.adminA, nil)
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			var reply struct {
				URL string `json:"url"`
			}
			decode(t, response, &reply)
			if reply.URL != "https://polar.test/checkout/1" {
				t.Fatalf("url=%q, want the URL Polar returned", reply.URL)
			}
			calls := f.fake.recorded()
			if len(calls) != 1 || calls[0].Path != "/v1/checkouts/" || calls[0].Auth != "Bearer polar-token-for-test" {
				t.Fatalf("calls=%v", calls)
			}
			body := calls[0].Body
			products, _ := body["products"].([]any)
			if len(products) != 1 || products[0] != product || body["external_customer_id"] != f.workspaceA ||
				body["customer_email"] != "admin@billing-alpha.example.com" {
				t.Fatalf("polar body=%v, want product %s for workspace %s", body, product, f.workspaceA)
			}
			if success, _ := body["success_url"].(string); success != testOrigin+"/settings/billing?checkout=success" {
				t.Fatalf("success_url=%q", success)
			}
		})
	}
}

func TestCheckoutIgnoresAnyWorkspaceOrCustomerFromTheClient(t *testing.T) {
	f := newBillingFixture(t)
	response := request(t, f.api, http.MethodPost, "/api/v1/billing/checkout",
		map[string]any{"plan": "team_monthly", "workspace_id": f.workspaceB, "external_customer_id": f.workspaceB, "customer_id": "cus_other"}, f.adminA, nil)
	if response.Code == http.StatusOK {
		calls := f.fake.recorded()
		if len(calls) != 1 || calls[0].Body["external_customer_id"] != f.workspaceA {
			t.Fatalf("client-sent workspace changed the checkout: %v", calls)
		}
		return
	}
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400 or a checkout for the session's own workspace", response.Code)
	}
	if calls := f.fake.recorded(); len(calls) != 0 {
		t.Fatalf("a rejected request reached Polar: %v", calls)
	}
}

func TestCheckoutRejectsUnknownPlans(t *testing.T) {
	f := newBillingFixture(t)
	for _, body := range []any{map[string]any{"plan": "enterprise"}, map[string]any{"plan": ""}, map[string]any{}, map[string]any{"plan": 7}} {
		if got := request(t, f.api, http.MethodPost, "/api/v1/billing/checkout", body, f.adminA, nil).Code; got != http.StatusBadRequest {
			t.Errorf("body %v: status=%d, want 400", body, got)
		}
	}
	if calls := f.fake.recorded(); len(calls) != 0 {
		t.Fatalf("invalid requests reached Polar: %v", calls)
	}
}

func TestCheckoutRefusesAWorkspaceThatAlreadyPays(t *testing.T) {
	f := newBillingFixture(t)
	f.mustDeliver(t, "msg_paid", activeTeam(f.workspaceA, "sub_paid", time.Now()).event("subscription.active"))
	response := request(t, f.api, http.MethodPost, "/api/v1/billing/checkout", map[string]any{"plan": "team_yearly"}, f.adminA, nil)
	if response.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s, want 409: a second subscription would bill twice", response.Code, response.Body.String())
	}
	if calls := f.fake.recorded(); len(calls) != 0 {
		t.Fatalf("calls=%v, want none", calls)
	}
}

func TestCheckoutReportsAPolarFailureAsBadGateway(t *testing.T) {
	f := newBillingFixture(t)
	f.fake.status["/v1/checkouts/"] = http.StatusInternalServerError
	response := request(t, f.api, http.MethodPost, "/api/v1/billing/checkout", map[string]any{"plan": "team_monthly"}, f.adminA, nil)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s, want 502", response.Code, response.Body.String())
	}
	if body := response.Body.String(); strings.Contains(body, "polar-token-for-test") || strings.Contains(body, "fake failure") {
		t.Fatalf("error leaks upstream detail: %s", body)
	}
}

func TestPortalOpensThePolarPortalOfTheSessionsWorkspace(t *testing.T) {
	f := newBillingFixture(t)
	f.mustDeliver(t, "msg_portal", activeTeam(f.workspaceA, "sub_portal", time.Now()).event("subscription.active"))
	response := request(t, f.api, http.MethodPost, "/api/v1/billing/portal", nil, f.adminA, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var reply struct {
		URL string `json:"url"`
	}
	decode(t, response, &reply)
	calls := f.fake.recorded()
	if reply.URL != "https://polar.test/portal/1" || len(calls) != 1 || calls[0].Path != "/v1/customer-sessions/" ||
		calls[0].Body["external_customer_id"] != f.workspaceA || calls[0].Body["return_url"] != testOrigin+"/settings/billing" {
		t.Fatalf("url=%q calls=%v", reply.URL, calls)
	}
}

func TestPortalWithoutAPaymentHistoryIsNotFoundAndNeverCallsPolar(t *testing.T) {
	f := newBillingFixture(t)
	f.mustDeliver(t, "msg_other_paid", activeTeam(f.workspaceA, "sub_a_only", time.Now()).event("subscription.active"))
	if got := request(t, f.api, http.MethodPost, "/api/v1/billing/portal", nil, f.adminB, nil).Code; got != http.StatusNotFound {
		t.Fatalf("status=%d, want 404 for a workspace that never paid (A's customer is not B's)", got)
	}
	if calls := f.fake.recorded(); len(calls) != 0 {
		t.Fatalf("calls=%v, want none", calls)
	}
}

func TestPortalStaysAvailableAfterTheSubscriptionEnded(t *testing.T) {
	f := newBillingFixture(t)
	now := time.Now()
	ended := now.Add(-time.Hour)
	f.mustDeliver(t, "msg_gone", subscription{ID: "sub_gone", Workspace: f.workspaceA, Product: productMonthly, Status: "canceled",
		PeriodEnd: ended, EndedAt: &ended, ModifiedAt: now}.event("subscription.revoked"))
	if got := getBilling(t, f.api, f.adminA); got.Plan != "free" {
		t.Fatalf("plan=%q, want free", got.Plan)
	}
	if got := request(t, f.api, http.MethodPost, "/api/v1/billing/portal", nil, f.adminA, nil).Code; got != http.StatusOK {
		t.Fatalf("status=%d, want 200: invoices and card stay reachable", got)
	}
}

func TestPortalReportsAPolarFailureAsBadGateway(t *testing.T) {
	f := newBillingFixture(t)
	f.mustDeliver(t, "msg_pf", activeTeam(f.workspaceA, "sub_pf", time.Now()).event("subscription.active"))
	f.fake.status["/v1/customer-sessions/"] = http.StatusServiceUnavailable
	if got := request(t, f.api, http.MethodPost, "/api/v1/billing/portal", nil, f.adminA, nil).Code; got != http.StatusBadGateway {
		t.Fatalf("status=%d, want 502", got)
	}
}

func TestConfigAdvertisesBilling(t *testing.T) {
	f := newBillingFixture(t)
	var config struct {
		Billing struct {
			Enabled bool `json:"enabled"`
		} `json:"billing"`
	}
	decode(t, request(t, f.api, http.MethodGet, "/api/v1/config", nil, "", nil), &config)
	if !config.Billing.Enabled {
		t.Fatal("config.billing.enabled=false with Polar configured")
	}
}

func TestBillingDisabledRemovesEveryBillingRoute(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Self Hosted", "admin@self-hosted.example.com")
	for _, route := range [][2]string{
		{http.MethodGet, "/api/v1/billing"}, {http.MethodPost, "/api/v1/billing/checkout"},
		{http.MethodPost, "/api/v1/billing/portal"}, {http.MethodPost, webhookPath},
	} {
		if got := request(t, api, route[0], route[1], map[string]any{"plan": "team_monthly"}, admin, nil).Code; got != http.StatusNotFound {
			t.Errorf("%s %s with billing disabled: status=%d, want 404", route[0], route[1], got)
		}
	}
	var config struct {
		Billing struct {
			Enabled bool `json:"enabled"`
		} `json:"billing"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/config", nil, "", nil), &config)
	if config.Billing.Enabled {
		t.Fatal("config.billing.enabled=true without Polar configuration")
	}
}
