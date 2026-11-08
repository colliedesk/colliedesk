package httpapi_test

import (
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/serverconfig"
)

// The operator console translates what the server says by code (docs/internals/operator-console.md › Message
// codes): beside each English text the API sends its code and params, and a param never holds a secret.

type codedMessage struct {
	Code   string         `json:"code"`
	Params map[string]any `json:"params"`
}

type codedCheck struct {
	OK          bool                    `json:"ok"`
	Errors      map[string]string       `json:"errors"`
	FieldErrors map[string]codedMessage `json:"field_errors"`
	Test        struct {
		Name    string         `json:"name"`
		OK      bool           `json:"ok"`
		Message string         `json:"message"`
		Code    string         `json:"code"`
		Params  map[string]any `json:"params"`
	} `json:"test"`
	Guards []struct {
		Key           string         `json:"key"`
		Preview       string         `json:"preview"`
		PreviewCode   string         `json:"preview_code"`
		PreviewParams map[string]any `json:"preview_params"`
		Refused       string         `json:"refused"`
		RefusedCode   string         `json:"refused_code"`
		RefusedParams map[string]any `json:"refused_params"`
	} `json:"guards"`
}

type codedRefusal struct {
	Error struct {
		Code        string                  `json:"code"`
		Message     string                  `json:"message"`
		MessageCode string                  `json:"message_code"`
		Params      map[string]any          `json:"params"`
		Fields      map[string]string       `json:"fields"`
		FieldErrors map[string]codedMessage `json:"field_errors"`
	} `json:"error"`
}

// checkCoded posts a check and fails when the answer repeats canary, the secret the change carries.
func checkCoded(t *testing.T, e *operatorEnv, cookie *http.Cookie, body map[string]any, canary string) codedCheck {
	t.Helper()
	response := operatorRequest(t, e.api, http.MethodPost, "/api/v1/operator/config/check", body, cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("check: %d %s", response.Code, response.Body.String())
	}
	noSecretIn(t, response, canary)
	var answer codedCheck
	decode(t, response, &answer)
	return answer
}

func noSecretIn(t *testing.T, response *httptest.ResponseRecorder, canary string) {
	t.Helper()
	if canary != "" && strings.Contains(response.Body.String(), canary) {
		t.Fatalf("the answer repeats the secret: %s", response.Body.String())
	}
}

// fieldCode expects the message of key to carry code, under the same keys as errors.
func fieldCode(t *testing.T, answer codedCheck, key, code string) codedMessage {
	t.Helper()
	keys := slices.Sorted(maps.Keys(answer.Errors))
	coded := slices.Sorted(maps.Keys(answer.FieldErrors))
	for _, name := range coded {
		if !slices.Contains(keys, name) {
			t.Fatalf("field_errors has %s, errors does not: %v %v", name, keys, coded)
		}
	}
	if answer.FieldErrors[key].Code != code {
		t.Fatalf("%s: code %q, want %s (errors %v, field_errors %v)", key, answer.FieldErrors[key].Code, code, answer.Errors, answer.FieldErrors)
	}
	return answer.FieldErrors[key]
}

func TestTheServerAIProvidersErrorsAndTestsCarryCodes(t *testing.T) {
	fake := &aiprovider.Fake{}
	e := newOperatorEnvWith(t, false, func(snap serverconfig.Snapshot) error { _, err := ai.ServerProviderFromEnv(snap.Getenv); return err },
		func(config *httpapi.Config) { config.AI.Build = func(ai.Spec) (aiprovider.Provider, error) { return fake, nil } })
	cookie := e.signIn("ops@example.com")
	const key = "sk-ant-canary-never-echoed"
	secrets := map[string]any{"ai.server.api_key": key}

	bogus := checkCoded(t, e, cookie, map[string]any{"set": map[string]any{"ai.server.provider": "bogus"}, "secrets": secrets}, key)
	fieldCode(t, bogus, "ai.server.provider", "ai_server_provider_invalid")

	// A message about several settings carries its code under each, with their names, not variables.
	half := checkCoded(t, e, cookie, map[string]any{"set": map[string]any{"ai.server.provider": "anthropic"}}, "")
	missing := fieldCode(t, half, "ai.server.api_key", "ai_server_half_configured")
	if missing.Params["missing"] != "ai.server.api_key, ai.server.models" || half.FieldErrors["ai.server.models"].Code != "ai_server_half_configured" {
		t.Fatalf("half configured: %+v", half)
	}

	twice := checkCoded(t, e, cookie, map[string]any{"set": map[string]any{"ai.server.provider": "anthropic", "ai.server.models": "triage=a,triage=b"}, "secrets": secrets}, key)
	if coded := fieldCode(t, twice, "ai.server.models", "ai_server_models_twice"); coded.Params["slot"] != "triage" {
		t.Fatalf("params: %+v", coded)
	}
	prices := checkCoded(t, e, cookie, map[string]any{"set": map[string]any{"ai.server.provider": "anthropic", "ai.server.models": "triage=a",
		"ai.server.monthly_allowance": "seat=3"}, "secrets": secrets}, key)
	if coded := fieldCode(t, prices, "ai.server.monthly_allowance", "ai_server_allowance_unknown"); coded.Params["key"] != "seat" {
		t.Fatalf("params: %+v", coded)
	}

	// A save refused for the same reason carries the same codes.
	refused := e.patch(cookie, map[string]any{"base_version": e.version(cookie), "set": map[string]any{"ai.server.provider": "bogus"}})
	var body codedRefusal
	decode(t, refused, &body)
	if refused.Code != http.StatusUnprocessableEntity || body.Error.FieldErrors["ai.server.provider"].Code != "ai_server_provider_invalid" || body.Error.Fields["ai.server.provider"] == "" {
		t.Fatalf("save: %d %s", refused.Code, refused.Body.String())
	}

	// Live tests: the answer, the refusal (the provider's words as detail, never the key) and the models.
	ok := map[string]any{"set": map[string]any{"ai.server.provider": "anthropic", "ai.server.models": "triage=claude-haiku-5-5"}, "secrets": secrets, "test": "ai"}
	fake.Script(aiprovider.FakeReply{Response: aiprovider.Response{Text: "OK"}})
	if answer := checkCoded(t, e, cookie, ok, key); answer.Test.Code != "test_ai_ok" || answer.Test.Params["model"] != "claude-haiku-5-5" {
		t.Fatalf("ai test: %+v", answer.Test)
	}
	fake.Script(aiprovider.FakeReply{Err: errors.New("401 invalid x-api-key " + key)})
	if answer := checkCoded(t, e, cookie, ok, key); answer.Test.Code != "test_ai_refused" || !strings.Contains(answer.Test.Params["detail"].(string), "401") {
		t.Fatalf("ai refused: %+v", answer.Test)
	}
	if answer := checkCoded(t, e, cookie, map[string]any{"test": "ai_models"}, ""); answer.Test.Code != "test_ai_models_needs_provider" || answer.Test.Message != "Pick a provider first." {
		t.Fatalf("models without a provider: %+v", answer.Test)
	}
	fake.FakeModels = []string{"claude-haiku-5-5"}
	listed := checkCoded(t, e, cookie, map[string]any{"set": map[string]any{"ai.server.provider": "anthropic"}, "secrets": secrets, "test": "ai_models"}, key)
	if listed.Test.Code != "test_ai_models_ok" || listed.Test.Params["count"] != float64(1) || listed.Test.Message != "The provider offers 1 model. Nothing was saved." {
		t.Fatalf("models: %+v", listed.Test)
	}
}
