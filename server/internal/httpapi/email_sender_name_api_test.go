package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

func senderName(t *testing.T, api http.Handler, token string) string {
	t.Helper()
	response := request(t, api, http.MethodGet, "/api/v1/channels/email", nil, token, nil)
	var settings struct {
		Channel *struct {
			SenderName *string `json:"sender_name"`
		} `json:"channel"`
	}
	decode(t, response, &settings)
	if response.Code != http.StatusOK || settings.Channel == nil || settings.Channel.SenderName == nil {
		t.Fatalf("GET email channel status=%d body=%s", response.Code, response.Body.String())
	}
	return *settings.Channel.SenderName
}

func putChannel(t *testing.T, api http.Handler, token string, body map[string]any) (int, string) {
	t.Helper()
	response := request(t, api, http.MethodPut, "/api/v1/channels/email", body, token, nil)
	return response.Code, response.Body.String()
}

func TestTheEmailChannelSenderNameStartsAsTheWorkspaceNameAndIsEditable(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Sender Desk", "admin@sender-desk.example.com")

	if status, body := putChannel(t, api, admin, map[string]any{"support_address": "help@sender-desk.example.com"}); status != http.StatusOK {
		t.Fatalf("connect status=%d body=%s", status, body)
	}
	if got := senderName(t, api, admin); got != "Sender Desk" {
		t.Fatalf("a new channel's sender name = %q, want the workspace name", got)
	}

	if status, body := putChannel(t, api, admin, map[string]any{"support_address": "help@sender-desk.example.com", "sender_name": "  Colliedesk Support "}); status != http.StatusOK ||
		!strings.Contains(body, `"sender_name":"Colliedesk Support"`) {
		t.Fatalf("set sender name status=%d body=%s", status, body)
	}
	if status, body := putChannel(t, api, admin, map[string]any{"support_address": "support@sender-desk.example.com"}); status != http.StatusOK {
		t.Fatalf("change address status=%d body=%s", status, body)
	}
	if got := senderName(t, api, admin); got != "Colliedesk Support" {
		t.Fatalf("an update without sender_name changed it to %q", got)
	}

	invalid := []any{"", "   ", strings.Repeat("x", 101), "Acme\r\nBcc: victim@example.com", "Tab\tname", "Spoof ‮gnp", 42, nil}
	for _, value := range invalid {
		status, body := putChannel(t, api, admin, map[string]any{"support_address": "support@sender-desk.example.com", "sender_name": value})
		if status != http.StatusBadRequest || !strings.Contains(body, `"code":"invalid_request"`) || !strings.Contains(body, "sender_name") {
			t.Errorf("sender_name %q status=%d body=%s, want 400 invalid_request naming sender_name", value, status, body)
		}
	}
	if got := senderName(t, api, admin); got != "Colliedesk Support" {
		t.Fatalf("a refused update changed the sender name to %q", got)
	}

	agent := joinAsAgent(t, api, admin, "agent@sender-desk.example.com")
	if status, _ := putChannel(t, api, agent, map[string]any{"support_address": "support@sender-desk.example.com", "sender_name": "Agent Pick"}); status != http.StatusForbidden {
		t.Fatalf("agent PUT status=%d, want 403", status)
	}
	if got := senderName(t, api, agent); got != "Colliedesk Support" {
		t.Fatalf("an agent reads sender name %q", got)
	}
}

func TestANewChannelCanBeConnectedWithItsOwnSenderName(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "acceptance-20261015-b", "admin@first-name.example.com")
	if status, body := putChannel(t, api, admin, map[string]any{"support_address": "help@first-name.example.com", "sender_name": "First Name Support"}); status != http.StatusOK {
		t.Fatalf("connect status=%d body=%s", status, body)
	}
	if got := senderName(t, api, admin); got != "First Name Support" {
		t.Fatalf("sender name = %q", got)
	}
}

func TestSenderNamesStayInTheirOwnWorkspace(t *testing.T) {
	api, service := testAPI(t)
	adminA := createSession(t, api, service, "Sender Scope A", "admin@sender-scope-a.example.com")
	adminB := createSession(t, api, service, "Sender Scope B", "admin@sender-scope-b.example.com")
	putChannel(t, api, adminA, map[string]any{"support_address": "help@sender-scope-a.example.com"})
	if status, body := putChannel(t, api, adminB, map[string]any{"support_address": "help@sender-scope-b.example.com", "sender_name": "B Support"}); status != http.StatusOK {
		t.Fatalf("B status=%d body=%s", status, body)
	}
	if got := senderName(t, api, adminA); got != "Sender Scope A" {
		t.Fatalf("workspace A sender name = %q after B changed its own", got)
	}
	if got := senderName(t, api, adminB); got != "B Support" {
		t.Fatalf("workspace B sender name = %q", got)
	}
}
