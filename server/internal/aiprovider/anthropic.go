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

func (p *anthropicProvider) Complete(ctx context.Context, request Request) (Response, error) {
	if err := request.Validate(); err != nil {
		return Response{}, err
	}
	params := anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(request.Model),
		MaxTokens: int64(request.MaxOutput),
		// Effort is always sent: Claude models default to different levels. No sampling
		// parameters and no thinking switch: current models refuse both.
		OutputConfig: anthropic.BetaOutputConfigParam{Effort: anthropic.BetaOutputConfigEffort(request.Effort)},
	}
	if request.Schema != nil {
		params.OutputConfig.Format = anthropic.BetaJSONOutputFormatParam{Schema: request.Schema}
	}
	if request.System != "" {
		params.System = []anthropic.BetaTextBlockParam{{Text: request.System}}
	}
	for _, message := range request.Messages {
		block := anthropic.NewBetaTextBlock(message.Text)
		role := anthropic.BetaMessageParamRoleUser
		if message.Role == RoleAssistant {
			role = anthropic.BetaMessageParamRoleAssistant
		}
		params.Messages = append(params.Messages, anthropic.BetaMessageParam{Role: role, Content: []anthropic.BetaContentBlockParamUnion{block}})
	}
	if p.fallbacks && fallbackModels[request.Model] {
		params.Betas = []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01}
		params.Fallbacks = anthropic.BetaFallbacksParamUnion{OfDefault: constant.ValueOf[constant.Default]()}
	}

	var raw *http.Response
	message, err := p.client.Beta.Messages.New(ctx, params, option.WithResponseInto(&raw))
	if err != nil {
		var apiErr *anthropic.Error
		switch {
		case errors.As(err, &apiErr) && apiErr.Response != nil:
			return Response{}, statusError(apiErr.StatusCode, apiErr.Response.Header, apiErr.RequestID)
		case raw != nil:
			// An answer came back but is not the API's: a redirect, or a proxy's HTML page.
			if raw.StatusCode >= 300 {
				return Response{}, statusError(raw.StatusCode, raw.Header, raw.Header.Get("Request-Id"))
			}
			return Response{}, &Error{Code: ErrorInvalidResponse, Status: raw.StatusCode, RequestID: raw.Header.Get("Request-Id")}
		}
		return Response{}, transportError(ctx, err)
	}
	requestID := ""
	if raw != nil {
		requestID = raw.Header.Get("Request-Id")
	}
	return anthropicResponse(request, message, requestID), nil
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
