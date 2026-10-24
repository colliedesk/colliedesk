package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/colliedesk/colliedesk/server/internal/google"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
	"github.com/jackc/pgx/v5/pgxpool"
)

const aiCanaryKey = "sk-ai-api-canary-DO-NOT-PRINT-5k9q"

type aiDesk struct {
	api        http.Handler
	pool       *pgxpool.Pool
	fake       *aiprovider.Fake
	adminA     string
	adminB     string
	providerB  string
	providerA  string
	workspaceA string
}

func newAIDesk(t *testing.T, mutate func(*httpapi.Config)) aiDesk {
	t.Helper()
	fake := &aiprovider.Fake{}
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		keys, _ := tokenseal.ParseKeys("k1:AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM=")
		config.Google = google.Config{Keys: keys}
		config.AI = fakeAIConfig(fake)
		if mutate != nil {
			mutate(config)
		}
	})
	desk := aiDesk{api: api, pool: pool, fake: fake}
	desk.adminA = createSession(t, api, service, "AI Desk A", "admin@ai-a.example.com")
	desk.adminB = createSession(t, api, service, "AI Desk B", "admin@ai-b.example.com")
	desk.providerA = create(t, api, desk.adminA, "/api/v1/ai/providers", map[string]any{"kind": "anthropic", "label": "Team key", "api_key": aiCanaryKey, "models": []string{"claude-sonnet-5-5"}})
	desk.providerB = create(t, api, desk.adminB, "/api/v1/ai/providers", map[string]any{"kind": "anthropic", "label": "B key", "api_key": "sk-b-1111", "models": []string{"claude-haiku-5-5"}})
	desk.workspaceA = workspaceID(t, api, desk.adminA)
	return desk
}

func TestAIProviderRoutesNeverCrossWorkspaces(t *testing.T) {
	desk := newAIDesk(t, nil)
	other := "/api/v1/ai/providers/" + desk.providerB
	for _, call := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPatch, other, map[string]any{"label": "taken over"}},
		{http.MethodPut, other + "/key", map[string]any{"api_key": "sk-x-0000"}},
		{http.MethodPost, other + "/check", nil},
		{http.MethodDelete, other, nil},
	} {
		wantResponse(t, request(t, desk.api, call.method, call.path, call.body, desk.adminA, nil), http.StatusNotFound, "not_found")
	}
	wantResponse(t, request(t, desk.api, http.MethodPut, "/api/v1/ai/slots/draft", map[string]any{"source": "provider", "provider_id": desk.providerB, "enabled": true}, desk.adminA, nil),
		http.StatusBadRequest, "invalid_request")
	var settings ai.Settings
	decode(t, request(t, desk.api, http.MethodGet, "/api/v1/ai", nil, desk.adminA, nil), &settings)
	if len(settings.Providers) != 1 || settings.Providers[0].ID != desk.providerA {
		t.Fatalf("workspace A sees providers %+v", settings.Providers)
	}
	for _, event := range settings.Events {
		if event.ProviderLabel != nil && *event.ProviderLabel == "B key" {
			t.Fatal("workspace A sees B's history")
		}
	}
	if n := queryString(t, desk.pool, `SELECT count(*)::text FROM ai_providers WHERE label='B key'`); n != "1" {
		t.Fatal("B's provider changed")
	}
}

func TestAIKeysNeverLeaveTheServer(t *testing.T) {
	desk := newAIDesk(t, nil)
	if response := request(t, desk.api, http.MethodGet, "/api/v1/ai", nil, desk.adminA, nil); strings.Contains(response.Body.String(), aiCanaryKey) {
		t.Fatal("the settings return the key")
	}
	exported := request(t, desk.api, http.MethodGet, "/api/v1/export", nil, desk.adminA, nil)
	if exported.Code != http.StatusOK {
		t.Fatalf("export: %d %s", exported.Code, exported.Body.String())
	}
	files := unzip(t, exported.Body.Bytes())
	if strings.Contains(string(unzipAll(t, exported.Body.Bytes())), aiCanaryKey) {
		t.Fatal("the export holds the key")
	}
	if !strings.Contains(string(files["ai-providers.ndjson"]), `"key_hint":"5k9q"`) || strings.Contains(string(files["ai-providers.ndjson"]), "B key") ||
		!strings.Contains(string(files["ai-usage.ndjson"]), `"feature":"check"`) || !strings.Contains(string(files["ai-settings-events.ndjson"]), "provider_added") {
		t.Fatalf("export AI files: %s / %s", files["ai-providers.ndjson"], files["ai-usage.ndjson"])
	}
	if n := queryString(t, desk.pool, `SELECT count(*)::text FROM ai_providers WHERE position($1::bytea in sealed_key) > 0`, []byte(aiCanaryKey)); n != "0" {
		t.Fatal("the key is stored in clear")
	}
	// A token below admin is refused before the key could be filed in an agent request.
	draft := issueToken(t, desk.api, desk.adminA, "drafts", "draft").Token
	wantResponse(t, request(t, desk.api, http.MethodPut, "/api/v1/ai/providers/"+desk.providerA+"/key", map[string]any{"api_key": aiCanaryKey + "2"}, draft, nil),
		http.StatusForbidden, "credentials_need_admin_token")
	if n := queryString(t, desk.pool, `SELECT count(*)::text FROM agent_requests WHERE body::text LIKE '%'||$1||'%'`, aiCanaryKey); n != "0" {
		t.Fatal("an agent request holds the key")
	}
}

func TestAgentsReadAISettingsAndAdminsChangeThem(t *testing.T) {
	desk := newAIDesk(t, nil)
	agent := joinAsAgent(t, desk.api, desk.adminA, "agent@ai-a.example.com")
	if response := request(t, desk.api, http.MethodGet, "/api/v1/ai", nil, agent, nil); response.Code != http.StatusOK {
		t.Fatalf("agent reads settings: %d %s", response.Code, response.Body.String())
	}
	wantResponse(t, request(t, desk.api, http.MethodPut, "/api/v1/ai/slots/draft", map[string]any{"source": "provider", "provider_id": desk.providerA, "enabled": true}, agent, nil), http.StatusForbidden, "forbidden")
	wantResponse(t, request(t, desk.api, http.MethodPost, "/api/v1/ai/providers/"+desk.providerA+"/check", nil, agent, nil), http.StatusForbidden, "forbidden")
}

func TestTurningAFeatureOnWaitsForAPersonWhenAnAgentAsks(t *testing.T) {
	desk := newAIDesk(t, nil)
	admin := issueToken(t, desk.api, desk.adminA, "ai admin", "admin").Token
	response := request(t, desk.api, http.MethodPut, "/api/v1/ai/slots/draft", map[string]any{"source": "provider", "provider_id": desk.providerA, "enabled": true}, admin, nil)
	if response.Code != http.StatusAccepted {
		t.Fatalf("admin token turning a feature on: %d %s, want 202 (an agent request)", response.Code, response.Body.String())
	}
	if n := queryString(t, desk.pool, `SELECT count(*)::text FROM ai_slots`); n != "0" {
		t.Fatal("the feature was turned on without a person")
	}
}

func TestAICheckFailureSaysWhyAndSavesNothing(t *testing.T) {
	desk := newAIDesk(t, nil)
	desk.fake.Script(aiprovider.FakeReply{Err: &aiprovider.Error{Code: aiprovider.ErrorAuth, Status: 401}})
	response := request(t, desk.api, http.MethodPost, "/api/v1/ai/providers", map[string]any{"kind": "anthropic", "label": "Wrong", "api_key": "sk-wrong-0000", "models": []string{"m"}}, desk.adminA, nil)
	wantResponse(t, response, http.StatusUnprocessableEntity, "ai_check_failed")
	if !strings.Contains(response.Body.String(), `"check_error_code":"auth"`) {
		t.Fatalf("body = %s", response.Body.String())
	}
	if n := queryString(t, desk.pool, `SELECT count(*)::text FROM ai_providers WHERE label='Wrong'`); n != "0" {
		t.Fatal("a provider that failed its check was saved")
	}
	private := request(t, desk.api, http.MethodPost, "/api/v1/ai/providers", map[string]any{"kind": "openai_compatible", "label": "Office", "base_url": "https://10.0.0.12/v1", "api_key": "sk-1", "models": []string{"m"}}, desk.adminA, nil)
	wantResponse(t, private, http.StatusUnprocessableEntity, "ai_check_failed")
	if !strings.Contains(private.Body.String(), "blocked_address") || !strings.Contains(private.Body.String(), "private network") {
		t.Fatalf("private URL body = %s", private.Body.String())
	}
}

func TestNoKeyRingRefusesAKeyAndSaysWhy(t *testing.T) {
	fake := &aiprovider.Fake{}
	api, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.AI = fakeAIConfig(fake) })
	admin := createSession(t, api, service, "No Keys", "admin@nokeys.example.com")
	wantResponse(t, request(t, api, http.MethodPost, "/api/v1/ai/providers", map[string]any{"kind": "anthropic", "label": "K", "api_key": "sk-1", "models": []string{"m"}}, admin, nil),
		http.StatusConflict, "secret_keys_missing")
	if len(fake.Requests()) != 0 {
		t.Fatal("a check was spent on a key that cannot be stored")
	}
}

func TestConfigSaysWhetherTheServerHasAnAIProvider(t *testing.T) {
	desk := newAIDesk(t, func(config *httpapi.Config) {
		config.AI.Server = &ai.ServerProvider{Kind: aiprovider.KindAnthropic, Label: "Colliedesk AI", APIKey: "server-secret", Models: map[string][]string{ai.SlotDraft: {"claude-sonnet-5-5"}}}
	})
	response := request(t, desk.api, http.MethodGet, "/api/v1/config", nil, "", nil)
	if !strings.Contains(response.Body.String(), `"ai":{"server_provider":true}`) {
		t.Fatalf("config = %s", response.Body.String())
	}
	settings := request(t, desk.api, http.MethodGet, "/api/v1/ai", nil, desk.adminA, nil)
	if strings.Contains(settings.Body.String(), "server-secret") || !strings.Contains(settings.Body.String(), "Colliedesk AI") {
		t.Fatalf("settings = %s", settings.Body.String())
	}
	slot := request(t, desk.api, http.MethodPut, "/api/v1/ai/slots/draft", map[string]any{"source": "server", "enabled": true}, desk.adminA, nil)
	if slot.Code != http.StatusOK || !strings.Contains(slot.Body.String(), `"active":true`) {
		t.Fatalf("server slot: %d %s", slot.Code, slot.Body.String())
	}
	wantResponse(t, request(t, desk.api, http.MethodPut, "/api/v1/ai/slots/triage", map[string]any{"source": "server", "enabled": true}, desk.adminA, nil),
		http.StatusConflict, "no_server_provider")
}

func TestABaseURLNeverChangesUnderAStoredKey(t *testing.T) {
	desk := newAIDesk(t, nil)
	router := create(t, desk.api, desk.adminA, "/api/v1/ai/providers", map[string]any{"kind": "openai_compatible", "label": "Router", "base_url": "https://api.good.example/v1", "api_key": aiCanaryKey, "models": []string{"m"}})
	admin := issueToken(t, desk.api, desk.adminA, "admin", "admin").Token
	for _, token := range []string{desk.adminA, admin} {
		response := request(t, desk.api, http.MethodPatch, "/api/v1/ai/providers/"+router, map[string]any{"base_url": "https://collector.evil.example/v1"}, token, nil)
		wantResponse(t, response, http.StatusBadRequest, "invalid_request")
	}
	if n := queryString(t, desk.pool, `SELECT count(*)::text FROM ai_providers WHERE base_url='https://api.good.example/v1'`); n != "1" {
		t.Fatal("the base URL changed")
	}
}

func TestAIUsageAndAutomaticLimitsStayInTheirWorkspace(t *testing.T) {
	desk := newAIDesk(t, nil)
	var usage ai.Usage
	decode(t, request(t, desk.api, http.MethodGet, "/api/v1/ai/usage", nil, desk.adminA, nil), &usage)
	if len(usage.Rows) != 1 || usage.Rows[0].ProviderLabel != "Team key" || usage.Rows[0].Feature != "check" || usage.Rows[0].Calls != 1 || usage.Rows[0].CostMicroUSD != nil {
		t.Fatalf("A's usage = %+v", usage)
	}
	// A call still running is neither done nor failed.
	if _, err := desk.pool.Exec(context.Background(), `INSERT INTO ai_usage(workspace_id,feature,billed_to,provider_id,provider_kind,provider_label,model,status,started_at)
		SELECT workspace_id,feature,billed_to,provider_id,provider_kind,provider_label,model,'started',now() FROM ai_usage WHERE workspace_id=$1 LIMIT 1`, desk.workspaceA); err != nil {
		t.Fatal(err)
	}
	decode(t, request(t, desk.api, http.MethodGet, "/api/v1/ai/usage", nil, desk.adminA, nil), &usage)
	if usage.Rows[0].Calls != 2 || usage.Rows[0].Failed != 0 {
		t.Fatalf("A's usage with a call running = %+v", usage.Rows[0])
	}
	wantResponse(t, request(t, desk.api, http.MethodGet, "/api/v1/ai/usage?month=October", nil, desk.adminA, nil), http.StatusBadRequest, "invalid_request")
	agent := joinAsAgent(t, desk.api, desk.adminA, "agent@ai-a.example.com")
	wantResponse(t, request(t, desk.api, http.MethodGet, "/api/v1/ai/usage", nil, agent, nil), http.StatusForbidden, "forbidden")
	wantResponse(t, request(t, desk.api, http.MethodPut, "/api/v1/ai/automatic-limits", map[string]any{"per_day": 5, "per_minute": 1}, agent, nil), http.StatusForbidden, "forbidden")

	limits := request(t, desk.api, http.MethodPut, "/api/v1/ai/automatic-limits", map[string]any{"per_day": 5, "per_minute": 1}, desk.adminA, nil)
	if limits.Code != http.StatusOK || !strings.Contains(limits.Body.String(), `"per_day":5`) {
		t.Fatalf("set limits: %d %s", limits.Code, limits.Body.String())
	}
	wantResponse(t, request(t, desk.api, http.MethodPut, "/api/v1/ai/automatic-limits", map[string]any{"per_day": -1, "per_minute": 1}, desk.adminA, nil), http.StatusBadRequest, "invalid_request")
	var settingsB ai.Settings
	decode(t, request(t, desk.api, http.MethodGet, "/api/v1/ai", nil, desk.adminB, nil), &settingsB)
	if settingsB.Automatic.PerDay != ai.DefaultAutomaticPerDay {
		t.Fatalf("B's limits changed with A's: %+v", settingsB.Automatic)
	}
}

func TestAIModelsAreListedBeforeAProviderIsSavedAndSaveNothing(t *testing.T) {
	desk := newAIDesk(t, nil)
	desk.fake.FakeModels = []string{"claude-sonnet-5-5", "claude-haiku-5-5"}
	var list struct {
		Models []string `json:"models"`
	}
	response := request(t, desk.api, http.MethodPost, "/api/v1/ai/models", map[string]any{"kind": "anthropic", "api_key": "sk-new-5555"}, desk.adminA, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list: %d %s", response.Code, response.Body.String())
	}
	decode(t, response, &list)
	if strings.Join(list.Models, ",") != "claude-sonnet-5-5,claude-haiku-5-5" {
		t.Fatalf("models = %v", list.Models)
	}
	if n := queryString(t, desk.pool, `SELECT count(*)::text FROM ai_providers`); n != "2" {
		t.Fatalf("listing models saved a provider: %s providers", n)
	}
	if len(desk.fake.Requests()) != 2 {
		t.Fatalf("listing models sent %d model requests, want only the 2 checks of the fixture", len(desk.fake.Requests()))
	}
	// A server that keeps no list says to type the names.
	desk.fake.ModelsErr = &aiprovider.Error{Code: aiprovider.ErrorModelNotFound, Status: 404}
	none := request(t, desk.api, http.MethodPost, "/api/v1/ai/models", map[string]any{"kind": "openai_compatible", "base_url": "https://llm.example.com/v1", "api_key": "sk-1"}, desk.adminA, nil)
	wantResponse(t, none, http.StatusUnprocessableEntity, "ai_check_failed")
	if !strings.Contains(none.Body.String(), `"check_error_code":"model_not_found"`) || !strings.Contains(none.Body.String(), "Type the model names") {
		t.Fatalf("no list body = %s", none.Body.String())
	}
	// The same input rules as adding a provider.
	wantResponse(t, request(t, desk.api, http.MethodPost, "/api/v1/ai/models", map[string]any{"kind": "anthropic", "base_url": "https://proxy.example.com", "api_key": "sk-1"}, desk.adminA, nil),
		http.StatusBadRequest, "invalid_request")
	wantResponse(t, request(t, desk.api, http.MethodPost, "/api/v1/ai/models", map[string]any{"kind": "anthropic"}, desk.adminA, nil),
		http.StatusBadRequest, "invalid_request")
}

func TestAIModelListingSpendsOnlyItsOwnWorkspaceChecks(t *testing.T) {
	desk := newAIDesk(t, nil)
	desk.fake.FakeModels = []string{"claude-sonnet-5-5"}
	list := func(token string) *httptest.ResponseRecorder {
		return request(t, desk.api, http.MethodPost, "/api/v1/ai/models", map[string]any{"kind": "anthropic", "api_key": "sk-new-6666"}, token, nil)
	}
	// The fixture spent one check per workspace adding its provider.
	for range 9 {
		if response := list(desk.adminA); response.Code != http.StatusOK {
			t.Fatalf("list in A: %d %s", response.Code, response.Body.String())
		}
	}
	wantResponse(t, list(desk.adminA), http.StatusTooManyRequests, "ai_checks_rate_limited")
	if response := list(desk.adminB); response.Code != http.StatusOK {
		t.Fatalf("workspace B is held by A's checks: %d %s", response.Code, response.Body.String())
	}
	if n := queryString(t, desk.pool, `SELECT count(*)::text FROM ai_providers`); n != "2" {
		t.Fatalf("listing models saved a provider: %s providers", n)
	}
}
