package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// Every time the channel routes return is UTC, whatever the server's own timezone: removed channels,
// a channel's page, its history and an email channel's settings.
func TestChannelTimesAreUTC(t *testing.T) {
	local := time.Local
	time.Local = time.FixedZone("UTC+7", 7*3600)
	t.Cleanup(func() { time.Local = local })
	api, service, _, _ := testAPIWithConfig(t, nil)
	admin := createSession(t, api, service, "UTC Times", "admin@utc-times.example.com")
	connectEmail(t, api, admin, "support@utc-times.example.com")
	email := emailChannelOf(t, api, admin, "").Channel.ChannelID
	billing := addEmailChannel(t, api, admin, "billing@utc-times.example.com")
	form := addWebForm(t, api, admin, "Contact form", []string{"https://shop.example.com"})
	if response := request(t, api, http.MethodPatch, "/api/v1/channels/"+form.ID, map[string]any{"enabled": false}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("turn off: %d", response.Code)
	}
	if response := request(t, api, http.MethodDelete, "/api/v1/channels/"+billing, nil, admin, nil); response.Code != http.StatusNoContent {
		t.Fatalf("remove: %d %s", response.Code, response.Body.String())
	}
	for _, path := range []string{"/api/v1/channels?include_removed=true", "/api/v1/channels/" + email, "/api/v1/channels/" + form.ID,
		"/api/v1/channels/events", "/api/v1/channels/" + email + "/email", "/api/v1/channels/email", "/api/v1/channels/web-form"} {
		response := request(t, api, http.MethodGet, path, nil, admin, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s: %d %s", path, response.Code, response.Body.String())
		}
		var body any
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		times := 0
		walkTimes(body, func(value string) {
			times++
			if parsed, _ := time.Parse(time.RFC3339Nano, value); parsed.Location() != time.UTC {
				t.Errorf("GET %s: %q is not UTC", path, value)
			}
		})
		if times == 0 && path != "/api/v1/channels/web-form" {
			t.Errorf("GET %s returned no time to check", path)
		}
	}
}

// walkTimes calls found with every string of value that is an RFC 3339 time.
func walkTimes(value any, found func(string)) {
	switch v := value.(type) {
	case map[string]any:
		for _, item := range v {
			walkTimes(item, found)
		}
	case []any:
		for _, item := range v {
			walkTimes(item, found)
		}
	case string:
		if _, err := time.Parse(time.RFC3339Nano, v); err == nil {
			found(v)
		}
	}
}
