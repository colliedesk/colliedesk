package httpapi_test

import (
	"net/http"
	"testing"
)

// The web app tells a link to a conversation or customer that is not there from a load that failed by
// the status alone: a 404, the same for an id that never existed, one that is not an id at all, and one
// of another workspace. Anything else (a 5xx) keeps the page's Retry.
func TestALinkToAMissingConversationOrContactAnswers404(t *testing.T) {
	api, service := testAPI(t)
	token := createSession(t, api, service, "Links Alpha", "admin@links-alpha.example.com")
	other := createSession(t, api, service, "Links Bravo", "admin@links-bravo.example.com")
	foreignContact := create(t, api, other, "/api/v1/contacts", map[string]any{"name": "Zed", "email": "zed@links-bravo.example.com"})
	foreignConversation := create(t, api, other, "/api/v1/conversations", map[string]any{"contact_id": foreignContact, "subject": "Elsewhere"})

	for _, path := range []string{"/api/v1/conversations/", "/api/v1/contacts/"} {
		foreign := foreignConversation
		if path == "/api/v1/contacts/" {
			foreign = foreignContact
		}
		bodies := map[string]string{}
		for name, id := range map[string]string{"missing": "00000000-0000-0000-0000-000000000000", "not an id": "abc", "other workspace": foreign} {
			response := request(t, api, http.MethodGet, path+id, nil, token, nil)
			if response.Code != http.StatusNotFound {
				t.Errorf("GET %s%s (%s) status=%d body=%s, want 404", path, id, name, response.Code, response.Body.String())
			}
			bodies[name] = withoutRequestID(response.Body.String())
		}
		if bodies["other workspace"] != bodies["missing"] {
			t.Errorf("GET %s tells another workspace's id from a missing one: %q vs %q", path, bodies["other workspace"], bodies["missing"])
		}
	}
}
