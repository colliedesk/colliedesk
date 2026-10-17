package httpapi

import (
	"mime"
	"net/http"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/store"
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
		Kind string   `json:"kind"`
		Body string   `json:"body"`
		CC   []string `json:"cc"`
	}
	if err := decodeJSON(request, &input); err != nil || (input.Kind != "outgoing" && input.Kind != "note") || strings.TrimSpace(input.Body) == "" {
		writeError(response, http.StatusBadRequest, "invalid_request", "A reply or note body is required.")
		return
	}
	identity := principal(request)
	actor, ok := messageActor(response, identity, input.Kind)
	if !ok {
		return
	}
	// A conversation of another workspace is refused before anything about the Cc list is said.
	if _, err := a.store.GetConversation(request.Context(), identity.Workspace.ID, request.PathValue("id")); err != nil {
		writeStoreError(response, err)
		return
	}
	cc, refusal := a.cleanCC(request.Context(), identity.Workspace.ID, input.Kind, input.CC)
	if refusal != nil {
		a.writeUploadError(response, refusal)
		return
	}
	item, err := a.store.CreateMessageWithCC(request.Context(), identity.Workspace.ID, request.PathValue("id"), actor, input.Kind, input.Body, cc)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, item)
}

// messageActor refuses an agent token whose tier does not cover the message kind (a note needs
// draft, an outgoing reply needs send) and returns who writes the message. What an agent token
// writes is marked with the token so the timeline shows it as the token's work.
func messageActor(response http.ResponseWriter, identity auth.Principal, kind string) (store.Actor, bool) {
	need := auth.ScopeDraft
	if kind == "outgoing" {
		need = auth.ScopeSend
	}
	if !auth.ScopeAllows(identity.Scope, need) {
		writeScopeInsufficient(response, identity.Scope, need)
		return store.Actor{}, false
	}
	actor := store.Actor{AgentID: identity.Agent.ID}
	if identity.Scope != "" {
		actor.TokenID = identity.SessionID
	}
	return actor, true
}
