package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

const (
	foundingPath      = "/api/v1/founding"
	productsFounding  = "/v1/products/" + productFounding
	subscriptionsPath = "/v1/subscriptions/"
)

type foundingView struct {
	Slots int  `json:"slots"`
	Taken *int `json:"taken"`
	Open  bool `json:"open"`
}

func (f billingFixture) refreshFounding() error {
	return f.api.(*httpapi.Server).RefreshFounding(context.Background())
}

func (f billingFixture) founding(t *testing.T) (foundingView, *httptest.ResponseRecorder) {
	t.Helper()
	response := request(t, f.api, http.MethodGet, foundingPath, nil, "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GET %s status=%d body=%s", foundingPath, response.Code, response.Body.String())
	}
	var view foundingView
	decode(t, response, &view)
	return view, response
}

func (f billingFixture) foundingEvent(workspace, id string) []byte {
	sub := activeTeam(workspace, id, time.Now())
	sub.Product = productFounding
	return sub.event("subscription.active")
}

func (f billingFixture) offersFounding(t *testing.T) bool {
	t.Helper()
	for _, plan := range getBilling(t, f.api, f.adminA).CheckoutPlans {
		if plan == "founding" {
			return true
		}
	}
	return false
}

func (f billingFixture) checkout(t *testing.T, plan string) *httptest.ResponseRecorder {
	t.Helper()
	return request(t, f.api, http.MethodPost, "/api/v1/billing/checkout", map[string]any{"plan": plan}, f.adminA, nil)
}

func TestFoundingEndpointIsUnknownUntilTheFirstCountAndNeverCallsPolar(t *testing.T) {
	f := newBillingFixture(t)
	view, response := f.founding(t)
	if view.Slots != 10 || view.Taken != nil || !view.Open {
		t.Fatalf("before the first count: %+v, want slots 10, taken null, open", view)
	}
	if !strings.Contains(response.Body.String(), `"taken":null`) {
		t.Fatalf("body=%s, want an explicit null for taken", response.Body.String())
	}
	if got := response.Header().Get("Cache-Control"); got != "public, max-age=60" {
		t.Fatalf("Cache-Control=%q", got)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "https://colliedesk.com" {
		t.Fatalf("Access-Control-Allow-Origin=%q", got)
	}
	validateContractResponse(t, newRequest(t, http.MethodGet, foundingPath, nil, "", nil), response)

	f.fake.setHolders(productFounding, 3)
	if err := f.refreshFounding(); err != nil {
		t.Fatal(err)
	}
	view, response = f.founding(t)
	if view.Slots != 10 || view.Taken == nil || *view.Taken != 3 || !view.Open {
		t.Fatalf("after the count: %+v, want 3 of 10 taken and open", view)
	}
	validateContractResponse(t, newRequest(t, http.MethodGet, foundingPath, nil, "", nil), response)
	before := len(f.fake.recorded())
	f.founding(t)
	if after := len(f.fake.recorded()); after != before {
		t.Fatalf("the endpoint made %d Polar calls, want none", after-before)
	}
}

func TestFoundingCountAsksPolarForThatProductWithAuthAndSlotStatuses(t *testing.T) {
	f := newBillingFixture(t)
	f.fake.setHolders(productFounding, 1)
	if err := f.refreshFounding(); err != nil {
		t.Fatal(err)
	}
	calls := f.fake.callsTo(subscriptionsPath)
	if len(calls) != 1 || calls[0].Auth != "Bearer polar-token-for-test" {
		t.Fatalf("count calls=%+v", calls)
	}
	for _, want := range []string{"product_id=" + productFounding, "status=active", "status=trialing", "status=past_due", "limit=1"} {
		if !strings.Contains(calls[0].Query, want) {
			t.Errorf("query %q lacks %q", calls[0].Query, want)
		}
	}
}

func TestTenthFoundingSlotArchivesTheProductAndClosesFounding(t *testing.T) {
	f := newBillingFixture(t)
	f.fake.setHolders(productFounding, 10)
	f.mustDeliver(t, "msg_tenth", f.foundingEvent(f.workspaceA, "sub_tenth"))

	patches := f.fake.callsTo(productsFounding)
	archives := 0
	for _, call := range patches {
		if call.Body["is_archived"] == true {
			archives++
		}
	}
	if archives != 1 || !f.fake.isArchived(productFounding) {
		t.Fatalf("archive requests=%d archived=%v, want exactly one archive of the Founding product", archives, f.fake.isArchived(productFounding))
	}
	if view, _ := f.founding(t); view.Open || view.Taken == nil || *view.Taken != 10 {
		t.Fatalf("endpoint=%+v, want 10 taken and closed", view)
	}
	if f.offersFounding(t) {
		t.Fatal("billing still offers founding after it closed")
	}
	if got := strings.Join(getBilling(t, f.api, f.adminA).CheckoutPlans, ","); got != "solo_monthly,solo_yearly,small_monthly,small_yearly,team_monthly,team_yearly,team25_monthly,team25_yearly" {
		t.Fatalf("checkout_plans=%s", got)
	}
}

func TestCheckoutOfFoundingIsRefusedOnceClosedAndOtherPlansStillWork(t *testing.T) {
	f := newBillingFixture(t)
	f.fake.setHolders(productFounding, 10)
	if err := f.refreshFounding(); err != nil {
		t.Fatal(err)
	}
	response := f.checkout(t, "founding")
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"founding_closed"`) {
		t.Fatalf("status=%d body=%s, want 409 founding_closed", response.Code, response.Body.String())
	}
	if len(f.fake.callsTo("/v1/checkouts/")) != 0 {
		t.Fatal("a closed Founding plan reached the checkout call")
	}
	if response := f.checkout(t, "team_monthly"); response.Code != http.StatusOK {
		t.Fatalf("team_monthly status=%d body=%s", response.Code, response.Body.String())
	}
	invalid := request(t, f.api, http.MethodPost, "/api/v1/billing/checkout", map[string]any{"plan": "nope"}, f.adminA, nil)
	if invalid.Code != http.StatusBadRequest || strings.Contains(invalid.Body.String(), "founding") ||
		!strings.Contains(invalid.Body.String(), "team25_monthly or team25_yearly") {
		t.Fatalf("invalid plan: status=%d body=%s, want a message that lists only the plans on sale", invalid.Code, invalid.Body.String())
	}
}

// A team of 11 to 25 agents buys Team 25 while Founding still has slots: the web app offers both
// side by side, so the server must sell both, each with its own product.
func TestTeam25CheckoutIsSoldWhileFoundingIsOpen(t *testing.T) {
	for plan, product := range map[string]string{"team25_monthly": productTeam25Monthly, "team25_yearly": productTeam25Yearly} {
		t.Run(plan, func(t *testing.T) {
			f := newBillingFixture(t)
			f.fake.setHolders(productFounding, 3)
			if err := f.refreshFounding(); err != nil {
				t.Fatal(err)
			}
			if !f.offersFounding(t) {
				t.Fatal("Founding is not offered with 3 of 10 slots taken")
			}
			if offered := "," + strings.Join(getBilling(t, f.api, f.adminA).CheckoutPlans, ",") + ","; !strings.Contains(offered, ","+plan+",") {
				t.Fatalf("checkout_plans=%s does not list %s while Founding is open", offered, plan)
			}
			response := f.checkout(t, plan)
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s, want 200", response.Code, response.Body.String())
			}
			checkouts := f.fake.callsTo("/v1/checkouts/")
			if len(checkouts) != 1 {
				t.Fatalf("checkout calls=%v, want one", checkouts)
			}
			if products, _ := checkouts[0].Body["products"].([]any); len(products) != 1 || products[0] != product {
				t.Fatalf("polar products=%v, want only %s", products, product)
			}
		})
	}
}

func TestNinthSlotLeavesFoundingOpen(t *testing.T) {
	f := newBillingFixture(t)
	f.fake.setHolders(productFounding, 9)
	f.mustDeliver(t, "msg_ninth", f.foundingEvent(f.workspaceA, "sub_ninth"))
	if view, _ := f.founding(t); !view.Open || view.Taken == nil || *view.Taken != 9 {
		t.Fatalf("endpoint=%+v, want 9 taken and open", view)
	}
	if f.fake.isArchived(productFounding) || !f.offersFounding(t) {
		t.Fatal("founding was closed at 9 slots")
	}
}

func TestFoundingEventWithoutAWorkspaceStillRefreshesTheCount(t *testing.T) {
	f := newBillingFixture(t)
	f.fake.setHolders(productFounding, 10)
	response := f.deliver(t, "msg_static_link", f.foundingEvent("", "sub_static"))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "ignored") {
		t.Fatalf("status=%d body=%s, want 200 ignored", response.Code, response.Body.String())
	}
	if len(f.fake.callsTo(subscriptionsPath)) != 1 || !f.fake.isArchived(productFounding) {
		t.Fatalf("count calls=%d archived=%v, want the buyer without a workspace to count and close Founding",
			len(f.fake.callsTo(subscriptionsPath)), f.fake.isArchived(productFounding))
	}
	if view, _ := f.founding(t); view.Open {
		t.Fatalf("endpoint=%+v, want closed", view)
	}
}

func TestFailedRefreshNeverChangesTheWebhookAnswer(t *testing.T) {
	f := newBillingFixture(t)
	f.fake.status[subscriptionsPath] = http.StatusInternalServerError
	response := f.deliver(t, "msg_refresh_fails", f.foundingEvent(f.workspaceA, "sub_refresh_fails"))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "applied") {
		t.Fatalf("status=%d body=%s, want 200 applied", response.Code, response.Body.String())
	}
	if got := getBilling(t, f.api, f.adminA); got.Plan != "founding" {
		t.Fatalf("plan=%q, want the subscription applied", got.Plan)
	}
	if view, _ := f.founding(t); view.Taken != nil || !view.Open {
		t.Fatalf("endpoint=%+v, want unknown and open after a failed count", view)
	}
	if !f.offersFounding(t) {
		t.Fatal("unknown state must keep founding on offer; the Polar archive is the hard stop")
	}
	other := request(t, f.api, http.MethodPost, "/api/v1/billing/checkout", map[string]any{"plan": "founding"}, f.adminB, nil)
	if response := other; response.Code != http.StatusOK {
		t.Fatalf("checkout while unknown: status=%d body=%s, want 200", response.Code, response.Body.String())
	}
}

func TestAProductArchivedByHandClosesFoundingBelowTheCap(t *testing.T) {
	f := newBillingFixture(t)
	f.fake.setHolders(productFounding, 2)
	f.fake.setArchived(productFounding)
	if err := f.refreshFounding(); err != nil {
		t.Fatal(err)
	}
	if view, _ := f.founding(t); view.Open || view.Taken == nil || *view.Taken != 2 {
		t.Fatalf("endpoint=%+v, want 2 taken and closed", view)
	}
	if f.offersFounding(t) || f.checkout(t, "founding").Code != http.StatusConflict {
		t.Fatal("an archived Founding product must not be offered or sold in the app")
	}
	for _, call := range f.fake.callsTo(productsFounding) {
		if call.Body != nil {
			t.Fatalf("patched an already archived product: %+v", call)
		}
	}
}

func TestClosingIsOneWayWhenASubscriptionLaterEnds(t *testing.T) {
	f := newBillingFixture(t)
	f.fake.setHolders(productFounding, 10)
	f.mustDeliver(t, "msg_close", f.foundingEvent(f.workspaceA, "sub_close"))
	f.fake.setHolders(productFounding, 9)
	f.mustDeliver(t, "msg_cancel", f.foundingEvent("", "sub_cancel"))
	if view, _ := f.founding(t); view.Open || view.Taken == nil || *view.Taken != 9 {
		t.Fatalf("endpoint=%+v, want 9 taken and still closed", view)
	}
	if !f.fake.isArchived(productFounding) || f.offersFounding(t) {
		t.Fatal("founding reopened after a slot was freed")
	}
}

func TestFailedArchiveStillClosesTheAppAndIsRetriedByTheNextEvent(t *testing.T) {
	f := newBillingFixture(t)
	f.fake.setHolders(productFounding, 10)
	f.fake.status["PATCH "+productsFounding] = http.StatusForbidden
	if response := f.deliver(t, "msg_archive_fails", f.foundingEvent(f.workspaceA, "sub_archive_fails")); response.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200 though the archive failed", response.Code)
	}
	if f.fake.isArchived(productFounding) || f.offersFounding(t) || f.checkout(t, "founding").Code != http.StatusConflict {
		t.Fatalf("archived=%v offered=%v, want the app closed and Polar not yet archived", f.fake.isArchived(productFounding), f.offersFounding(t))
	}
	delete(f.fake.status, "PATCH "+productsFounding)
	f.mustDeliver(t, "msg_retry", f.foundingEvent("", "sub_retry"))
	if !f.fake.isArchived(productFounding) {
		t.Fatal("the next Founding event did not retry the archive")
	}
}

func TestOtherProductsNeverTriggerAFoundingCount(t *testing.T) {
	f := newBillingFixture(t)
	f.mustDeliver(t, "msg_team", activeTeam(f.workspaceA, "sub_team", time.Now()).event("subscription.active"))
	if calls := f.fake.recorded(); len(calls) != 0 {
		t.Fatalf("a Team event made Polar calls: %+v", calls)
	}
}

func TestFoundingEndpointIsAbsentWithoutBilling(t *testing.T) {
	api, _, _, _ := testAPIWithConfig(t, nil)
	if response := request(t, api, http.MethodGet, foundingPath, nil, "", nil); response.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404 on a self-hosted server", response.Code)
	}
}
