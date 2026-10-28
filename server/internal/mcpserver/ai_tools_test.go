package mcpserver_test

import (
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/colliedesk/colliedesk/server/internal/google"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
)

const aiToolKey = "sk-mcp-canary-DO-NOT-PRINT-7w2e"

func TestAnAdminTokenManagesAIProvidersThroughTheTools(t *testing.T) {
	fake := &aiprovider.Fake{}
	e := newEnvWith(t, func(config *httpapi.Config) {
		keys, _ := tokenseal.ParseKeys("k1:AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM=")
		config.Google = google.Config{Keys: keys}
		config.AI = ai.Config{Build: func(ai.Spec) (aiprovider.Provider, error) { return fake, nil }}
	})
	w := e.newWorkspace("AI Tools Desk", "admin@ai-tools.example.com")
	admin := e.connect(w.adminTier, "")

	added := e.ok(admin, "colliedesk_ai_provider_add", map[string]any{"kind": "anthropic", "label": "Team key", "api_key": aiToolKey, "models": []string{"claude-sonnet-5-5"}})
	if strings.Contains(stringify(added), aiToolKey) || at(t, added, "provider", "key_hint") != "7w2e" {
		t.Fatalf("added = %v", added)
	}
	id := at(t, added, "provider", "id").(string)
	renamed := e.ok(admin, "colliedesk_ai_provider_update", map[string]any{"provider_id": id, "label": "Renamed"})
	if at(t, renamed, "provider", "label") != "Renamed" {
		t.Fatalf("renamed = %v", renamed)
	}
	if checked := e.ok(admin, "colliedesk_ai_provider_check", map[string]any{"provider_id": id}); at(t, checked, "provider", "check_status") != "ok" {
		t.Fatalf("checked = %v", checked)
	}
	if keyed := e.ok(admin, "colliedesk_ai_provider_key_set", map[string]any{"provider_id": id, "api_key": "sk-new-key-AB12"}); at(t, keyed, "provider", "key_hint") != "AB12" {
		t.Fatalf("new key = %v", keyed)
	}
	fake.Script(aiprovider.FakeReply{Err: &aiprovider.Error{Code: aiprovider.ErrorAuth, Status: 401}})
	if message := e.fails(admin, "colliedesk_ai_provider_key_set", map[string]any{"provider_id": id, "api_key": "sk-bad"}); !strings.Contains(message, "refused this key") {
		t.Fatalf("refused key: %q", message)
	}

	// Turning a feature on always waits for a person, whatever the tier.
	filed := e.ok(admin, "colliedesk_ai_slot_set_request", map[string]any{"slot": "draft", "source": "provider", "provider_id": id, "enabled": true})
	if at(t, filed, "agent_request", "status") != "pending" {
		t.Fatalf("slot request = %v", filed)
	}
	reader := e.connect(w.read, "")
	settings := e.ok(reader, "colliedesk_ai_settings_get", nil)
	if strings.Contains(stringify(settings), aiToolKey) || !strings.Contains(stringify(settings), "Renamed") || strings.Contains(stringify(settings), `"enabled":true`) {
		t.Fatalf("settings = %v", settings)
	}
	if message := e.fails(reader, "colliedesk_ai_provider_add", map[string]any{"kind": "anthropic", "label": "X", "api_key": aiToolKey, "models": []string{"m"}}); !strings.Contains(message, "admin") {
		t.Fatalf("read token adding a provider: %q", message)
	}

	removed := e.ok(admin, "colliedesk_ai_provider_remove", map[string]any{"provider_id": id})
	if _, ok := removed["turned_off"]; !ok {
		t.Fatalf("removed = %v", removed)
	}
	if message := e.fails(admin, "colliedesk_ai_provider_check", map[string]any{"provider_id": id}); !strings.Contains(message, "No AI provider with that id") {
		t.Fatalf("check a deleted provider: %q", message)
	}
}
