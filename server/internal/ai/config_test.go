package ai_test

import (
	"math"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/domain"
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

// A cache price the operator leaves out is the usual share of the input price, never free: a limit in
// dollars counts what the cache costs. A written price, zero included, is kept.
func TestCachePricesLeftOutAreSharesOfTheInputPrice(t *testing.T) {
	env := map[string]string{"AI_SERVER_PROVIDER": "anthropic", "AI_SERVER_API_KEY": "k", "AI_SERVER_MODELS": "draft=claude-haiku-5-5",
		"AI_SERVER_PRICES": "claude-haiku-5-5=1:5, other=2:8:0:0, third=4:8:0.5"}
	server, err := ai.ServerProviderFromEnv(func(key string) string { return env[key] })
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]ai.Price{
		"claude-haiku-5-5": {Input: 1, Output: 5, CacheRead: 0.1, CacheWrite: 1.25},
		"other":            {Input: 2, Output: 8},
		"third":            {Input: 4, Output: 8, CacheRead: 0.5, CacheWrite: 5},
	}
	for model, price := range want {
		got := server.Prices[model]
		if math.Abs(got.CacheRead-price.CacheRead) > 1e-9 || math.Abs(got.CacheWrite-price.CacheWrite) > 1e-9 || got.Input != price.Input || got.Output != price.Output {
			t.Errorf("%s = %+v, want %+v", model, got, price)
		}
	}
}

// The server's provider is bounded in credits: one reply draft or one triage suggestion is one credit.
// A workspace gets its credits each month, and a trial its own number for the whole trial.
func TestMonthlyCreditsFromEnv(t *testing.T) {
	base := map[string]string{"AI_SERVER_PROVIDER": "anthropic", "AI_SERVER_API_KEY": "k", "AI_SERVER_MODELS": "draft=claude-haiku-5-5"}
	with := func(credits string) map[string]string {
		values := map[string]string{"AI_SERVER_MONTHLY_CREDITS": credits}
		for key, value := range base {
			values[key] = value
		}
		return values
	}
	for value, want := range map[string]ai.Allowance{
		"1000":                      {PerWorkspace: 1000},
		"1000,trial=500":            {PerWorkspace: 1000, Trial: 500},
		"workspace=1000, trial=500": {PerWorkspace: 1000, Trial: 500},
	} {
		server, err := ai.ServerProviderFromEnv(getenv(with(value)))
		if err != nil {
			t.Fatalf("%q: %v", value, err)
		}
		if server.Allowance == nil || *server.Allowance != want {
			t.Fatalf("%q = %+v, want %+v", value, server.Allowance, want)
		}
	}
	// Credits need no prices: no model is priced here.
	if server, err := ai.ServerProviderFromEnv(getenv(base)); err != nil || server.Allowance != nil {
		t.Fatalf("without credits: %+v %v, want no limit", server, err)
	}
	for _, value := range []string{"ten", "1000.5", "-1", "agent=3,trial=2", "1000,trial=x"} {
		if _, err := ai.ServerProviderFromEnv(getenv(with(value))); err == nil || !strings.Contains(err.Error(), "ai.server.monthly_credits") {
			t.Fatalf("%q: %v, want an error naming ai.server.monthly_credits", value, err)
		}
	}
}

// A dollar amount left in the old setting must never be read as credits: the server stops and names the new one.
// A stored dollar value must not stop the configuration from loading (that would drop every other
// setting with it), and must never be read as credits: the server's provider gets no credits until
// AI_SERVER_MONTHLY_CREDITS is set.
func TestTheOldDollarAllowanceAloneGivesNoCreditsAndIsFlagged(t *testing.T) {
	values := map[string]string{"AI_SERVER_PROVIDER": "anthropic", "AI_SERVER_API_KEY": "k", "AI_SERVER_MODELS": "draft=claude-haiku-5-5",
		"AI_SERVER_MONTHLY_ALLOWANCE": "10"}
	server, err := ai.ServerProviderFromEnv(getenv(values))
	if err != nil {
		t.Fatalf("error = %v, want the configuration to load", err)
	}
	if !server.LegacyAllowance {
		t.Fatal("LegacyAllowance = false, want the old setting flagged")
	}
	if server.Allowance == nil {
		t.Fatal("Allowance = nil (no limit), want 0 credits")
	}
	if got := server.Allowance.For(domain.Access{Plan: domain.PlanTrial}); got != 0 {
		t.Fatalf("trial credits = %d, want 0", got)
	}
	if got := server.Allowance.For(domain.Access{Plan: domain.PlanTeam}); got != 0 {
		t.Fatalf("plan credits = %d, want 0", got)
	}
}

func TestTheCreditsSettingWinsOverTheOldDollarAllowance(t *testing.T) {
	values := map[string]string{"AI_SERVER_PROVIDER": "anthropic", "AI_SERVER_API_KEY": "k", "AI_SERVER_MODELS": "draft=claude-haiku-5-5",
		"AI_SERVER_MONTHLY_ALLOWANCE": "10", "AI_SERVER_MONTHLY_CREDITS": "1000,trial=500"}
	server, err := ai.ServerProviderFromEnv(getenv(values))
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if !server.LegacyAllowance {
		t.Fatal("LegacyAllowance = false, want the old setting flagged so the operator removes it")
	}
	if got := server.Allowance.For(domain.Access{Plan: domain.PlanTeam}); got != 1000 {
		t.Fatalf("plan credits = %d, want 1000", got)
	}
	if got := server.Allowance.For(domain.Access{Plan: domain.PlanTrial}); got != 500 {
		t.Fatalf("trial credits = %d, want 500", got)
	}
}
