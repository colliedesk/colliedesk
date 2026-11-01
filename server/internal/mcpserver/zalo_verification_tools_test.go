package mcpserver_test

import (
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/google"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
)

// An agent with an admin token does the whole verification with MCP alone, and every answer carries the
// absolute addresses to give the person for the Zalo app: the check says what is missing, one update
// sets Zalo's code, and its answer has the URL prefix to verify and the webhook URL.
func TestAnAgentVerifiesAZaloWebhookAddressWithMCPAlone(t *testing.T) {
	// The channel's secrets are sealed with the server's key ring (MAILBOX_TOKEN_KEYS).
	e := newEnvWith(t, func(config *httpapi.Config) {
		keys, _ := tokenseal.ParseKeys("k1:AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM=")
		config.Google = google.Config{Keys: keys}
	})
	w := e.newWorkspace("Zalo Agent Desk", "admin@zalo-agent.example.com")
	session := e.connect(w.adminTier, "")
	config := func(answer map[string]any) map[string]any {
		t.Helper()
		channel, _ := answer["channel"].(map[string]any)
		config, _ := channel["config"].(map[string]any)
		if config == nil {
			t.Fatalf("no config in %v", answer)
		}
		return config
	}
	created := e.ok(session, "colliedesk_channels_create", map[string]any{"type": "zalo", "config": map[string]any{"oa_id": "5550002", "app_id": "700004"}})
	id, _ := created["channel"].(map[string]any)["id"].(string)
	if got := config(created); got["verification_prefix_url"] != testOrigin+"/api/v1/channels/zalo/" || got["webhook_url"] != testOrigin+"/api/v1/channels/zalo/webhook" {
		t.Fatalf("the addresses to give = %v, %v", got["verification_prefix_url"], got["webhook_url"])
	}
	for _, secret := range []string{"oa_secret_key", "app_secret", "access_token", "refresh_token"} {
		e.ok(session, "colliedesk_channels_credentials_set", map[string]any{"channel_id": id, "name": secret, "value": "value-of-" + secret})
	}
	checked := e.ok(session, "colliedesk_channels_check", map[string]any{"channel_id": id})
	status, _ := checked["channel"].(map[string]any)["status"].(map[string]any)
	if message, _ := status["message"].(string); !strings.Contains(message, "site_verification") {
		t.Fatalf("a channel with no code and no delivery yet: status %v, want it to say to set site_verification", status)
	}
	updated := e.ok(session, "colliedesk_channels_update", map[string]any{"channel_id": id, "config": map[string]any{"site_verification": "zalo_verifierAgentCode_0001.html"}})
	if got := config(updated); got["site_verification"] != "AgentCode_0001" || got["verification_prefix_url"] != testOrigin+"/api/v1/channels/zalo/" {
		t.Fatalf("after the code = %v", got)
	}
	checked = e.ok(session, "colliedesk_channels_check", map[string]any{"channel_id": id})
	status, _ = checked["channel"].(map[string]any)["status"].(map[string]any)
	if message, _ := status["message"].(string); strings.Contains(message, "site_verification") {
		t.Fatalf("a channel with its code still asks for one: %v", status)
	}
}
