package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/store"
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
	case errors.Is(err, ai.ErrFeatureOff):
		writeError(response, http.StatusConflict, "ai_feature_off", "This AI feature is off in this workspace, or its provider is gone. An admin turns it on in Settings → AI.")
	case errors.Is(err, ai.ErrDraftsRateLimited):
		response.Header().Set("Retry-After", "60")
		writeError(response, http.StatusTooManyRequests, "ai_drafts_rate_limited", "At most 30 AI drafts a minute in a workspace; try again shortly.")
	case errors.Is(err, ai.ErrDraftNotFound):
		writeError(response, http.StatusNotFound, "not_found", "No AI draft with that id on this conversation.")
	case errors.Is(err, ai.ErrConversationNotFound):
		writeError(response, http.StatusNotFound, "not_found", "No conversation with that id in this workspace.")
	case errors.Is(err, ai.ErrTriageNotFound):
		writeError(response, http.StatusNotFound, "not_found", "No AI triage suggestion with that id on this conversation.")
	case errors.Is(err, ai.ErrTriageDecided):
		writeError(response, http.StatusConflict, "ai_triage_decided", "This suggestion is not ready, or was already accepted, dismissed or replaced.")
	case errors.Is(err, ai.ErrDraftDecided):
		writeError(response, http.StatusConflict, "ai_draft_decided", "This AI draft was already sent, discarded or replaced.")
	case errors.Is(err, ai.ErrAllowanceReached):
		writeError(response, http.StatusPaymentRequired, "ai_allowance_reached", "This month's included AI is used up. Add your own provider in Settings → AI, or move to a larger plan.")
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

func (a *API) getAIUsage(response http.ResponseWriter, request *http.Request) {
	month, err := a.ai.ParseMonth(request.URL.Query().Get("month"))
	if err != nil {
		writeAIError(response, err)
		return
	}
	usage, err := a.ai.Usage(request.Context(), principal(request).Workspace.ID, month)
	if err != nil {
		writeAIError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, usage)
}

func (a *API) setAIAutomaticLimits(response http.ResponseWriter, request *http.Request) {
	var input struct {
		PerDay    *int `json:"per_day"`
		PerMinute *int `json:"per_minute"`
	}
	if err := decodeJSON(request, &input); err != nil || input.PerDay == nil || input.PerMinute == nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send per_day (0 to 10000) and per_minute (0 to 1000).")
		return
	}
	limits, err := a.ai.SetAutomaticLimits(request.Context(), principal(request).Workspace.ID, aiActor(request), *input.PerDay, *input.PerMinute)
	if err != nil {
		writeAIError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, limits)
}

func (a *API) createAIDraft(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Prompt string `json:"prompt"`
	}
	if request.ContentLength != 0 {
		if err := decodeJSON(request, &input); err != nil {
			writeError(response, http.StatusBadRequest, "invalid_request", "Send prompt, what the reply should say (optional, at most 2000 characters).")
			return
		}
	}
	draft, err := a.ai.CreateDraft(request.Context(), principal(request).Workspace.ID, aiActor(request), request.PathValue("id"), input.Prompt)
	if err != nil {
		writeAIError(response, err)
		return
	}
	writeJSON(response, http.StatusAccepted, draft)
}

func (a *API) listAIDrafts(response http.ResponseWriter, request *http.Request) {
	drafts, err := a.ai.ListDrafts(request.Context(), principal(request).Workspace.ID, request.PathValue("id"))
	if err != nil {
		writeAIError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": drafts})
}

func (a *API) getAIDraft(response http.ResponseWriter, request *http.Request) {
	wait := 0
	if value := request.URL.Query().Get("wait"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 || parsed > 20 {
			writeError(response, http.StatusBadRequest, "invalid_request", "wait is 0 to 20 seconds.")
			return
		}
		wait = parsed
	}
	draft, err := a.ai.WaitDraft(request.Context(), principal(request).Workspace.ID, request.PathValue("id"), request.PathValue("draftId"), time.Duration(wait)*time.Second)
	if err != nil {
		writeAIError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, draft)
}

func (a *API) discardAIDraft(response http.ResponseWriter, request *http.Request) {
	draft, err := a.ai.DiscardDraft(request.Context(), principal(request).Workspace.ID, aiActor(request), request.PathValue("id"), request.PathValue("draftId"))
	if err != nil {
		writeAIError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, draft)
}

func (a *API) getAITriage(response http.ResponseWriter, request *http.Request) {
	triage, err := a.ai.LatestTriage(request.Context(), principal(request).Workspace.ID, request.PathValue("id"))
	if err != nil {
		writeAIError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"suggestion": triage})
}

func (a *API) createAITriage(response http.ResponseWriter, request *http.Request) {
	triage, err := a.ai.CreateTriage(request.Context(), principal(request).Workspace.ID, aiActor(request), request.PathValue("id"))
	if err != nil {
		writeAIError(response, err)
		return
	}
	writeJSON(response, http.StatusAccepted, triage)
}

// acceptAITriage applies what the person accepts of an open suggestion (the rest of it without a
// body) to the conversation, through the same changes as a person's own, then records it.
func (a *API) acceptAITriage(response http.ResponseWriter, request *http.Request) {
	identity := principal(request)
	workspace, conversation, id := identity.Workspace.ID, request.PathValue("id"), request.PathValue("suggestionId")
	current, err := a.ai.GetTriage(request.Context(), workspace, conversation, id)
	if err != nil {
		writeAIError(response, err)
		return
	}
	// Accepting all of it again changes nothing.
	if current.Outcome != nil && *current.Outcome == "accepted" {
		writeJSON(response, http.StatusOK, current)
		return
	}
	want := ai.TriageSet{TagIDs: current.TagIDs, Priority: current.Priority, AssigneeAgentID: current.AssigneeAgentID}
	if request.ContentLength != 0 {
		want = ai.TriageSet{}
		if err := decodeJSON(request, &want); err != nil {
			writeError(response, http.StatusBadRequest, "invalid_request", "Send tag_ids, priority and assignee_agent_id to accept, each from the suggestion; no body accepts the rest of it.")
			return
		}
	}
	set, err := a.ai.TriageToApply(request.Context(), workspace, conversation, id, want)
	if err != nil {
		writeAIError(response, err)
		return
	}
	// Tags are added one by one, as a person adds them: a tag someone added meanwhile stays.
	for _, tag := range set.TagIDs {
		if _, err := a.store.AddConversationTag(request.Context(), workspace, conversation, tag); err != nil {
			writeStoreError(response, err)
			return
		}
	}
	update := store.ConversationUpdate{Priority: set.Priority, ActorID: identity.Agent.ID}
	if set.AssigneeAgentID != nil {
		update.AssigneeID = &set.AssigneeAgentID
	}
	if update.Priority != nil || update.AssigneeID != nil {
		if _, err := a.store.UpdateConversation(request.Context(), workspace, conversation, update); err != nil {
			writeStoreError(response, err)
			return
		}
	}
	triage, err := a.ai.AcceptedTriage(request.Context(), workspace, aiActor(request), conversation, id, set)
	if err != nil {
		writeAIError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, triage)
}

func (a *API) dismissAITriage(response http.ResponseWriter, request *http.Request) {
	triage, err := a.ai.DismissTriage(request.Context(), principal(request).Workspace.ID, aiActor(request), request.PathValue("id"), request.PathValue("suggestionId"))
	if err != nil {
		writeAIError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, triage)
}
