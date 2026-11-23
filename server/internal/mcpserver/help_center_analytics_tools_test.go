package mcpserver_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/google"
	"github.com/colliedesk/colliedesk/server/internal/hcanalytics"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
)

// analyticsToolSecret is the GA4 API secret the test saves; no tool answer may carry it.
const analyticsToolSecret = "Tool_Canary_Secret_41c"

// lateStore lets the sender be built before the test database exists; the env fills it.
type lateStore struct{ hcanalytics.Store }

func TestHelpCenterAnalyticsToolsSaveTestAndNeverAnswerTheSecret(t *testing.T) {
	var mu sync.Mutex
	var plausiblePaths []string
	plausible := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		plausiblePaths = append(plausiblePaths, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(plausible.Close)
	keys, _ := tokenseal.ParseKeys("k1:AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM=")
	late := &lateStore{}
	sender := hcanalytics.New(late, hcanalytics.Options{Origin: testOrigin,
		AllowDial: map[string]bool{strings.TrimPrefix(plausible.URL, "http://"): true}})
	e := newEnvWith(t, func(config *httpapi.Config) {
		config.Google = google.Config{Keys: keys}
		config.Analytics = sender
	})
	late.Store = store.New(e.pool).WithSealKeys(keys)
	w := e.newWorkspace("Analytics Tools Desk", "admin@analytics-tools.example.com")
	read, send, admin := e.connect(w.read, ""), e.connect(w.send, ""), e.connect(w.adminTier, "")

	empty := e.ok(read, "colliedesk_help_center_analytics_get", nil)
	if at(t, empty, "analytics", "provider") != nil || at(t, empty, "analytics", "plausible_base_url") != "https://plausible.io" {
		t.Fatalf("a new workspace has analytics: %v", empty)
	}
	e.refused(read, "colliedesk_help_center_analytics_test", nil)
	e.refused(send, "colliedesk_help_center_analytics_set", map[string]any{"provider": "plausible", "enabled": false})
	if message := e.fails(admin, "colliedesk_help_center_analytics_test", nil); !strings.Contains(message, "PUT /api/v1/help-center/analytics") {
		t.Fatalf("a test before any save does not say to save first: %q", message)
	}
	if message := e.fails(admin, "colliedesk_help_center_analytics_set", map[string]any{"provider": "plausible", "enabled": true,
		"plausible_domain": "help.example.com", "plausible_base_url": "https://10.0.0.7"}); !strings.Contains(message, "plausible_base_url") {
		t.Fatalf("a private Plausible was not refused by field: %q", message)
	}

	ga4 := e.ok(admin, "colliedesk_help_center_analytics_set", map[string]any{"provider": "ga4", "enabled": false,
		"ga4_measurement_id": "G-TOOLS1234", "ga4_api_secret": analyticsToolSecret})
	if raw, _ := json.Marshal(ga4); strings.Contains(string(raw), analyticsToolSecret) {
		t.Fatalf("the set answer carries the API secret: %s", raw)
	}
	if at(t, ga4, "analytics", "ga4_api_secret_set") != true || at(t, ga4, "analytics", "ga4_measurement_id") != "G-TOOLS1234" {
		t.Fatalf("saved GA4 = %v", ga4)
	}

	saved := e.ok(admin, "colliedesk_help_center_analytics_set", map[string]any{"provider": "plausible", "enabled": true,
		"plausible_domain": "help.example.com", "plausible_base_url": plausible.URL})
	if at(t, saved, "analytics", "provider") != "plausible" || at(t, saved, "analytics", "enabled") != true ||
		at(t, saved, "analytics", "plausible_domain") != "help.example.com" || at(t, saved, "analytics", "updated_by_name") == nil {
		t.Fatalf("saved Plausible = %v", saved)
	}
	tested := e.ok(admin, "colliedesk_help_center_analytics_test", nil)
	if at(t, tested, "test", "ok") != true || !strings.Contains(at(t, tested, "test", "message").(string), "Plausible accepted") {
		t.Fatalf("test = %v", tested)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(plausiblePaths) != 1 || plausiblePaths[0] != "POST /api/event" {
		t.Fatalf("Plausible received %v, want one POST /api/event", plausiblePaths)
	}
	if got := e.ok(read, "colliedesk_help_center_analytics_get", nil); at(t, got, "analytics", "provider") != "plausible" {
		t.Fatalf("read after the save = %v", got)
	}
}

// The analytics tools work on the help center help_center_id names; the primary keeps its own.
func TestHelpCenterAnalyticsToolsWorkOnOneHelpCenterByID(t *testing.T) {
	plausible := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(plausible.Close)
	keys, _ := tokenseal.ParseKeys("k1:AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM=")
	late := &lateStore{}
	sender := hcanalytics.New(late, hcanalytics.Options{Origin: testOrigin,
		AllowDial: map[string]bool{strings.TrimPrefix(plausible.URL, "http://"): true}})
	e := newEnvWith(t, func(config *httpapi.Config) {
		config.Google = google.Config{Keys: keys}
		config.Analytics = sender
	})
	late.Store = store.New(e.pool).WithSealKeys(keys)
	w := e.newWorkspace("Analytics By Center", "admin@analytics-by-center.example.com")
	other := e.newWorkspace("Analytics Elsewhere", "admin@analytics-elsewhere.example.com")
	admin, send := e.connect(w.adminTier, ""), e.connect(w.send, "")
	hardware, _ := at(t, e.ok(admin, "colliedesk_help_center_create", map[string]any{"name": "Hardware", "handle": "hardware", "locale": "en"}), "help_center", "id").(string)

	e.refused(send, "colliedesk_help_center_analytics_set", map[string]any{"help_center_id": hardware, "provider": "plausible", "enabled": false})
	saved := e.ok(admin, "colliedesk_help_center_analytics_set", map[string]any{"help_center_id": hardware, "provider": "plausible", "enabled": true,
		"plausible_domain": "hardware.example.com", "plausible_base_url": plausible.URL})
	if at(t, saved, "analytics", "plausible_domain") != "hardware.example.com" {
		t.Fatalf("set on the second help center = %v", saved)
	}
	if got := e.ok(admin, "colliedesk_help_center_analytics_get", map[string]any{"help_center_id": hardware}); at(t, got, "analytics", "provider") != "plausible" {
		t.Fatalf("get on the second help center = %v", got)
	}
	if got := e.ok(admin, "colliedesk_help_center_analytics_get", nil); at(t, got, "analytics", "provider") != nil {
		t.Fatalf("the primary's analytics = %v, want untouched", got)
	}
	tested := e.ok(admin, "colliedesk_help_center_analytics_test", map[string]any{"help_center_id": hardware})
	if at(t, tested, "test", "ok") != true {
		t.Fatalf("test on the second help center = %v", tested)
	}
	if message := e.fails(admin, "colliedesk_help_center_analytics_test", nil); !strings.Contains(message, "PUT /api/v1/help-center/analytics") {
		t.Fatalf("a test of the primary, which saved nothing, does not say to save first: %q", message)
	}
	stranger := e.connect(other.adminTier, "")
	for _, tool := range []string{"colliedesk_help_center_analytics_get", "colliedesk_help_center_analytics_test"} {
		if message := e.fails(stranger, tool, map[string]any{"help_center_id": hardware}); !strings.Contains(message, "colliedesk_help_centers_list") {
			t.Errorf("%s with another workspace's help center: %q", tool, message)
		}
	}
}
