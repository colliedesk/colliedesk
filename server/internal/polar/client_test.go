package polar_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/polar"
)

// fakePolar stands in for the Polar API, an external boundary. It records the last request.
type fakePolar struct {
	server *httptest.Server
	method string
	path   string
	query  url.Values
	auth   string
	body   map[string]any
	status int
	reply  string
	// perPath answers "METHOD path" with its own reply; other requests get status and reply.
	perPath map[string]canned
}

type canned struct {
	status int
	body   string
}

func newFakePolar(t *testing.T, status int, reply string) *fakePolar {
	t.Helper()
	fake := &fakePolar{status: status, reply: reply}
	fake.server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		fake.method, fake.path, fake.query, fake.auth = request.Method, request.URL.Path, request.URL.Query(), request.Header.Get("Authorization")
		fake.body = nil
		_ = json.NewDecoder(request.Body).Decode(&fake.body)
		response.Header().Set("Content-Type", "application/json")
		answer := canned{fake.status, fake.reply}
		if specific, ok := fake.perPath[request.Method+" "+request.URL.Path]; ok {
			answer = specific
		}
		response.WriteHeader(answer.status)
		_, _ = response.Write([]byte(answer.body))
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakePolar) client() *polar.Client {
	return polar.NewClient(polar.Config{AccessToken: "token-for-test", APIURL: f.server.URL})
}

func TestCreateCheckoutSendsProductAndExternalCustomer(t *testing.T) {
	fake := newFakePolar(t, http.StatusCreated, `{"id":"co_1","url":"https://polar.test/checkout/co_1"}`)
	url, err := fake.client().CreateCheckout(context.Background(), polar.CheckoutRequest{
		ProductID: "prod-1", CustomerID: "cus-1",
		SuccessURL:  "https://desk.example.com/settings/billing?checkout=success",
		EmbedOrigin: "https://desk.example.com",
	})
	if err != nil || url != "https://polar.test/checkout/co_1" {
		t.Fatalf("url=%q err=%v", url, err)
	}
	if fake.method != http.MethodPost || fake.path != "/v1/checkouts/" || fake.auth != "Bearer token-for-test" {
		t.Fatalf("request=%s %s auth=%q", fake.method, fake.path, fake.auth)
	}
	products, _ := fake.body["products"].([]any)
	if len(products) != 1 || products[0] != "prod-1" {
		t.Fatalf("products=%v", fake.body["products"])
	}
	if fake.body["embed_origin"] != "https://desk.example.com" {
		t.Fatalf("embed_origin=%v", fake.body["embed_origin"])
	}
	if fake.body["customer_id"] != "cus-1" || fake.body["success_url"] != "https://desk.example.com/settings/billing?checkout=success" {
		t.Fatalf("body=%v", fake.body)
	}
}

func TestCreatePortalSessionSendsExternalCustomer(t *testing.T) {
	fake := newFakePolar(t, http.StatusCreated, `{"id":"cs_1","customer_portal_url":"https://polar.test/portal/abc"}`)
	url, err := fake.client().CreatePortalSession(context.Background(), "ws-1", "https://desk.example.com/settings/billing")
	if err != nil || url != "https://polar.test/portal/abc" {
		t.Fatalf("url=%q err=%v", url, err)
	}
	if fake.method != http.MethodPost || fake.path != "/v1/customer-sessions/" || fake.body["external_customer_id"] != "ws-1" ||
		fake.body["return_url"] != "https://desk.example.com/settings/billing" {
		t.Fatalf("request=%s %s body=%v", fake.method, fake.path, fake.body)
	}
}

func TestPortalSessionReportsUnknownCustomer(t *testing.T) {
	fake := newFakePolar(t, http.StatusNotFound, `{"detail":"Customer not found"}`)
	_, err := fake.client().CreatePortalSession(context.Background(), "ws-1", "https://desk.example.com/")
	if !errors.Is(err, polar.ErrCustomerNotFound) {
		t.Fatalf("err=%v, want ErrCustomerNotFound", err)
	}
}

func TestPortalReportsUnknownCustomerOn422(t *testing.T) {
	fake := newFakePolar(t, http.StatusUnprocessableEntity, `{"detail":[{"msg":"Customer not found"}]}`)
	if _, err := fake.client().CreatePortalSession(context.Background(), "ws-1", ""); !errors.Is(err, polar.ErrCustomerNotFound) {
		t.Fatalf("err=%v, want ErrCustomerNotFound", err)
	}
}

func TestEnsureCustomerCreatesWhenTheExternalIDIsUnknown(t *testing.T) {
	fake := newFakePolar(t, http.StatusNotFound, `{}`)
	fake.perPath = map[string]canned{
		"GET /v1/customers/external/ws-1": {http.StatusNotFound, `{"detail":"Customer not found"}`},
		"POST /v1/customers/":             {http.StatusCreated, `{"id":"cus-new"}`},
	}
	id, err := fake.client().EnsureCustomer(context.Background(), "ws-1", "admin+cd@example.com", "Beta Desk")
	if err != nil || id != "cus-new" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if fake.method != http.MethodPost || fake.body["external_id"] != "ws-1" || fake.body["email"] != "admin+cd@example.com" || fake.body["name"] != "Beta Desk" {
		t.Fatalf("last request=%s body=%v", fake.method, fake.body)
	}
}

func TestEnsureCustomerReusesAnExistingOne(t *testing.T) {
	fake := newFakePolar(t, http.StatusNotFound, `{}`)
	fake.perPath = map[string]canned{"GET /v1/customers/external/ws-1": {http.StatusOK, `{"id":"cus-old","external_id":"ws-1"}`}}
	id, err := fake.client().EnsureCustomer(context.Background(), "ws-1", "a@example.com", "Beta Desk")
	if err != nil || id != "cus-old" || fake.method != http.MethodGet {
		t.Fatalf("id=%q err=%v method=%s, want the existing customer and no create", id, err, fake.method)
	}
}

func TestEnsureCustomerFailsWhenPolarRefusesTheCreate(t *testing.T) {
	fake := newFakePolar(t, http.StatusNotFound, `{}`)
	fake.perPath = map[string]canned{
		"GET /v1/customers/external/ws-1": {http.StatusNotFound, `{}`},
		"POST /v1/customers/":             {http.StatusUnprocessableEntity, `{"detail":"email exists"}`},
	}
	_, err := fake.client().EnsureCustomer(context.Background(), "ws-1", "a@example.com", "Beta Desk")
	if !errors.Is(err, polar.ErrCustomerConflict) {
		t.Fatalf("err=%v, want ErrCustomerConflict so the caller can try another address", err)
	}
}

func TestClientErrorsNeverCarryTheToken(t *testing.T) {
	fake := newFakePolar(t, http.StatusUnauthorized, `{"detail":"bad token-for-test"}`)
	_, err := fake.client().CreateCheckout(context.Background(), polar.CheckoutRequest{ProductID: "p", CustomerID: "c"})
	if err == nil || strings.Contains(err.Error(), "token-for-test") {
		t.Fatalf("err=%v, want a failure without the token", err)
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("err=%v, want the upstream status", err)
	}
}

func TestClientRejectsAReplyWithoutURL(t *testing.T) {
	fake := newFakePolar(t, http.StatusCreated, `{"id":"co_1"}`)
	if _, err := fake.client().CreateCheckout(context.Background(), polar.CheckoutRequest{ProductID: "p", CustomerID: "c"}); err == nil {
		t.Fatal("want an error when Polar returns no checkout url")
	}
}

func TestHasPaidSubscriptionReadsTheCustomersSubscriptionsByExternalID(t *testing.T) {
	now := time.Date(2027, 11, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		reply string
		want  bool
	}{
		{"none", `{"items":[],"pagination":{"total_count":0,"max_page":0}}`, false},
		{"active", `{"items":[{"status":"active","current_period_end":"2027-12-01T00:00:00Z","ended_at":null}],"pagination":{"total_count":1,"max_page":1}}`, true},
		{"past due keeps the plan", `{"items":[{"status":"past_due","current_period_end":"2027-10-01T00:00:00Z","ended_at":null}],"pagination":{"total_count":1,"max_page":1}}`, true},
		{"canceled but paid until the future", `{"items":[{"status":"canceled","current_period_end":"2027-11-15T00:00:00Z","ended_at":null}],"pagination":{"total_count":1,"max_page":1}}`, true},
		{"canceled and over", `{"items":[{"status":"canceled","current_period_end":"2027-10-15T00:00:00Z","ended_at":"2027-10-15T00:00:00Z"}],"pagination":{"total_count":1,"max_page":1}}`, false},
		{"one dead and one live", `{"items":[{"status":"canceled","ended_at":"2027-01-01T00:00:00Z"},{"status":"trialing","ended_at":null}],"pagination":{"total_count":2,"max_page":1}}`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := newFakePolar(t, http.StatusOK, c.reply)
			got, err := fake.client().HasPaidSubscription(context.Background(), "ws-1", now)
			if err != nil || got != c.want {
				t.Fatalf("HasPaidSubscription = %v, %v; want %v", got, err, c.want)
			}
			if fake.method != http.MethodGet || fake.path != "/v1/subscriptions/" {
				t.Fatalf("request %s %s", fake.method, fake.path)
			}
		})
	}
}

func TestHasPaidSubscriptionFailsOnPolarErrors(t *testing.T) {
	fake := newFakePolar(t, http.StatusInternalServerError, `{}`)
	if got, err := fake.client().HasPaidSubscription(context.Background(), "ws-1", time.Now()); err == nil || got {
		t.Fatalf("got %v, %v; want an error and false", got, err)
	}
}
