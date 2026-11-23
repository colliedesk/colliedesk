package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// helpCenterRoute is one call that names a help center, by help_center_id or in its path.
type helpCenterRoute struct {
	method, path string
	body         func(id string) any
}

// helpCenterRoutes are every route that takes a help_center_id; path holds %s where the id goes.
var helpCenterRoutes = []helpCenterRoute{
	{http.MethodGet, "/api/v1/help-center/theme?help_center_id=%s", nil},
	{http.MethodPut, "/api/v1/help-center/theme?help_center_id=%s", func(string) any {
		return map[string]any{"colors": map[string]any{"primary": "#aa3300"}, "custom_css": "body { color: #111111; }"}
	}},
	{http.MethodPost, "/api/v1/help-center/theme/check?help_center_id=%s", func(string) any {
		return map[string]any{"colors": map[string]any{"text": "#111111"}}
	}},
	{http.MethodPost, "/api/v1/help-center/theme/preview?help_center_id=%s", func(string) any {
		return map[string]any{"page": "home", "theme": map[string]any{}}
	}},
	{http.MethodGet, "/api/v1/help-center/domain?help_center_id=%s", nil},
	{http.MethodPut, "/api/v1/help-center/domain?help_center_id=%s", func(string) any {
		return map[string]any{"hostname": "help.taken-by-id.example"}
	}},
	{http.MethodPost, "/api/v1/help-center/domain/check?help_center_id=%s", nil},
	{http.MethodDelete, "/api/v1/help-center/domain?help_center_id=%s", nil},
	{http.MethodGet, "/api/v1/help-center/languages?help_center_id=%s", nil},
	{http.MethodPut, "/api/v1/help-center/languages?help_center_id=%s", func(string) any {
		return map[string]any{"languages": []map[string]any{{"locale": "de", "visible": true}}}
	}},
	{http.MethodGet, "/api/v1/help-center/search-console?help_center_id=%s", nil},
	{http.MethodPut, "/api/v1/help-center/search-console?help_center_id=%s", func(string) any {
		return map[string]any{"google_site_verification": "taken-token-0123456789"}
	}},
	{http.MethodGet, "/api/v1/help-center/analytics?help_center_id=%s", nil},
	{http.MethodPut, "/api/v1/help-center/analytics?help_center_id=%s", func(string) any {
		return map[string]any{"provider": "plausible", "enabled": true, "plausible_domain": "taken.example"}
	}},
	{http.MethodPost, "/api/v1/help-center/analytics/test?help_center_id=%s", nil},
	{http.MethodPut, "/api/v1/kb/order", func(id string) any {
		return map[string]any{"parent_id": nil, "help_center_id": id, "collection_ids": []string{}, "article_ids": []string{}}
	}},
	{http.MethodGet, "/api/v1/help-centers/%s", nil},
	{http.MethodPatch, "/api/v1/help-centers/%s", func(string) any { return map[string]any{"title": "Taken"} }},
	{http.MethodDelete, "/api/v1/help-centers/%s", nil},
}

// primarySettingsPaths are the reads of the primary help center's settings that a refused call must
// leave as they were.
var primarySettingsPaths = []string{
	"/api/v1/help-center/theme", "/api/v1/help-center/domain", "/api/v1/help-center/languages",
	"/api/v1/help-center/search-console", "/api/v1/help-center/analytics", "/api/v1/help-centers",
}

// A help center of another workspace, or one that was deleted, is not found by any route that takes a
// help_center_id: 404, never 403, never the primary's settings in its place, and nothing changes.
func TestEveryRouteThatNamesAHelpCenterIsNotFoundForAnotherWorkspaceOrADeletedOne(t *testing.T) {
	f := newTwoHelpCenters(t)
	other := createSession(t, f.api, f.service, "Other Desk", "admin@other-desk.example.com")
	deleted := createHelpCenter(t, f.api, f.admin, "Spare", "spare")
	kbCall(t, f.api, http.MethodDelete, "/api/v1/help-centers/"+deleted.ID, nil, f.admin, http.StatusNoContent, nil)

	before := primarySettings(t, f)
	for _, c := range []struct {
		name, session, id string
	}{
		{"another workspace's help center", other, f.hardware},
		{"a deleted help center", f.admin, deleted.ID},
	} {
		for _, route := range helpCenterRoutes {
			path := strings.Replace(route.path, "%s", c.id, 1)
			var body any
			if route.body != nil {
				body = route.body(c.id)
			}
			response := namedCall(t, f.api, route.method, path, body, c.session)
			if response.Code != http.StatusNotFound {
				t.Errorf("%s: %s %s = %d %s, want 404", c.name, route.method, path, response.Code, response.Body.String())
				continue
			}
			for _, leak := range []string{hostMain, "Main answers", f.workspace} {
				if strings.Contains(response.Body.String(), leak) {
					t.Errorf("%s: %s %s answers with %q: %s", c.name, route.method, path, leak, response.Body.String())
				}
			}
		}
	}
	after := primarySettings(t, f)
	for path, was := range before {
		if after[path] != was {
			t.Errorf("the primary's %s changed:\nbefore %s\nafter  %s", path, was, after[path])
		}
	}
	wantPage(t, f.at(hostHardware, "/"), http.StatusOK, []string{"Hardware answers"}, nil)
}

func primarySettings(t *testing.T, f *twoHelpCenters) map[string]string {
	t.Helper()
	read := map[string]string{}
	for _, path := range primarySettingsPaths {
		read[path] = kbCall(t, f.api, http.MethodGet, path, nil, f.admin, http.StatusOK, nil).Body.String()
	}
	return read
}

// namedCall sends one contract-valid request and checks the answer against the contract, whatever its status.
func namedCall(t *testing.T, api http.Handler, method, path string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := newRequest(t, method, path, body, token, nil)
	validateContractRequest(t, req)
	response := serve(api, req)
	validateContractResponse(t, newRequest(t, method, path, body, token, nil), response)
	return response
}
