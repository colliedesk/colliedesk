package aiprovider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// MaxListedModels bounds a model list: OpenRouter offers a few hundred, and a person picks a few.
const MaxListedModels = 1000

// ModelLister is an adapter that can say which models its key may use, so a person picks a model
// instead of typing its id. Listing is free at every provider: it is not metered.
type ModelLister interface {
	Models(ctx context.Context) ([]string, error)
}

// Models lists the Claude models the key may use (GET /v1/models).
func (p *anthropicProvider) Models(ctx context.Context) ([]string, error) {
	pager := p.client.Models.ListAutoPaging(ctx, anthropic.ModelListParams{Limit: anthropic.Int(MaxListedModels)})
	var ids []string
	for pager.Next() && len(ids) < MaxListedModels {
		ids = append(ids, pager.Current().ID)
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

// Models lists the models of an OpenAI-compatible server (GET {BaseURL}/models), sorted by id.
func (p *openAIProvider) Models(ctx context.Context) ([]string, error) {
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
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if len(data) > maxResponseBytes || json.Unmarshal(data, &parsed) != nil {
		return nil, &Error{Code: ErrorInvalidResponse, Status: reply.StatusCode, RequestID: requestID}
	}
	ids := make([]string, 0, len(parsed.Data))
	seen := map[string]bool{}
	for _, model := range parsed.Data {
		if id := strings.TrimSpace(model.ID); id != "" && !seen[id] {
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

// Models lists FakeModels; the fake has no network to ask.
func (f *Fake) Models(context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ModelsErr != nil {
		return nil, f.ModelsErr
	}
	return append([]string(nil), f.FakeModels...), nil
}
