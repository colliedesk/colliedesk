package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

// errAnswered means a create already wrote its refusal to the response; nothing is remembered for its key.
var errAnswered = errors.New("the create already answered")

// The operations whose Idempotency-Key is remembered; one key names one request of each.
const (
	createKBArticleOperation    = "kb_article"
	createSavedReplyOperation   = "saved_reply"
	sendTestMessageOperation    = "test_message"
	createInviteOperation       = "invite"
	createAgentTokenOperation   = "agent_token"
	createConversationOperation = "conversation"
	createContactOperation      = "contact"
	createTagOperation          = "tag"
	createSavedViewOperation    = "saved_view"
	createKBCollectionOperation = "kb_collection"
	createChannelOperation      = "channel"
	addAIProviderOperation      = "ai_provider"
	problemReportOperation      = "problem_report"
	previewImportOperation      = "import_preview"
	previewAIChatOperation      = "ai_chat_preview"
	decideIntakeOperation       = "intake_decide"
	requeueIntakeOperation      = "intake_requeue"
	moveMessagesOperation       = "message_move"
	undoMessageMoveOperation    = "message_move_undo"
)

// shownOnce is a create's answer that holds a secret shown once (an invite link, a token): the first
// request answers shown, and its key remembers only kept, so the secret is never stored to answer a repeat.
type shownOnce struct{ shown, kept any }

// createOnce answers a create request that may carry an Idempotency-Key: the first request runs create and
// answers 201 with what it made; a repeat with the same key and input answers 200 with that same answer and
// runs nothing; the key with other input answers 422. input is what makes two requests the same one; the
// author is added to it. create writes its own refusals and returns errAnswered after doing so.
func (a *API) createOnce(response http.ResponseWriter, request *http.Request, operation string, input []string, create func(context.Context) (any, error)) {
	a.runOnce(response, request, http.StatusCreated, operation, input, create)
}

// runOnce is createOnce for a request whose first answer is first instead of 201, such as 202 for a job
// that goes on in the background; repeats still answer 200.
func (a *API) runOnce(response http.ResponseWriter, request *http.Request, first int, operation string, input []string, create func(context.Context) (any, error)) {
	key, ok := idempotencyKey(request)
	if !ok {
		writeInvalidIdempotencyKey(response)
		return
	}
	who := principal(request)
	fingerprint := store.RequestFingerprint(append([]string{who.Agent.ID}, input...)...)
	var shown any
	answer, replayed, err := a.store.CreateOnce(request.Context(), who.Workspace.ID, operation, key, fingerprint, func(ctx context.Context) (any, error) {
		made, err := create(ctx)
		if once, ok := made.(shownOnce); ok {
			shown = once.shown
			return once.kept, err
		}
		return made, err
	})
	switch {
	case errors.Is(err, errAnswered):
		return
	case errors.Is(err, store.ErrRequestKeyReused):
		writeError(response, http.StatusUnprocessableEntity, "idempotency_key_reused",
			"This Idempotency-Key was already used for another request. Send a new key for a new one.")
		return
	case errors.Is(err, store.ErrRequestInProgress):
		writeError(response, http.StatusConflict, "request_in_progress",
			"A request with this Idempotency-Key is still running. Try again in a moment to get its answer.")
		return
	case err != nil:
		writeInternalError(response, err)
		return
	}
	if replayed {
		writeJSON(response, http.StatusOK, json.RawMessage(answer))
		return
	}
	if shown != nil {
		writeJSON(response, first, shown)
		return
	}
	writeJSON(response, first, json.RawMessage(answer))
}
