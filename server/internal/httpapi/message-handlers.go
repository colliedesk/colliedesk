package httpapi

import (
	"context"
	"encoding/json"
	"errors"
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
	// A person's screen gets remote images through the image proxy; an agent token reads the HTML as stored.
	if identity.Scope == "" {
		for i := range items {
			items[i].BodyHTML = a.presentMessageHTML(identity.Workspace.ID, items[i].BodyHTML)
		}
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
		// SeenReplyNumber is the latest reply the sender had seen when they started writing; a newer one
		// refuses the reply (409 reply_conflict). Without it nothing is checked.
		SeenReplyNumber *int `json:"seen_reply_number"`
		// AIDraftID names the AI draft the reply was written from, so its outcome is recorded.
		AIDraftID string `json:"ai_draft_id"`
	}
	if err := decodeJSON(request, &input); err != nil || (input.Kind != "outgoing" && input.Kind != "note") || strings.TrimSpace(input.Body) == "" ||
		(input.SeenReplyNumber != nil && *input.SeenReplyNumber < 0) || (input.AIDraftID != "" && (input.Kind != "outgoing" || !contactIDPattern.MatchString(strings.ToLower(input.AIDraftID)))) {
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
	actor.AIDraftID = strings.ToLower(input.AIDraftID)
	item, err := a.store.CreateReply(request.Context(), identity.Workspace.ID, request.PathValue("id"), actor, input.Kind, input.Body, cc, input.SeenReplyNumber)
	if a.writeReplyConflict(response, identity, err) {
		return
	}
	if err != nil {
		writeAIError(response, err)
		return
	}
	a.recordReply(identity, input.Kind)
	writeJSON(response, http.StatusCreated, item)
}

// fileMessageRequest turns a message the token may not write into an agent request: a reply becomes a
// reply draft a person edits, sends or discards; a note from a read token waits like any other action.
// A reply draft names every recipient in its summary, and a token below send may copy only people who
// are already on the conversation, so a draft cannot slip a new address past the person who sends it.
func (a *API) fileMessageRequest(response http.ResponseWriter, request *http.Request, identity auth.Principal, kind, body string, cc []string) {
	conversationID := request.PathValue("id")
	call := map[string]any{"kind": kind, "body": body}
	requestKind, summary := domain.AgentRequestAction, ""
	if kind == "outgoing" {
		requestKind = domain.AgentRequestReplyDraft
		clean, refusal := a.cleanCC(request.Context(), identity.Workspace.ID, kind, cc)
		if refusal != nil {
			a.writeUploadError(response, refusal)
			return
		}
		contact, known, err := a.store.ConversationParticipants(request.Context(), identity.Workspace.ID, conversationID)
		if err != nil {
			writeStoreError(response, err)
			return
		}
		if !auth.ScopeAllows(identity.Scope, auth.ScopeSend) {
			for _, address := range clean {
				if !known[address] {
					writeError(response, http.StatusBadRequest, "invalid_request", address+" is not on this conversation. "+
						"A draft from a token below send may copy only the customer and people already copied; a person can add others when sending.")
					return
				}
			}
		}
		if len(clean) > 0 {
			call["cc"] = clean
		}
		summary = replySummary(contact, clean, body)
	} else if len(cc) > 0 {
		call["cc"] = cc
	}
	encoded, err := json.Marshal(call)
	if err != nil {
		writeInternalError(response, err)
		return
	}
	a.fileAgentRequest(response, request, messageRoute, identity, requestKind, encoded, summary)
}

// replySummary names every recipient before the text, so a copy is never hidden behind a long reply.
func replySummary(contact string, cc []string, body string) string {
	recipients := "to " + contact
	if len(cc) > 0 {
		recipients += ", cc " + strings.Join(cc, ", ")
	}
	return "Reply to the customer (" + recipients + "): " + strings.Join(strings.Fields(body), " ")
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
		// A reply whose text the approver changed is theirs; it still names the request it came from.
		actor.RequestID = replay.requestID
		if !replay.textEdited {
			actor.TokenID = replay.tokenID
		}
	}
	return actor, true
}

// writeReplyConflict answers 409 reply_conflict, with the replies the sender had not seen and the
// latest number to send with again, and reports whether err was that refusal.
func (a *API) writeReplyConflict(response http.ResponseWriter, identity auth.Principal, err error) bool {
	var conflict *domain.ReplyConflictError
	if !errors.As(err, &conflict) {
		return false
	}
	if identity.Scope == "" {
		for index := range conflict.Newer {
			conflict.Newer[index].BodyHTML = a.presentMessageHTML(identity.Workspace.ID, conflict.Newer[index].BodyHTML)
		}
	}
	writeJSON(response, http.StatusConflict, map[string]any{"error": errorBody(response, map[string]any{
		"code": "reply_conflict", "message": "Someone replied to this conversation after you started writing. Nothing was sent: read the new reply, then send again if yours still fits.",
		"latest_reply_number": conflict.Latest, "newer_replies": conflict.Newer})})
	return true
}
