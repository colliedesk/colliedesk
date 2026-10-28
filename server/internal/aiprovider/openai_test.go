package aiprovider

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

const openAIReplyText = `{"id":"chatcmpl-1","model":"gpt-test","choices":[{"index":0,
 "message":{"role":"assistant","content":"{\"ok\":true}"},"finish_reason":"stop"}],
 "usage":{"prompt_tokens":30,"completion_tokens":7,"prompt_tokens_details":{"cached_tokens":10}}}`

func newTestOpenAI(t *testing.T, fake *fakeAPI, apiKey string) Provider {
	t.Helper()
	provider, err := NewOpenAICompatible(OpenAIConfig{BaseURL: fake.server.URL + "/v1/", APIKey: apiKey, HTTPClient: fake.server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func TestOpenAICompatibleRequestAndAnswer(t *testing.T) {
	fake := newFakeAPI(t, http.StatusOK, openAIReplyText)
	response, err := newTestOpenAI(t, fake, secretKey).Complete(context.Background(), draftRequest())
	if err != nil {
		t.Fatal(err)
	}
	hit := fake.recorded(t)[0]
	if hit.path != "/v1/chat/completions" || hit.header.Get("Authorization") != "Bearer "+secretKey {
		t.Fatalf("path %q auth %q", hit.path, hit.header.Get("Authorization"))
	}
	messages, _ := hit.body["messages"].([]any)
	if len(messages) != 2 || messages[0].(map[string]any)["role"] != "system" || messages[1].(map[string]any)["content"] != "customer text: my order is late" {
		t.Fatalf("messages = %v", messages)
	}
	format, _ := hit.body["response_format"].(map[string]any)
	if format["type"] != "json_schema" {
		t.Fatalf("response_format = %v", format)
	}
	// Not api.openai.com: max_tokens, and no reasoning field the server may refuse.
	if hit.body["max_tokens"] != float64(1024) || hit.body["reasoning_effort"] != nil || hit.body["max_completion_tokens"] != nil || hit.body["provider"] != nil {
		t.Fatalf("body = %v", hit.body)
	}
	for _, refused := range []string{"temperature", "top_p"} {
		if _, ok := hit.body[refused]; ok {
			t.Fatalf("request carries %q", refused)
		}
	}
	want := Usage{Reported: true, InputTokens: 20, OutputTokens: 7, CacheReadTokens: 10}
	if response.JSON != `{"ok":true}` || response.Stop != StopDone || response.Model != "gpt-test" || response.Usage != want {
		t.Fatalf("response = %+v", response)
	}
}

func TestOpenAIHostGetsItsOwnParameterNames(t *testing.T) {
	fake := newFakeAPI(t, http.StatusOK, openAIReplyText)
	transport := &rewriteTransport{target: fake.server.URL, next: fake.server.Client().Transport}
	provider, err := NewOpenAICompatible(OpenAIConfig{BaseURL: "https://api.openai.com/v1", APIKey: "k", HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Complete(context.Background(), draftRequest()); err != nil {
		t.Fatal(err)
	}
	body := fake.recorded(t)[0].body
	if body["max_completion_tokens"] != float64(1024) || body["reasoning_effort"] != "low" || body["max_tokens"] != nil {
		t.Fatalf("body = %v", body)
	}
}

func TestOpenRouterKeepsAJSONSchemaOnProvidersThatTakeIt(t *testing.T) {
	fake := newFakeAPI(t, http.StatusOK, openAIReplyText)
	transport := &rewriteTransport{target: fake.server.URL, next: fake.server.Client().Transport}
	provider, err := NewOpenAICompatible(OpenAIConfig{BaseURL: "https://openrouter.ai/api/v1", APIKey: "k", HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Complete(context.Background(), draftRequest()); err != nil {
		t.Fatal(err)
	}
	body := fake.recorded(t)[0].body
	routing, _ := body["provider"].(map[string]any)
	if routing["require_parameters"] != true || body["max_tokens"] != float64(1024) || body["reasoning_effort"] != nil {
		t.Fatalf("body = %v", body)
	}
}

// rewriteTransport sends a request meant for a real host to the test server.
type rewriteTransport struct {
	target string
	next   http.RoundTripper
}

func (r *rewriteTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	target, _ := url.Parse(r.target)
	clone := request.Clone(request.Context())
	clone.URL.Scheme, clone.URL.Host, clone.Host = target.Scheme, target.Host, target.Host
	return r.next.RoundTrip(clone)
}

func TestOpenAICompatibleWithoutKeySendsNoAuthorization(t *testing.T) {
	fake := newFakeAPI(t, http.StatusOK, openAIReplyText)
	if _, err := newTestOpenAI(t, fake, "").Complete(context.Background(), draftRequest()); err != nil {
		t.Fatal(err)
	}
	if got := fake.recorded(t)[0].header.Get("Authorization"); got != "" {
		t.Fatalf("Authorization = %q", got)
	}
}

func TestOpenAICompatibleMissingUsageIsUnknownNotZero(t *testing.T) {
	fake := newFakeAPI(t, http.StatusOK, `{"model":"llama3","choices":[{"message":{"content":"hi"},"finish_reason":"stop"}]}`)
	request := draftRequest()
	request.Schema = nil
	response, err := newTestOpenAI(t, fake, "").Complete(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if response.Usage.Reported || len(response.Hops) != 1 || response.Hops[0].Usage.Reported || response.Text != "hi" || response.JSON != "" {
		t.Fatalf("response = %+v", response)
	}
}

func TestOpenAICompatibleOutOfCreditIsNotARateLimit(t *testing.T) {
	fake := newFakeAPI(t, http.StatusTooManyRequests, `{"error":{"message":"You exceeded your current quota","type":"insufficient_quota","code":"insufficient_quota"}}`)
	fake.header.Set("Retry-After", "3")
	_, err := newTestOpenAI(t, fake, secretKey).Complete(context.Background(), draftRequest())
	var providerErr *Error
	if !errors.As(err, &providerErr) || providerErr.Code != ErrorQuotaExhausted || providerErr.Retryable() {
		t.Fatalf("error = %+v", err)
	}

	fake = newFakeAPI(t, http.StatusTooManyRequests, `{"error":{"message":"slow down","type":"requests","code":"rate_limit_exceeded"}}`)
	fake.header.Set("Retry-After", "3")
	_, err = newTestOpenAI(t, fake, secretKey).Complete(context.Background(), draftRequest())
	if !errors.As(err, &providerErr) || providerErr.Code != ErrorRateLimited || !providerErr.Retryable() {
		t.Fatalf("error = %+v", err)
	}
	if strings.Contains(err.Error(), "slow down") || strings.Contains(err.Error(), secretKey) {
		t.Fatalf("error leaks the body or key: %q", err)
	}
}

func TestOpenAICompatibleAnswersThatAreNotAnswers(t *testing.T) {
	for name, reply := range map[string]string{
		"not json":   `<html>proxy error</html>`,
		"no choices": `{"choices":[]}`,
		"too large":  `{"choices":[{"message":{"content":"` + strings.Repeat("a", maxResponseBytes) + `"}}]}`,
	} {
		fake := newFakeAPI(t, http.StatusOK, reply)
		_, err := newTestOpenAI(t, fake, "").Complete(context.Background(), draftRequest())
		if CodeOf(err) != ErrorInvalidResponse {
			t.Fatalf("%s: error = %v", name, err)
		}
	}
}

func TestOpenAICompatibleStopReasons(t *testing.T) {
	for reply, want := range map[string]StopReason{
		`{"choices":[{"message":{"content":"x"},"finish_reason":"length"}]}`:               StopMaxTokens,
		`{"choices":[{"message":{"content":null,"refusal":"no"},"finish_reason":"stop"}]}`: StopRefused,
		`{"choices":[{"message":{"content":""},"finish_reason":"content_filter"}]}`:        StopRefused,
		`{"choices":[{"message":{"content":"x"},"finish_reason":"tool_calls"}]}`:           StopOther,
	} {
		fake := newFakeAPI(t, http.StatusOK, reply)
		response, err := newTestOpenAI(t, fake, "").Complete(context.Background(), draftRequest())
		if err != nil || response.Stop != want || response.JSON != "" {
			t.Fatalf("%s: %+v %v, want stop %s", reply, response, err, want)
		}
	}
}

func TestOpenAICompatibleBaseURL(t *testing.T) {
	for _, bad := range []string{"", "ftp://x/v1", "https://", "https://user:pass@x/v1", "https://x/v1?key=1", "not a url"} {
		if _, err := NewOpenAICompatible(OpenAIConfig{BaseURL: bad}); err == nil {
			t.Fatalf("base URL %q accepted", bad)
		}
	}
}

func TestOpenAICompatibleNeverRetries(t *testing.T) {
	fake := newFakeAPI(t, http.StatusBadGateway, `bad gateway`)
	if _, err := newTestOpenAI(t, fake, "").Complete(context.Background(), draftRequest()); CodeOf(err) != ErrorUnavailable {
		t.Fatalf("error = %v", err)
	}
	if hits := len(fake.recorded(t)); hits != 1 {
		t.Fatalf("%d attempts, want 1", hits)
	}
}
