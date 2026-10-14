package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestImportedOriginHTMLAndTimeEntriesAreReturned(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	token := createSession(t, api, service, "Origin Desk", "admin@origin.example.com")
	contactID := create(t, api, token, "/api/v1/contacts", map[string]any{"name": "Ada", "email": "ada@origin.example.com"})
	conversationID := create(t, api, token, "/api/v1/conversations", map[string]any{"contact_id": contactID, "subject": "Imported"})
	messageID := create(t, api, token, "/api/v1/conversations/"+conversationID+"/messages", map[string]any{"kind": "note", "body": "Hi there"})
	seedImportDetails(t, pool, workspaceID(t, api, token), conversationID, messageID)

	var conversation struct {
		Origin *struct {
			System, ExternalID, Type string
			DueAt                    string `json:"due_at"`
		} `json:"origin"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/conversations/"+conversationID, nil, token, nil), &conversation)
	if conversation.Origin == nil || conversation.Origin.System != "freshdesk" || conversation.Origin.Type != "Problem" || conversation.Origin.DueAt != "2025-01-18T23:30:00Z" {
		t.Fatalf("conversation origin = %+v", conversation.Origin)
	}
	var timeline struct {
		Items []struct {
			BodyHTML *string `json:"body_html"`
			Origin   *struct {
				Kind string   `json:"kind"`
				To   []string `json:"to"`
			} `json:"origin"`
		} `json:"items"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/conversations/"+conversationID+"/messages", nil, token, nil), &timeline)
	if len(timeline.Items) != 1 || timeline.Items[0].BodyHTML == nil || !strings.Contains(*timeline.Items[0].BodyHTML, "<b>there</b>") ||
		timeline.Items[0].Origin == nil || timeline.Items[0].Origin.Kind != "public_note" || strings.Join(timeline.Items[0].Origin.To, ",") != "a@example.com" {
		t.Fatalf("timeline = %+v", timeline.Items)
	}
	var entries struct {
		Items []struct {
			Minutes   int    `json:"minutes"`
			AgentName string `json:"agent_name"`
			Billable  bool   `json:"billable"`
		} `json:"items"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/conversations/"+conversationID+"/time-entries", nil, token, nil), &entries)
	if len(entries.Items) != 1 || entries.Items[0].Minutes != 90 || entries.Items[0].AgentName != "Former Agent" || !entries.Items[0].Billable {
		t.Fatalf("time entries = %+v", entries.Items)
	}
}

func TestTimeEntriesAreWorkspaceScoped(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	tokenA := createSession(t, api, service, "Time A", "admin@time-a.example.com")
	tokenB := createSession(t, api, service, "Time B", "admin@time-b.example.com")
	contactA := create(t, api, tokenA, "/api/v1/contacts", map[string]any{"name": "A", "email": "a@time.example.com"})
	conversationA := create(t, api, tokenA, "/api/v1/conversations", map[string]any{"contact_id": contactA, "subject": "Billable"})
	messageA := create(t, api, tokenA, "/api/v1/conversations/"+conversationA+"/messages", map[string]any{"kind": "note", "body": "x"})
	seedImportDetails(t, pool, workspaceID(t, api, tokenA), conversationA, messageA)
	response := request(t, api, http.MethodGet, "/api/v1/conversations/"+conversationA+"/time-entries", nil, tokenB, nil)
	if response.Code != http.StatusNotFound || strings.Contains(response.Body.String(), "Former Agent") {
		t.Fatalf("workspace B read A's time entries: status=%d body=%s", response.Code, response.Body.String())
	}
}
