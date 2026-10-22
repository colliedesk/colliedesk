package aiprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
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
}

// NewOpenAICompatible returns the OpenAI-compatible adapter.
func NewOpenAICompatible(config OpenAIConfig) (Provider, error) {
	base, err := url.Parse(strings.TrimRight(config.BaseURL, "/"))
	if err != nil || (base.Scheme != "https" && base.Scheme != "http") || base.Host == "" {
		return nil, errors.New("aiprovider: the base URL must be an http or https URL")
	}
	if base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, errors.New("aiprovider: the base URL must not carry credentials, a query or a fragment")
	}
	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = NewHTTPClient(HTTPOptions{})
	}
	return &openAIProvider{
		endpoint:   base.String() + "/chat/completions",
		apiKey:     config.APIKey,
		http:       httpClient,
		openAIHost: strings.EqualFold(base.Hostname(), "api.openai.com"),
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

func (p *openAIProvider) Complete(ctx context.Context, request Request) (Response, error) {
	if err := request.Validate(); err != nil {
		return Response{}, err
	}
	body := map[string]any{"model": request.Model, "stream": false}
	messages := make([]openAIMessage, 0, len(request.Messages)+1)
	if request.System != "" {
		messages = append(messages, openAIMessage{Role: "system", Content: request.System})
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
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return Response{}, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(encoded))
	if err != nil {
		return Response{}, &Error{Code: ErrorBadRequest}
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	if p.apiKey != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	reply, err := p.http.Do(httpRequest)
	if err != nil {
		return Response{}, transportError(ctx, err)
	}
	defer reply.Body.Close()
	requestID := reply.Header.Get("X-Request-Id")
	data, err := io.ReadAll(io.LimitReader(reply.Body, maxResponseBytes+1))
	if err != nil {
		return Response{}, transportError(ctx, err)
	}
	if reply.StatusCode < 200 || reply.StatusCode > 299 {
		providerErr := statusError(reply.StatusCode, reply.Header, requestID)
		if reply.StatusCode == http.StatusTooManyRequests && openAIQuotaExhausted(data) {
			providerErr.Code, providerErr.RetryAfter = ErrorQuotaExhausted, 0
		}
		return Response{}, providerErr
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
