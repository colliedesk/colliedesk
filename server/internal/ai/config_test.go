package ai_test

import (
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/ai"
)

func getenv(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestServerProviderFromEnv(t *testing.T) {
	none, err := ai.ServerProviderFromEnv(getenv(nil))
	if err != nil || none != nil {
		t.Fatalf("nothing set: %+v %v", none, err)
	}
	server, err := ai.ServerProviderFromEnv(getenv(map[string]string{
		"AI_SERVER_PROVIDER": "anthropic", "AI_SERVER_API_KEY": "sk-secret", "AI_SERVER_LABEL": "Colliedesk AI",
		"AI_SERVER_MODELS": "triage=claude-haiku-5-5, draft=claude-sonnet-5-5|claude-opus-5-5",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if server.Label != "Colliedesk AI" || strings.Join(server.ModelsFor(ai.SlotDraft), ",") != "claude-sonnet-5-5,claude-opus-5-5" || server.ModelsFor(ai.SlotTriage)[0] != "claude-haiku-5-5" {
		t.Fatalf("server = %+v", server)
	}
	local, err := ai.ServerProviderFromEnv(getenv(map[string]string{
		"AI_SERVER_PROVIDER": "openai_compatible", "AI_SERVER_BASE_URL": "http://ollama:11434/v1", "AI_SERVER_MODELS": "draft=llama-4-scout",
	}))
	if err != nil || local.Label != ai.DefaultServerLabel || local.APIKey != "" || len(local.ModelsFor(ai.SlotTriage)) != 0 {
		t.Fatalf("local server = %+v %v", local, err)
	}

	for name, values := range map[string]map[string]string{
		"key only":        {"AI_SERVER_API_KEY": "sk-secret"},
		"no key":          {"AI_SERVER_PROVIDER": "anthropic", "AI_SERVER_MODELS": "draft=m"},
		"no models":       {"AI_SERVER_PROVIDER": "anthropic", "AI_SERVER_API_KEY": "sk-secret"},
		"no base URL":     {"AI_SERVER_PROVIDER": "openai_compatible", "AI_SERVER_MODELS": "draft=m"},
		"unknown kind":    {"AI_SERVER_PROVIDER": "gemini", "AI_SERVER_API_KEY": "sk-secret", "AI_SERVER_MODELS": "draft=m"},
		"unknown slot":    {"AI_SERVER_PROVIDER": "anthropic", "AI_SERVER_API_KEY": "sk-secret", "AI_SERVER_MODELS": "summary=m"},
		"slot twice":      {"AI_SERVER_PROVIDER": "anthropic", "AI_SERVER_API_KEY": "sk-secret", "AI_SERVER_MODELS": "draft=a,draft=b"},
		"empty model":     {"AI_SERVER_PROVIDER": "anthropic", "AI_SERVER_API_KEY": "sk-secret", "AI_SERVER_MODELS": "draft=a|"},
		"bad base URL":    {"AI_SERVER_PROVIDER": "openai_compatible", "AI_SERVER_BASE_URL": "ftp://x", "AI_SERVER_MODELS": "draft=m"},
		"URL with secret": {"AI_SERVER_PROVIDER": "openai_compatible", "AI_SERVER_BASE_URL": "https://u:sk-secret@x/v1", "AI_SERVER_MODELS": "draft=m"},
	} {
		_, err := ai.ServerProviderFromEnv(getenv(values))
		if err == nil {
			t.Fatalf("%s: accepted", name)
		}
		if strings.Contains(err.Error(), "sk-secret") {
			t.Fatalf("%s: error repeats the key: %v", name, err)
		}
	}
}
