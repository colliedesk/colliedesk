package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestEmptyCollectionsEncodeAsArrays(t *testing.T) {
	api, authService := testAPI(t)
	token := createSession(t, api, authService, "Empty Desk", "admin@empty.example.com")

	for _, path := range []string{
		"/api/v1/conversations",
		"/api/v1/contacts",
		"/api/v1/agents",
		"/api/v1/tags",
		"/api/v1/auth/invites",
	} {
		response := request(t, api, http.MethodGet, path, nil, token, nil)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"items":[`) {
			t.Errorf("GET %s status=%d body=%s, want an array", path, response.Code, response.Body.String())
		}
	}

	contactID := create(t, api, token, "/api/v1/contacts", map[string]any{
		"name": "Empty Example", "email": "empty@example.com",
	})
	contact := request(t, api, http.MethodGet, "/api/v1/contacts/"+contactID, nil, token, nil)
	if !strings.Contains(contact.Body.String(), `"conversations":[]`) {
		t.Errorf("contact body=%s, want empty conversations array", contact.Body.String())
	}

	conversationID := create(t, api, token, "/api/v1/conversations", map[string]any{
		"contact_id": contactID, "subject": "Empty timeline",
	})
	conversation := request(t, api, http.MethodGet, "/api/v1/conversations/"+conversationID, nil, token, nil)
	if !strings.Contains(conversation.Body.String(), `"tags":[]`) {
		t.Errorf("conversation body=%s, want empty tags array", conversation.Body.String())
	}
	messages := request(t, api, http.MethodGet, "/api/v1/conversations/"+conversationID+"/messages", nil, token, nil)
	if !strings.Contains(messages.Body.String(), `"items":[]`) {
		t.Errorf("messages body=%s, want empty items array", messages.Body.String())
	}
}
