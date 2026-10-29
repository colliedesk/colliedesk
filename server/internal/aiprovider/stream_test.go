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
)

// sseAPI stands in for a provider's streaming endpoint: it answers each path with a fixed body and
// records the requests.
type sseAPI struct {
	server  *httptest.Server
	mu      sync.Mutex
	bodies  map[string][]map[string]any
	replies map[string]string
	types   map[string]string
}

func newSSEAPI(t *testing.T) *sseAPI {
	t.Helper()
	api := &sseAPI{bodies: map[string][]map[string]any{}, replies: map[string]string{}, types: map[string]string{}}
	api.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(data, &body)
		api.mu.Lock()
		api.bodies[r.URL.Path] = append(api.bodies[r.URL.Path], body)
		reply, kind := api.replies[r.URL.Path], api.types[r.URL.Path]
		api.mu.Unlock()
		if kind == "" {
			kind = "text/event-stream"
		}
		w.Header().Set("Content-Type", kind)
		w.Header().Set("Request-Id", "req_stream")
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(api.server.Close)
	return api
}

func (a *sseAPI) answer(path, kind, body string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.replies[path], a.types[path] = body, kind
}

func (a *sseAPI) sent(t *testing.T, path string) []map[string]any {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.bodies[path]
}

func sse(events ...string) string {
	var out strings.Builder
	for _, event := range events {
		var head struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal([]byte(event), &head)
		out.WriteString("event: " + head.Type + "\ndata: " + event + "\n\n")
	}
	return out.String()
}

const claudeStreamStart = `{"type":"message_start","message":{"id":"msg_s","type":"message","role":"assistant","model":"claude-haiku-5-5","content":[],"stop_reason":null,"usage":{"input_tokens":40,"output_tokens":1,"cache_read_input_tokens":3000,"cache_creation_input_tokens":0}}}`

func chatRequest() Request {
	return Request{Model: "claude-haiku-5-5", Effort: EffortLow, MaxOutput: 800, NoThinking: true, CacheHistory: true,
		SystemParts: []SystemPart{{Text: "rules"}, {Text: "workspace"}, {Text: "articles", Cache: true}},
		Messages:    []Message{{Role: RoleUser, Text: "first"}, {Role: RoleAssistant, Text: "answer"}, {Role: RoleUser, Text: "how do I ship?"}}}
}

// A Claude stream hands out its text in order and ends with the usage of the whole answer; the
// request caches the system prefix at the marked block and the history automatically, turns
// thinking off where the model allows it, and asks for no fallback, which would replace text
// already shown.
func TestAnthropicStreamsTextInOrderWithCacheBreakpoints(t *testing.T) {
	api := newSSEAPI(t)
	api.answer("/v1/messages", "", sse(claudeStreamStart,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Ship it "}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"from Settings [#12]."}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":9}}`,
		`{"type":"message_stop"}`))
	provider, err := NewAnthropic(AnthropicConfig{APIKey: secretKey, BaseURL: api.server.URL, HTTPClient: api.server.Client(), forceFallbacks: true})
	if err != nil {
		t.Fatal(err)
	}
	var pieces []string
	response, err := provider.Stream(context.Background(), chatRequest(), func(text string) { pieces = append(pieces, text) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(pieces, "|") != "Ship it |from Settings [#12]." || response.Text != "Ship it from Settings [#12]." || response.Stop != StopDone {
		t.Fatalf("pieces=%q response=%+v", pieces, response)
	}
	if usage := response.Usage; !usage.Reported || usage.InputTokens != 40 || usage.OutputTokens != 9 || usage.CacheReadTokens != 3000 || response.RequestID != "req_stream" {
		t.Fatalf("usage=%+v id=%q", usage, response.RequestID)
	}
	body := api.sent(t, "/v1/messages")[0]
	if body["stream"] != true || body["max_tokens"] != float64(800) || body["fallbacks"] != nil {
		t.Fatalf("stream=%v max_tokens=%v fallbacks=%v", body["stream"], body["max_tokens"], body["fallbacks"])
	}
	if thinking, _ := body["thinking"].(map[string]any); thinking["type"] != "disabled" {
		t.Errorf("thinking = %v", body["thinking"])
	}
	if top, _ := body["cache_control"].(map[string]any); top["type"] != "ephemeral" {
		t.Errorf("top-level cache_control = %v", body["cache_control"])
	}
	system, _ := body["system"].([]any)
	if len(system) != 3 {
		t.Fatalf("system = %v", body["system"])
	}
	for i, block := range system {
		_, cached := block.(map[string]any)["cache_control"]
		if cached != (i == 2) {
			t.Errorf("system block %d cache_control=%v", i, cached)
		}
	}
	messages, _ := body["messages"].([]any)
	if last := messages[len(messages)-1].(map[string]any); last["role"] != "user" {
		t.Errorf("the request ends with a prefill: %v", last)
	}

	// A model that refuses a thinking switch gets none.
	request := chatRequest()
	request.Model = "claude-opus-5-5"
	if _, err := provider.Stream(context.Background(), request, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if bodies := api.sent(t, "/v1/messages"); bodies[1]["thinking"] != nil {
		t.Errorf("thinking sent to a model that refuses it: %v", bodies[1]["thinking"])
	}
}

// An error event in the middle of a stream fails the call with its code and keeps the usage the
// provider reported when it read the prompt, so the call is still metered.
func TestAnthropicStreamErrorKeepsTheUsageSoFar(t *testing.T) {
	api := newSSEAPI(t)
	api.answer("/v1/messages", "", sse(claudeStreamStart,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Half "}}`,
		`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`))
	provider, err := NewAnthropic(AnthropicConfig{APIKey: secretKey, BaseURL: api.server.URL, HTTPClient: api.server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	response, err := provider.Stream(context.Background(), chatRequest(), func(text string) { got.WriteString(text) })
	if err == nil {
		t.Fatal("a stream that ended with an error succeeded")
	}
	if got.String() != "Half " || !response.Usage.Reported || response.Usage.InputTokens != 40 || response.Usage.CacheReadTokens != 3000 {
		t.Fatalf("text=%q usage=%+v err=%v", got.String(), response.Usage, err)
	}
}

// count_tokens is asked with the request's system and messages; warming the cache sends the same
// prefix with max_tokens 0 and no stream.
func TestAnthropicCountsTokensAndWarmsTheCache(t *testing.T) {
	api := newSSEAPI(t)
	api.answer("/v1/messages/count_tokens", "application/json", `{"input_tokens":39279}`)
	api.answer("/v1/messages", "application/json", `{"id":"msg_w","type":"message","role":"assistant","model":"claude-haiku-5-5","content":[],
		"stop_reason":"max_tokens","usage":{"input_tokens":5,"output_tokens":0,"cache_creation_input_tokens":39000,"cache_read_input_tokens":0}}`)
	provider, err := NewAnthropic(AnthropicConfig{APIKey: secretKey, BaseURL: api.server.URL, HTTPClient: api.server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	tokens, exact, err := CountTokens(context.Background(), provider, chatRequest())
	if err != nil || !exact || tokens != 39279 {
		t.Fatalf("count = %d exact=%v err=%v", tokens, exact, err)
	}
	counted := api.sent(t, "/v1/messages/count_tokens")[0]
	if system, _ := counted["system"].([]any); len(system) != 3 || counted["max_tokens"] != nil {
		t.Fatalf("count body = %v", counted)
	}

	warm := chatRequest()
	warm.Warm, warm.MaxOutput = true, 0
	response, err := provider.Complete(context.Background(), warm)
	if err != nil || response.Usage.CacheWriteTokens != 39000 {
		t.Fatalf("warm = %+v err=%v", response, err)
	}
	if body := api.sent(t, "/v1/messages")[0]; body["max_tokens"] != float64(0) || body["stream"] == true {
		t.Fatalf("warm body max_tokens=%v stream=%v", body["max_tokens"], body["stream"])
	}
	if _, err := provider.Stream(context.Background(), warm, func(string) {}); err == nil {
		t.Error("a warming request was streamed")
	}
	if !Caches(provider) {
		t.Error("Claude keeps a prompt cache")
	}
}

// An OpenAI-compatible stream reads its chunks, asks for the usage in the last one and joins the
// system blocks; a connection that closes before the answer ends fails as unavailable.
func TestOpenAIStreamsChunksAndReadsTheUsage(t *testing.T) {
	api := newSSEAPI(t)
	api.answer("/v1/chat/completions", "", "data: "+`{"id":"c1","model":"gpt-x","choices":[{"delta":{"content":"Ship "}}]}`+"\n\n"+
		"data: "+`{"id":"c1","model":"gpt-x","choices":[{"delta":{"content":"it."},"finish_reason":"stop"}]}`+"\n\n"+
		"data: "+`{"id":"c1","model":"gpt-x","choices":[],"usage":{"prompt_tokens":120,"completion_tokens":4,"prompt_tokens_details":{"cached_tokens":100}}}`+"\n\n"+
		"data: [DONE]\n\n")
	provider, err := NewOpenAICompatible(OpenAIConfig{BaseURL: api.server.URL + "/v1", HTTPClient: api.server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if Caches(provider) {
		t.Error("an OpenAI-compatible provider has no cache to warm")
	}
	var pieces []string
	response, err := provider.Stream(context.Background(), chatRequest(), func(text string) { pieces = append(pieces, text) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(pieces, "|") != "Ship |it." || response.Text != "Ship it." || response.Stop != StopDone ||
		response.Usage.InputTokens != 20 || response.Usage.CacheReadTokens != 100 || response.Usage.OutputTokens != 4 {
		t.Fatalf("pieces=%q response=%+v", pieces, response)
	}
	body := api.sent(t, "/v1/chat/completions")[0]
	options, _ := body["stream_options"].(map[string]any)
	messages, _ := body["messages"].([]any)
	if body["stream"] != true || options["include_usage"] != true || messages[0].(map[string]any)["content"] != "rules\n\nworkspace\n\narticles" {
		t.Fatalf("body = %v", body)
	}

	api.answer("/v1/chat/completions", "", "data: "+`{"id":"c2","choices":[{"delta":{"content":"Cut"}}]}`+"\n\n")
	if _, err := provider.Stream(context.Background(), chatRequest(), func(string) {}); CodeOf(err) != ErrorUnavailable {
		t.Fatalf("cut stream err = %v", err)
	}
}

// The fake streams scripted deltas in order, fails after them when scripted to, and stops at a
// canceled context with the scripted usage kept.
func TestFakeStreamsScriptedDeltas(t *testing.T) {
	fake := &Fake{}
	usage := Usage{Reported: true, InputTokens: 7, OutputTokens: 3}
	failure := &Error{Code: ErrorOverloaded}
	hold := make(chan struct{})
	fake.Script(FakeReply{Deltas: []string{"a", "b"}, Response: Response{Stop: StopDone, Usage: usage}},
		FakeReply{Deltas: []string{"x"}, Response: Response{Usage: usage}, Err: failure},
		FakeReply{Deltas: []string{"1", "2"}, Response: Response{Usage: usage}, Hold: hold})
	var pieces []string
	response, err := fake.Stream(context.Background(), chatRequest(), func(text string) { pieces = append(pieces, text) })
	if err != nil || response.Text != "ab" || strings.Join(pieces, "") != "ab" {
		t.Fatalf("first = %+v %v", response, err)
	}
	if _, err := fake.Stream(context.Background(), chatRequest(), func(string) {}); !errors.Is(err, failure) {
		t.Fatalf("second err = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	pieces = nil
	response, err = fake.Stream(ctx, chatRequest(), func(text string) {
		pieces = append(pieces, text)
		cancel()
	})
	if err == nil || strings.Join(pieces, "") != "1" || !response.Usage.Reported || response.Usage.InputTokens != 7 {
		t.Fatalf("canceled = %+v %v pieces=%q", response, err, pieces)
	}
	if len(fake.Requests()) != 3 {
		t.Fatalf("requests = %d", len(fake.Requests()))
	}
}
