package aiprovider

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestAnthropicListsTheModelsTheKeyMayUse(t *testing.T) {
	fake := newFakeAPI(t, http.StatusOK, `{"data":[{"type":"model","id":"claude-sonnet-5-5","display_name":"Claude Sonnet 5.5","created_at":"2026-08-01T00:00:00Z"},`+
		`{"type":"model","id":"claude-haiku-5-5","display_name":"Claude Haiku 5.5","created_at":"2026-07-01T00:00:00Z"}],"has_more":false,"first_id":"claude-sonnet-5-5","last_id":"claude-haiku-5-5"}`)
	provider := newTestAnthropic(t, fake, false)
	models, err := provider.(ModelLister).Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"claude-sonnet-5-5", "claude-haiku-5-5"}; !reflect.DeepEqual(models, want) {
		t.Fatalf("models = %v, want %v (newest first, as the API lists them)", models, want)
	}
	hits := fake.recorded(t)
	if len(hits) != 1 || hits[0].path != "/v1/models" || hits[0].header.Get("X-Api-Key") != secretKey {
		t.Fatalf("request = %+v, want one GET /v1/models with the key", hits)
	}
}

func TestAnthropicModelListRefusedKeyIsAnAuthError(t *testing.T) {
	fake := newFakeAPI(t, http.StatusUnauthorized, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`)
	_, err := newTestAnthropic(t, fake, false).(ModelLister).Models(context.Background())
	var providerErr *Error
	if !errors.As(err, &providerErr) || providerErr.Code != ErrorAuth {
		t.Fatalf("err = %v, want an auth error", err)
	}
}

func TestOpenAICompatibleListsModelsSortedWithoutDuplicates(t *testing.T) {
	fake := newFakeAPI(t, http.StatusOK, `{"object":"list","data":[{"id":"openai/gpt-5"},{"id":"anthropic/claude-sonnet-5-5"},{"id":"openai/gpt-5"},{"id":" "}]}`)
	provider, err := NewOpenAICompatible(OpenAIConfig{BaseURL: fake.server.URL + "/api/v1/", APIKey: "sk-or", HTTPClient: fake.server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	models, err := provider.(ModelLister).Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"anthropic/claude-sonnet-5-5", "openai/gpt-5"}; !reflect.DeepEqual(models, want) {
		t.Fatalf("models = %v, want %v", models, want)
	}
	hits := fake.recorded(t)
	if len(hits) != 1 || hits[0].path != "/api/v1/models" || hits[0].header.Get("Authorization") != "Bearer sk-or" {
		t.Fatalf("request = %+v, want one GET /api/v1/models with the key", hits)
	}
}

func TestOpenAICompatibleModelListThatIsNotJSONIsAnInvalidResponse(t *testing.T) {
	fake := newFakeAPI(t, http.StatusOK, `<html>proxy</html>`)
	provider, err := NewOpenAICompatible(OpenAIConfig{BaseURL: fake.server.URL + "/v1", HTTPClient: fake.server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.(ModelLister).Models(context.Background())
	var providerErr *Error
	if !errors.As(err, &providerErr) || providerErr.Code != ErrorInvalidResponse {
		t.Fatalf("err = %v, want an invalid response", err)
	}
	if hits := fake.recorded(t); len(hits) != 1 || hits[0].header.Get("Authorization") != "" {
		t.Fatalf("a server without a key gets no Authorization header: %+v", hits)
	}
}

func TestAnthropicListsOnlyModelsThatTakeEffortAndStructuredOutput(t *testing.T) {
	model := func(id, lifecycle string, structured, effort bool) string {
		return `{"type":"model","id":"` + id + `","display_name":"` + id + `","created_at":"2026-08-01T00:00:00Z","lifecycle":"` + lifecycle + `",` +
			`"capabilities":{"structured_outputs":{"supported":` + strconv.FormatBool(structured) + `},` +
			`"effort":{"supported":` + strconv.FormatBool(effort) + `,"low":{"supported":` + strconv.FormatBool(effort) + `},"medium":{"supported":` + strconv.FormatBool(effort) + `},"high":{"supported":` + strconv.FormatBool(effort) + `}}}}`
	}
	fake := newFakeAPI(t, http.StatusOK, `{"data":[`+strings.Join([]string{
		model("claude-sonnet-5-5", "active", true, true),
		model("claude-3-haiku-20240307", "deprecated", false, false),
		model("claude-sonnet-4-20250514", "active", false, true),
		model("claude-opus-4-1", "retired", true, true),
	}, ",")+`],"has_more":false}`)
	models, err := newTestAnthropic(t, fake, false).(ModelLister).Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"claude-sonnet-5-5"}; !reflect.DeepEqual(models, want) {
		t.Fatalf("models = %v, want only %v: Colliedesk always sends effort and a JSON schema", models, want)
	}
}

func TestOpenAIListsOnlyReasoningChatModels(t *testing.T) {
	fake := newFakeAPI(t, http.StatusOK, `{"data":[{"id":"gpt-3.5-turbo"},{"id":"babbage-002"},{"id":"gpt-4o"},{"id":"chatgpt-image-latest"},`+
		`{"id":"gpt-5-mini"},{"id":"gpt-5-mini-2025-08-07"},{"id":"gpt-5-chat-latest"},{"id":"gpt-5-codex"},{"id":"gpt-5-pro"},{"id":"gpt-6-astra"},`+
		`{"id":"o4-mini"},{"id":"o1-mini"},{"id":"gpt-realtime"},{"id":"gpt-4o-mini-tts"},{"id":"text-embedding-3-small"},{"id":"omni-moderation-latest"},`+
		`{"id":"gpt-5-search-api"},{"id":"o4-mini-deep-research"},{"id":"gpt-image-1"},{"id":"gpt-audio"}]}`)
	transport := &rewriteTransport{target: fake.server.URL, next: fake.server.Client().Transport}
	provider, err := NewOpenAICompatible(OpenAIConfig{BaseURL: "https://api.openai.com/v1", APIKey: "sk", HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	models, err := provider.(ModelLister).Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"gpt-5-mini", "gpt-5-mini-2025-08-07", "gpt-6-astra", "o4-mini"}; !reflect.DeepEqual(models, want) {
		t.Fatalf("models = %v, want %v: on the OpenAI API Colliedesk sends reasoning_effort and a JSON schema to chat completions", models, want)
	}
}

func TestOpenRouterListsOnlyModelsThatTakeStructuredOutput(t *testing.T) {
	fake := newFakeAPI(t, http.StatusOK, `{"data":[{"id":"openai/gpt-5","supported_parameters":["max_tokens","response_format","structured_outputs"]},`+
		`{"id":"meta/llama-2","supported_parameters":["max_tokens"]},{"id":"own/model"}]}`)
	provider, err := NewOpenAICompatible(OpenAIConfig{BaseURL: fake.server.URL + "/api/v1", APIKey: "sk-or", HTTPClient: fake.server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	models, err := provider.(ModelLister).Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// A server that says nothing about its parameters keeps its models; one that says it cannot answer
	// with a JSON schema does not.
	if want := []string{"openai/gpt-5", "own/model"}; !reflect.DeepEqual(models, want) {
		t.Fatalf("models = %v, want %v", models, want)
	}
}
