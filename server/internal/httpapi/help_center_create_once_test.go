package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Three fast presses of Create help center with one Idempotency-Key make one help center and answer it
// three times, with its address; a new key with the same handle is still refused as taken.
func TestARepeatedKeyCreatesOneHelpCenter(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Center Keys", "admin@center-keys.example.com")
	enableHelpCenter(t, api, admin, "Main answers", true)
	center := map[string]any{"name": "Hardware", "handle": "hardware", "locale": "en"}
	responses := threeAtOnce(t, func() *httptest.ResponseRecorder {
		return postWithKey(t, api, admin, "/api/v1/help-centers", "add-center-1", center)
	})
	ids := map[string]bool{}
	for _, response := range responses {
		ids[idOf(t, response, "id")] = true
		if !strings.HasSuffix(idOf(t, response, "url"), "/hardware") {
			t.Fatalf("answer without its address: %s", response.Body.String())
		}
	}
	if len(ids) != 1 {
		t.Fatalf("three presses returned %d different help centers, want the same one", len(ids))
	}
	if count := queryString(t, pool, `SELECT count(*)::text FROM help_centers WHERE handle='hardware'`); count != "1" {
		t.Fatalf("help centers stored = %s, want 1", count)
	}
	if again := postWithKey(t, api, admin, "/api/v1/help-centers", "add-center-2", center); again.Code != http.StatusConflict {
		t.Fatalf("a second help center with the same handle: status=%d body=%s, want 409", again.Code, again.Body.String())
	}
	other := map[string]any{"name": "Hardware", "handle": "printers", "locale": "en"}
	if reused := postWithKey(t, api, admin, "/api/v1/help-centers", "add-center-1", other); reused.Code != http.StatusUnprocessableEntity {
		t.Fatalf("the key with another body: status=%d body=%s, want 422", reused.Code, reused.Body.String())
	}
}
