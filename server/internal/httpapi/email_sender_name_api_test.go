package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
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
	if status, body := putChannel(t, api, adminB, map[string]any{"sender_name": "B Team"}); status != http.StatusOK {
		t.Fatalf("B sender name alone status=%d body=%s", status, body)
	}
	if got := senderName(t, api, adminA); got != "Sender Scope A" {
		t.Fatalf("workspace A sender name = %q after B changed its own", got)
	}
	if got := senderName(t, api, adminB); got != "B Team" {
		t.Fatalf("workspace B sender name = %q", got)
	}
}

// Saving only the sender name never touches the address: a stale address in a browser tab or a typo
// in an agent's call cannot reset domain verification or retire the provider domain.
func TestSavingOnlyTheSenderNameKeepsTheAddressAndItsVerification(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, nil)
	admin := createSession(t, api, service, "Keep Address", "admin@keep-address.example.com")
	if status, body := putChannel(t, api, admin, map[string]any{"sender_name": "Keep Support"}); status != http.StatusBadRequest || !strings.Contains(body, "support address") {
		t.Fatalf("sender name before the channel is connected: status=%d body=%s, want 400 asking for the address", status, body)
	}
	if status, body := putChannel(t, api, admin, map[string]any{"support_address": "help@keep-address.example.com"}); status != http.StatusOK {
		t.Fatalf("connect status=%d body=%s", status, body)
	}
	workspace := workspaceID(t, api, admin)
	if _, err := pool.Exec(context.Background(), `UPDATE email_channels SET domain_verified_at='2026-10-20T08:00:00Z',provider_domain_id='pd-keep-address' WHERE workspace_id=$1`, workspace); err != nil {
		t.Fatal(err)
	}

	status, body := putChannel(t, api, admin, map[string]any{"sender_name": " Keep Support "})
	if status != http.StatusOK || !strings.Contains(body, `"sender_name":"Keep Support"`) || !strings.Contains(body, `"support_address":"help@keep-address.example.com"`) {
		t.Fatalf("sender name alone: status=%d body=%s", status, body)
	}
	row := queryString(t, pool, `SELECT support_address||' '||domain_verified_at::text||' '||COALESCE(provider_domain_id,'none')||' '||
		(SELECT count(*) FROM email_provider_domain_trash WHERE workspace_id=$1)::text FROM email_channels WHERE workspace_id=$1`, workspace)
	if want := "help@keep-address.example.com 2026-10-20 08:00:00+00 pd-keep-address 0"; row != want {
		t.Fatalf("after saving the sender name: %q, want %q", row, want)
	}
	if status, body := putChannel(t, api, admin, map[string]any{}); status != http.StatusBadRequest {
		t.Fatalf("an empty body status=%d body=%s, want 400", status, body)
	}
}

// email_channel_saved counts a workspace connecting email or moving it to a new address; renaming the
// sender is not activation.
func TestOnlyConnectingOrMovingTheChannelCountsAsSavingIt(t *testing.T) {
	funnel := &recordingFunnel{}
	api, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.Funnel = funnel })
	admin := createSession(t, api, service, "Counted Desk", "admin@counted-desk.example.com")
	steps := []map[string]any{
		{"support_address": "help@counted-desk.example.com"},
		{"sender_name": "Counted Support"},
		{"support_address": "help@counted-desk.example.com", "sender_name": "Counted Team"},
		{"support_address": "support@counted-desk.example.com"},
	}
	for _, body := range steps {
		if status, text := putChannel(t, api, admin, body); status != http.StatusOK {
			t.Fatalf("PUT %v status=%d body=%s", body, status, text)
		}
	}
	if got := len(funnel.named("email_channel_saved")); got != 2 {
		t.Fatalf("email_channel_saved recorded %d times, want 2 (connect and the new address)", got)
	}
}
