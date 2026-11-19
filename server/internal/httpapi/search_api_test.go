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

// A search of several words finds the conversations that have every word, each in the subject, the
// message text or the customer's name or email, in any order: "Alan refund" finds Alan's refund
// question though no field holds those two words side by side.
func TestSearchFindsEveryWordAnywhereInTheConversation(t *testing.T) {
	api, service := testAPI(t)
	token := createSession(t, api, service, "Words Desk", "admin@words.example.com")
	alan := create(t, api, token, "/api/v1/contacts", map[string]any{"name": "Alan Baker", "email": "alan@words.example.com"})
	grace := create(t, api, token, "/api/v1/contacts", map[string]any{"name": "Grace Hopper", "email": "grace@words.example.com"})
	refund := create(t, api, token, "/api/v1/conversations", map[string]any{"contact_id": alan, "subject": "Money back for order ORD-5120"})
	create(t, api, token, "/api/v1/conversations/"+refund+"/messages", map[string]any{"kind": "note", "body": "He asked for a refund twice."})
	create(t, api, token, "/api/v1/conversations", map[string]any{"contact_id": grace, "subject": "Refund for a broken printer"})
	for _, c := range []struct {
		words string
		want  []string
	}{
		{"Alan refund", []string{refund}},
		{"refund   ALAN", []string{refund}},
		{"alan@words ORD-5120", []string{refund}},
		{"Alan printer", nil},
		{"Alan zzz", nil},
	} {
		got := conversationIDs(t, request(t, api, http.MethodGet, "/api/v1/conversations?search="+url.QueryEscape(c.words), nil, token, nil))
		if len(got) != len(c.want) || (len(c.want) == 1 && got[0] != c.want[0]) {
			t.Errorf("search %q = %v, want %v", c.words, got, c.want)
		}
	}
}
