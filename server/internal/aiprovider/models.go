package aiprovider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

// MaxListedModels bounds a model list: OpenRouter offers a few hundred, and a person picks a few.
const MaxListedModels = 1000

// ModelLister is an adapter that can say which models its key may use, so a person picks a model
// instead of typing its id. Listing is free at every provider: it is not metered.
type ModelLister interface {
	Models(ctx context.Context) ([]string, error)
}

// Models lists the Claude models the key may use that answer what Colliedesk sends (GET /v1/models):
// every request carries an effort and a JSON schema, so a model that does not take both, or is
// retired, is left out. The API says so per model; a list without capabilities (a proxy) is kept.
func (p *anthropicProvider) Models(ctx context.Context) ([]string, error) {
	pager := p.client.Models.ListAutoPaging(ctx, anthropic.ModelListParams{Limit: anthropic.Int(MaxListedModels)})
	var ids []string
	for pager.Next() && len(ids) < MaxListedModels {
		if model := pager.Current(); claudeAnswersColliedesk(model) {
			ids = append(ids, model.ID)
		}
	}
	if err := pager.Err(); err != nil {
		var apiErr *anthropic.Error
		if errors.As(err, &apiErr) && apiErr.Response != nil {
			return nil, statusError(apiErr.StatusCode, apiErr.Response.Header, apiErr.RequestID)
		}
		return nil, transportError(ctx, err)
	}
	// The API lists the newest first, which is the order a person wants.
	return ids, nil
}

// claudeAnswersColliedesk reports whether a model takes the low, medium and high efforts and a JSON
// schema and is not retired.
func claudeAnswersColliedesk(model anthropic.ModelInfo) bool {
	if model.Lifecycle == anthropic.ModelInfoLifecycleRetired {
		return false
	}
	if !model.JSON.Capabilities.Valid() {
		return true
	}
	effort := model.Capabilities.Effort
	return model.Capabilities.StructuredOutputs.Supported && effort.Supported &&
		effort.Low.Supported && effort.Medium.Supported && effort.High.Supported
}

// Models lists the models of an OpenAI-compatible server (GET {BaseURL}/models), sorted by id, that
// answer what Colliedesk sends: on the OpenAI API a reasoning chat model (openAIAnswersColliedesk);
// on a server that lists each model's parameters (OpenRouter), one that takes a JSON schema. A model
// past its shutdown or expiration date, or served only in batches, is left out.
func (p *openAIProvider) Models(ctx context.Context) ([]string, error) {
	listed, err := p.listModels(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(listed))
	seen := map[string]bool{}
	today := time.Now().UTC().Format(time.DateOnly)
	for _, model := range listed {
		id := strings.TrimSpace(model.ID)
		if p.openAIHost && !openAIAnswersColliedesk(id) {
			continue
		}
		if model.ShutdownDate != nil && *model.ShutdownDate != "" && *model.ShutdownDate <= today ||
			model.ExpirationDate != nil && *model.ExpirationDate != "" && *model.ExpirationDate < today {
			continue
		}
		// An OpenRouter :batch entry answers only its Batch API, never a chat completion.
		if strings.HasSuffix(id, ":batch") {
			continue
		}
		// OpenRouter names the models that answer a json_schema response_format "structured_outputs".
		if model.SupportedParameters != nil && !slices.Contains(*model.SupportedParameters, "structured_outputs") {
			continue
		}
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) > MaxListedModels {
		ids = ids[:MaxListedModels]
	}
	return ids, nil
}

// listedModel is one model of GET {BaseURL}/models.
type listedModel struct {
	ID                  string    `json:"id"`
	SupportedParameters *[]string `json:"supported_parameters"`
	// ShutdownDate is the day OpenAI turns a model off (YYYY-MM-DD), or null; GET /models
	// keeps listing a model after it, and every request to it then fails with model_not_found.
	ShutdownDate *string `json:"shutdown_date"`
	// ExpirationDate is OpenRouter's: the day after which it may remove the model.
	ExpirationDate *string `json:"expiration_date"`
	// Reasoning is OpenRouter's, on a model that reasons: the efforts it takes, null for every one.
	Reasoning *struct {
		SupportedEfforts []string `json:"supported_efforts"`
	} `json:"reasoning"`
}

func (p *openAIProvider) listModels(ctx context.Context) ([]listedModel, error) {
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(p.endpoint, "/chat/completions")+"/models", nil)
	if err != nil {
		return nil, &Error{Code: ErrorBadRequest}
	}
	httpRequest.Header.Set("Accept", "application/json")
	if p.apiKey != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	reply, err := p.http.Do(httpRequest)
	if err != nil {
		return nil, transportError(ctx, err)
	}
	defer reply.Body.Close()
	requestID := reply.Header.Get("X-Request-Id")
	if reply.StatusCode < 200 || reply.StatusCode > 299 {
		return nil, statusError(reply.StatusCode, reply.Header, requestID)
	}
	data, err := io.ReadAll(io.LimitReader(reply.Body, maxResponseBytes+1))
	if err != nil {
		return nil, transportError(ctx, err)
	}
	var parsed struct {
		Data []listedModel `json:"data"`
	}
	if len(data) > maxResponseBytes || json.Unmarshal(data, &parsed) != nil {
		return nil, &Error{Code: ErrorInvalidResponse, Status: reply.StatusCode, RequestID: requestID}
	}
	return parsed.Data, nil
}

// How long what OpenRouter says of its models' reasoning is kept, and how long a failed ask is.
const (
	reasoningListFor     = time.Hour
	reasoningListRetryIn = 5 * time.Minute
	reasoningListTimeout = 5 * time.Second
)

// reasoningLists keeps, per OpenRouter endpoint, which models reason and at which efforts, so a
// request asks GET /models at most once an hour (it is public and free).
var reasoningLists = struct {
	sync.Mutex
	byEndpoint map[string]*reasoningList
}{byEndpoint: map[string]*reasoningList{}}

type reasoningList struct {
	sync.Mutex
	until time.Time
	// efforts maps a model that reasons to the efforts it takes; nil takes every one.
	efforts map[string][]string
}

// reasonsAt reports whether OpenRouter lists the model as one that reasons at the effort. When the
// list cannot be read it says no: the request then goes without an effort, as it did before.
func (p *openAIProvider) reasonsAt(ctx context.Context, model string, effort Effort) bool {
	reasoningLists.Lock()
	list := reasoningLists.byEndpoint[p.endpoint]
	if list == nil {
		list = &reasoningList{}
		reasoningLists.byEndpoint[p.endpoint] = list
	}
	reasoningLists.Unlock()
	list.Lock()
	defer list.Unlock()
	if now := time.Now(); now.After(list.until) {
		lookup, cancel := context.WithTimeout(ctx, reasoningListTimeout)
		listed, err := p.listModels(lookup)
		cancel()
		if err != nil {
			list.efforts, list.until = nil, now.Add(reasoningListRetryIn)
		} else {
			list.efforts, list.until = map[string][]string{}, now.Add(reasoningListFor)
			for _, listed := range listed {
				if listed.SupportedParameters != nil && slices.Contains(*listed.SupportedParameters, "reasoning") {
					var efforts []string
					if listed.Reasoning != nil {
						efforts = listed.Reasoning.SupportedEfforts
					}
					list.efforts[listed.ID] = efforts
				}
			}
		}
	}
	efforts, ok := list.efforts[model]
	if !ok {
		// A variant (model:nitro, model:floor) reasons as its model does.
		if base, _, cut := strings.Cut(model, ":"); cut {
			efforts, ok = list.efforts[base]
		}
	}
	return ok && (efforts == nil || slices.Contains(efforts, string(effort)))
}

// Models lists FakeModels; the fake has no network to ask.
func (f *Fake) Models(context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ModelsErr != nil {
		return nil, f.ModelsErr
	}
	return append([]string(nil), f.FakeModels...), nil
}

// openAIUnusable marks the OpenAI models that are not chat models (images, audio, embeddings,
// moderation), answer only on the Responses API (codex, pro, deep research, computer use), or take
// no reasoning effort (chat-latest).
var openAIUnusable = regexp.MustCompile(`(image|audio|realtime|transcribe|tts|search|embedding|moderation|codex|-pro\b|deep-research|computer-use|chat-latest)`)

// openAIReasoningModel matches OpenAI's reasoning model lines: gpt-5 and later, and the o series.
var openAIReasoningModel = regexp.MustCompile(`^(gpt-([5-9]|[1-9][0-9])([.-]|$)|o[1-9]([.-]|$))`)

// openAIAnswersColliedesk reports whether a model of the OpenAI API answers what Colliedesk sends to
// chat completions: reasoning_effort and a strict JSON schema, which only its reasoning models take
// (the older o1-mini and o1-preview take neither).
func openAIAnswersColliedesk(id string) bool {
	return openAIReasoningModel.MatchString(id) && !openAIUnusable.MatchString(id) &&
		!strings.HasPrefix(id, "o1-mini") && !strings.HasPrefix(id, "o1-preview")
}
