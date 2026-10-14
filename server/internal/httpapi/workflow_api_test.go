package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestAgentWorkflowAndWorkspaceIsolation(t *testing.T) {
	api, authService := testAPI(t)
	tokenA := createSession(t, api, authService, "Alpha Desk", "admin@alpha.example.com")
	tokenB := createSession(t, api, authService, "Beta Desk", "admin@beta.example.com")

	contactA := create(t, api, tokenA, "/api/v1/contacts", map[string]any{"name": "Alex Example", "email": "alex@example.com"})
	contactB := create(t, api, tokenB, "/api/v1/contacts", map[string]any{"name": "Blair Example", "email": "blair@example.com"})
	conversationA := create(t, api, tokenA, "/api/v1/conversations", map[string]any{"contact_id": contactA, "subject": "Cannot sign in"})
	conversationB := create(t, api, tokenB, "/api/v1/conversations", map[string]any{"contact_id": contactB, "subject": "Billing question"})

	contactHistory := request(t, api, http.MethodGet, "/api/v1/contacts/"+contactA, nil, tokenA, nil)
	if contactHistory.Code != http.StatusOK || !strings.Contains(contactHistory.Body.String(), conversationA) {
		t.Fatalf("contact history status=%d body=%s", contactHistory.Code, contactHistory.Body.String())
	}
	var contactDetail struct {
		CreatedAt     string `json:"created_at"`
		Conversations []struct {
			ID string `json:"id"`
		} `json:"conversations"`
	}
	decode(t, contactHistory, &contactDetail)
	if !strings.HasSuffix(contactDetail.CreatedAt, "Z") || len(contactDetail.Conversations) != 1 {
		t.Fatalf("contact detail is not UTC or lacks history: %+v", contactDetail)
	}

	contacts := request(t, api, http.MethodGet, "/api/v1/contacts?search=alex", nil, tokenA, nil)
	if contacts.Code != http.StatusOK || !strings.Contains(contacts.Body.String(), contactA) {
		t.Fatalf("contact list status=%d body=%s", contacts.Code, contacts.Body.String())
	}

	for _, path := range []string{
		"/api/v1/conversations/" + conversationB,
		"/api/v1/contacts/" + contactB,
		"/api/v1/conversations/" + conversationB + "/messages",
	} {
		response := request(t, api, http.MethodGet, path, nil, tokenA, nil)
		if response.Code != http.StatusNotFound {
			t.Errorf("GET %s status=%d, want 404", path, response.Code)
		}
	}
	crossWorkspaceUpdate := request(t, api, http.MethodPatch, "/api/v1/conversations/"+conversationB, map[string]any{
		"status": "closed",
	}, tokenA, nil)
	if crossWorkspaceUpdate.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace update status=%d, want 404", crossWorkspaceUpdate.Code)
	}
	crossWorkspaceMessage := request(t, api, http.MethodPost, "/api/v1/conversations/"+conversationB+"/messages", map[string]any{
		"kind": "note", "body": "Must not be written.",
	}, tokenA, nil)
	if crossWorkspaceMessage.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace message status=%d, want 404", crossWorkspaceMessage.Code)
	}

	for _, message := range []map[string]any{
		{"kind": "outgoing", "body": "Please try the new link."},
		{"kind": "note", "body": "Customer confirmed identity."},
	} {
		response := request(t, api, http.MethodPost, "/api/v1/conversations/"+conversationA+"/messages", message, tokenA, nil)
		if response.Code != http.StatusCreated {
			t.Fatalf("create message status=%d body=%s", response.Code, response.Body.String())
		}
	}

	update := request(t, api, http.MethodPatch, "/api/v1/conversations/"+conversationA, map[string]any{
		"status": "pending", "priority": "high", "assignee_id": nil, "tag_ids": []string{},
	}, tokenA, nil)
	if update.Code != http.StatusOK || !strings.Contains(update.Body.String(), `"status":"pending"`) {
		t.Fatalf("update status=%d body=%s", update.Code, update.Body.String())
	}

	list := request(t, api, http.MethodGet, "/api/v1/conversations?status=pending&assignee=unassigned&search=sign&page=1&page_size=20", nil, tokenA, nil)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), conversationA) || strings.Contains(list.Body.String(), conversationB) {
		t.Fatalf("filtered list status=%d body=%s", list.Code, list.Body.String())
	}
}
