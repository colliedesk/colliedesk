package polar_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/polar"
)

// fakePolar stands in for the Polar API, an external boundary. It records the last request.
type fakePolar struct {
	server *httptest.Server
	method string
	path   string
	auth   string
	body   map[string]any
	status int
	reply  string
}

func newFakePolar(t *testing.T, status int, reply string) *fakePolar {
	t.Helper()
	fake := &fakePolar{status: status, reply: reply}
	fake.server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		fake.method, fake.path, fake.auth = request.Method, request.URL.Path, request.Header.Get("Authorization")
		fake.body = nil
		_ = json.NewDecoder(request.Body).Decode(&fake.body)
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(fake.status)
		_, _ = response.Write([]byte(fake.reply))
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
		ProductID: "prod-1", ExternalCustomerID: "ws-1", CustomerEmail: "admin@example.com",
		SuccessURL: "https://desk.example.com/settings/billing?checkout=success",
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
	if fake.body["external_customer_id"] != "ws-1" || fake.body["customer_email"] != "admin@example.com" ||
		fake.body["success_url"] != "https://desk.example.com/settings/billing?checkout=success" {
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

func TestClientErrorsNeverCarryTheToken(t *testing.T) {
	fake := newFakePolar(t, http.StatusUnauthorized, `{"detail":"bad token-for-test"}`)
	_, err := fake.client().CreateCheckout(context.Background(), polar.CheckoutRequest{ProductID: "p", ExternalCustomerID: "w"})
	if err == nil || strings.Contains(err.Error(), "token-for-test") {
		t.Fatalf("err=%v, want a failure without the token", err)
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("err=%v, want the upstream status", err)
	}
}

func TestClientRejectsAReplyWithoutURL(t *testing.T) {
	fake := newFakePolar(t, http.StatusCreated, `{"id":"co_1"}`)
	if _, err := fake.client().CreateCheckout(context.Background(), polar.CheckoutRequest{ProductID: "p", ExternalCustomerID: "w"}); err == nil {
		t.Fatal("want an error when Polar returns no checkout url")
	}
}
