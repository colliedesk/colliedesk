package httpapi

import (
	"errors"
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// writeSavedReplyError maps the refusals of saved reply writes; anything else is a store failure.
func writeSavedReplyError(response http.ResponseWriter, err error) {
	var invalid *domain.InvalidSavedReplyError
	switch {
	case errors.As(err, &invalid):
		writeError(response, http.StatusBadRequest, "invalid_request", invalid.Message)
	case errors.Is(err, domain.ErrDuplicateTitle):
		writeError(response, http.StatusConflict, "duplicate_title", domain.ErrDuplicateTitle.Error()+". Titles are compared ignoring case.")
	case errors.Is(err, domain.ErrSavedReplyLimit):
		writeError(response, http.StatusConflict, "saved_reply_limit", domain.ErrSavedReplyLimit.Error()+".")
	case errors.Is(err, domain.ErrSavedReplyNotFound):
		writeError(response, http.StatusNotFound, "not_found", "The saved reply was not found.")
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
	reply, err := a.store.CreateSavedReply(request.Context(), who.Workspace.ID, who.Agent.ID, input.Title, input.Body)
	if err != nil {
		writeSavedReplyError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, reply)
}

func (a *API) updateSavedReply(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Title *string `json:"title"`
		Body  *string `json:"body"`
	}
	if err := decodeJSON(request, &input); err != nil || (input.Title == nil && input.Body == nil) {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send a new title, new text, or both.")
		return
	}
	reply, err := a.store.UpdateSavedReply(request.Context(), principal(request).Workspace.ID, request.PathValue("id"), input.Title, input.Body)
	if err != nil {
		writeSavedReplyError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, reply)
}

func (a *API) deleteSavedReply(response http.ResponseWriter, request *http.Request) {
	ok, err := a.store.DeleteSavedReply(request.Context(), principal(request).Workspace.ID, request.PathValue("id"))
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
