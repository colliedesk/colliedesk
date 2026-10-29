package aiprovider

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"
)

// AnthropicAPIURL is the Claude API. Server-side refusal fallbacks exist only there.
const AnthropicAPIURL = "https://api.anthropic.com"

// fallbackModels are the models that accept `fallbacks: "default"`: on a policy decline the API
// re-runs the request on another model inside the same call. Claude Haiku 5.5 has no fallback.
var fallbackModels = map[string]bool{
	"claude-fable-5-1":  true,
	"claude-opus-5-5":   true,
	"claude-opus-5":     true,
	"claude-sonnet-5-5": true,
}

// AnthropicConfig configures the Claude adapter.
type AnthropicConfig struct {
	APIKey string
	// BaseURL is empty for the Claude API, or a proxy that speaks the Messages API.
	BaseURL string
	// HTTPClient is nil for a client that dials public addresses only (netguard).
	HTTPClient *http.Client
	// forceFallbacks is a test seam: fallbacks are otherwise sent only to the Claude API.
	forceFallbacks bool
}

type anthropicProvider struct {
	client    anthropic.Client
	fallbacks bool
}

// NewAnthropic returns the Claude adapter. The SDK client is built without its environment
// defaults, so ANTHROPIC_API_KEY, ANTHROPIC_AUTH_TOKEN, ANTHROPIC_BASE_URL, a local `ant` profile
// or custom-header variables on the host never reach a workspace's request. Its retries are off:
// one call is one attempt, and the caller meters and retries attempts itself.
func NewAnthropic(config AnthropicConfig) (Provider, error) {
	if config.APIKey == "" {
		return nil, errors.New("aiprovider: the Claude adapter needs an API key")
	}
	baseURL := AnthropicAPIURL
	if strings.TrimSpace(config.BaseURL) != "" {
		parsed, err := parseBaseURL(config.BaseURL)
		if err != nil {
			return nil, err
		}
		baseURL = parsed.String()
	}
	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = NewHTTPClient(HTTPOptions{Guarded: true})
	}
	client := anthropic.NewClient(
		option.WithoutEnvironmentDefaults(),
		option.WithBaseURL(baseURL+"/"),
		option.WithAPIKey(config.APIKey),
		option.WithHTTPClient(httpClient),
		option.WithMaxRetries(0),
	)
	return &anthropicProvider{client: client, fallbacks: baseURL == AnthropicAPIURL || config.forceFallbacks}, nil
}

func (p *anthropicProvider) Kind() Kind { return KindAnthropic }

func (p *anthropicProvider) Check(ctx context.Context, model string) (Response, error) {
	return p.Complete(ctx, checkRequest(model))
}

// thinkingOffModels take `thinking: {type: "disabled"}` at effort high or lower; the others refuse
// it (Claude Opus 5.5, Claude Sonnet 5.5) or never think unasked, so a request leaves it out.
var thinkingOffModels = map[string]bool{
	"claude-haiku-5-5": true,
	"claude-opus-5":    true,
	"claude-sonnet-5":  true,
	"claude-opus-4-8":  true,
	"claude-opus-4-7":  true,
}

// params builds the Messages API request. fallbacks is false for a stream: text already shown
// cannot be taken back when another model takes over.
func (p *anthropicProvider) params(request Request, fallbacks bool) anthropic.BetaMessageNewParams {
	params := anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(request.Model),
		MaxTokens: int64(request.MaxOutput),
		// Effort is always sent: Claude models default to different levels. No sampling
		// parameters: current models refuse them.
		OutputConfig: anthropic.BetaOutputConfigParam{Effort: anthropic.BetaOutputConfigEffort(request.Effort)},
	}
	if request.Schema != nil {
		params.OutputConfig.Format = anthropic.BetaJSONOutputFormatParam{Schema: request.Schema}
	}
	if request.NoThinking && thinkingOffModels[request.Model] && request.Effort != "xhigh" && request.Effort != "max" {
		params.Thinking = anthropic.BetaThinkingConfigParamUnion{OfDisabled: &anthropic.BetaThinkingConfigDisabledParam{}}
	}
	if request.System != "" {
		params.System = []anthropic.BetaTextBlockParam{{Text: request.System}}
	}
	for _, part := range request.SystemParts {
		block := anthropic.BetaTextBlockParam{Text: part.Text}
		if part.Cache {
			block.CacheControl = anthropic.NewBetaCacheControlEphemeralParam()
		}
		params.System = append(params.System, block)
	}
	if request.CacheHistory {
		params.CacheControl = anthropic.NewBetaCacheControlEphemeralParam()
	}
	for _, message := range request.Messages {
		block := anthropic.NewBetaTextBlock(message.Text)
		role := anthropic.BetaMessageParamRoleUser
		if message.Role == RoleAssistant {
			role = anthropic.BetaMessageParamRoleAssistant
		}
		params.Messages = append(params.Messages, anthropic.BetaMessageParam{Role: role, Content: []anthropic.BetaContentBlockParamUnion{block}})
	}
	if fallbacks && p.fallbacks && fallbackModels[request.Model] {
		params.Betas = []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01}
		params.Fallbacks = anthropic.BetaFallbacksParamUnion{OfDefault: constant.ValueOf[constant.Default]()}
	}
	return params
}

// callError maps a failed call: the API's error, an answer that is not the API's (a redirect, a
// proxy's HTML page), or the transport's.
func callError(ctx context.Context, err error, raw *http.Response) error {
	var apiErr *anthropic.Error
	switch {
	case errors.As(err, &apiErr) && apiErr.Response != nil:
		return statusError(apiErr.StatusCode, apiErr.Response.Header, apiErr.RequestID)
	case raw != nil:
		if raw.StatusCode >= 300 {
			return statusError(raw.StatusCode, raw.Header, raw.Header.Get("Request-Id"))
		}
		return &Error{Code: ErrorInvalidResponse, Status: raw.StatusCode, RequestID: raw.Header.Get("Request-Id")}
	}
	return transportError(ctx, err)
}

func (p *anthropicProvider) Complete(ctx context.Context, request Request) (Response, error) {
	if err := request.Validate(); err != nil {
		return Response{}, err
	}
	var raw *http.Response
	message, err := p.client.Beta.Messages.New(ctx, p.params(request, !request.Warm), option.WithResponseInto(&raw))
	if err != nil {
		return Response{}, callError(ctx, err, raw)
	}
	requestID := ""
	if raw != nil {
		requestID = raw.Header.Get("Request-Id")
	}
	return anthropicResponse(request, message, requestID), nil
}

func (p *anthropicProvider) Stream(ctx context.Context, request Request, onText func(text string)) (Response, error) {
	if err := request.Validate(); err != nil {
		return Response{}, err
	}
	if request.Warm {
		return Response{}, errors.New("aiprovider: a request that warms the cache is never streamed")
	}
	var raw *http.Response
	stream := p.client.Beta.Messages.NewStreaming(ctx, p.params(request, false), option.WithResponseInto(&raw))
	defer stream.Close()
	var message anthropic.BetaMessage
	for stream.Next() {
		event := stream.Current()
		if err := message.Accumulate(event); err != nil {
			return p.partial(request, message, raw), &Error{Code: ErrorInvalidResponse, RequestID: requestIDOf(raw)}
		}
		if event.Type == "content_block_delta" && event.Delta.Type == "text_delta" && event.Delta.Text != "" {
			onText(event.Delta.Text)
		}
	}
	if err := stream.Err(); err != nil {
		return p.partial(request, message, raw), callError(ctx, err, raw)
	}
	if message.ID == "" {
		return Response{}, &Error{Code: ErrorInvalidResponse, RequestID: requestIDOf(raw)}
	}
	return anthropicResponse(request, &message, requestIDOf(raw)), nil
}

// partial is what a stream that failed had answered: the usage the provider reported once it read
// the prompt (message_start), or nothing when it never did.
func (p *anthropicProvider) partial(request Request, message anthropic.BetaMessage, raw *http.Response) Response {
	if message.ID == "" {
		return Response{}
	}
	response := anthropicResponse(request, &message, requestIDOf(raw))
	response.Stop = StopOther
	return response
}

func requestIDOf(raw *http.Response) string {
	if raw == nil {
		return ""
	}
	return raw.Header.Get("Request-Id")
}

// CountTokens asks count_tokens, which is free: the same system, messages and settings as the request.
func (p *anthropicProvider) CountTokens(ctx context.Context, request Request) (int64, error) {
	if request.Model == "" || len(request.Messages) == 0 {
		return 0, errors.New("aiprovider: a count needs a model and a message")
	}
	params := p.params(request, false)
	count := anthropic.BetaMessageCountTokensParams{Model: params.Model, Messages: params.Messages, OutputConfig: params.OutputConfig}
	if len(params.System) > 0 {
		count.System = anthropic.BetaMessageCountTokensParamsSystemUnion{OfBetaTextBlockArray: params.System}
	}
	var raw *http.Response
	result, err := p.client.Beta.Messages.CountTokens(ctx, count, option.WithResponseInto(&raw))
	if err != nil {
		return 0, callError(ctx, err, raw)
	}
	return result.InputTokens, nil
}

// anthropicResponse reads the answer. With a fallback the content holds a `fallback` block at each
// switch point and only the text after the last one is the answer; usage.iterations holds one
// entry per model that ran, which is what each model bills.
func anthropicResponse(request Request, message *anthropic.BetaMessage, requestID string) Response {
	var text strings.Builder
	for _, block := range message.Content {
		switch block.Type {
		case "fallback":
			text.Reset()
		case "text":
			text.WriteString(block.Text)
		}
	}
	response := Response{Text: text.String(), Model: string(message.Model), RequestID: requestID}
	if response.RequestID == "" {
		response.RequestID = message.ID
	}
	switch message.StopReason {
	case anthropic.BetaStopReasonEndTurn, anthropic.BetaStopReasonStopSequence:
		response.Stop = StopDone
	case anthropic.BetaStopReasonMaxTokens, anthropic.BetaStopReasonModelContextWindowExceeded:
		response.Stop = StopMaxTokens
	case anthropic.BetaStopReasonRefusal:
		response.Stop = StopRefused
	default:
		response.Stop = StopOther
	}
	if response.Stop == StopDone {
		response.JSON = schemaJSON(request, response.Text)
	}

	for _, iteration := range message.Usage.Iterations {
		if iteration.Type != "message" && iteration.Type != "fallback_message" {
			continue
		}
		model := string(iteration.Model)
		switch {
		case model != "":
		case iteration.Type == "message":
			model = request.Model
		default:
			model = response.Model
		}
		response.Hops = append(response.Hops, Hop{Model: model, Usage: Usage{
			Reported:         true,
			InputTokens:      iteration.InputTokens,
			OutputTokens:     iteration.OutputTokens,
			CacheReadTokens:  iteration.CacheReadInputTokens,
			CacheWriteTokens: iteration.CacheCreationInputTokens,
		}})
	}
	if len(response.Hops) == 0 {
		response.Hops = []Hop{{Model: response.Model, Usage: Usage{
			Reported:         true,
			InputTokens:      message.Usage.InputTokens,
			OutputTokens:     message.Usage.OutputTokens,
			CacheReadTokens:  message.Usage.CacheReadInputTokens,
			CacheWriteTokens: message.Usage.CacheCreationInputTokens,
		}}}
	}
	for _, hop := range response.Hops {
		response.Usage = response.Usage.Add(hop.Usage)
	}
	return response
}
