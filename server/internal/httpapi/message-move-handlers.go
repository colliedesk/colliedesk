package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

// Moving customer messages between conversations (docs/user/inbox.md "Move messages"): to another
// conversation, into a new one, or back to a chat channel's intake queue, and undoing it. Nothing is sent
// to the customer.

func writeMoveError(response http.ResponseWriter, err error) {
	var invalid *store.MoveInvalidError
	switch {
	case errors.As(err, &invalid):
		writeError(response, http.StatusBadRequest, "invalid_request", invalid.Reason)
	case errors.Is(err, pgx.ErrNoRows):
		writeError(response, http.StatusNotFound, "not_found", "No conversation or move with that id in this workspace.")
	case errors.Is(err, store.ErrMoveSourceHasActivity):
		writeError(response, http.StatusConflict, "source_has_activity",
			"Someone replied in this conversation, wrote a note or assigned it, so it is not closed: move its messages without close_source.")
	case errors.Is(err, store.ErrMoveSuperseded):
		writeError(response, http.StatusConflict, "move_superseded", "A later move took some of these messages; undo that one first. Nothing changed.")
	case errors.Is(err, store.ErrMoveDecided):
		writeError(response, http.StatusConflict, "move_decided", "The intake card these messages went to was decided since; change that decision instead. Nothing changed.")
	case errors.Is(err, store.ErrNoIntakeQueue):
		writeError(response, http.StatusUnprocessableEntity, "no_intake_queue",
			"Only messages of a chat channel's group or chat with an intake queue go back to it; move these to a conversation instead.")
	default:
		writeStoreError(response, err)
	}
}

type moveMessagesInput struct {
	MessageIDs []string `json:"message_ids"`
	To         struct {
		ConversationID string `json:"conversation_id"`
		New            *struct {
			Subject string `json:"subject"`
		} `json:"new"`
		Queue *bool `json:"queue"`
	} `json:"to"`
	CloseSource bool `json:"close_source"`
}

func (a *API) moveConversationMessages(response http.ResponseWriter, request *http.Request) {
	id := request.PathValue("id")
	if !uuidShape.MatchString(id) {
		writeMoveError(response, pgx.ErrNoRows)
		return
	}
	var input moveMessagesInput
	if _, err := readJSONBody(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send message_ids and to: {conversation_id}, {new: {subject}} or {queue: true}.")
		return
	}
	target := store.MoveTarget{ConversationID: input.To.ConversationID}
	if input.To.New != nil {
		if input.To.New.Subject == "" {
			writeError(response, http.StatusBadRequest, "invalid_request", "A new conversation needs a subject.")
			return
		}
		target.NewSubject = input.To.New.Subject
	}
	if input.To.Queue != nil {
		if !*input.To.Queue {
			writeError(response, http.StatusBadRequest, "invalid_request", "to.queue is true when the messages go back to the intake queue.")
			return
		}
		target.Queue = true
	}
	// A press sent again (its Idempotency-Key) answers the first move and moves nothing more.
	fingerprint := append([]string{id, target.ConversationID, target.NewSubject, strconv.FormatBool(target.Queue), strconv.FormatBool(input.CloseSource)}, input.MessageIDs...)
	a.runOnce(response, request, http.StatusOK, moveMessagesOperation, fingerprint, func(ctx context.Context) (any, error) {
		result, err := a.store.MoveMessages(ctx, principal(request).Workspace.ID, id,
			store.MoveInput{MessageIDs: input.MessageIDs, To: target, CloseSource: input.CloseSource}, moveActor(request), a.now())
		if err != nil {
			writeMoveError(response, err)
			return nil, errAnswered
		}
		return result, nil
	})
}

func (a *API) listMessageMoves(response http.ResponseWriter, request *http.Request) {
	moves, err := a.store.MessageMovesOf(request.Context(), principal(request).Workspace.ID, request.PathValue("id"))
	if err != nil {
		writeMoveError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": moves})
}

func (a *API) undoMessageMove(response http.ResponseWriter, request *http.Request) {
	a.runOnce(response, request, http.StatusOK, undoMessageMoveOperation, []string{request.PathValue("id")}, func(ctx context.Context) (any, error) {
		move, err := a.store.UndoMessageMove(ctx, principal(request).Workspace.ID, request.PathValue("id"), moveActor(request), a.now())
		if err != nil {
			writeMoveError(response, err)
			return nil, errAnswered
		}
		return move, nil
	})
}

// moveActor is who a move is recorded for, as a saved reply change records it: the person, the agent token
// they used, and for an approved agent request the token that asked.
func moveActor(request *http.Request) store.Actor {
	return savedReplyActor(request.Context(), principal(request))
}
