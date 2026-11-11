package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/metaconversions"
)

// fakeGraphAPI stands in for Meta's Graph API, the external boundary: it keeps every request it gets
// and answers with the next status in its list (200 once the list is used up).
type fakeGraphAPI struct {
	mu       sync.Mutex
	server   *httptest.Server
	statuses []int
	requests []graphRequest
}

type graphRequest struct {
	Path string
	Body map[string]any
	Raw  string
}

func newFakeGraphAPI(t *testing.T, statuses ...int) *fakeGraphAPI {
	t.Helper()
	fake := &fakeGraphAPI{statuses: statuses}
	fake.server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		raw, _ := io.ReadAll(request.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		fake.mu.Lock()
		fake.requests = append(fake.requests, graphRequest{Path: request.URL.Path, Body: body, Raw: string(raw)})
		status := http.StatusOK
		if len(fake.statuses) > 0 {
			status, fake.statuses = fake.statuses[0], fake.statuses[1:]
		}
		fake.mu.Unlock()
		response.WriteHeader(status)
		if status == http.StatusOK {
			_, _ = response.Write([]byte(`{"events_received":1,"messages":[],"fbtrace_id":"trace"}`))
			return
		}
		_, _ = response.Write([]byte(`{"error":{"message":"Service temporarily unavailable","type":"OAuthException","code":2}}`))
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeGraphAPI) received() []graphRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]graphRequest(nil), f.requests...)
}

func (f *fakeGraphAPI) config() metaconversions.Config {
	return metaconversions.Config{DatasetID: "1234567890", AccessToken: "meta-test-token", TestEventCode: "TEST4242", Base: f.server.URL}
}

// metaSignupFixture is a cloud sign-up server that reports to the fake Graph API, and its worker.
func metaSignupFixture(t *testing.T, graph *fakeGraphAPI) (signupFixture, *metaconversions.Worker) {
	t.Helper()
	f := newSignupFixture(t, func(config *httpapi.Config) { config.MetaConversions = graph.config() })
	return f, metaconversions.NewWorker(database.New(f.pool), graph.config(), metaconversions.WorkerOptions{Now: f.clock.Now})
}

func (f signupFixture) signUpFromLink(t *testing.T, email string, attribution map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	req := newRequest(t, http.MethodPost, "/api/v1/auth/cloud/signup", map[string]any{"workspace_name": "Meta Acme", "attribution": attribution},
		f.token(t, map[string]any{"email": email}), nil)
	req.Header.Set("Origin", testOrigin)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh) Test/1.0")
	req.RemoteAddr = defaultPeer
	return serve(f.api, req)
}

func runMetaWorker(t *testing.T, worker *metaconversions.Worker) {
	t.Helper()
	if _, err := worker.RunOnce(t.Context()); err != nil {
		t.Fatalf("meta worker: %v", err)
	}
}

func TestSignupFromAMetaAdReportsOneStartTrialWithTheMinimum(t *testing.T) {
	graph := newFakeGraphAPI(t)
	f, worker := metaSignupFixture(t, graph)
	created := f.signUpFromLink(t, "ada@meta-acme.example.com", map[string]any{
		"utm_source": "meta", "fbclid": "test123", "click_id_seen_at": "2026-10-20T09:40:00Z",
	})
	wantResponse(t, created, http.StatusCreated, "")
	workspaceID := f.meAfter(t, created).Workspace.ID

	runMetaWorker(t, worker)
	runMetaWorker(t, worker)
	requests := graph.received()
	if len(requests) != 1 {
		t.Fatalf("Graph API requests=%d, want exactly one StartTrial", len(requests))
	}
	if requests[0].Path != "/v26.0/1234567890/events" {
		t.Fatalf("path=%q, want /v26.0/1234567890/events", requests[0].Path)
	}
	// 2026-10-20T10:00:00Z (the sign-up) is 1792490400; the click at 09:40:00Z is 1792489200000 ms.
	want := map[string]any{
		"access_token": "meta-test-token", "test_event_code": "TEST4242",
		"data": []any{map[string]any{
			"event_name": "StartTrial", "event_time": float64(1792490400), "event_id": workspaceID,
			"action_source": "website", "event_source_url": testOrigin + "/sign-in",
			"user_data": map[string]any{
				"fbc": "fb.1.1792489200000.test123", "client_ip_address": "203.0.113.7", "client_user_agent": "Mozilla/5.0 (Macintosh) Test/1.0",
			},
		}},
	}
	if !reflect.DeepEqual(requests[0].Body, want) {
		t.Fatalf("body=%s\nwant %v", requests[0].Raw, want)
	}
	for _, private := range []string{"ada@", "meta-acme", "Meta Acme"} {
		if strings.Contains(requests[0].Raw, private) {
			t.Fatalf("body carries %q: %s", private, requests[0].Raw)
		}
	}
}

func TestSignupWithoutAMetaClickIDReportsNothing(t *testing.T) {
	graph := newFakeGraphAPI(t)
	f, worker := metaSignupFixture(t, graph)
	wantResponse(t, f.signUpFromLink(t, "tik@meta-none.example.com", map[string]any{"utm_source": "tiktok", "ttclid": "E.C.P"}), http.StatusCreated, "")
	runMetaWorker(t, worker)
	if requests := graph.received(); len(requests) != 0 {
		t.Fatalf("Graph API requests=%d, want none for a sign-up without fbclid", len(requests))
	}
	if got := f.count(t, "meta_conversion_events"); got != "0" {
		t.Fatalf("meta_conversion_events=%s, want 0", got)
	}
}

func TestAFailedReportIsRetriedLaterWithTheSameEventIDAndTheSignupIsUnaffected(t *testing.T) {
	graph := newFakeGraphAPI(t, http.StatusServiceUnavailable)
	f, worker := metaSignupFixture(t, graph)
	created := f.signUpFromLink(t, "retry@meta-retry.example.com", map[string]any{"fbclid": "retry1"})
	wantResponse(t, created, http.StatusCreated, "")
	workspaceID := f.meAfter(t, created).Workspace.ID

	runMetaWorker(t, worker)
	runMetaWorker(t, worker)
	if requests := graph.received(); len(requests) != 1 {
		t.Fatalf("requests right after the failure=%d, want 1: the retry waits", len(requests))
	}
	if got := queryString(t, f.pool, `SELECT attempts||'/'||(sent_at IS NULL)||'/'||last_error FROM meta_conversion_events WHERE workspace_id=$1`, workspaceID); !strings.HasPrefix(got, "1/true/HTTP 503") {
		t.Fatalf("row after the failure=%q, want one attempt, not sent, the status kept", got)
	}
	f.clock.set(f.clock.Now().Add(5 * time.Minute))
	runMetaWorker(t, worker)
	f.clock.set(f.clock.Now().Add(time.Hour))
	runMetaWorker(t, worker)
	requests := graph.received()
	if len(requests) != 2 {
		t.Fatalf("requests=%d, want the failure and one retry", len(requests))
	}
	for i, request := range requests {
		data := request.Body["data"].([]any)[0].(map[string]any)
		if data["event_id"] != workspaceID || data["event_time"] != float64(1792490400) {
			t.Fatalf("request %d event_id=%v event_time=%v, want %s and the sign-up time both times", i, data["event_id"], data["event_time"], workspaceID)
		}
	}
}

func TestSelfHostedServerNeverReportsEvenWithTheVariablesSet(t *testing.T) {
	environ := map[string]string{
		"META_CONVERSIONS_DATASET_ID": "1234567890", "META_CONVERSIONS_ACCESS_TOKEN": "meta-test-token", "META_CONVERSIONS_TEST_EVENT_CODE": "TEST4242",
	}
	getenv := func(name string) string { return environ[name] }
	if config := metaconversions.ConfigFromEnv(getenv, false); config.Enabled() {
		t.Fatalf("self-hosted config=%+v is enabled, want off whatever the environment says", config)
	}
	if config := metaconversions.ConfigFromEnv(getenv, true); !config.Enabled() || config.DatasetID != "1234567890" || config.TestEventCode != "TEST4242" {
		t.Fatalf("cloud config=%+v, want the dataset and test code of the environment", config)
	}
	if config := metaconversions.ConfigFromEnv(func(string) string { return "" }, true); config.Enabled() {
		t.Fatal("cloud config without the variables is enabled, want off")
	}

	// A self-hosted server's worker sends nothing even if a row is there.
	graph := newFakeGraphAPI(t)
	f := newSignupFixture(t, nil)
	created := f.signUpFromLink(t, "self@meta-self.example.com", map[string]any{"fbclid": "self1"})
	wantResponse(t, created, http.StatusCreated, "")
	if got := f.count(t, "meta_conversion_events"); got != "0" {
		t.Fatalf("meta_conversion_events=%s on a server without the report, want 0", got)
	}
	workspaceID := f.meAfter(t, created).Workspace.ID
	if _, err := f.pool.Exec(t.Context(), `INSERT INTO meta_conversion_events(workspace_id,event_name,event_time,fbc,client_ip,user_agent,event_source_url,next_attempt_at)
		VALUES($1,'StartTrial',now(),'fb.1.1.self1','203.0.113.7','UA','https://desk.example/sign-in',now())`, workspaceID); err != nil {
		t.Fatal(err)
	}
	selfHosted := metaconversions.ConfigFromEnv(getenv, false)
	selfHosted.Base = graph.server.URL
	worker := metaconversions.NewWorker(database.New(f.pool), selfHosted, metaconversions.WorkerOptions{Now: f.clock.Now})
	runMetaWorker(t, worker)
	if requests := graph.received(); len(requests) != 0 {
		t.Fatalf("Graph API requests=%d from a self-hosted worker, want none", len(requests))
	}
}

func TestFirstPaymentReportsOneSubscribeWithThePlanPrice(t *testing.T) {
	graph := newFakeGraphAPI(t)
	f := newBillingFixtureWith(t, func(config *httpapi.Config) { config.MetaConversions = graph.config() })
	worker := metaconversions.NewWorker(database.New(f.pool), graph.config(), metaconversions.WorkerOptions{})
	// Workspace A signed up from a Meta ad and its StartTrial was already reported; B did not.
	if _, err := f.pool.Exec(t.Context(), `INSERT INTO meta_conversion_events(workspace_id,event_name,event_time,fbc,client_ip,user_agent,event_source_url,next_attempt_at,attempts,sent_at)
		VALUES($1,'StartTrial',now()-interval '1 day','fb.1.1792489200000.paid1','198.51.100.4','Mozilla/5.0 Paid','https://app.example/sign-in',now(),1,now()-interval '1 day')`, f.workspaceA); err != nil {
		t.Fatal(err)
	}
	paid := activeTeam(f.workspaceA, "sub_meta_paid", time.Now())
	paid.Product, paid.Amount, paid.Currency = productFounding, 9900, "usd"
	before := time.Now().Unix()
	f.mustDeliver(t, "msg_meta_paid", paid.event("subscription.active"))
	// Polar redelivers; a later update is not a new payment.
	f.mustDeliver(t, "msg_meta_paid", paid.event("subscription.active"))
	f.mustDeliver(t, "msg_meta_paid_update", paid.event("subscription.updated"))
	other := activeTeam(f.workspaceB, "sub_meta_other", time.Now())
	other.Amount, other.Currency = 4900, "usd"
	f.mustDeliver(t, "msg_meta_other", other.event("subscription.active"))

	runMetaWorker(t, worker)
	runMetaWorker(t, worker)
	requests := graph.received()
	if len(requests) != 1 {
		t.Fatalf("Graph API requests=%d, want one Subscribe for workspace A only", len(requests))
	}
	data := requests[0].Body["data"].([]any)[0].(map[string]any)
	eventTime, _ := data["event_time"].(float64)
	delete(data, "event_time")
	want := map[string]any{
		"event_name": "Subscribe", "event_id": f.workspaceA, "action_source": "website", "event_source_url": testOrigin + "/settings/billing",
		"user_data":   map[string]any{"fbc": "fb.1.1792489200000.paid1", "client_ip_address": "198.51.100.4", "client_user_agent": "Mozilla/5.0 Paid"},
		"custom_data": map[string]any{"value": 99.0, "currency": "USD"},
	}
	if !reflect.DeepEqual(data, want) {
		t.Fatalf("Subscribe=%s\nwant %v", requests[0].Raw, want)
	}
	if int64(eventTime) < before || int64(eventTime) > time.Now().Unix() {
		t.Fatalf("event_time=%v, want the time the payment was applied", eventTime)
	}
}
