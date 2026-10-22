package main

import (
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
)

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestAIServerSettingsFromTheEnvironment(t *testing.T) {
	off, err := aiServerFromEnv(env(nil))
	if err != nil || off.Enabled() {
		t.Fatalf("no variables: %+v %v, want AI off and no error", off, err)
	}
	claude, err := aiServerFromEnv(env(map[string]string{"AI_SERVER_PROVIDER": "anthropic", "AI_SERVER_API_KEY": "sk-test"}))
	if err != nil || !claude.Enabled() || claude.Settings.Model != "claude-sonnet-5-5" || claude.Settings.SelectModel != "claude-haiku-5-5" || claude.Settings.Effort != aiprovider.EffortLow {
		t.Fatalf("Claude defaults: %+v %v", claude.Settings, err)
	}
	if claude.Provider.Kind() != aiprovider.KindAnthropic {
		t.Errorf("provider kind = %s", claude.Provider.Kind())
	}
	local, err := aiServerFromEnv(env(map[string]string{"AI_SERVER_PROVIDER": "openai_compatible", "AI_SERVER_BASE_URL": "http://localhost:11434/v1",
		"AI_SERVER_MODEL": "qwen3", "AI_SERVER_EFFORT": "medium"}))
	if err != nil || !local.Enabled() || local.Settings.Model != "qwen3" || local.Settings.SelectModel != "" || local.Settings.Effort != aiprovider.EffortMedium {
		t.Fatalf("OpenAI-compatible: %+v %v, want the given model and no chooser unless set", local.Settings, err)
	}
	for name, values := range map[string]map[string]string{
		"unknown provider":           {"AI_SERVER_PROVIDER": "watson", "AI_SERVER_API_KEY": "k"},
		"Claude without a key":       {"AI_SERVER_PROVIDER": "anthropic"},
		"OpenAI-compatible no model": {"AI_SERVER_PROVIDER": "openai_compatible", "AI_SERVER_BASE_URL": "http://localhost:11434/v1"},
		"bad effort":                 {"AI_SERVER_PROVIDER": "anthropic", "AI_SERVER_API_KEY": "k", "AI_SERVER_EFFORT": "max"},
	} {
		if _, err := aiServerFromEnv(env(values)); err == nil || !strings.Contains(err.Error(), "AI_SERVER_") {
			t.Errorf("%s: err = %v, want one naming the variable", name, err)
		}
	}
	if _, err := aiServerFromEnv(env(map[string]string{"AI_SERVER_PROVIDER": "anthropic", "AI_SERVER_API_KEY": "sk-secret-value", "AI_SERVER_EFFORT": "max"})); err != nil && strings.Contains(err.Error(), "sk-secret-value") {
		t.Error("the error repeats the key")
	}
}
