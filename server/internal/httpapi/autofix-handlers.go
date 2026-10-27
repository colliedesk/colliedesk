package httpapi

import (
	"errors"
	"net/http"
	"slices"
	"strconv"

	"github.com/colliedesk/colliedesk/server/internal/autofix"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// writeAutofixError answers the errors fixes share; anything else goes to writeStoreError.
func writeAutofixError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, autofix.ErrFixNotFound):
		writeError(response, http.StatusNotFound, "fix_not_found", "No fix of this workspace has that id.")
	case errors.Is(err, autofix.ErrFixInProgress):
		writeError(response, http.StatusConflict, "fix_in_progress", autofix.ErrFixInProgress.Error()+".")
	case errors.Is(err, autofix.ErrClientKeyReused):
		writeError(response, http.StatusConflict, "client_key_reused", autofix.ErrClientKeyReused.Error()+".")
	case errors.Is(err, autofix.ErrFixChanged):
		writeError(response, http.StatusConflict, "fix_changed", "This fix changed since you read it. Nothing was changed; read it again.")
	case errors.Is(err, autofix.ErrMove):
		writeError(response, http.StatusConflict, "state_conflict", "This fix's state does not allow that. Nothing was changed; read it again.")
	case errors.Is(err, autofix.ErrBadPRURL):
		writeError(response, http.StatusBadRequest, "pull_request_not_in_repo", autofix.ErrBadPRURL.Error()+": https://github.com/<owner>/<repo>/pull/<number>.")
	case errors.Is(err, store.ErrBadCursor):
		writeError(response, http.StatusBadRequest, "invalid_request", "cursor must be the next_cursor of the previous page.")
	default:
		writeStoreError(response, err)
	}
}

func (a *API) getAutofixStatus(response http.ResponseWriter, request *http.Request) {
	status, err := a.store.AutofixStatus(request.Context(), principal(request).Workspace.ID)
	if err != nil {
		writeAutofixError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, status)
}

func (a *API) listFixes(response http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	filter := store.FixFilter{Group: query.Get("group"), Limit: 50, Cursor: query.Get("cursor")}
	if filter.Group != "" && !slices.Contains(autofix.Groups, filter.Group) {
		writeError(response, http.StatusBadRequest, "invalid_request", "group is needs_person, running or done.")
		return
	}
	if raw := query.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 100 {
			writeError(response, http.StatusBadRequest, "invalid_request", "limit must be a whole number from 1 to 100.")
			return
		}
		filter.Limit = limit
	}
	items, next, err := a.store.ListFixes(request.Context(), principal(request).Workspace.ID, filter)
	if err != nil {
		writeAutofixError(response, err)
		return
	}
	var cursor *string
	if next != "" {
		cursor = &next
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items, "next_cursor": cursor})
}

func (a *API) getFix(response http.ResponseWriter, request *http.Request) {
	fix, err := a.store.GetFix(request.Context(), principal(request).Workspace.ID, request.PathValue("id"))
	if err != nil {
		writeAutofixError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, fix)
}

func (a *API) listConversationFixes(response http.ResponseWriter, request *http.Request) {
	items, err := a.store.ListConversationFixes(request.Context(), principal(request).Workspace.ID, request.PathValue("id"))
	if err != nil {
		writeAutofixError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}
