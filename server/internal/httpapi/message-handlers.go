package httpapi

import (
	"mime"
	"net/http"
	"strings"
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
	if mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type")); err == nil && mediaType == "multipart/form-data" {
		a.createMessageWithFiles(response, request)
		return
	}
	var input struct {
		Kind string `json:"kind"`
		Body string `json:"body"`
	}
	if err := decodeJSON(request, &input); err != nil || (input.Kind != "outgoing" && input.Kind != "note") || strings.TrimSpace(input.Body) == "" {
		writeError(response, http.StatusBadRequest, "invalid_request", "A reply or note body is required.")
		return
	}
	identity := principal(request)
	item, err := a.store.CreateMessage(request.Context(), identity.Workspace.ID, request.PathValue("id"), identity.Agent.ID, input.Kind, input.Body)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, item)
}
