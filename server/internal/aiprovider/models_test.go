package aiprovider

import (
	"context"
	"errors"
	"net/http"
	"reflect"
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
