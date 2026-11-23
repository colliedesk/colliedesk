package hcanalytics_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/hcanalytics"
	"github.com/colliedesk/colliedesk/server/internal/pageview"
)

const readerAgent = "Mozilla/5.0 Test"

// hit is one request a stub service received.
type hit struct {
	path, query string
	header      http.Header
	body        map[string]any
}

// stub is a local stand-in for GA4 or Plausible that answers status, after hold when it is set.
type stub struct {
	server *httptest.Server
	mu     sync.Mutex
	hits   []hit
	status int
	hold   chan struct{}
	got    chan struct{}
}

func newStub(t *testing.T, status int) *stub {
	t.Helper()
	s := &stub{status: status, got: make(chan struct{}, 100)}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		s.mu.Lock()
		s.hits = append(s.hits, hit{path: r.URL.Path, query: r.URL.RawQuery, header: r.Header.Clone(), body: body})
		hold := s.hold
		s.mu.Unlock()
		s.got <- struct{}{}
		if hold != nil {
			select {
			case <-hold:
			case <-r.Context().Done():
			}
		}
		w.WriteHeader(s.status)
	}))
	t.Cleanup(s.server.Close)
	return s
}

func (s *stub) address() string { return strings.TrimPrefix(s.server.URL, "http://") }

func (s *stub) wait(t *testing.T, n int) []hit {
	t.Helper()
	for range n {
		select {
		case <-s.got:
		case <-time.After(5 * time.Second):
			t.Fatalf("the stub got %d of %d requests", len(s.snapshot()), n)
		}
	}
	return s.snapshot()
}

func (s *stub) snapshot() []hit {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]hit(nil), s.hits...)
}

// memoryStore holds the targets and the recorded results in memory. A full read that finds hold set
// copies the targets, tells listed and waits for hold, like a slow read a save overtakes. failRecord
// failures are returned by the next result writes.
type memoryStore struct {
	mu         sync.Mutex
	targets    []hcanalytics.Target
	results    []hcanalytics.Result
	hold       chan struct{}
	listed     chan struct{}
	failRecord int
}

func (m *memoryStore) HelpCenterAnalyticsTargets(context.Context) ([]hcanalytics.Target, error) {
	m.mu.Lock()
	targets, hold, listed := append([]hcanalytics.Target(nil), m.targets...), m.hold, m.listed
	m.mu.Unlock()
	if hold != nil {
		listed <- struct{}{}
		<-hold
	}
	return targets, nil
}

func (m *memoryStore) set(targets ...hcanalytics.Target) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.targets = targets
}

func (m *memoryStore) recorded() []hcanalytics.Result {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]hcanalytics.Result(nil), m.results...)
}

func (m *memoryStore) HelpCenterAnalyticsTargetIn(_ context.Context, workspaceID, helpCenterID string) (*hcanalytics.Target, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, target := range m.targets {
		if target.WorkspaceID == workspaceID && target.HelpCenterID == helpCenterID {
			return &target, nil
		}
	}
	return nil, nil
}

func (m *memoryStore) RecordHelpCenterAnalyticsResults(_ context.Context, results []hcanalytics.Result) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failRecord > 0 {
		m.failRecord--
		return errors.New("the database is away")
	}
	m.results = append(m.results, results...)
	return nil
}

func ga4Target(workspace string) hcanalytics.Target {
	return hcanalytics.Target{WorkspaceID: workspace, Provider: hcanalytics.GA4, MeasurementID: "G-TEST1234", APISecret: "secret-canary-value"}
}

func start(t *testing.T, store *memoryStore, options hcanalytics.Options) *hcanalytics.Sender {
	t.Helper()
	if options.NewKey == nil {
		options.NewKey = func() []byte { return []byte("day-key-one") }
	}
	if options.Origin == "" {
		options.Origin = "https://desk.example.com"
	}
	sender := hcanalytics.New(store, options)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { sender.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	if err := sender.Reload(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	return sender
}

func articleView(workspace, title string) pageview.Event {
	return pageview.Event{WorkspaceID: workspace, Kind: pageview.Article, Address: "/help/acme/a/7-reset-a-password", Title: title,
		Referrer: "https://www.example.org/forum", Locale: "en", ArticleID: "a1", Client: "192.0.2.10", UserAgent: readerAgent,
		At: time.Date(2026, 11, 21, 12, 4, 0, 0, time.UTC)}
}

func TestAGA4PageViewReachesTheMeasurementProtocolWithTheArticleAddressAndTitle(t *testing.T) {
	ga4 := newStub(t, http.StatusNoContent)
	store := &memoryStore{targets: []hcanalytics.Target{ga4Target("w1")}}
	sender := start(t, store, hcanalytics.Options{GA4Base: ga4.server.URL, AllowDial: map[string]bool{ga4.address(): true}})
	sender.PageViewed(articleView("w1", "Reset a password"))
	got := ga4.wait(t, 1)[0]
	if got.path != "/mp/collect" {
		t.Fatalf("path = %q, want /mp/collect", got.path)
	}
	query, _ := url.ParseQuery(got.query)
	if query.Get("measurement_id") != "G-TEST1234" || query.Get("api_secret") != "secret-canary-value" {
		t.Fatalf("query = %q", got.query)
	}
	if got.body["client_id"] != "74a25aed764a4b2d45ae09d8da0d51b5" {
		t.Fatalf("client_id = %v, want the HMAC of the workspace, the reader's address and agent under the day key", got.body["client_id"])
	}
	if got.body["timestamp_micros"] != float64(1795262640000000) {
		t.Fatalf("timestamp_micros = %v", got.body["timestamp_micros"])
	}
	events := got.body["events"].([]any)
	event := events[0].(map[string]any)
	params := event["params"].(map[string]any)
	want := map[string]any{"page_location": "https://desk.example.com/help/acme/a/7-reset-a-password", "page_title": "Reset a password",
		"page_referrer": "https://www.example.org/forum", "language": "en",
		// Google shows a Measurement Protocol event in its reports only with a session and an engagement time.
		"session_id": "1956797165", "engagement_time_msec": float64(1)}
	if event["name"] != "page_view" || len(params) != len(want) {
		t.Fatalf("event = %v", event)
	}
	for key, value := range want {
		if params[key] != value {
			t.Fatalf("params[%s] = %v, want %v", key, params[key], value)
		}
	}
}

func TestASearchPageSendsItsSearchTerm(t *testing.T) {
	ga4 := newStub(t, http.StatusNoContent)
	store := &memoryStore{targets: []hcanalytics.Target{ga4Target("w1")}}
	sender := start(t, store, hcanalytics.Options{GA4Base: ga4.server.URL, AllowDial: map[string]bool{ga4.address(): true}})
	sender.PageViewed(pageview.Event{WorkspaceID: "w1", Kind: pageview.Search, Address: "https://help.acme.example/search?q=refund", Title: "Search",
		Search: "refund", Locale: "de", Client: "192.0.2.10", UserAgent: readerAgent, At: time.Now()})
	params := ga4.wait(t, 1)[0].body["events"].([]any)[0].(map[string]any)["params"].(map[string]any)
	if params["search_term"] != "refund" || params["page_location"] != "https://help.acme.example/search?q=refund" || params["language"] != "de" {
		t.Fatalf("params = %v", params)
	}
	if _, ok := params["page_referrer"]; ok {
		t.Fatalf("an empty referrer is sent: %v", params)
	}
}

func TestAPlausiblePageviewCarriesTheReadersAgentAndAddress(t *testing.T) {
	plausible := newStub(t, http.StatusAccepted)
	store := &memoryStore{targets: []hcanalytics.Target{{WorkspaceID: "w2", Provider: hcanalytics.Plausible, Domain: "help.acme.example", BaseURL: plausible.server.URL}}}
	sender := start(t, store, hcanalytics.Options{AllowDial: map[string]bool{plausible.address(): true}})
	sender.PageViewed(articleView("w2", "Reset a password"))
	got := plausible.wait(t, 1)[0]
	if got.path != "/api/event" || got.header.Get("User-Agent") != readerAgent || got.header.Get("X-Forwarded-For") != "192.0.2.10" ||
		got.header.Get("Content-Type") != "application/json" {
		t.Fatalf("request = %s %v", got.path, got.header)
	}
	want := map[string]any{"name": "pageview", "url": "https://desk.example.com/help/acme/a/7-reset-a-password", "domain": "help.acme.example",
		"referrer": "https://www.example.org/forum"}
	if len(got.body) != len(want) {
		t.Fatalf("body = %v", got.body)
	}
	for key, value := range want {
		if got.body[key] != value {
			t.Fatalf("body[%s] = %v, want %v", key, got.body[key], value)
		}
	}
}

func TestAWorkspaceWithoutAnalyticsSendsNothing(t *testing.T) {
	ga4 := newStub(t, http.StatusNoContent)
	store := &memoryStore{targets: []hcanalytics.Target{ga4Target("w1")}}
	sender := start(t, store, hcanalytics.Options{GA4Base: ga4.server.URL, AllowDial: map[string]bool{ga4.address(): true}})
	sender.PageViewed(articleView("w-other", "Not sent"))
	sender.PageViewed(articleView("w1", "Sent"))
	hits := ga4.wait(t, 1)
	time.Sleep(50 * time.Millisecond)
	if hits = ga4.snapshot(); len(hits) != 1 {
		t.Fatalf("hits = %d, want 1: only the workspace with analytics sends", len(hits))
	}
}

func TestAFailedSendIsRetriedTwiceThenDroppedAndRecordedWithoutTheSecret(t *testing.T) {
	ga4 := newStub(t, http.StatusInternalServerError)
	store := &memoryStore{targets: []hcanalytics.Target{ga4Target("w1")}}
	sender := start(t, store, hcanalytics.Options{GA4Base: ga4.server.URL, AllowDial: map[string]bool{ga4.address(): true},
		RetryDelays: []time.Duration{time.Millisecond, time.Millisecond}})
	sender.PageViewed(articleView("w1", "Reset a password"))
	ga4.wait(t, 3)
	time.Sleep(50 * time.Millisecond)
	if hits := ga4.snapshot(); len(hits) != 3 {
		t.Fatalf("hits = %d, want 3: the first send and two retries", len(hits))
	}
	if err := sender.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.results) != 1 || store.results[0].WorkspaceID != "w1" || store.results[0].Error != "GA4 answered HTTP 500." || store.results[0].ErrorAt.IsZero() {
		t.Fatalf("results = %+v", store.results)
	}
	if strings.Contains(store.results[0].Error, "secret-canary-value") {
		t.Fatal("the recorded error carries the API secret")
	}
}

func TestASuccessfulSendIsRecordedAsLastSent(t *testing.T) {
	ga4 := newStub(t, http.StatusNoContent)
	store := &memoryStore{targets: []hcanalytics.Target{ga4Target("w1")}}
	sender := start(t, store, hcanalytics.Options{GA4Base: ga4.server.URL, AllowDial: map[string]bool{ga4.address(): true}})
	sender.PageViewed(articleView("w1", "Reset a password"))
	ga4.wait(t, 1)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := sender.Flush(context.Background()); err != nil {
			t.Fatal(err)
		}
		store.mu.Lock()
		n := len(store.results)
		store.mu.Unlock()
		if n > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(store.results) != 1 || store.results[0].SentAt.IsZero() || store.results[0].Error != "" {
		t.Fatalf("results = %+v", store.results)
	}
}

func TestAHangingServiceNeverMakesPageViewedWaitAndAFullQueueDropsTheOldest(t *testing.T) {
	ga4 := newStub(t, http.StatusNoContent)
	ga4.hold = make(chan struct{})
	defer close(ga4.hold)
	store := &memoryStore{targets: []hcanalytics.Target{ga4Target("w1")}}
	sender := start(t, store, hcanalytics.Options{GA4Base: ga4.server.URL, AllowDial: map[string]bool{ga4.address(): true},
		Senders: 1, QueueSize: 2, Timeout: 10 * time.Second})
	sender.PageViewed(articleView("w1", "first"))
	ga4.wait(t, 1) // the one sender now hangs on the first event
	for _, title := range []string{"second", "third", "fourth", "fifth"} {
		began := time.Now()
		sender.PageViewed(articleView("w1", title))
		if took := time.Since(began); took > 20*time.Millisecond {
			t.Fatalf("PageViewed took %s with the service hanging", took)
		}
	}
	if dropped := sender.Dropped(); dropped != 2 {
		t.Fatalf("dropped = %d, want 2: a queue of 2 keeps the newest two", dropped)
	}
}

func TestASendThatTimesOutIsRecorded(t *testing.T) {
	ga4 := newStub(t, http.StatusNoContent)
	ga4.hold = make(chan struct{})
	defer close(ga4.hold)
	store := &memoryStore{targets: []hcanalytics.Target{ga4Target("w1")}}
	sender := start(t, store, hcanalytics.Options{GA4Base: ga4.server.URL, AllowDial: map[string]bool{ga4.address(): true},
		Timeout: 20 * time.Millisecond, RetryDelays: []time.Duration{time.Millisecond, time.Millisecond}})
	sender.PageViewed(articleView("w1", "Reset a password"))
	ga4.wait(t, 3)
	time.Sleep(100 * time.Millisecond)
	if err := sender.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.results) != 1 || store.results[0].Error != "GA4 did not answer within 20ms." {
		t.Fatalf("results = %+v", store.results)
	}
}

func TestAPlausibleOnAPrivateAddressIsRefusedAndNamed(t *testing.T) {
	if _, err := hcanalytics.CheckBaseURL("https://10.0.0.5", nil); err == nil || !strings.Contains(err.Error(), "10.0.0.5") {
		t.Fatalf("CheckBaseURL(private) = %v, want a refusal naming 10.0.0.5", err)
	}
	for _, raw := range []string{"https://localhost:8000", "http://plausible.example.com", "ftp://plausible.example.com", "https://user@plausible.example.com", "https://plausible.example.com/?a=1"} {
		if _, err := hcanalytics.CheckBaseURL(raw, nil); err == nil {
			t.Fatalf("CheckBaseURL(%q) passed", raw)
		}
	}
	if got, err := hcanalytics.CheckBaseURL(" https://stats.example.com/ ", nil); err != nil || got != "https://stats.example.com" {
		t.Fatalf("CheckBaseURL(public) = %q, %v", got, err)
	}
	if got, err := hcanalytics.CheckBaseURL("", nil); err != nil || got != "https://plausible.io" {
		t.Fatalf("CheckBaseURL(empty) = %q, %v", got, err)
	}

	plausible := newStub(t, http.StatusAccepted)
	store := &memoryStore{targets: []hcanalytics.Target{{WorkspaceID: "w2", Provider: hcanalytics.Plausible, Domain: "help.acme.example", BaseURL: plausible.server.URL}}}
	sender := start(t, store, hcanalytics.Options{RetryDelays: []time.Duration{}})
	sender.PageViewed(articleView("w2", "Reset a password"))
	deadline := time.Now().Add(5 * time.Second)
	for len(store.results) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		if err := sender.Flush(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(store.results) != 1 || store.results[0].Error != "Plausible is at 127.0.0.1, a private address; Colliedesk sends only to public addresses." {
		t.Fatalf("results = %+v", store.results)
	}
	if hits := plausible.snapshot(); len(hits) != 0 {
		t.Fatalf("the private address got %d requests", len(hits))
	}
}
