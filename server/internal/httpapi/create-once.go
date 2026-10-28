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
	createKBArticleOperation  = "kb_article"
	createSavedReplyOperation = "saved_reply"
	sendTestMessageOperation  = "test_message"
)

// createOnce answers a create request that may carry an Idempotency-Key: the first request runs create and
// answers 201 with what it made; a repeat with the same key and input answers 200 with that same answer and
// runs nothing; the key with other input answers 422. input is what makes two requests the same one; the
// author is added to it. create writes its own refusals and returns errAnswered after doing so.
func (a *API) createOnce(response http.ResponseWriter, request *http.Request, operation string, input []string, create func(context.Context) (any, error)) {
	key, ok := idempotencyKey(request)
	if !ok {
		writeInvalidIdempotencyKey(response)
		return
	}
	who := principal(request)
	fingerprint := store.RequestFingerprint(append([]string{who.Agent.ID}, input...)...)
	answer, replayed, err := a.store.CreateOnce(request.Context(), who.Workspace.ID, operation, key, fingerprint, create)
	switch {
	case errors.Is(err, errAnswered):
		return
	case errors.Is(err, store.ErrRequestKeyReused):
		writeError(response, http.StatusUnprocessableEntity, "idempotency_key_reused",
			"This Idempotency-Key was already used for another request. Send a new key for a new one.")
		return
	case err != nil:
		writeInternalError(response, err)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	writeJSON(response, status, json.RawMessage(answer))
}
