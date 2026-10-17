package httpapi

import (
	"context"
	"encoding/json"
	"mime"
	"net/http"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
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

// messageRoute is the route a message is posted on; a call that waits for approval names it.
var messageRoute = Route{http.MethodPost, conversationRoutePrefix + "/messages", auth.PermissionConversationsWrite}

func (a *API) createMessage(response http.ResponseWriter, request *http.Request) {
	if mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type")); err == nil && mediaType == "multipart/form-data" {
		a.createMessageWithFiles(response, request)
		return
	}
	var input struct {
		Kind string   `json:"kind"`
		Body string   `json:"body"`
		CC   []string `json:"cc"`
		// Draft asks for a reply draft a person reviews, whatever the token's tier.
		Draft bool `json:"draft"`
	}
	if err := decodeJSON(request, &input); err != nil || (input.Kind != "outgoing" && input.Kind != "note") || strings.TrimSpace(input.Body) == "" {
		writeError(response, http.StatusBadRequest, "invalid_request", "A reply or note body is required.")
		return
	}
	identity := principal(request)
	if input.Draft && (identity.Scope == "" || input.Kind != "outgoing") {
		writeError(response, http.StatusBadRequest, "invalid_request", "Only an agent token drafts a reply for a person to review; a note or a person's reply is written directly.")
		return
	}
	actor, allowed := messageActor(request.Context(), identity, input.Kind)
	if !allowed || input.Draft {
		a.fileMessageRequest(response, request, identity, input.Kind, input.Body, input.CC)
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

// fileMessageRequest turns a message the token may not write into an agent request: a reply becomes a
// reply draft a person edits, sends or discards; a note from a read token waits like any other action.
func (a *API) fileMessageRequest(response http.ResponseWriter, request *http.Request, identity auth.Principal, kind, body string, cc []string) {
	call := map[string]any{"kind": kind, "body": body}
	if len(cc) > 0 {
		call["cc"] = cc
	}
	encoded, err := json.Marshal(call)
	if err != nil {
		writeInternalError(response, err)
		return
	}
	requestKind := domain.AgentRequestAction
	if kind == "outgoing" {
		requestKind = domain.AgentRequestReplyDraft
	}
	a.fileAgentRequest(response, request, messageRoute, identity, requestKind, encoded)
}

// messageActor returns who writes the message and whether the caller may write this kind now: a note
// needs draft, an outgoing reply needs send. What an agent token writes is marked with the token so
// the timeline shows it as the token's work; a reply sent by approving a token's draft is marked with
// that token and the request, and written by the approving person.
func messageActor(ctx context.Context, identity auth.Principal, kind string) (store.Actor, bool) {
	need := auth.ScopeDraft
	if kind == "outgoing" {
		need = auth.ScopeSend
	}
	if !auth.ScopeAllows(identity.Scope, need) {
		return store.Actor{}, false
	}
	actor := store.Actor{AgentID: identity.Agent.ID}
	if identity.Scope != "" {
		actor.TokenID = identity.SessionID
	}
	if replay, ok := approvalOf(ctx); ok {
		actor.TokenID, actor.RequestID = replay.tokenID, replay.requestID
	}
	return actor, true
}
