package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/hcanalytics"
)

// Each help center sends its page views to its own analytics: a save on one leaves the other's
// settings as they were, and a reader on one help center is counted only by that one's service.
func TestEachHelpCenterSendsItsPageViewsToItsOwnAnalytics(t *testing.T) {
	stub := newAnalyticsStub(t, false, map[string]string{"/debug/mp/collect": `{"validationMessages":[]}`})
	// One sender sends in queue order, so the second view arrives after the first.
	d := newAnalyticsDesk(t, "analytics-several", stub, hcanalytics.Options{Senders: 1})
	hardware := createHelpCenter(t, d.api, d.admin, "Hardware", "hardware")
	shelf := kbCollection(t, d.api, d.admin, map[string]any{"name": "Cables", "help_center_ids": []string{hardware.ID}})
	article := kbArticle(t, d.api, d.admin, map[string]any{"collection_id": shelf.ID, "title": "Replace a cable", "status": "published", "body_md": "Unplug it."})
	hardwarePage := fmt.Sprintf("/help/%s/hardware/a/%d-%s", d.slug, article.Number, article.Slug)
	on := "?help_center_id=" + hardware.ID

	main := map[string]any{"provider": "ga4", "enabled": true, "ga4_measurement_id": "G-MAIN1234", "ga4_api_secret": secretCanary}
	d.save(t, main, http.StatusOK)
	var saved map[string]any
	kbCall(t, d.api, http.MethodPut, "/api/v1/help-center/analytics"+on, map[string]any{"provider": "ga4", "enabled": true,
		"ga4_measurement_id": "G-HARD5678", "ga4_api_secret": secretCanary + "B"}, d.admin, http.StatusOK, &saved)
	if saved["ga4_measurement_id"] != "G-HARD5678" || saved["ga4_api_secret_set"] != true {
		t.Fatalf("saved on hardware = %v", saved)
	}
	if got := d.settings(t, d.admin); got["ga4_measurement_id"] != "G-MAIN1234" {
		t.Fatalf("the primary's settings after a save on hardware = %v", got)
	}
	var read map[string]any
	kbCall(t, d.api, http.MethodGet, "/api/v1/help-center/analytics"+on, nil, d.admin, http.StatusOK, &read)
	if read["ga4_measurement_id"] != "G-HARD5678" {
		t.Fatalf("hardware's settings = %v", read)
	}

	wantPage(t, visit(d.site, d.page, "192.0.2.21", "User-Agent", browserAgent), http.StatusOK, nil, nil)
	waitFor(t, "the primary's page view", func() bool { return len(stub.received()) == 1 })
	wantPage(t, visit(d.site, hardwarePage, "192.0.2.22", "User-Agent", browserAgent), http.StatusOK, []string{"Replace a cable"}, nil)
	waitFor(t, "hardware's page view", func() bool { return len(stub.received()) == 2 })
	calls := stub.received()
	if !strings.Contains(calls[0].Query, "measurement_id=G-MAIN1234") || !strings.Contains(calls[0].Body, d.page) {
		t.Errorf("the primary's view went to %+v", calls[0])
	}
	if !strings.Contains(calls[1].Query, "measurement_id=G-HARD5678") || !strings.Contains(calls[1].Query, "api_secret="+secretCanary+"B") ||
		!strings.Contains(calls[1].Body, hardwarePage) {
		t.Errorf("hardware's view went to %+v", calls[1])
	}
	if err := d.sender.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	kbCall(t, d.api, http.MethodGet, "/api/v1/help-center/analytics"+on, nil, d.admin, http.StatusOK, &read)
	if read["last_sent_at"] == nil {
		t.Errorf("hardware's status after a send = %v, want last_sent_at", read)
	}

	var test struct {
		OK bool `json:"ok"`
	}
	kbCall(t, d.api, http.MethodPost, "/api/v1/help-center/analytics/test"+on, nil, d.admin, http.StatusOK, &test)
	calls = stub.received()
	if last := calls[len(calls)-1]; !test.OK || !strings.Contains(last.Query, "measurement_id=G-HARD5678") {
		t.Errorf("a test on hardware = %+v, sent %+v", test, last)
	}

	// Turning the primary off leaves hardware sending.
	main["enabled"] = false
	d.save(t, main, http.StatusOK)
	before := len(stub.received())
	wantPage(t, visit(d.site, hardwarePage, "192.0.2.23", "User-Agent", browserAgent), http.StatusOK, nil, nil)
	waitFor(t, "hardware's page view after the primary was turned off", func() bool { return len(stub.received()) == before+1 })
}
