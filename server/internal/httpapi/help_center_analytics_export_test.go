package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/hcanalytics"
)

func exportedHelpCenter(t *testing.T, api http.Handler, token string) (map[string]any, string) {
	t.Helper()
	response := request(t, api, http.MethodGet, "/api/v1/export", nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", response.Code, response.Body.String())
	}
	files := unzip(t, response.Body.Bytes())
	lines := ndjsonLines(t, files["help-center.ndjson"])
	if len(lines) != 1 {
		t.Fatalf("help-center.ndjson = %v", lines)
	}
	var all strings.Builder
	for _, body := range files {
		all.Write(body)
	}
	return lines[0], all.String()
}

func TestExportCarriesTheHelpCenterAnalyticsButNeverTheAPISecret(t *testing.T) {
	d := newAnalyticsDesk(t, "analytics-export", nil, hcanalytics.Options{})
	center, _ := exportedHelpCenter(t, d.api, d.admin)
	if center["analytics"] != nil {
		t.Fatalf("analytics before any save = %v, want null", center["analytics"])
	}
	d.save(t, ga4Settings(true), http.StatusOK)
	center, all := exportedHelpCenter(t, d.api, d.admin)
	if strings.Contains(all, secretCanary) {
		t.Fatalf("the export carries the GA4 API secret")
	}
	analytics, _ := center["analytics"].(map[string]any)
	for key, want := range map[string]any{
		"provider": "ga4", "enabled": true, "ga4_measurement_id": "G-TEST1234", "ga4_api_secret_set": true,
		"plausible_domain": nil, "plausible_base_url": "https://plausible.io",
	} {
		if analytics[key] != want {
			t.Errorf("help-center.ndjson analytics.%s = %v, want %v", key, analytics[key], want)
		}
	}
	if _, ok := analytics["updated_at"].(string); !ok {
		t.Errorf("help-center.ndjson analytics.updated_at = %v, want a time", analytics["updated_at"])
	}
}

// A workspace that saves its analytics before it ever writes the help center still exports them.
func TestAnalyticsSavedBeforeTheHelpCenterAreExported(t *testing.T) {
	d := newAnalyticsDesk(t, "analytics-first", nil, hcanalytics.Options{})
	fresh := createSession(t, d.api, d.service, "analytics-fresh", "admin@analytics-fresh.example.com")
	kbCall(t, d.api, http.MethodPut, "/api/v1/help-center/analytics", map[string]any{"provider": "plausible", "enabled": false, "plausible_domain": "help.fresh.example"}, fresh, http.StatusOK, nil)
	center, _ := exportedHelpCenter(t, d.api, fresh)
	analytics, _ := center["analytics"].(map[string]any)
	if analytics["provider"] != "plausible" || analytics["plausible_domain"] != "help.fresh.example" || analytics["enabled"] != false {
		t.Fatalf("help-center.ndjson analytics = %v", center["analytics"])
	}
	if center["enabled"] != false {
		t.Fatalf("saving analytics turned the help center on: %v", center["enabled"])
	}
}
