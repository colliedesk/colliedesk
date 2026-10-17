package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// savedReplyActor is who a saved reply change is recorded for: the person, the agent token they used,
// and, when a person approved an agent request, that person with the token that asked and the request.
func savedReplyActor(ctx context.Context, who auth.Principal) store.Actor {
	actor := store.Actor{AgentID: who.Agent.ID}
	if who.Scope != "" {
		actor.TokenID = who.SessionID
	}
	if replay, ok := approvalOf(ctx); ok {
		actor.TokenID, actor.RequestID = replay.tokenID, replay.requestID
	}
	return actor
}

// writeSavedReplyError maps the refusals of saved reply writes; anything else is a store failure.
func writeSavedReplyError(response http.ResponseWriter, err error) {
	var invalid *domain.InvalidSavedReplyError
	switch {
	case errors.As(err, &invalid):
		writeError(response, http.StatusBadRequest, "invalid_request", invalid.Message)
	case errors.Is(err, domain.ErrDuplicateTitle):
		writeError(response, http.StatusConflict, "duplicate_title", domain.ErrDuplicateTitle.Error()+". Titles are compared ignoring case.")
	case errors.Is(err, domain.ErrSavedReplyChanged):
		writeError(response, http.StatusConflict, "changed_elsewhere", "This saved reply was changed by someone else. Reload to see their version, then edit again.")
	case errors.Is(err, domain.ErrSavedReplyLimit):
		writeError(response, http.StatusConflict, "saved_reply_limit", domain.ErrSavedReplyLimit.Error()+".")
	case errors.Is(err, domain.ErrSavedReplyNotFound):
		writeError(response, http.StatusNotFound, "not_found", "The saved reply was not found.")
	case errors.Is(err, domain.ErrSavedReplyVersionNotFound):
		writeError(response, http.StatusNotFound, "not_found", "The saved reply has no version with that number.")
	default:
		writeStoreError(response, err)
	}
}

func (a *API) listSavedReplies(response http.ResponseWriter, request *http.Request) {
	items, err := a.store.ListSavedReplies(request.Context(), principal(request).Workspace.ID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

func (a *API) createSavedReply(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "A saved reply needs a title and text.")
		return
	}
	who := principal(request)
	reply, err := a.store.CreateSavedReply(request.Context(), who.Workspace.ID, savedReplyActor(request.Context(), who), input.Title, input.Body)
	if err != nil {
		writeSavedReplyError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, reply)
}

func (a *API) updateSavedReply(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Title     *string    `json:"title"`
		Body      *string    `json:"body"`
		UpdatedAt *time.Time `json:"updated_at"`
	}
	if err := decodeJSON(request, &input); err != nil || (input.Title == nil && input.Body == nil) || input.UpdatedAt == nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send the updated_at you last read, and a new title, new text, or both.")
		return
	}
	who := principal(request)
	reply, err := a.store.UpdateSavedReply(request.Context(), who.Workspace.ID, savedReplyActor(request.Context(), who), request.PathValue("id"), *input.UpdatedAt, input.Title, input.Body)
	if err != nil {
		writeSavedReplyError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, reply)
}

func (a *API) deleteSavedReply(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	ok, err := a.store.DeleteSavedReply(request.Context(), who.Workspace.ID, savedReplyActor(request.Context(), who), request.PathValue("id"))
	if err != nil {
		writeStoreError(response, err)
		return
	}
	if !ok {
		writeError(response, http.StatusNotFound, "not_found", "The saved reply was not found.")
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (a *API) listTrashedSavedReplies(response http.ResponseWriter, request *http.Request) {
	items, err := a.store.ListTrashedSavedReplies(request.Context(), principal(request).Workspace.ID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

func (a *API) restoreSavedReply(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	reply, err := a.store.UndeleteSavedReply(request.Context(), who.Workspace.ID, savedReplyActor(request.Context(), who), request.PathValue("id"))
	if errors.Is(err, domain.ErrSavedReplyNotFound) {
		writeError(response, http.StatusNotFound, "not_found", "No saved reply with this id is in the trash.")
		return
	}
	if err != nil {
		writeSavedReplyError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, reply)
}

func (a *API) listSavedReplyVersions(response http.ResponseWriter, request *http.Request) {
	items, err := a.store.SavedReplyVersions(request.Context(), principal(request).Workspace.ID, request.PathValue("id"))
	if err != nil {
		writeSavedReplyError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

func (a *API) restoreSavedReplyVersion(response http.ResponseWriter, request *http.Request) {
	version, err := strconv.Atoi(request.PathValue("version"))
	if err != nil || version < 1 {
		writeError(response, http.StatusNotFound, "not_found", "The saved reply has no version with that number.")
		return
	}
	var input struct {
		UpdatedAt *time.Time `json:"updated_at"`
	}
	if err := decodeJSON(request, &input); err != nil || input.UpdatedAt == nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send the updated_at you last read.")
		return
	}
	who := principal(request)
	reply, err := a.store.RestoreSavedReplyVersion(request.Context(), who.Workspace.ID, savedReplyActor(request.Context(), who), request.PathValue("id"), version, *input.UpdatedAt)
	if err != nil {
		writeSavedReplyError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, reply)
}
