package httpapi_test

import (
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

func TestTrialWorkspaceBillingShowsLimitsAndSeatUsage(t *testing.T) {
	f := newBillingFixture(t)
	got := getBilling(t, f.api, f.adminA)
	if got.Plan != "trial" || got.Status != "none" || got.CurrentPeriodEnd != nil || got.CancelAtPeriodEnd || got.Locked {
		t.Fatalf("billing=%+v, want an unlocked trial workspace with no subscription", got)
	}
	if got.Limits.Agents != 10 {
		t.Fatalf("limits=%+v, want 10 agents", got.Limits)
	}
	if got.Usage.Agents != 2 {
		t.Fatalf("usage=%+v, want 2 agents (admin + invited agent)", got.Usage)
	}
}

func TestBillingFromOneWorkspaceNeverShowsAnothers(t *testing.T) {
	f := newBillingFixture(t)
	contact := create(t, f.api, f.adminA, "/api/v1/contacts", map[string]any{"name": "Only A", "email": "a@customer.example"})
	create(t, f.api, f.adminA, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "A only"})
	f.mustDeliver(t, "msg_iso", activeTeam(f.workspaceA, "sub_iso", time.Now()).event("subscription.active"))

	b := getBilling(t, f.api, f.adminB)
	if b.Plan != "trial" || b.Usage.Agents != 1 {
		t.Fatalf("workspace B sees billing=%+v, want its own trial state with 1 agent", b)
	}
	if a := getBilling(t, f.api, f.adminA); a.Plan != "team" || a.Usage.Agents != 2 {
		t.Fatalf("workspace A billing=%+v", a)
	}
}

func TestCheckoutUsesTheSessionsWorkspaceCustomerAndThePlansProduct(t *testing.T) {
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
			created := f.fake.callsTo("/v1/customers/")
			if len(created) != 1 || created[0].Auth != "Bearer polar-token-for-test" || created[0].Body["external_id"] != f.workspaceA {
				t.Fatalf("customer create calls=%v, want one for workspace A", created)
			}
			if created[0].Body["email"] != "admin@billing-alpha.example.com" {
				t.Fatalf("customer email=%v, want the admin's real address so receipts reach them", created[0].Body["email"])
			}
			checkouts := f.fake.callsTo("/v1/checkouts/")
			if len(checkouts) != 1 {
				t.Fatalf("checkout calls=%v", checkouts)
			}
			body := checkouts[0].Body
			products, _ := body["products"].([]any)
			if len(products) != 1 || products[0] != product || body["customer_id"] != f.fake.customerID(f.workspaceA) {
				t.Fatalf("polar body=%v, want product %s for workspace %s's customer", body, product, f.workspaceA)
			}
			if _, set := body["customer_email"]; set {
				t.Fatalf("checkout identifies the customer by email: %v", body)
			}
			if success, _ := body["success_url"].(string); success != testOrigin+"/settings/billing?checkout=success" {
				t.Fatalf("success_url=%q", success)
			}
		})
	}
}

// Polar keeps one customer per email, but one person can administer several workspaces. Each
// workspace gets its own Polar customer, so an order is never attached to the other workspace.
func TestCheckoutGivesEachWorkspaceItsOwnPolarCustomerEvenForTheSameAdminEmail(t *testing.T) {
	f := newBillingFixture(t)
	sameAdmin := createSession(t, f.api, f.service, "Billing Charlie", "admin@billing-alpha.example.com")
	workspaceC := workspaceID(t, f.api, sameAdmin)
	for _, token := range []string{f.adminA, sameAdmin, f.adminA} {
		if got := request(t, f.api, http.MethodPost, "/api/v1/billing/checkout", map[string]any{"plan": "team_monthly"}, token, nil); got.Code != http.StatusOK {
			t.Fatalf("checkout status=%d body=%s", got.Code, got.Body.String())
		}
	}
	customerA, customerC := f.fake.customerID(f.workspaceA), f.fake.customerID(workspaceC)
	if customerA == "" || customerC == "" || customerA == customerC {
		t.Fatalf("customers A=%q C=%q, want two distinct customers", customerA, customerC)
	}
	var used []any
	for _, call := range f.fake.callsTo("/v1/checkouts/") {
		used = append(used, call.Body["customer_id"])
	}
	if len(used) != 3 || used[0] != customerA || used[1] != customerC || used[2] != customerA {
		t.Fatalf("checkout customers=%v, want A, C, A", used)
	}
	// A takes the real address. C's first try with it is refused (Polar keeps emails unique), so C
	// is created once more with the workspace-tagged address. A's second checkout creates nothing.
	created := f.fake.callsTo("/v1/customers/")
	if len(created) != 3 {
		t.Fatalf("customer create calls=%d, want 3", len(created))
	}
	tagged := "admin+cd" + strings.ReplaceAll(workspaceC, "-", "")[:8] + "@billing-alpha.example.com"
	if created[0].Body["email"] != "admin@billing-alpha.example.com" || created[1].Body["email"] != "admin@billing-alpha.example.com" || created[2].Body["email"] != tagged {
		t.Fatalf("emails tried=%v, %v, %v; want real, real (refused), %s", created[0].Body["email"], created[1].Body["email"], created[2].Body["email"], tagged)
	}
}

func TestCheckoutIgnoresAnyWorkspaceOrCustomerFromTheClient(t *testing.T) {
	f := newBillingFixture(t)
	response := request(t, f.api, http.MethodPost, "/api/v1/billing/checkout",
		map[string]any{"plan": "team_monthly", "workspace_id": f.workspaceB, "external_customer_id": f.workspaceB, "customer_id": "cus_other"}, f.adminA, nil)
	if response.Code == http.StatusOK {
		calls := f.fake.callsTo("/v1/checkouts/")
		if len(calls) != 1 || calls[0].Body["customer_id"] != f.fake.customerID(f.workspaceA) || f.fake.customerID(f.workspaceB) != "" {
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

func TestPortalTreatsPolarsUnknownCustomerAnswersAsNotFound(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusUnprocessableEntity} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			f := newBillingFixture(t)
			f.mustDeliver(t, "msg_nf", activeTeam(f.workspaceA, "sub_nf", time.Now()).event("subscription.active"))
			f.fake.status["/v1/customer-sessions/"] = status
			if got := request(t, f.api, http.MethodPost, "/api/v1/billing/portal", nil, f.adminA, nil).Code; got != http.StatusNotFound {
				t.Errorf("Polar %d: status=%d, want 404", status, got)
			}
		})
	}
}

func TestPortalStaysAvailableAfterTheSubscriptionEnded(t *testing.T) {
	f := newBillingFixture(t)
	now := time.Now()
	ended := now.Add(-time.Hour)
	f.mustDeliver(t, "msg_gone", subscription{ID: "sub_gone", Workspace: f.workspaceA, Product: productMonthly, Status: "canceled",
		PeriodEnd: ended, EndedAt: &ended, ModifiedAt: now}.event("subscription.revoked"))
	if got := getBilling(t, f.api, f.adminA); got.Plan != "trial" {
		t.Fatalf("plan=%q, want trial", got.Plan)
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
