package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/auth"
)

// AI settings (docs/user/ai.md). The handlers read and change the workspace's providers and features;
// the checks that call a provider run here and wait for its answer, bounded by the service.

func aiActor(request *http.Request) ai.Actor {
	who := principal(request)
	actor := ai.Actor{AgentID: who.Agent.ID}
	if who.IsToken() {
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
	// The provider's key never enters the fingerprint (it is a secret, and a fingerprint is a plain hash): a
	// label is unique in the workspace, so the label, kind, address and models already name one provider.
	a.createOnce(response, request, addAIProviderOperation, append([]string{input.Kind, input.Label, input.BaseURL}, input.Models...), func(ctx context.Context) (any, error) {
		provider, err := a.ai.AddProvider(ctx, principal(request).Workspace.ID, aiActor(request),
			ai.ProviderInput{Kind: input.Kind, Label: input.Label, BaseURL: input.BaseURL, APIKey: input.APIKey, Models: input.Models})
		if err != nil {
			writeAIError(response, err)
			return nil, errAnswered
		}
		return provider, nil
	})
}

// listAIModels lists the models a provider's key may use, before the provider is saved, so a person
// picks them instead of typing ids. Nothing is stored.
func (a *API) listAIModels(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Kind    string `json:"kind"`
		BaseURL string `json:"base_url"`
		APIKey  string `json:"api_key"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send kind (anthropic or openai_compatible), api_key and, for openai_compatible, base_url.")
		return
	}
	models, err := a.ai.ListModels(request.Context(), principal(request).Workspace.ID, ai.ModelsInput{Kind: input.Kind, BaseURL: input.BaseURL, APIKey: input.APIKey})
	var check *ai.CheckError
	if errors.As(err, &check) {
		writeJSON(response, http.StatusUnprocessableEntity, map[string]any{"error": errorBody(response, map[string]any{
			"code": "ai_check_failed", "check_error_code": string(check.Code), "message": aiModelsMessage(check),
		})})
		return
	}
	if err != nil {
		writeAIError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"models": models})
}

// aiModelsMessage says why the models could not be listed. A server that keeps no list is common
// (some OpenAI-compatible servers), so the way on is to type the model names.
func aiModelsMessage(check *ai.CheckError) string {
	switch check.Code {
	case "model_not_found", "bad_request", "invalid_response":
		return "This provider does not list its models. Type the model names instead."
	case "auth":
		return "The provider refused this key. Copy it again from the provider's console."
	case "permission":
		return "This key may not list the provider's models. Type the model names instead."
	}
	return strings.Replace(aiCheckMessage(check), " Nothing was saved.", "", 1)
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
		// Absent: kept as they are.
		Automatic  *bool           `json:"automatic"`
		AutoParts  *[]string       `json:"auto_parts"`
		MonthlyCap json.RawMessage `json:"monthly_cap_micro_usd"`
	}
	if err := decodeJSON(request, &input); err != nil || input.Enabled == nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send source (server, provider or none), enabled, and optional provider_id, model and effort.")
		return
	}
	slotInput := ai.SlotInput{Source: input.Source, ProviderID: input.ProviderID, Model: input.Model, Effort: input.Effort, Enabled: *input.Enabled,
		Automatic: input.Automatic, AutoParts: autoParts(input.AutoParts)}
	// Absent: kept; null: removed.
	if len(input.MonthlyCap) > 0 {
		slotInput.MonthlyCapSet = true
		if string(input.MonthlyCap) != "null" {
			var cap int64
			if err := json.Unmarshal(input.MonthlyCap, &cap); err != nil {
				writeError(response, http.StatusBadRequest, "invalid_request", "monthly_cap_micro_usd is a whole number of micro-dollars, or null.")
				return
			}
			slotInput.MonthlyCap = &cap
		}
	}
	slot, err := a.ai.SetSlot(request.Context(), principal(request).Workspace.ID, aiActor(request), request.PathValue("slot"), slotInput)
	if err != nil {
		writeAIError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, slot)
}

func autoParts(parts *[]string) []string {
	if parts == nil {
		return nil
	}
	if *parts == nil {
		return []string{}
	}
	return *parts
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
		logMissingSettings("ai_provider_keys", "MAILBOX_TOKEN_KEYS")
		writeError(response, http.StatusConflict, "secret_keys_missing", "This server cannot store API keys yet: its key that encrypts them is not set up, so ask whoever runs it. Nothing was saved.")
	case errors.Is(err, ai.ErrLimit):
		writeError(response, http.StatusConflict, "ai_provider_limit", "A workspace can have 5 AI providers. Delete one to add another.")
	case errors.Is(err, ai.ErrLabelTaken):
		writeError(response, http.StatusConflict, "ai_provider_name_taken", "Another provider of this workspace has that name.")
	case errors.Is(err, ai.ErrServerProviderOff):
		writeFeatureOff(response)
	case errors.Is(err, ai.ErrNoServerProvider):
		writeError(response, http.StatusConflict, "no_server_provider", "This server has no AI provider for that feature. Add your own provider, or ask the server operator.")
	case errors.Is(err, ai.ErrFeatureOff):
		writeError(response, http.StatusConflict, "ai_feature_off", "This AI feature is off in this workspace, or its provider is gone. An admin turns it on in Settings → Built-in AI.")
	case errors.Is(err, ai.ErrDraftsRateLimited):
		response.Header().Set("Retry-After", "60")
		writeError(response, http.StatusTooManyRequests, "ai_drafts_rate_limited", "At most 30 AI drafts a minute in a workspace; try again shortly.")
	case errors.Is(err, ai.ErrDraftNotFound):
		writeError(response, http.StatusNotFound, "not_found", "No AI draft with that id on this conversation.")
	case errors.Is(err, ai.ErrConversationNotFound):
		writeError(response, http.StatusNotFound, "not_found", "No conversation with that id in this workspace.")
	case errors.Is(err, ai.ErrTriageNotFound):
		writeError(response, http.StatusNotFound, "not_found", "No AI triage suggestion with that id on this conversation.")
	case errors.Is(err, ai.ErrTriageNotAutoApplied):
		writeError(response, http.StatusConflict, "ai_triage_not_auto_applied", "AI applied nothing of this suggestion by itself, so there is nothing to undo.")
	case errors.Is(err, ai.ErrTriageNothing):
		writeError(response, http.StatusConflict, "ai_triage_nothing", "This suggestion names nothing to apply: the conversation already has it, or a person set it first.")
	case errors.Is(err, ai.ErrTriageDecided):
		writeError(response, http.StatusConflict, "ai_triage_decided", "This suggestion is not ready, or was already accepted, dismissed or replaced.")
	case errors.Is(err, ai.ErrDraftDecided):
		writeError(response, http.StatusConflict, "ai_draft_decided", "This AI draft was already sent, discarded or replaced.")
	case errors.Is(err, ai.ErrAllowanceReached):
		writeError(response, http.StatusPaymentRequired, "ai_allowance_reached", "This month's included AI is used up. Add your own provider in Settings → Built-in AI, or move to a larger plan.")
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
		return "The key works but may not use " + checkedModel(check) + ". Tick another model first. Nothing was saved."
	case "model_not_found":
		return "The provider has no model " + checkedModel(check) + ", or this key may not use it. Tick another model first. Nothing was saved."
	case "blocked_address":
		return "This address is on a private network, which this server does not call for workspaces. Use a public https address, or ask the server operator to allow private addresses."
	case "quota_exhausted":
		return "The provider account is out of credit. Add credit there, then try again. Nothing was saved."
	case "timeout":
		return "The provider did not answer in time. Try again. Nothing was saved."
	case "rate_limited", "overloaded":
		return "The provider is busy right now. Try again in a minute. Nothing was saved."
	case "bad_request", "too_large":
		return "The provider refused the request for " + checkedModel(check) + "; it may not support structured output. Tick another model first. Nothing was saved."
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
		PerDay      *int `json:"per_day"`
		PerMinute   *int `json:"per_minute"`
		DraftPerDay *int `json:"draft_per_day"`
	}
	if err := decodeJSON(request, &input); err != nil || input.PerDay == nil || input.PerMinute == nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send per_day (0 to 10000) and per_minute (0 to 1000), and draft_per_day (0 to 10000) to change it.")
		return
	}
	limits, err := a.ai.SetAutomaticLimits(request.Context(), principal(request).Workspace.ID, aiActor(request), *input.PerDay, *input.PerMinute, input.DraftPerDay)
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
	triage, autoApplied, err := a.ai.LatestTriage(request.Context(), principal(request).Workspace.ID, request.PathValue("id"))
	if err != nil {
		writeAIError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"suggestion": triage, "auto_applied": autoApplied})
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
// body, or with one naming nothing) to the conversation, as a person's change, and records it.
// Accepting or undoing a suggestion changes the conversation's tags, priority and assignee, as
// PATCH /api/v1/conversations/{id} does: a token also needs conversations:update for it.
var (
	aiTriageAcceptRoute = Route{http.MethodPost, "/api/v1/conversations/{id}/ai-triage/{suggestionId}/accept", auth.PermissionConversationsWrite}
	aiTriageUndoRoute   = Route{http.MethodPost, "/api/v1/conversations/{id}/ai-triage/{suggestionId}/undo", auth.PermissionConversationsWrite}
)

func (a *API) acceptAITriage(response http.ResponseWriter, request *http.Request) {
	identity := principal(request)
	if a.askUnlessHeld(response, request, aiTriageAcceptRoute, identity, auth.PermConversationsUpdate, nil, nil) {
		return
	}
	var want ai.TriageSet
	// A call from MCP without arguments has no body at all.
	if request.Body == nil {
		request.Body = http.NoBody
	}
	if err := decodeJSON(request, &want); err != nil && !errors.Is(err, io.EOF) {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send tag_ids, priority and assignee_agent_id to accept, each from the suggestion; no body accepts the rest of it.")
		return
	}
	triage, err := a.ai.AcceptTriage(request.Context(), identity.Workspace.ID, aiActor(request), request.PathValue("id"), request.PathValue("suggestionId"), &want)
	if err != nil {
		writeAIError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, triage)
}

// undoAITriage takes back what AI applied of a suggestion by itself: the parts named (tags, priority,
// assignee), or all of it without a body.
func (a *API) undoAITriage(response http.ResponseWriter, request *http.Request) {
	if a.askUnlessHeld(response, request, aiTriageUndoRoute, principal(request), auth.PermConversationsUpdate, nil, nil) {
		return
	}
	var input struct {
		Parts []string `json:"parts"`
	}
	if request.Body == nil {
		request.Body = http.NoBody
	}
	if err := decodeJSON(request, &input); err != nil && !errors.Is(err, io.EOF) {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send parts (tags, priority, assignee) to undo some of it; no body undoes all of it.")
		return
	}
	triage, err := a.ai.UndoTriage(request.Context(), principal(request).Workspace.ID, aiActor(request), request.PathValue("id"), request.PathValue("suggestionId"), input.Parts)
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

// checkedModel names the model a check asked for, as people read it in Settings.
func checkedModel(check *ai.CheckError) string {
	if check.Model == "" {
		return "the default model"
	}
	return `"` + check.Model + `"`
}
