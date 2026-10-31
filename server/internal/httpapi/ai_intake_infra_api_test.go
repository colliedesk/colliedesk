package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// An OpenRouter provider keeps requests on providers by data policy. Its admin token tightens the
// routing at once; loosening it lets customers' data reach providers that store or train on it, so it
// waits for a person, and nothing changes before. Another server has no routing to set.
func TestLooseningOpenRouterRoutingFromATokenWaitsForAPerson(t *testing.T) {
	d := newLevelDesk(t)
	router := create(t, d.api, d.adminA, "/api/v1/ai/providers", map[string]any{"kind": "openai_compatible", "label": "OpenRouter",
		"base_url": "https://openrouter.ai/api/v1", "api_key": "sk-or-route-0001", "models": []string{"openai/gpt-5"},
		"routing": map[string]any{"data_collection": "deny", "zdr": true}})
	routing := func() string {
		return queryString(t, d.pool, `SELECT routing::text FROM ai_providers WHERE id=$1`, router)
	}
	if got := routing(); got != `{"zdr": true, "data_collection": "deny"}` {
		t.Fatalf("stored routing = %s", got)
	}
	admin := issueToken(t, d.api, d.adminA, "routing admin", "admin").Token
	loosen := map[string]any{"routing": map[string]any{"data_collection": "allow", "zdr": true}}
	response := request(t, d.api, http.MethodPatch, "/api/v1/ai/providers/"+router, loosen, admin, nil)
	if response.Code != http.StatusAccepted {
		t.Fatalf("token loosens: %d %s", response.Code, response.Body.String())
	}
	var filed struct {
		RequestID string `json:"request_id"`
		Summary   string `json:"summary"`
	}
	decode(t, response, &filed)
	if !strings.Contains(filed.Summary, "OpenRouter") || !strings.Contains(filed.Summary, "store or train") {
		t.Fatalf("summary = %q", filed.Summary)
	}
	if got := routing(); !strings.Contains(got, "deny") {
		t.Fatalf("a filed loosening changed the routing: %s", got)
	}
	if approved := approve(t, d.api, d.adminA, filed.RequestID, nil); approved.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", approved.Code, approved.Body.String())
	}
	if got := routing(); got != `{"zdr": true}` {
		t.Fatalf("after approval = %s", got)
	}
	tighten := map[string]any{"routing": map[string]any{"data_collection": "deny", "zdr": true}}
	if response := request(t, d.api, http.MethodPatch, "/api/v1/ai/providers/"+router, tighten, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("token tightens: %d %s", response.Code, response.Body.String())
	}
	if response := request(t, d.api, http.MethodPatch, "/api/v1/ai/providers/"+d.providerA, tighten, d.adminA, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("routing on Anthropic: %d %s", response.Code, response.Body.String())
	}
	var shown struct {
		Providers []struct {
			ID      string          `json:"id"`
			Routing json.RawMessage `json:"routing"`
		} `json:"providers"`
	}
	decode(t, request(t, d.api, http.MethodGet, "/api/v1/ai", nil, d.adminA, nil), &shown)
	for _, provider := range shown.Providers {
		want := "null"
		if provider.ID == router {
			want = `{"data_collection":"deny","zdr":true}`
		}
		if string(provider.Routing) != want {
			t.Fatalf("provider %s routing = %s, want %s", provider.ID, provider.Routing, want)
		}
	}
	if response := request(t, d.api, http.MethodPatch, "/api/v1/ai/providers/"+router, loosen, d.adminB, nil); response.Code != http.StatusNotFound {
		t.Fatalf("another workspace's admin: %d", response.Code)
	}
	exported := request(t, d.api, http.MethodGet, "/api/v1/export", nil, d.adminA, nil)
	if !strings.Contains(string(unzip(t, exported.Body.Bytes())["ai-providers.ndjson"]), `"routing":{"zdr":true,"data_collection":"deny"}`) {
		t.Fatalf("export providers: %s", unzip(t, exported.Body.Bytes())["ai-providers.ndjson"])
	}
}

// Intake's fallback model is one more of its provider's models: triage has none, the provider cannot
// drop it, a new one takes intake's channels back to suggest (its gate was passed without it), and the
// export keeps it.
func TestIntakesFallbackModelOverTheAPI(t *testing.T) {
	d := newLevelDesk(t)
	two := create(t, d.api, d.adminA, "/api/v1/ai/providers", map[string]any{"kind": "anthropic", "label": "Two models", "api_key": "sk-level-0005",
		"models": []string{"claude-sonnet-5-5", "claude-haiku-5-5"}})
	d.setSlot(t, d.adminA, "triage", map[string]any{"source": "provider", "provider_id": two, "fallback_model": "claude-haiku-5-5", "enabled": true}, http.StatusBadRequest)
	d.setSlot(t, d.adminA, "intake", map[string]any{"source": "provider", "provider_id": two, "enabled": true}, http.StatusOK)
	d.setLevel(t, d.adminA, d.chatA, "intake", map[string]any{"scope": "group", "level": "auto"})
	slot := d.setSlot(t, d.adminA, "intake", map[string]any{"source": "provider", "provider_id": two, "fallback_model": "claude-haiku-5-5", "enabled": true}, http.StatusOK)
	if slot.FallbackModel == nil || *slot.FallbackModel != "claude-haiku-5-5" {
		t.Fatalf("slot = %+v", slot)
	}
	if item, _ := effective(d.levels(t, d.adminA, d.chatA), "group", "intake"); item.Level == nil || *item.Level != "suggest" {
		t.Fatalf("intake after a new fallback model = %+v", item)
	}
	response := request(t, d.api, http.MethodPatch, "/api/v1/ai/providers/"+two, map[string]any{"models": []string{"claude-sonnet-5-5"}}, d.adminA, nil)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "claude-haiku-5-5") {
		t.Fatalf("drop the fallback model: %d %s", response.Code, response.Body.String())
	}
	exported := request(t, d.api, http.MethodGet, "/api/v1/export", nil, d.adminA, nil)
	if !strings.Contains(string(unzip(t, exported.Body.Bytes())["ai-slots.ndjson"]), `"fallback_model":"claude-haiku-5-5"`) {
		t.Fatalf("export slots: %s", unzip(t, exported.Body.Bytes())["ai-slots.ndjson"])
	}
	limits := request(t, d.api, http.MethodPut, "/api/v1/ai/automatic-limits", map[string]any{"per_day": 300, "per_minute": 30, "intake_per_day": 50}, d.adminA, nil)
	if limits.Code != http.StatusOK || !strings.Contains(limits.Body.String(), `"intake_per_day":50`) {
		t.Fatalf("limits: %d %s", limits.Code, limits.Body.String())
	}
}
