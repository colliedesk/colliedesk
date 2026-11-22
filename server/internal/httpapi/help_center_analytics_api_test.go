package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/google"
	"github.com/colliedesk/colliedesk/server/internal/hcanalytics"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
	"github.com/jackc/pgx/v5/pgxpool"
)

// secretCanary is the GA4 API secret the tests save; no answer and no stored byte may carry it.
const secretCanary = "Canary_Secret_7f3a9"

// analyticsCall is one request an analytics service stub received.
type analyticsCall struct {
	Path  string
	Query string
	Body  string
}

// analyticsStub stands in for GA4 or Plausible: it keeps every request and answers each path with
// a fixed body, or waits until the test ends when hang is set.
type analyticsStub struct {
	server *httptest.Server
	mu     sync.Mutex
	calls  []analyticsCall
}

func newAnalyticsStub(t *testing.T, hang bool, bodies map[string]string) *analyticsStub {
	t.Helper()
	stub := &analyticsStub{}
	release := make(chan struct{})
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		stub.mu.Lock()
		stub.calls = append(stub.calls, analyticsCall{Path: r.URL.Path, Query: r.URL.RawQuery, Body: string(body)})
		stub.mu.Unlock()
		if hang {
			<-release
			return
		}
		_, _ = w.Write([]byte(bodies[r.URL.Path]))
	}))
	t.Cleanup(func() { close(release); stub.server.Close() })
	return stub
}

func (s *analyticsStub) host() string { return strings.TrimPrefix(s.server.URL, "http://") }

func (s *analyticsStub) received() []analyticsCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]analyticsCall(nil), s.calls...)
}

// analyticsDesk is a help center with one published article whose page views go to a sender
// pointed at stub.
type analyticsDesk struct {
	api, site   http.Handler
	pool        *pgxpool.Pool
	service     *auth.Service
	sender      *hcanalytics.Sender
	admin, page string
	slug        string
}

func newAnalyticsDesk(t *testing.T, name string, stub *analyticsStub, options hcanalytics.Options) analyticsDesk {
	t.Helper()
	pool := testPool(t)
	keys, _ := tokenseal.ParseKeys("k1:AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM=")
	options.Origin = testOrigin
	if stub != nil {
		options.GA4Base, options.AllowDial = stub.server.URL, map[string]bool{stub.host(): true}
	}
	d := analyticsDesk{pool: pool, sender: hcanalytics.New(store.New(pool).WithSealKeys(keys), options)}
	api, service, _, _ := testAPIOnPool(t, pool, func(config *httpapi.Config) {
		config.Google = google.Config{Keys: keys}
		config.PageViews, config.Analytics = d.sender, d.sender
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go d.sender.Run(ctx)
	d.api, d.site, d.service = api, helpCenterSite(api), service
	d.admin = createSession(t, api, service, name, "admin@"+name+".example.com")
	d.slug = workspaceSlug(t, api, d.admin)
	enableHelpCenter(t, api, d.admin, name+" help", true)
	collection := kbCollection(t, api, d.admin, map[string]any{"name": "Guides"})
	article := kbArticle(t, api, d.admin, map[string]any{"collection_id": collection.ID, "title": "Reset a password", "status": "published", "body_md": "Open Settings."})
	d.page = fmt.Sprintf("/help/%s/a/%d-%s", d.slug, article.Number, article.Slug)
	return d
}

func (d analyticsDesk) save(t *testing.T, body map[string]any, want int) map[string]any {
	t.Helper()
	var out map[string]any
	response := kbCall(t, d.api, http.MethodPut, "/api/v1/help-center/analytics", body, d.admin, want, &out)
	if strings.Contains(response.Body.String(), secretCanary) {
		t.Fatalf("the PUT answer carries the API secret: %s", response.Body.String())
	}
	return out
}

func (d analyticsDesk) settings(t *testing.T, token string) map[string]any {
	t.Helper()
	var out map[string]any
	response := kbCall(t, d.api, http.MethodGet, "/api/v1/help-center/analytics", nil, token, http.StatusOK, &out)
	if strings.Contains(response.Body.String(), secretCanary) {
		t.Fatalf("the GET answer carries the API secret: %s", response.Body.String())
	}
	return out
}

func ga4Settings(enabled bool) map[string]any {
	return map[string]any{"provider": "ga4", "enabled": enabled, "ga4_measurement_id": "G-TEST1234", "ga4_api_secret": secretCanary}
}

func TestHelpCenterAnalyticsKeepsTheAPISecretWriteOnlyAndSealed(t *testing.T) {
	d := newAnalyticsDesk(t, "analytics-secret", nil, hcanalytics.Options{})
	if got := d.settings(t, d.admin); got["provider"] != nil || got["enabled"] != false || got["ga4_api_secret_set"] != false || got["plausible_base_url"] != "https://plausible.io" {
		t.Fatalf("settings before any save = %v", got)
	}
	if got := d.save(t, ga4Settings(true), http.StatusOK); got["ga4_api_secret_set"] != true || got["ga4_measurement_id"] != "G-TEST1234" || got["enabled"] != true {
		t.Fatalf("saved = %v", got)
	}
	var row string
	if err := d.pool.QueryRow(context.Background(), `SELECT row_to_json(h)::text || encode(h.ga4_api_secret_sealed, 'escape') FROM help_center_analytics h`).Scan(&row); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(row, secretCanary) {
		t.Fatalf("the stored row carries the API secret in clear: %s", row)
	}
	kept := d.save(t, map[string]any{"provider": "ga4", "enabled": true, "ga4_measurement_id": "G-TEST5678"}, http.StatusOK)
	if kept["ga4_api_secret_set"] != true || kept["ga4_measurement_id"] != "G-TEST5678" {
		t.Fatalf("a save without ga4_api_secret must keep the stored one: %v", kept)
	}
	var refused struct {
		Error struct {
			Code   string            `json:"code"`
			Fields map[string]string `json:"fields"`
		} `json:"error"`
	}
	response := kbCall(t, d.api, http.MethodPut, "/api/v1/help-center/analytics", map[string]any{"provider": "ga4", "enabled": true, "ga4_measurement_id": "G-TEST5678", "ga4_api_secret": ""}, d.admin, http.StatusUnprocessableEntity, nil)
	if err := json.Unmarshal(response.Body.Bytes(), &refused); err != nil || refused.Error.Code != "validation_failed" || refused.Error.Fields["ga4_api_secret"] == "" {
		t.Fatalf("removing the secret of an enabled GA4 target = %s, want validation_failed on ga4_api_secret", response.Body.String())
	}
	if got := d.settings(t, d.admin); got["ga4_api_secret_set"] != true {
		t.Fatalf("a refused save changed the secret: %v", got)
	}
	removed := d.save(t, map[string]any{"provider": "ga4", "enabled": false, "ga4_measurement_id": "G-TEST5678", "ga4_api_secret": ""}, http.StatusOK)
	if removed["ga4_api_secret_set"] != false {
		t.Fatalf("an empty ga4_api_secret must remove the stored one: %v", removed)
	}
}

func TestHelpCenterAnalyticsOfOneWorkspaceIsNotSeenByAnother(t *testing.T) {
	d := newAnalyticsDesk(t, "analytics-own", nil, hcanalytics.Options{})
	d.save(t, ga4Settings(true), http.StatusOK)
	other := createSession(t, d.api, d.service, "analytics-other", "admin@analytics-other.example.com")
	if got := d.settings(t, other); got["provider"] != nil || got["ga4_measurement_id"] != nil || got["ga4_api_secret_set"] != false {
		t.Fatalf("another workspace sees %v", got)
	}
	kbCall(t, d.api, http.MethodPut, "/api/v1/help-center/analytics", map[string]any{"provider": "plausible", "enabled": false, "plausible_domain": "help.other.example"}, other, http.StatusOK, nil)
	if got := d.settings(t, d.admin); got["provider"] != "ga4" || got["plausible_domain"] != nil {
		t.Fatalf("another workspace's save changed this one: %v", got)
	}
}

func TestAPlausibleOnAPrivateAddressIsRefusedAndNothingIsSaved(t *testing.T) {
	d := newAnalyticsDesk(t, "analytics-private", nil, hcanalytics.Options{})
	for base, want := range map[string]string{
		"http://127.0.0.1:8000":   "127.0.0.1 is a private address; Colliedesk sends only to a Plausible on a public address",
		"https://10.0.0.5":        "10.0.0.5 is a private address; Colliedesk sends only to a Plausible on a public address",
		"https://stats.localhost": "stats.localhost is this server's own address; Colliedesk sends only to a Plausible on a public address",
	} {
		var refused struct {
			Error struct {
				Fields map[string]string `json:"fields"`
			} `json:"error"`
		}
		kbCall(t, d.api, http.MethodPut, "/api/v1/help-center/analytics", map[string]any{"provider": "plausible", "enabled": true, "plausible_domain": "help.acme.example", "plausible_base_url": base}, d.admin, http.StatusUnprocessableEntity, &refused)
		if refused.Error.Fields["plausible_base_url"] != want {
			t.Fatalf("%s: plausible_base_url = %q, want %q", base, refused.Error.Fields["plausible_base_url"], want)
		}
	}
	if got := d.settings(t, d.admin); got["provider"] != nil {
		t.Fatalf("a refused save was stored: %v", got)
	}
}

func TestTheAnalyticsTestSendsOneEventAsThePersonAndSaysWhatTheServiceAnswered(t *testing.T) {
	stub := newAnalyticsStub(t, false, map[string]string{"/debug/mp/collect": `{"validationMessages":[]}`})
	d := newAnalyticsDesk(t, "analytics-test", stub, hcanalytics.Options{})
	var result struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	kbCall(t, d.api, http.MethodPost, "/api/v1/help-center/analytics/test", nil, d.admin, http.StatusConflict, &result)
	if result.Error.Code != "analytics_not_set" {
		t.Fatalf("a test before any save = %q, want analytics_not_set", result.Error.Code)
	}
	d.save(t, ga4Settings(false), http.StatusOK)
	var answer hcanalytics.TestResult
	kbCall(t, d.api, http.MethodPost, "/api/v1/help-center/analytics/test", nil, d.admin, http.StatusOK, &answer)
	if !answer.OK || !strings.Contains(answer.Message, "Realtime") {
		t.Fatalf("test = %+v", answer)
	}
	calls := stub.received()
	if len(calls) != 2 || calls[0].Path != "/debug/mp/collect" || calls[1].Path != "/mp/collect" ||
		!strings.Contains(calls[1].Query, "api_secret="+secretCanary) || !strings.Contains(calls[1].Body, testOrigin+"/colliedesk-analytics-test") {
		t.Fatalf("the service received %+v, want the validation endpoint then one page view of the test page", calls)
	}
}

func TestAnAPISecretIsRefusedWhenTheServerCannotSealIt(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, nil)
	admin := createSession(t, api, service, "analytics-nokeys", "admin@analytics-nokeys.example.com")
	response := kbCall(t, api, http.MethodPut, "/api/v1/help-center/analytics", ga4Settings(true), admin, http.StatusConflict, nil)
	if code := errorCode(t, response); code != "secret_keys_missing" {
		t.Fatalf("code = %q, want secret_keys_missing", code)
	}
	kbCall(t, api, http.MethodPut, "/api/v1/help-center/analytics", map[string]any{"provider": "plausible", "enabled": true, "plausible_domain": "help.acme.example"}, admin, http.StatusOK, nil)
	response = kbCall(t, api, http.MethodPost, "/api/v1/help-center/analytics/test", nil, admin, http.StatusServiceUnavailable, nil)
	if code := errorCode(t, response); code != "analytics_unavailable" {
		t.Fatalf("a test on a server without the sender = %q, want analytics_unavailable", code)
	}
}
