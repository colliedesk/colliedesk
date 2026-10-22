package aiprovider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeAPI stands in for a provider's HTTP API, an external boundary. It records every request.
type fakeAPI struct {
	server *httptest.Server
	mu     sync.Mutex
	hits   []recorded
	status int
	header http.Header
	reply  string
	delay  time.Duration
}

type recorded struct {
	path   string
	header http.Header
	body   map[string]any
}

func newFakeAPI(t *testing.T, status int, reply string) *fakeAPI {
	t.Helper()
	fake := &fakeAPI{status: status, reply: reply, header: http.Header{}}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(data, &body)
		fake.mu.Lock()
		fake.hits = append(fake.hits, recorded{path: r.URL.Path, header: r.Header.Clone(), body: body})
		status, reply, delay := fake.status, fake.reply, fake.delay
		for key, values := range fake.header {
			w.Header()[key] = values
		}
		fake.mu.Unlock()
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeAPI) recorded(t *testing.T) []recorded {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recorded(nil), f.hits...)
}

const secretKey = "sk-ant-secret-key-1234"

const claudeReply = `{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-5-5",
 "content":[{"type":"text","text":"{\"ok\":true}"}],"stop_reason":"end_turn",
 "usage":{"input_tokens":12,"output_tokens":3,"cache_read_input_tokens":4,"cache_creation_input_tokens":2}}`

func newTestAnthropic(t *testing.T, fake *fakeAPI, fallbacks bool) Provider {
	t.Helper()
	provider, err := NewAnthropic(AnthropicConfig{APIKey: secretKey, BaseURL: fake.server.URL, HTTPClient: fake.server.Client(), forceFallbacks: fallbacks})
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func draftRequest() Request {
	return Request{
		Model: "claude-sonnet-5-5", Effort: EffortLow, System: "You draft replies.",
		Messages:  []Message{{Role: RoleUser, Text: "customer text: my order is late"}},
		MaxOutput: 1024,
		Schema:    map[string]any{"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}}},
	}
}

func TestAnthropicSendsOnlyItsOwnCredentialAndIgnoresTheHostEnvironment(t *testing.T) {
	// The SDK reads credentials in order and stops at the first one, so each set reaches a
	// different source: an env key, a bearer token, then custom headers and profiles.
	for name, env := range map[string]map[string]string{
		"api key":      {"ANTHROPIC_API_KEY": "sk-ant-operator-env-key"},
		"bearer token": {"ANTHROPIC_AUTH_TOKEN": "env-bearer-token"},
		"headers":      {"ANTHROPIC_CUSTOM_HEADERS": "X-Stray: yes", "ANTHROPIC_PROFILE": "someone"},
	} {
		t.Run(name, func(t *testing.T) {
			stray := newFakeAPI(t, http.StatusOK, claudeReply)
			t.Setenv("ANTHROPIC_BASE_URL", stray.server.URL)
			for key, value := range env {
				t.Setenv(key, value)
			}
			fake := newFakeAPI(t, http.StatusOK, claudeReply)
			response, err := newTestAnthropic(t, fake, false).Complete(context.Background(), draftRequest())
			if err != nil {
				t.Fatal(err)
			}
			hits := fake.recorded(t)
			if len(hits) != 1 || len(stray.recorded(t)) != 0 {
				t.Fatalf("hits: configured %d, env base URL %d; want 1 and 0", len(hits), len(stray.recorded(t)))
			}
			hit := hits[0]
			if hit.path != "/v1/messages" {
				t.Fatalf("path = %q", hit.path)
			}
			if got := hit.header.Get("X-Api-Key"); got != secretKey {
				t.Fatalf("x-api-key = %q, want the configured key", got)
			}
			if got := hit.header.Get("Authorization"); got != "" {
				t.Fatalf("Authorization = %q, want none", got)
			}
			if got := hit.header.Get("X-Stray"); got != "" {
				t.Fatalf("custom header from the environment was sent: %q", got)
			}
			if response.JSON != `{"ok":true}` || response.Stop != StopDone || response.Model != "claude-sonnet-5-5" {
				t.Fatalf("response = %+v", response)
			}
			want := Usage{Reported: true, InputTokens: 12, OutputTokens: 3, CacheReadTokens: 4, CacheWriteTokens: 2}
			if response.Usage != want || len(response.Hops) != 1 || response.Hops[0].Usage != want {
				t.Fatalf("usage = %+v hops = %+v", response.Usage, response.Hops)
			}
		})
	}
}

func TestAnthropicRequestShape(t *testing.T) {
	fake := newFakeAPI(t, http.StatusOK, claudeReply)
	if _, err := newTestAnthropic(t, fake, false).Complete(context.Background(), draftRequest()); err != nil {
		t.Fatal(err)
	}
	body := fake.recorded(t)[0].body
	config, _ := body["output_config"].(map[string]any)
	if config["effort"] != "low" {
		t.Fatalf("output_config.effort = %v, want low (always sent)", config["effort"])
	}
	format, _ := config["format"].(map[string]any)
	if format["type"] != "json_schema" || format["schema"] == nil {
		t.Fatalf("output_config.format = %v", format)
	}
	for _, refused := range []string{"temperature", "top_p", "top_k", "thinking", "fallbacks"} {
		if _, ok := body[refused]; ok {
			t.Fatalf("request carries %q: %v", refused, body)
		}
	}
	if body["max_tokens"] != float64(1024) || body["model"] != "claude-sonnet-5-5" {
		t.Fatalf("body = %v", body)
	}
	system, _ := body["system"].([]any)
	if len(system) != 1 || system[0].(map[string]any)["text"] != "You draft replies." {
		t.Fatalf("system = %v", body["system"])
	}
}

func TestAnthropicNeverRetriesSoEveryAttemptCanBeMetered(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusTooManyRequests, 529, http.StatusRequestTimeout} {
		fake := newFakeAPI(t, status, `{"type":"error","error":{"type":"api_error","message":"boom"}}`)
		_, err := newTestAnthropic(t, fake, false).Complete(context.Background(), draftRequest())
		if err == nil {
			t.Fatalf("HTTP %d: no error", status)
		}
		if hits := len(fake.recorded(t)); hits != 1 {
			t.Fatalf("HTTP %d: %d attempts, want exactly 1", status, hits)
		}
	}
}

func TestAnthropicErrorsAreTypedAndLeakNothing(t *testing.T) {
	cases := []struct {
		status    int
		code      ErrorCode
		retryable bool
	}{
		{http.StatusBadRequest, ErrorBadRequest, false},
		{http.StatusUnauthorized, ErrorAuth, false},
		{http.StatusPaymentRequired, ErrorQuotaExhausted, false},
		{http.StatusForbidden, ErrorPermission, false},
		{http.StatusNotFound, ErrorModelNotFound, false},
		{http.StatusRequestEntityTooLarge, ErrorTooLarge, false},
		{http.StatusTooManyRequests, ErrorRateLimited, true},
		{http.StatusInternalServerError, ErrorUnavailable, true},
		{529, ErrorOverloaded, true},
	}
	for _, tc := range cases {
		fake := newFakeAPI(t, tc.status, `{"type":"error","error":{"type":"x","message":"echo customer text: my order is late `+secretKey+`"}}`)
		fake.header.Set("Retry-After", "7")
		fake.header.Set("Request-Id", "req_42")
		_, err := newTestAnthropic(t, fake, false).Complete(context.Background(), draftRequest())
		var providerErr *Error
		if !errors.As(err, &providerErr) {
			t.Fatalf("HTTP %d: error %T %v is not *Error", tc.status, err, err)
		}
		if providerErr.Code != tc.code || providerErr.Retryable() != tc.retryable || providerErr.Status != tc.status {
			t.Fatalf("HTTP %d: got %+v, want code %s retryable %v", tc.status, providerErr, tc.code, tc.retryable)
		}
		if tc.status == http.StatusTooManyRequests && providerErr.RetryAfter != 7*time.Second {
			t.Fatalf("retry-after = %v, want 7s", providerErr.RetryAfter)
		}
		if providerErr.RequestID != "req_42" {
			t.Fatalf("request id = %q", providerErr.RequestID)
		}
		for _, leak := range []string{secretKey, "my order is late", fake.server.URL} {
			if strings.Contains(err.Error(), leak) {
				t.Fatalf("HTTP %d: error %q leaks %q", tc.status, err, leak)
			}
		}
	}
}

func TestAnthropicTimeoutIsTyped(t *testing.T) {
	fake := newFakeAPI(t, http.StatusOK, claudeReply)
	fake.delay = 2 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := newTestAnthropic(t, fake, false).Complete(ctx, draftRequest())
	if CodeOf(err) != ErrorTimeout {
		t.Fatalf("error = %v, want timeout", err)
	}
}

func TestAnthropicRefusalAndMaxTokensAreAnswersNotErrors(t *testing.T) {
	refusal := `{"id":"msg_2","type":"message","role":"assistant","model":"claude-haiku-5-5","content":[],
	 "stop_reason":"refusal","stop_details":{"type":"refusal","category":"cyber"},"usage":{"input_tokens":9,"output_tokens":0}}`
	fake := newFakeAPI(t, http.StatusOK, refusal)
	response, err := newTestAnthropic(t, fake, false).Complete(context.Background(), draftRequest())
	if err != nil || response.Stop != StopRefused || response.JSON != "" || !response.Usage.Reported || response.Usage.InputTokens != 9 {
		t.Fatalf("refusal: %+v %v", response, err)
	}

	truncated := `{"id":"msg_3","type":"message","role":"assistant","model":"claude-sonnet-5-5",
	 "content":[{"type":"text","text":"{\"ok\":tr"}],"stop_reason":"max_tokens","usage":{"input_tokens":9,"output_tokens":256}}`
	fake = newFakeAPI(t, http.StatusOK, truncated)
	response, err = newTestAnthropic(t, fake, false).Complete(context.Background(), draftRequest())
	if err != nil || response.Stop != StopMaxTokens || response.JSON != "" {
		t.Fatalf("max_tokens: %+v %v", response, err)
	}
}

func TestAnthropicFallbackBillsEachModelThatRan(t *testing.T) {
	reply := `{"id":"msg_4","type":"message","role":"assistant","model":"claude-opus-4-8",
	 "content":[{"type":"fallback","from":{"model":"claude-sonnet-5-5"},"to":{"model":"claude-opus-4-8"}},{"type":"text","text":"{\"ok\":true}"}],
	 "stop_reason":"end_turn",
	 "usage":{"input_tokens":100,"output_tokens":20,
	  "iterations":[{"type":"message","model":"claude-sonnet-5-5","input_tokens":100,"output_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0},
	               {"type":"fallback_message","model":"claude-opus-4-8","input_tokens":100,"output_tokens":20,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}]}}`
	fake := newFakeAPI(t, http.StatusOK, reply)
	response, err := newTestAnthropic(t, fake, true).Complete(context.Background(), draftRequest())
	if err != nil {
		t.Fatal(err)
	}
	hit := fake.recorded(t)[0]
	if hit.body["fallbacks"] != "default" || !strings.Contains(hit.header.Get("Anthropic-Beta"), "server-side-fallback-2026-07-01") {
		t.Fatalf("fallbacks = %v, beta = %q", hit.body["fallbacks"], hit.header.Get("Anthropic-Beta"))
	}
	if response.Model != "claude-opus-4-8" || response.JSON != `{"ok":true}` {
		t.Fatalf("response = %+v", response)
	}
	if len(response.Hops) != 2 || response.Hops[0].Model != "claude-sonnet-5-5" || response.Hops[1].Model != "claude-opus-4-8" {
		t.Fatalf("hops = %+v", response.Hops)
	}
	if response.Usage.InputTokens != 200 || response.Usage.OutputTokens != 20 {
		t.Fatalf("usage = %+v, want the sum of both hops", response.Usage)
	}
}

func TestAnthropicFallbacksOnlyForModelsThatHaveThem(t *testing.T) {
	fake := newFakeAPI(t, http.StatusOK, claudeReply)
	request := draftRequest()
	request.Model = "claude-haiku-5-5"
	if _, err := newTestAnthropic(t, fake, true).Complete(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if hit := fake.recorded(t)[0]; hit.body["fallbacks"] != nil || hit.header.Get("Anthropic-Beta") != "" {
		t.Fatalf("Haiku request carries fallbacks: %v %q", hit.body["fallbacks"], hit.header.Get("Anthropic-Beta"))
	}
}

func TestRequestsProvidersRefuseAreNeverSent(t *testing.T) {
	fake := newFakeAPI(t, http.StatusOK, claudeReply)
	provider := newTestAnthropic(t, fake, false)
	bad := []Request{
		{Effort: EffortLow, Messages: []Message{{Role: RoleUser, Text: "x"}}, MaxOutput: 10},
		{Model: "m", Effort: "max", Messages: []Message{{Role: RoleUser, Text: "x"}}, MaxOutput: 10},
		{Model: "m", Effort: EffortLow, Messages: []Message{{Role: RoleUser, Text: "x"}, {Role: RoleAssistant, Text: "prefill"}}, MaxOutput: 10},
		{Model: "m", Effort: EffortLow, Messages: []Message{{Role: RoleUser, Text: "x"}}},
		{Model: "m", Effort: EffortLow, Messages: []Message{{Role: "system", Text: "x"}, {Role: RoleUser, Text: "x"}}, MaxOutput: 10},
	}
	for i, request := range bad {
		if _, err := provider.Complete(context.Background(), request); err == nil {
			t.Fatalf("request %d was accepted", i)
		}
	}
	if hits := len(fake.recorded(t)); hits != 0 {
		t.Fatalf("%d invalid requests reached the provider", hits)
	}
}

func TestAnthropicCheckUsesTheRealRequestShape(t *testing.T) {
	fake := newFakeAPI(t, http.StatusOK, claudeReply)
	response, err := newTestAnthropic(t, fake, false).Check(context.Background(), "claude-haiku-5-5")
	if err != nil || !response.Usage.Reported {
		t.Fatalf("check: %+v %v", response, err)
	}
	body := fake.recorded(t)[0].body
	config, _ := body["output_config"].(map[string]any)
	if body["model"] != "claude-haiku-5-5" || config["effort"] != "low" || config["format"] == nil {
		t.Fatalf("check body = %v", body)
	}
}
