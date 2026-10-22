package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// AI drafts and triage suggestions (docs/internals/ai-drafts.md). The conversation screen reads the
// draft on its own; it never waits for a model.

func writeAIError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrAIUnavailable):
		writeError(response, http.StatusConflict, "ai_unavailable", "This server has no AI provider. The administrator of the server sets one up (AI_SERVER_PROVIDER and AI_SERVER_API_KEY).")
	case errors.Is(err, domain.ErrAIDraftsOff):
		writeError(response, http.StatusConflict, "ai_drafts_off", "AI drafts are turned off for this workspace. An admin turns them on in Settings, AI.")
	case errors.Is(err, domain.ErrAIDraftNotFound):
		writeError(response, http.StatusNotFound, "not_found", "The conversation or its AI draft was not found.")
	case errors.Is(err, domain.ErrAIDraftState):
		writeError(response, http.StatusConflict, "ai_draft_state", "The AI draft can no longer be changed this way. Nothing was changed.")
	default:
		writeStoreError(response, err)
	}
}

// aiDraftPath reads the conversation id and, when the route has one, the draft id; anything that is
// not an id is a draft that does not exist.
func aiDraftPath(response http.ResponseWriter, request *http.Request, withDraft bool) (string, string, bool) {
	conversationID := strings.ToLower(request.PathValue("id"))
	draftID := strings.ToLower(request.PathValue("draftId"))
	if !contactIDPattern.MatchString(conversationID) || (withDraft && !contactIDPattern.MatchString(draftID)) {
		writeAIError(response, domain.ErrAIDraftNotFound)
		return "", "", false
	}
	return conversationID, draftID, true
}

func (a *API) getAIDraft(response http.ResponseWriter, request *http.Request) {
	conversationID, _, ok := aiDraftPath(response, request, false)
	if !ok {
		return
	}
	workspaceID := principal(request).Workspace.ID
	draft, err := a.store.LatestAIDraft(request.Context(), workspaceID, conversationID)
	if err != nil {
		writeAIError(response, err)
		return
	}
	settings, err := a.store.AISettings(request.Context(), workspaceID)
	if err != nil {
		writeAIError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"draft": draft, "available": settings.Available, "drafts_enabled": settings.DraftsEnabled})
}

// aiDraftRequestsPerHour is how many new drafts one workspace may ask for in an hour, by hand or by an
// agent token; drafts written when customers write do not count.
const aiDraftRequestsPerHour = 60

func (a *API) requestAIDraft(response http.ResponseWriter, request *http.Request) {
	conversationID, _, ok := aiDraftPath(response, request, false)
	if !ok {
		return
	}
	if allowed, wait := a.aiDraftRequests.allow(principal(request).Workspace.ID, a.now()); !allowed {
		response.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		writeError(response, http.StatusTooManyRequests, "rate_limited", fmt.Sprintf("A workspace can ask for %d new AI drafts an hour. Try again later.", aiDraftRequestsPerHour))
		return
	}
	draft, err := a.store.RequestAIDraft(request.Context(), principal(request).Workspace.ID, conversationID, principal(request).Agent.ID)
	if err != nil {
		writeAIError(response, err)
		return
	}
	writeJSON(response, http.StatusAccepted, map[string]any{"draft": draft})
}

func (a *API) discardAIDraft(response http.ResponseWriter, request *http.Request) {
	a.changeAIDraft(response, request, a.store.DiscardAIDraft)
}

func (a *API) cancelAutoAnswer(response http.ResponseWriter, request *http.Request) {
	a.changeAIDraft(response, request, a.store.CancelAutoAnswer)
}

func (a *API) changeAIDraft(response http.ResponseWriter, request *http.Request, change func(ctx context.Context, workspaceID, conversationID, draftID, agentID string) (domain.AIDraft, error)) {
	conversationID, draftID, ok := aiDraftPath(response, request, true)
	if !ok {
		return
	}
	draft, err := change(request.Context(), principal(request).Workspace.ID, conversationID, draftID, principal(request).Agent.ID)
	if err != nil {
		writeAIError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"draft": draft})
}

func (a *API) applyAITriage(response http.ResponseWriter, request *http.Request) {
	conversationID, draftID, ok := aiDraftPath(response, request, true)
	if !ok {
		return
	}
	workspaceID := principal(request).Workspace.ID
	draft, err := a.store.ApplyAITriage(request.Context(), workspaceID, conversationID, draftID, principal(request).Agent.ID)
	if err != nil {
		writeAIError(response, err)
		return
	}
	conversation, err := a.store.GetConversation(request.Context(), workspaceID, conversationID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"draft": draft, "conversation": conversation})
}

// aiUsageWindow is the period the settings page counts usage over.
const aiUsageWindow = 30 * 24 * time.Hour

func (a *API) getAISettings(response http.ResponseWriter, request *http.Request) {
	workspaceID := principal(request).Workspace.ID
	settings, err := a.store.AISettings(request.Context(), workspaceID)
	if err != nil {
		writeAIError(response, err)
		return
	}
	topics, err := a.store.AITopicRecords(request.Context(), workspaceID)
	if err != nil {
		writeAIError(response, err)
		return
	}
	usage, err := a.store.AIUsageSummary(request.Context(), workspaceID, a.now().Add(-aiUsageWindow))
	if err != nil {
		writeAIError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"settings": settings, "topics": topics, "usage": usage})
}

func (a *API) updateAISettings(response http.ResponseWriter, request *http.Request) {
	var input struct {
		DraftsEnabled          *bool `json:"drafts_enabled"`
		AutoAnswerEnabled      *bool `json:"auto_answer_enabled"`
		AutoAnswerDelaySeconds *int  `json:"auto_answer_delay_seconds"`
		AutoAnswerMinReviewed  *int  `json:"auto_answer_min_reviewed"`
		AutoAnswerMinAccuracy  *int  `json:"auto_answer_min_accuracy"`
	}
	inRange := func(value *int, low, high int) bool { return value == nil || (*value >= low && *value <= high) }
	if err := decodeJSON(request, &input); err != nil || !inRange(input.AutoAnswerDelaySeconds, 0, 86400) ||
		!inRange(input.AutoAnswerMinReviewed, 3, 1000) || !inRange(input.AutoAnswerMinAccuracy, 50, 100) {
		writeError(response, http.StatusBadRequest, "invalid_request",
			"Send any of drafts_enabled, auto_answer_enabled (true or false), auto_answer_delay_seconds (0 to 86400), auto_answer_min_reviewed (3 to 1000) and auto_answer_min_accuracy (50 to 100).")
		return
	}
	settings, err := a.store.UpdateAISettings(request.Context(), principal(request).Workspace.ID, principal(request).Agent.ID, store.AISettingsChange{
		DraftsEnabled: input.DraftsEnabled, AutoAnswerEnabled: input.AutoAnswerEnabled, AutoAnswerDelaySeconds: input.AutoAnswerDelaySeconds,
		AutoAnswerMinReviewed: input.AutoAnswerMinReviewed, AutoAnswerMinAccuracy: input.AutoAnswerMinAccuracy})
	if err != nil {
		writeAIError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, settings)
}
