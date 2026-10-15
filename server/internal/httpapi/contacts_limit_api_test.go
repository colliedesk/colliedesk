package httpapi_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type contactPageBody struct {
	Items []struct {
		Name string `json:"name"`
	} `json:"items"`
	HasMore *bool `json:"has_more"`
}

func listContactsPage(t *testing.T, api http.Handler, token, query string) (int, contactPageBody, string) {
	t.Helper()
	response := request(t, api, http.MethodGet, "/api/v1/contacts?"+query, nil, token, nil)
	var body contactPageBody
	if response.Code == http.StatusOK {
		decode(t, response, &body)
	}
	return response.Code, body, response.Body.String()
}

func contactNames(page contactPageBody) string {
	names := make([]string, len(page.Items))
	for index, item := range page.Items {
		names[index] = item.Name
	}
	return strings.Join(names, ",")
}

func TestContactListStopsAtTheLimitAndSaysWhenMoreMatched(t *testing.T) {
	api, service := testAPI(t)
	token := createSession(t, api, service, "Limit Alpha", "admin@limit-alpha.example.com")
	other := createSession(t, api, service, "Limit Bravo", "admin@limit-bravo.example.com")
	ids := map[string]string{}
	for _, name := range []string{"Ada", "Bob", "Cy"} {
		ids[name] = create(t, api, token, "/api/v1/contacts", map[string]any{"name": name, "email": strings.ToLower(name) + "@limit.example.com"})
	}
	for _, name := range []string{"Dan", "Eve", "Fay", "Gus"} {
		create(t, api, other, "/api/v1/contacts", map[string]any{"name": name, "email": strings.ToLower(name) + "@limit-other.example.com"})
	}

	cases := []struct {
		query    string
		names    string
		hasMore  bool
		hasField bool
	}{
		{"limit=2", "Ada,Bob", true, true},
		{"limit=3", "Ada,Bob,Cy", false, true},
		{"limit=200", "Ada,Bob,Cy", false, true},
		{"limit=1&search=" + url.QueryEscape("@limit.example.com"), "Ada", true, true},
		{"limit=1&search=" + url.QueryEscape("cy@"), "Cy", false, true},
		{"limit=1&ids=" + url.QueryEscape(ids["Cy"]+","+ids["Ada"]+","+ids["Bob"]), "Ada", true, true},
		{"", "Ada,Bob,Cy", false, false},
	}
	for _, c := range cases {
		status, page, raw := listContactsPage(t, api, token, c.query)
		if status != http.StatusOK {
			t.Errorf("GET /contacts?%s status=%d body=%s", c.query, status, raw)
			continue
		}
		if got := contactNames(page); got != c.names {
			t.Errorf("GET /contacts?%s returned [%s], want [%s] (another workspace's contacts must not count)", c.query, got, c.names)
		}
		if c.hasField != (page.HasMore != nil) || (page.HasMore != nil && *page.HasMore != c.hasMore) {
			t.Errorf("GET /contacts?%s has_more=%v, want %v (present only with a limit: %v)", c.query, page.HasMore, c.hasMore, c.hasField)
		}
	}

	for _, bad := range []string{"limit=0", "limit=-1", "limit=abc", "limit=201", "limit="} {
		status, _, raw := listContactsPage(t, api, token, bad)
		if status != http.StatusBadRequest || !strings.Contains(raw, "limit") || !strings.Contains(raw, "1 to 200") {
			t.Errorf("GET /contacts?%s status=%d body=%s, want 400 naming limit and 1 to 200", bad, status, raw)
		}
	}
}
