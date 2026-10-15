package httpapi

import (
	"net/http"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/auth"
)

func (a *API) listMessages(response http.ResponseWriter, request *http.Request) {
	identity := principal(request)
	conversationID := request.PathValue("id")
	if _, err := a.store.GetConversation(request.Context(), identity.Workspace.ID, conversationID); err != nil {
		writeStoreError(response, err)
		return
	}
	items, err := a.store.ListMessages(request.Context(), identity.Workspace.ID, conversationID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

func (a *API) createMessage(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Kind string `json:"kind"`
		Body string `json:"body"`
	}
	if err := decodeJSON(request, &input); err != nil || (input.Kind != "outgoing" && input.Kind != "note") || strings.TrimSpace(input.Body) == "" {
		writeError(response, http.StatusBadRequest, "invalid_request", "A reply or note body is required.")
		return
	}
	identity := principal(request)
	need := auth.ScopeDraft
	if input.Kind == "outgoing" {
		need = auth.ScopeSend
	}
	if !auth.ScopeAllows(identity.Scope, need) {
		writeScopeInsufficient(response, identity.Scope, need)
		return
	}
	item, err := a.store.CreateMessage(request.Context(), identity.Workspace.ID, request.PathValue("id"), identity.Agent.ID, input.Kind, input.Body)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, item)
}
