package aiprovider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

// OpenAIConfig configures the OpenAI-compatible adapter: one adapter for OpenAI, OpenRouter, Groq,
// Ollama, vLLM and LM Studio, all of which serve POST {BaseURL}/chat/completions.
type OpenAIConfig struct {
	// BaseURL includes the API version path, as each provider documents it:
	// https://api.openai.com/v1, https://openrouter.ai/api/v1, http://localhost:11434/v1.
	BaseURL string
	// APIKey is empty for a server without authentication (a local Ollama).
	APIKey     string
	HTTPClient *http.Client
}

type openAIProvider struct {
	endpoint   string
	apiKey     string
	http       *http.Client
	openAIHost bool
	// openRouterHost: OpenRouter routes each request to one of several providers of the model.
	openRouterHost bool
}

// NewOpenAICompatible returns the OpenAI-compatible adapter.
func NewOpenAICompatible(config OpenAIConfig) (Provider, error) {
	base, err := parseBaseURL(config.BaseURL)
	if err != nil {
		return nil, err
	}
	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = NewHTTPClient(HTTPOptions{Guarded: true})
	}
	return &openAIProvider{
		endpoint:       base.String() + "/chat/completions",
		apiKey:         config.APIKey,
		http:           httpClient,
		openAIHost:     strings.EqualFold(base.Hostname(), "api.openai.com"),
		openRouterHost: strings.EqualFold(base.Hostname(), "openrouter.ai"),
	}, nil
}

func (p *openAIProvider) Kind() Kind { return KindOpenAICompatible }

func (p *openAIProvider) Check(ctx context.Context, model string) (Response, error) {
	return p.Complete(ctx, checkRequest(model))
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChoice struct {
	Message struct {
		Content *string `json:"content"`
		Refusal *string `json:"refusal"`
	} `json:"message"`
	FinishReason string `json:"finish_reason"`
}

type openAIReply struct {
	ID      string         `json:"id"`
	Model   string         `json:"model"`
	Choices []openAIChoice `json:"choices"`
	Usage   *struct {
		PromptTokens        int64 `json:"prompt_tokens"`
		CompletionTokens    int64 `json:"completion_tokens"`
		PromptTokensDetails *struct {
			CachedTokens int64 `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
}

// body builds the chat completion request.
func (p *openAIProvider) body(request Request, stream bool) map[string]any {
	body := map[string]any{"model": request.Model, "stream": stream}
	if stream {
		// The last chunk then carries the usage; without it a streamed call is not metered.
		body["stream_options"] = map[string]any{"include_usage": true}
	}
	messages := make([]openAIMessage, 0, len(request.Messages)+1)
	if system := request.SystemText(); system != "" {
		messages = append(messages, openAIMessage{Role: "system", Content: system})
	}
	for _, message := range request.Messages {
		messages = append(messages, openAIMessage{Role: string(message.Role), Content: message.Text})
	}
	body["messages"] = messages
	// OpenAI's reasoning models refuse max_tokens and take reasoning_effort; the servers that copy
	// the API (Ollama, vLLM, OpenRouter) read max_tokens and may refuse an unknown reasoning field.
	if p.openAIHost {
		body["max_completion_tokens"] = request.MaxOutput
		body["reasoning_effort"] = string(request.Effort)
	} else {
		body["max_tokens"] = request.MaxOutput
	}
	if request.Schema != nil {
		body["response_format"] = map[string]any{
			"type":        "json_schema",
			"json_schema": map[string]any{"name": "answer", "strict": true, "schema": request.Schema},
		}
		// By default OpenRouter may route to a provider that ignores response_format; this keeps the
		// request on the providers that take every parameter it carries.
		if p.openRouterHost {
			body["provider"] = map[string]any{"require_parameters": true}
		}
	}
	return body
}

// send posts a body and returns the reply when its status is a success; the caller closes it.
func (p *openAIProvider) send(ctx context.Context, body map[string]any, accept string) (*http.Response, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(encoded))
	if err != nil {
		return nil, &Error{Code: ErrorBadRequest}
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", accept)
	if p.apiKey != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	reply, err := p.http.Do(httpRequest)
	if err != nil {
		return nil, transportError(ctx, err)
	}
	if reply.StatusCode < 200 || reply.StatusCode > 299 {
		defer reply.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(reply.Body, maxResponseBytes))
		providerErr := statusError(reply.StatusCode, reply.Header, reply.Header.Get("X-Request-Id"))
		if reply.StatusCode == http.StatusTooManyRequests && openAIQuotaExhausted(data) {
			providerErr.Code, providerErr.RetryAfter = ErrorQuotaExhausted, 0
		}
		return nil, providerErr
	}
	return reply, nil
}

func (p *openAIProvider) Complete(ctx context.Context, request Request) (Response, error) {
	if err := request.Validate(); err != nil {
		return Response{}, err
	}
	if request.Warm {
		return Response{}, errors.New("aiprovider: this provider keeps no prompt cache to warm")
	}
	reply, err := p.send(ctx, p.body(request, false), "application/json")
	if err != nil {
		return Response{}, err
	}
	defer reply.Body.Close()
	requestID := reply.Header.Get("X-Request-Id")
	data, err := io.ReadAll(io.LimitReader(reply.Body, maxResponseBytes+1))
	if err != nil {
		return Response{}, transportError(ctx, err)
	}
	if len(data) > maxResponseBytes {
		return Response{}, &Error{Code: ErrorInvalidResponse, Status: reply.StatusCode, RequestID: requestID}
	}
	var parsed openAIReply
	if err := json.Unmarshal(data, &parsed); err != nil || len(parsed.Choices) == 0 {
		return Response{}, &Error{Code: ErrorInvalidResponse, Status: reply.StatusCode, RequestID: requestID}
	}
	return openAIResponse(request, parsed, requestID), nil
}

// openAIChunk is one event of a streamed chat completion.
type openAIChunk struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Content *string `json:"content"`
			Refusal *string `json:"refusal"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens        int64 `json:"prompt_tokens"`
		CompletionTokens    int64 `json:"completion_tokens"`
		PromptTokensDetails *struct {
			CachedTokens int64 `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
}

// Stream reads server-sent events ("data: {chunk}" lines, then "data: [DONE]") into the same reply
// Complete reads.
func (p *openAIProvider) Stream(ctx context.Context, request Request, onText func(text string)) (Response, error) {
	if err := request.Validate(); err != nil {
		return Response{}, err
	}
	if request.Warm {
		return Response{}, errors.New("aiprovider: a request that warms the cache is never streamed")
	}
	reply, err := p.send(ctx, p.body(request, true), "text/event-stream")
	if err != nil {
		return Response{}, err
	}
	defer reply.Body.Close()
	requestID := reply.Header.Get("X-Request-Id")
	var parsed openAIReply
	var text, refusal strings.Builder
	finish := ""
	read := 0
	lines := bufio.NewScanner(reply.Body)
	lines.Buffer(make([]byte, 64<<10), 1<<20)
	done := false
	for !done && lines.Scan() {
		line := lines.Text()
		read += len(line)
		if read > maxResponseBytes {
			return openAIResponse(request, parsed, requestID), &Error{Code: ErrorInvalidResponse, Status: reply.StatusCode, RequestID: requestID}
		}
		data, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		data = strings.TrimSpace(data)
		if data == "[DONE]" {
			done = true
			break
		}
		var chunk openAIChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return Response{}, &Error{Code: ErrorInvalidResponse, Status: reply.StatusCode, RequestID: requestID}
		}
		if parsed.ID == "" {
			parsed.ID = chunk.ID
		}
		if chunk.Model != "" {
			parsed.Model = chunk.Model
		}
		if chunk.Usage != nil {
			parsed.Usage = chunk.Usage
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content != nil && *choice.Delta.Content != "" {
				text.WriteString(*choice.Delta.Content)
				onText(*choice.Delta.Content)
			}
			if choice.Delta.Refusal != nil {
				refusal.WriteString(*choice.Delta.Refusal)
			}
			if choice.FinishReason != nil {
				finish = *choice.FinishReason
			}
		}
	}
	choice := openAIChoice{FinishReason: finish}
	content, refused := text.String(), refusal.String()
	choice.Message.Content = &content
	if refused != "" {
		choice.Message.Refusal = &refused
	}
	parsed.Choices = []openAIChoice{choice}
	if err := lines.Err(); err != nil {
		return openAIResponse(request, parsed, requestID), transportError(ctx, err)
	}
	if !done && finish == "" {
		// The connection closed before the answer ended.
		return openAIResponse(request, parsed, requestID), &Error{Code: ErrorUnavailable, Status: reply.StatusCode, RequestID: requestID}
	}
	return openAIResponse(request, parsed, requestID), nil
}

// openAIQuotaExhausted tells OpenAI's "out of credit" 429 from a rate limit: retrying it only
// spends attempts.
func openAIQuotaExhausted(data []byte) bool {
	var envelope struct {
		Error struct {
			Code string `json:"code"`
			Type string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &envelope) != nil {
		return false
	}
	return envelope.Error.Code == "insufficient_quota" || envelope.Error.Type == "insufficient_quota"
}

func openAIResponse(request Request, parsed openAIReply, requestID string) Response {
	choice := parsed.Choices[0]
	response := Response{Model: parsed.Model, RequestID: requestID}
	if response.Model == "" {
		response.Model = request.Model
	}
	if response.RequestID == "" {
		response.RequestID = parsed.ID
	}
	if choice.Message.Content != nil {
		response.Text = *choice.Message.Content
	}
	switch {
	case choice.Message.Refusal != nil && *choice.Message.Refusal != "":
		response.Stop = StopRefused
	case choice.FinishReason == "stop":
		response.Stop = StopDone
	case choice.FinishReason == "length":
		response.Stop = StopMaxTokens
	case choice.FinishReason == "content_filter":
		response.Stop = StopRefused
	default:
		response.Stop = StopOther
	}
	if response.Stop == StopDone {
		response.JSON = schemaJSON(request, response.Text)
	}
	usage := Usage{}
	if parsed.Usage != nil {
		usage = Usage{Reported: true, InputTokens: parsed.Usage.PromptTokens, OutputTokens: parsed.Usage.CompletionTokens}
		if details := parsed.Usage.PromptTokensDetails; details != nil {
			// OpenAI counts cached tokens inside prompt_tokens; keep input as the uncached part so
			// input + cache read is the prompt, as with Claude.
			usage.CacheReadTokens = details.CachedTokens
			usage.InputTokens -= details.CachedTokens
		}
	}
	response.Usage = usage
	response.Hops = []Hop{{Model: response.Model, Usage: usage}}
	return response
}
