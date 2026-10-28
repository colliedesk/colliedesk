package mcpserver_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestADraftTokenSendsItsAgentATestMessageInItsOwnWorkspace(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Test Message Desk", "admin@test-message.example.com")
	other := e.newWorkspace("Other Test Desk", "admin@other-test.example.com")
	for _, item := range []workspace{w, other} {
		if status, body := e.rest(http.MethodPut, "/api/v1/channels/email", map[string]any{"support_address": "support@" + item.id + ".example.com"}, item.admin); status != http.StatusOK {
			t.Fatalf("connect email: status=%d body=%v", status, body)
		}
	}
	session := e.connect(w.draft, "")
	sent := e.ok(session, "colliedesk_email_test_message_send", nil)
	if sent["sent_to"] != "admin@test-message.example.com" || sent["conversation_id"] == "" {
		t.Fatalf("test message = %v", sent)
	}
	conversation := e.ok(session, "colliedesk_conversation_get", map[string]any{"conversation_id": sent["conversation_id"]})
	if !strings.Contains(stringify(conversation), "Test message from Colliedesk") {
		t.Fatalf("the test conversation = %v", conversation)
	}
	_, channels := e.rest(http.MethodGet, "/api/v1/channels", nil, other.admin)
	otherChannel := ""
	for _, item := range channels["items"].([]any) {
		if channel := item.(map[string]any); channel["type"] == "email" {
			otherChannel = channel["id"].(string)
		}
	}
	if message := e.fails(session, "colliedesk_email_test_message_send", map[string]any{"channel_id": otherChannel}); !strings.Contains(message, "No email channel") {
		t.Fatalf("another workspace's channel: %q", message)
	}
}
