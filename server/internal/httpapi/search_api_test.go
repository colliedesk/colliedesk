package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestSearchTreatsPercentUnderscoreAndBackslashLiterally(t *testing.T) {
	api, service := testAPI(t)
	token := createSession(t, api, service, "Search Desk", "admin@search.example.com")
	percentContact := create(t, api, token, "/api/v1/contacts", map[string]any{"name": "Literal %", "email": "percent@search.example.com"})
	plainContact := create(t, api, token, "/api/v1/contacts", map[string]any{"name": "Plain", "email": "plain@search.example.com"})
	if got := contactIDs(t, request(t, api, http.MethodGet, "/api/v1/contacts?search=%25", nil, token, nil)); len(got) != 1 || got[0] != percentContact {
		t.Fatalf("percent contact search=%v, want [%s], plain=%s", got, percentContact, plainContact)
	}

	contactID := create(t, api, token, "/api/v1/contacts", map[string]any{"name": "Conversation owner", "email": "owner@search.example.com"})
	for _, item := range []struct {
		literal string
		subject string
	}{
		{"%", "Contains % percent"},
		{"_", "Contains _ underscore"},
		{`\`, `Contains \ backslash`},
	} {
		conversationID := create(t, api, token, "/api/v1/conversations", map[string]any{"contact_id": contactID, "subject": item.subject})
		path := "/api/v1/conversations?search=" + url.QueryEscape(item.literal)
		got := conversationIDs(t, request(t, api, http.MethodGet, path, nil, token, nil))
		if len(got) != 1 || got[0] != conversationID {
			t.Fatalf("search %q=%v, want [%s]", item.literal, got, conversationID)
		}
	}
}

func contactIDs(t *testing.T, response *httptest.ResponseRecorder) []string {
	t.Helper()
	var list struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	decode(t, response, &list)
	ids := make([]string, len(list.Items))
	for index, item := range list.Items {
		ids[index] = item.ID
	}
	return ids
}
