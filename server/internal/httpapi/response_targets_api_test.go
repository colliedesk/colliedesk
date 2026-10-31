package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

func contractTargets() map[string]any {
	hours := []map[string]any{}
	for _, day := range []string{"mon", "tue", "wed", "thu", "fri"} {
		hours = append(hours, map[string]any{"day": day, "open": true, "from": "08:00", "to": "17:00"})
	}
	hours = append(hours, map[string]any{"day": "sat", "open": false}, map[string]any{"day": "sun", "open": false})
	four := 240
	return map[string]any{"enabled": true, "timezone": "Asia/Bangkok", "hours": hours, "holidays": []map[string]any{{"date": "2026-12-25", "name": "Closed"}},
		"targets": map[string]any{"urgent": map[string]any{"first_response_minutes": four, "resolution_minutes": 540},
			"high": map[string]any{"first_response_minutes": four}, "normal": map[string]any{"first_response_minutes": four}, "low": map[string]any{"first_response_minutes": four}},
		"warn_before_minutes": 60, "channel_ids": nil, "alert_email": true}
}

// A workspace's response targets start off with no hours and no targets; an admin sets them in one
// document, a teammate reads them, a wrong part says where, and another workspace never sees them.
func TestResponseTargetsAreOneDocumentAnAdminSets(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, nil)
	admin := createSession(t, api, service, "Targets Desk", "admin@targets.example.com")
	other := createSession(t, api, service, "Targets Other", "admin@targets-other.example.com")
	response := request(t, api, http.MethodGet, "/api/v1/response-targets", nil, admin, nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"enabled":false`) || !strings.Contains(response.Body.String(), `"first_response_minutes":null`) {
		t.Fatalf("defaults: %d %s", response.Code, response.Body.String())
	}
	if response := request(t, api, http.MethodPut, "/api/v1/response-targets", contractTargets(), admin, nil); response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), `"timezone":"Asia/Bangkok"`) || !strings.Contains(response.Body.String(), `"resolution_minutes":540`) {
		t.Fatalf("put: %d %s", response.Code, response.Body.String())
	}
	for _, c := range []struct {
		change func(map[string]any)
		field  string
	}{
		{func(d map[string]any) { d["hours"].([]map[string]any)[2]["to"] = "07:00" }, "hours.2.to"},
		{func(d map[string]any) { d["timezone"] = "Mars/Base" }, "timezone"},
		{func(d map[string]any) { d["holidays"] = []map[string]any{{"date": "2026-02-30", "name": "x"}} }, "holidays.0.date"},
		{func(d map[string]any) {
			d["targets"].(map[string]any)["urgent"] = map[string]any{"first_response_minutes": 0}
		}, "targets.urgent.first_response_minutes"},
		{func(d map[string]any) { d["warn_before_minutes"] = -5 }, "warn_before_minutes"},
		{func(d map[string]any) { d["channel_ids"] = []string{"00000000-0000-4000-8000-000000000000"} }, "channel_ids.0"},
	} {
		document := contractTargets()
		c.change(document)
		response := request(t, api, http.MethodPut, "/api/v1/response-targets", document, admin, nil)
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"`+c.field+`"`) {
			t.Fatalf("%s: %d %s", c.field, response.Code, response.Body.String())
		}
	}
	if response := request(t, api, http.MethodGet, "/api/v1/response-targets", nil, other, nil); strings.Contains(response.Body.String(), "Asia/Bangkok") {
		t.Fatalf("another workspace sees them: %s", response.Body.String())
	}
	// Each teammate turns their own emails off, and reads it back.
	if response := request(t, api, http.MethodGet, "/api/v1/response-targets", nil, admin, nil); !strings.Contains(response.Body.String(), `"my_alert_email":true`) {
		t.Fatalf("mine: %s", response.Body.String())
	}
	if response := request(t, api, http.MethodPatch, "/api/v1/agents/me/sla-alerts", map[string]any{"email": false}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("off for me: %d %s", response.Code, response.Body.String())
	}
	if response := request(t, api, http.MethodGet, "/api/v1/response-targets", nil, admin, nil); !strings.Contains(response.Body.String(), `"my_alert_email":false`) {
		t.Fatalf("mine after: %s", response.Body.String())
	}
	if response := request(t, api, http.MethodGet, "/api/v1/response-targets", nil, other, nil); !strings.Contains(response.Body.String(), `"my_alert_email":true`) {
		t.Fatalf("another workspace's admin's: %s", response.Body.String())
	}
	// Off again: kept as they were.
	document := contractTargets()
	document["enabled"] = false
	if response := request(t, api, http.MethodPut, "/api/v1/response-targets", document, admin, nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"enabled":false`) {
		t.Fatalf("off: %d %s", response.Code, response.Body.String())
	}
}
