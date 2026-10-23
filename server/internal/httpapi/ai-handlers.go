package httpapi

import (
	"errors"
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/ai"
)

// AI settings (docs/user/ai.md). The handlers read and change the workspace's providers and features;
// the checks that call a provider run here and wait for its answer, bounded by the service.

func aiActor(request *http.Request) ai.Actor {
	who := principal(request)
	actor := ai.Actor{AgentID: who.Agent.ID}
	if who.Scope != "" {
		actor.TokenID = who.SessionID
	}
	return actor
}

func (a *API) getAISettings(response http.ResponseWriter, request *http.Request) {
	settings, err := a.ai.Settings(request.Context(), principal(request).Workspace.ID)
	if err != nil {
		writeAIError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, settings)
}

func (a *API) addAIProvider(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Kind    string   `json:"kind"`
		Label   string   `json:"label"`
		BaseURL string   `json:"base_url"`
		APIKey  string   `json:"api_key"`
		Models  []string `json:"models"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send kind (anthropic or openai_compatible), label, models, api_key and, for openai_compatible, base_url.")
		return
	}
	provider, err := a.ai.AddProvider(request.Context(), principal(request).Workspace.ID, aiActor(request),
		ai.ProviderInput{Kind: input.Kind, Label: input.Label, BaseURL: input.BaseURL, APIKey: input.APIKey, Models: input.Models})
	if err != nil {
		writeAIError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, provider)
}

func (a *API) updateAIProvider(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Label  *string   `json:"label"`
		Models *[]string `json:"models"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send label or models. A provider's base URL never changes: add the new address as another provider, with its key.")
		return
	}
	provider, err := a.ai.UpdateProvider(request.Context(), principal(request).Workspace.ID, aiActor(request), request.PathValue("id"),
		ai.ProviderChange{Label: input.Label, Models: input.Models})
	if err != nil {
		writeAIError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, provider)
}

func (a *API) setAIProviderKey(response http.ResponseWriter, request *http.Request) {
	var input struct {
		APIKey string `json:"api_key"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send api_key, the provider's new key.")
		return
	}
	provider, err := a.ai.SetKey(request.Context(), principal(request).Workspace.ID, aiActor(request), request.PathValue("id"), input.APIKey)
	if err != nil {
		writeAIError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, provider)
}

func (a *API) checkAIProvider(response http.ResponseWriter, request *http.Request) {
	provider, err := a.ai.CheckProvider(request.Context(), principal(request).Workspace.ID, aiActor(request), request.PathValue("id"))
	if err != nil {
		writeAIError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, provider)
}

func (a *API) removeAIProvider(response http.ResponseWriter, request *http.Request) {
	off, err := a.ai.RemoveProvider(request.Context(), principal(request).Workspace.ID, aiActor(request), request.PathValue("id"))
	if err != nil {
		writeAIError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"turned_off": off})
}

func (a *API) setAISlot(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Source     string `json:"source"`
		ProviderID string `json:"provider_id"`
		Model      string `json:"model"`
		Effort     string `json:"effort"`
		Enabled    *bool  `json:"enabled"`
	}
	if err := decodeJSON(request, &input); err != nil || input.Enabled == nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send source (server, provider or none), enabled, and optional provider_id, model and effort.")
		return
	}
	slot, err := a.ai.SetSlot(request.Context(), principal(request).Workspace.ID, aiActor(request), request.PathValue("slot"),
		ai.SlotInput{Source: input.Source, ProviderID: input.ProviderID, Model: input.Model, Effort: input.Effort, Enabled: *input.Enabled})
	if err != nil {
		writeAIError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, slot)
}

func writeAIError(response http.ResponseWriter, err error) {
	var invalid *ai.InvalidError
	var check *ai.CheckError
	var inUse *ai.InUseError
	switch {
	case errors.As(err, &invalid):
		writeError(response, http.StatusBadRequest, "invalid_request", invalid.Message)
	case errors.As(err, &check):
		writeJSON(response, http.StatusUnprocessableEntity, map[string]any{"error": errorBody(response, map[string]any{
			"code": "ai_check_failed", "check_error_code": string(check.Code), "message": aiCheckMessage(check),
		})})
	case errors.As(err, &inUse):
		writeError(response, http.StatusConflict, "ai_model_in_use", inUse.Error()+".")
	case errors.Is(err, ai.ErrNotFound):
		writeError(response, http.StatusNotFound, "not_found", "No AI provider or feature with that name in this workspace.")
	case errors.Is(err, ai.ErrKeysMissing):
		writeError(response, http.StatusConflict, "secret_keys_missing", "This server cannot store API keys yet: the operator has not set MAILBOX_TOKEN_KEYS, which encrypts them at rest. Nothing was saved.")
	case errors.Is(err, ai.ErrLimit):
		writeError(response, http.StatusConflict, "ai_provider_limit", "A workspace can have 5 AI providers. Delete one to add another.")
	case errors.Is(err, ai.ErrLabelTaken):
		writeError(response, http.StatusConflict, "ai_provider_name_taken", "Another provider of this workspace has that name.")
	case errors.Is(err, ai.ErrNoServerProvider):
		writeError(response, http.StatusConflict, "no_server_provider", "This server has no AI provider for that feature. Add your own provider, or ask the server operator.")
	case errors.Is(err, ai.ErrCheckRateLimited):
		response.Header().Set("Retry-After", "60")
		writeError(response, http.StatusTooManyRequests, "ai_checks_rate_limited", "At most 10 provider checks a minute in a workspace; try again shortly.")
	default:
		writeStoreError(response, err)
	}
}

// aiCheckMessage says what a failed check means and what to do; Settings shows it under the field.
func aiCheckMessage(check *ai.CheckError) string {
	switch check.Code {
	case "auth":
		return "The provider refused this key. Copy it again from the provider's console. Nothing was saved."
	case "permission":
		return "The key works but may not use this model. Nothing was saved."
	case "model_not_found":
		return "The provider has no model with that name; check the first model in the list. Nothing was saved."
	case "blocked_address":
		return "This address is on a private network, which this server does not call for workspaces. Use a public https address, or ask the server operator to allow private addresses."
	case "quota_exhausted":
		return "The provider account is out of credit. Add credit there, then try again. Nothing was saved."
	case "timeout":
		return "The provider did not answer in time. Try again. Nothing was saved."
	case "rate_limited", "overloaded":
		return "The provider is busy right now. Try again in a minute. Nothing was saved."
	case "bad_request", "too_large":
		return "The provider refused the request; it may not support structured output for this model. Nothing was saved."
	}
	return "The provider could not be reached or did not answer properly. Check the address and try again. Nothing was saved."
}
