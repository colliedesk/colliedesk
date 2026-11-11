package httpapi

import (
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
	var items []domain.Message
	var err error
	if request.URL.Query().Has("idempotency_key") {
		items, err = a.messagesSentUnderKey(response, request, conversationID)
		if errors.Is(err, errAnswered) {
			return
		}
	} else {
		items, err = a.store.ListMessages(request.Context(), identity.Workspace.ID, conversationID)
	}
	if err != nil {
		writeStoreError(response, err)
		return
	}
	// A person's screen gets remote images through the image proxy; an agent token reads the HTML as stored.
	if !identity.IsToken() {
		for i := range items {
			items[i].BodyHTML = a.presentMessageHTML(identity.Workspace.ID, items[i].BodyHTML)
		}
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

// messagesSentUnderKey answers the message a send with the idempotency_key query stored in the
// conversation: one item, or none when no send under that key arrived.
func (a *API) messagesSentUnderKey(response http.ResponseWriter, request *http.Request, conversationID string) ([]domain.Message, error) {
	key := request.URL.Query().Get("idempotency_key")
	if key == "" || !validIdempotencyKey(key) {
		writeInvalidIdempotencyKey(response)
		return nil, errAnswered
	}
	message, found, err := a.store.SentUnderKey(request.Context(), principal(request).Workspace.ID, conversationID, key)
	if err != nil || !found {
		return []domain.Message{}, err
	}
	return []domain.Message{message}, nil
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
		// Draft was a reply draft filed for approval; POST /reply-drafts writes one now.
		Draft bool `json:"draft"`
		// SeenReplyNumber is the latest reply the sender had seen when they started writing; a newer one
		// refuses the reply (409 reply_conflict). Without it nothing is checked.
		SeenReplyNumber *int `json:"seen_reply_number"`
		// AIDraftID is the AI draft the reply was written from: its outcome is recorded with the reply.
		AIDraftID string `json:"ai_draft_id"`
		// AIDraftFileIDs are the files of that draft the reply carries; left out, all of them.
		AIDraftFileIDs *[]string `json:"ai_draft_file_ids"`
	}
	if err := decodeJSON(request, &input); err != nil || (input.Kind != "outgoing" && input.Kind != "note") || strings.TrimSpace(input.Body) == "" ||
		(input.SeenReplyNumber != nil && *input.SeenReplyNumber < 0) {
		writeError(response, http.StatusBadRequest, "invalid_request", "A reply or note body is required.")
		return
	}
	key, ok := idempotencyKey(request)
	if !ok {
		writeInvalidIdempotencyKey(response)
		return
	}
	identity := principal(request)
	if input.Draft {
		writeError(response, http.StatusBadRequest, "invalid_request", "To leave a reply for a person to send, post it to /api/v1/conversations/{id}/reply-drafts.")
		return
	}
	actor, need := messageActor(identity, input.Kind)
	if !identity.Can(need) {
		hint := ""
		if input.Kind == "outgoing" {
			hint = "To leave the words for a person to send, post them to /api/v1/conversations/{id}/reply-drafts (colliedesk_reply_draft)."
		}
		a.writePermissionMissingHint(response, request, identity, hint, need)
		return
	}
	if input.AIDraftID != "" && input.Kind != "outgoing" {
		writeError(response, http.StatusBadRequest, "invalid_request", "ai_draft_id goes only with a reply, not with a note.")
		return
	}
	if input.AIDraftFileIDs != nil && input.AIDraftID == "" {
		writeError(response, http.StatusBadRequest, "invalid_request", "ai_draft_file_ids goes only with ai_draft_id, the draft whose files the reply carries.")
		return
	}
	actor.AIDraftID, actor.AIDraftFileIDs = input.AIDraftID, input.AIDraftFileIDs
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
	item, replayed, err := a.store.CreateReplyOnce(request.Context(), identity.Workspace.ID, request.PathValue("id"), key, actor, input.Kind, input.Body, cc, input.SeenReplyNumber)
	if a.writeReplyConflict(response, identity, err) || writeKeyReused(response, err) {
		return
	}
	if err != nil {
		writeStoreError(response, err)
		return
	}
	if replayed {
		writeJSON(response, http.StatusOK, item)
		return
	}
	a.recordReply(identity, input.Kind)
	writeJSON(response, http.StatusCreated, item)
}

// maxIdempotencyKey bounds the Idempotency-Key header; a UUID is 36 characters.
const maxIdempotencyKey = 200

// idempotencyKey reads the optional Idempotency-Key header of a new message: the client's name for one
// submit, the same on every retry of it. ok is false for a key that is too long or not printable ASCII.
func idempotencyKey(request *http.Request) (string, bool) {
	key := request.Header.Get("Idempotency-Key")
	if !validIdempotencyKey(key) {
		return "", false
	}
	return key, true
}

// validIdempotencyKey reports whether key is at most 200 printable ASCII characters without spaces.
func validIdempotencyKey(key string) bool {
	if len(key) > maxIdempotencyKey {
		return false
	}
	for index := 0; index < len(key); index++ {
		if key[index] < 0x21 || key[index] > 0x7e {
			return false
		}
	}
	return true
}

func writeInvalidIdempotencyKey(response http.ResponseWriter) {
	writeError(response, http.StatusBadRequest, "invalid_request", "Idempotency-Key must be 1 to 200 printable ASCII characters without spaces.")
}

// writeKeyReused answers a key that already names another message.
func writeKeyReused(response http.ResponseWriter, err error) bool {
	if !errors.Is(err, store.ErrIdempotencyKeyReused) {
		return false
	}
	writeError(response, http.StatusUnprocessableEntity, "idempotency_key_reused",
		"This Idempotency-Key was already used for another message. Send a new key for a new message.")
	return true
}

// messageActor returns who writes the message and the permission this kind needs: a note needs
// messages:note, an outgoing reply messages:send. What an agent token writes is marked with the token so
// the timeline shows it as the token's work.
func messageActor(identity auth.Principal, kind string) (store.Actor, string) {
	need := auth.PermMessagesNote
	if kind == "outgoing" {
		need = auth.PermMessagesSend
	}
	actor := store.Actor{AgentID: identity.Agent.ID}
	if identity.IsToken() {
		actor.TokenID = identity.SessionID
	}
	return actor, need
}

// writeReplyConflict answers 409 reply_conflict, with the replies the sender had not seen and the
// latest number to send with again, and reports whether err was that refusal.
func (a *API) writeReplyConflict(response http.ResponseWriter, identity auth.Principal, err error) bool {
	var conflict *domain.ReplyConflictError
	if !errors.As(err, &conflict) {
		return false
	}
	if !identity.IsToken() {
		for index := range conflict.Newer {
			conflict.Newer[index].BodyHTML = a.presentMessageHTML(identity.Workspace.ID, conflict.Newer[index].BodyHTML)
		}
	}
	writeJSON(response, http.StatusConflict, map[string]any{"error": errorBody(response, map[string]any{
		"code": "reply_conflict", "message": "Someone replied to this conversation after you started writing. Nothing was sent: read the new reply, then send again if yours still fits.",
		"latest_reply_number": conflict.Latest, "newer_replies": conflict.Newer})})
	return true
}
