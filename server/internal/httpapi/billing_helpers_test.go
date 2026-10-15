package httpapi_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/polar"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	webhookPath = "/api/v1/webhooks/polar"
	// A Standard Webhooks secret: "whsec_" plus the base64 of the 32-byte key below.
	billingWebhookSecret = "whsec_Y29sbGllZGVzay10ZXN0LXNpZ25pbmcta2V5LTMyYnk="
	billingWebhookKey    = "colliedesk-test-signing-key-32by"

	productMonthly  = "prod-team-monthly"
	productYearly   = "prod-team-yearly"
	productFounding = "prod-founding"
)

// polarCall is one request the server made to the fake Polar API.
type polarCall struct {
	Path string
	Auth string
	Body map[string]any
}

// fakePolarAPI stands in for the Polar API, an external boundary.
type fakePolarAPI struct {
	server *httptest.Server
	mu     sync.Mutex
	calls  []polarCall
	// status overrides the reply status for a path; zero means success.
	status map[string]int
}

func newFakePolarAPI(t *testing.T) *fakePolarAPI {
	t.Helper()
	fake := &fakePolarAPI{status: map[string]int{}}
	fake.server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(request.Body).Decode(&body)
		fake.mu.Lock()
		fake.calls = append(fake.calls, polarCall{Path: request.URL.Path, Auth: request.Header.Get("Authorization"), Body: body})
		number := len(fake.calls)
		status := fake.status[request.URL.Path]
		fake.mu.Unlock()
		response.Header().Set("Content-Type", "application/json")
		if status != 0 {
			response.WriteHeader(status)
			_, _ = response.Write([]byte(`{"detail":"fake failure"}`))
			return
		}
		response.WriteHeader(http.StatusCreated)
		switch request.URL.Path {
		case "/v1/checkouts/":
			fmt.Fprintf(response, `{"id":"co_%d","url":"https://polar.test/checkout/%d"}`, number, number)
		case "/v1/customer-sessions/":
			fmt.Fprintf(response, `{"id":"cs_%d","customer_portal_url":"https://polar.test/portal/%d"}`, number, number)
		}
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakePolarAPI) recorded() []polarCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]polarCall(nil), f.calls...)
}

func (f *fakePolarAPI) config() polar.Config {
	return polar.Config{
		AccessToken: "polar-token-for-test", WebhookSecret: billingWebhookSecret, APIURL: f.server.URL,
		Products: polar.Products{TeamMonthly: productMonthly, TeamYearly: productYearly, Founding: productFounding},
	}
}

type billingFixture struct {
	api         http.Handler
	service     *auth.Service
	pool        *pgxpool.Pool
	fake        *fakePolarAPI
	adminA      string
	adminB      string
	workspaceA  string
	workspaceB  string
	agentTokenA string
}

func newBillingFixture(t *testing.T) billingFixture {
	t.Helper()
	fake := newFakePolarAPI(t)
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.Billing = fake.config() })
	adminA := createSession(t, api, service, "Billing Alpha", "admin@billing-alpha.example.com")
	adminB := createSession(t, api, service, "Billing Bravo", "admin@billing-bravo.example.com")
	_, agentURL := createContractInvite(t, api, adminA, "agent@billing-alpha.example.com", "agent")
	var agent struct {
		Token string `json:"token"`
	}
	decode(t, request(t, api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, agentURL)}, "", nil), &agent)
	return billingFixture{api: api, service: service, pool: pool, fake: fake, adminA: adminA, adminB: adminB,
		workspaceA: workspaceID(t, api, adminA), workspaceB: workspaceID(t, api, adminB), agentTokenA: agent.Token}
}

// subscription describes the Polar subscription payload fields the server reads.
type subscription struct {
	ID                string
	Workspace         string // customer.external_id; empty sends null
	Product           string
	Status            string
	PeriodEnd         time.Time
	CancelAtPeriodEnd bool
	EndedAt           *time.Time
	ModifiedAt        time.Time
}

func (s subscription) event(eventType string) []byte {
	var external any
	if s.Workspace != "" {
		external = s.Workspace
	}
	var ended any
	if s.EndedAt != nil {
		ended = s.EndedAt.Format(time.RFC3339)
	}
	payload := map[string]any{
		"type": eventType, "timestamp": time.Now().UTC().Format(time.RFC3339),
		"data": map[string]any{
			"id": s.ID, "status": s.Status, "product_id": s.Product, "customer_id": "cus_" + s.ID,
			"current_period_end": s.PeriodEnd.Format(time.RFC3339), "cancel_at_period_end": s.CancelAtPeriodEnd,
			"ended_at": ended, "modified_at": s.ModifiedAt.Format(time.RFC3339),
			"customer": map[string]any{"id": "cus_" + s.ID, "external_id": external},
		},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return encoded
}

// sign computes the Standard Webhooks signature with the standard library, independently of the
// server code: HMAC-SHA256 over "<id>.<timestamp>.<body>", base64, prefixed "v1,".
func sign(id string, timestamp int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(billingWebhookKey))
	mac.Write([]byte(id + "." + strconv.FormatInt(timestamp, 10) + "."))
	mac.Write(body)
	return "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func webhookRequest(id string, timestamp int64, signature string, body []byte) *http.Request {
	req := httptest.NewRequest(http.MethodPost, webhookPath, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if id != "" {
		req.Header.Set("webhook-id", id)
	}
	req.Header.Set("webhook-timestamp", strconv.FormatInt(timestamp, 10))
	if signature != "" {
		req.Header.Set("webhook-signature", signature)
	}
	return req
}

// signedWebhookRequest signs body now.
func signedWebhookRequest(id string, body []byte) *http.Request {
	now := time.Now().Unix()
	return webhookRequest(id, now, sign(id, now, body), body)
}

func (f billingFixture) deliver(t *testing.T, id string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	return serve(f.api, signedWebhookRequest(id, body))
}

func (f billingFixture) mustDeliver(t *testing.T, id string, body []byte) {
	t.Helper()
	if response := f.deliver(t, id, body); response.Code != http.StatusOK {
		t.Fatalf("webhook %s status=%d body=%s", id, response.Code, response.Body.String())
	}
}

type billingView struct {
	Plan              string     `json:"plan"`
	Status            string     `json:"status"`
	CurrentPeriodEnd  *time.Time `json:"current_period_end"`
	CancelAtPeriodEnd bool       `json:"cancel_at_period_end"`
	Limits            struct {
		Agents                int  `json:"agents"`
		ConversationsPerMonth *int `json:"conversations_per_month"`
	} `json:"limits"`
	Usage struct {
		Agents                 int `json:"agents"`
		ConversationsThisMonth int `json:"conversations_this_month"`
	} `json:"usage"`
}

func getBilling(t *testing.T, api http.Handler, token string) billingView {
	t.Helper()
	response := request(t, api, http.MethodGet, "/api/v1/billing", nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/billing status=%d body=%s", response.Code, response.Body.String())
	}
	var view billingView
	decode(t, response, &view)
	return view
}

func rowCount(t *testing.T, pool *pgxpool.Pool, table string) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
