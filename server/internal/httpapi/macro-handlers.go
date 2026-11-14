package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

func writeMacroError(response http.ResponseWriter, err error) {
	var invalid *domain.InvalidMacroError
	switch {
	case errors.As(err, &invalid):
		writeError(response, http.StatusBadRequest, "invalid_request", invalid.Message)
	case errors.Is(err, domain.ErrMacroTitleTaken):
		writeError(response, http.StatusConflict, "duplicate_title", "A macro with this title already exists. Titles are compared ignoring case, among your personal macros and among the shared ones.")
	case errors.Is(err, domain.ErrMacroLimit):
		writeError(response, http.StatusConflict, "macro_limit", "There are already 500 shared macros in this workspace, or 100 personal ones of yours, counting only the ones not in the trash. Delete one first.")
	case errors.Is(err, domain.ErrMacroChanged):
		writeError(response, http.StatusConflict, "changed_elsewhere", "This macro was changed by someone else. Reload to see their version, then edit again.")
	case errors.Is(err, domain.ErrMacroNotFound):
		writeError(response, http.StatusNotFound, "not_found", "That macro was not found in this workspace.")
	case errors.Is(err, domain.ErrMacroApplicationGone):
		writeError(response, http.StatusNotFound, "not_found", "That macro application was not found on this conversation.")
	case errors.Is(err, domain.ErrMacroConversationGone):
		writeError(response, http.StatusNotFound, "not_found", "That conversation was not found in this workspace.")
	case errors.Is(err, domain.ErrMacroTagUnknown):
		writeError(response, http.StatusBadRequest, "invalid_request", "A tag on this macro is not in this workspace. Read the tags and send those ids.")
	case errors.Is(err, domain.ErrMacroAssignee):
		writeError(response, http.StatusBadRequest, "invalid_request", "The assignee is not an active member of this workspace. Read the team and send that agent's id, or null to leave the conversation unassigned.")
	default:
		writeStoreError(response, err)
	}
}

func macroID(response http.ResponseWriter, request *http.Request, name string, missing error) (string, bool) {
	id := strings.ToLower(request.PathValue(name))
	if !contactIDPattern.MatchString(id) {
		writeMacroError(response, missing)
		return "", false
	}
	return id, true
}

func (a *API) listMacros(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	items, err := a.store.ListMacros(request.Context(), who.Workspace.ID, who.Agent.ID)
	if err != nil {
		writeMacroError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, struct {
		Items []domain.Macro `json:"items"`
	}{items})
}

func (a *API) listTrashedMacros(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	items, err := a.store.ListDeletedMacros(request.Context(), who.Workspace.ID, who.Agent.ID)
	if err != nil {
		writeMacroError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, struct {
		Items []domain.Macro `json:"items"`
	}{items})
}

func (a *API) createMacro(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Title   string              `json:"title"`
		Body    string              `json:"body"`
		Scope   string              `json:"scope"`
		Actions domain.MacroActions `json:"actions"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "A macro needs a title, a scope (personal or shared) and, when it has them, actions.")
		return
	}
	title, body, scope, actions, err := domain.CleanMacro(input.Title, input.Body, input.Scope, input.Actions)
	if err != nil {
		writeMacroError(response, err)
		return
	}
	who := principal(request)
	a.createOnce(response, request, createMacroOperation, []string{title, body, scope, string(mustJSON(actions))}, func(ctx context.Context) (any, error) {
		macro, err := a.store.CreateMacro(ctx, who.Workspace.ID, who.Agent.ID, title, body, scope, actions)
		if err != nil {
			writeMacroError(response, err)
			return nil, errAnswered
		}
		return macro, nil
	})
}

func (a *API) updateMacro(response http.ResponseWriter, request *http.Request) {
	id, ok := macroID(response, request, "id", domain.ErrMacroNotFound)
	if !ok {
		return
	}
	var input struct {
		UpdatedAt *time.Time           `json:"updated_at"`
		Title     *string              `json:"title"`
		Body      *string              `json:"body"`
		Scope     *string              `json:"scope"`
		Actions   *domain.MacroActions `json:"actions"`
	}
	if err := decodeJSON(request, &input); err != nil || input.UpdatedAt == nil || (input.Title == nil && input.Body == nil && input.Scope == nil && input.Actions == nil) {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send the updated_at you last read, and a new title, text, scope or actions.")
		return
	}
	who := principal(request)
	current, err := a.store.ListMacros(request.Context(), who.Workspace.ID, who.Agent.ID)
	if err != nil {
		writeMacroError(response, err)
		return
	}
	var found *domain.Macro
	for i := range current {
		if current[i].ID == id {
			found = &current[i]
			break
		}
	}
	if found == nil {
		writeMacroError(response, domain.ErrMacroNotFound)
		return
	}
	title, body, scope, actions := found.Title, found.Body, found.Scope, found.Actions
	if input.Title != nil {
		title = *input.Title
	}
	if input.Body != nil {
		body = *input.Body
	}
	if input.Scope != nil {
		scope = *input.Scope
	}
	if input.Actions != nil {
		actions = *input.Actions
	}
	title, body, scope, actions, err = domain.CleanMacro(title, body, scope, actions)
	if err != nil {
		writeMacroError(response, err)
		return
	}
	change := store.MacroChange{Seen: *input.UpdatedAt, Title: &title, Body: &body, Scope: &scope, Actions: &actions}
	macro, err := a.store.UpdateMacro(request.Context(), who.Workspace.ID, who.Agent.ID, id, change)
	if err != nil {
		writeMacroError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, macro)
}

func (a *API) deleteMacro(response http.ResponseWriter, request *http.Request) {
	id, ok := macroID(response, request, "id", domain.ErrMacroNotFound)
	if !ok {
		return
	}
	seen, err := time.Parse(time.RFC3339Nano, request.URL.Query().Get("updated_at"))
	if err != nil {
		seen, err = time.Parse(time.RFC3339, request.URL.Query().Get("updated_at"))
	}
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send the updated_at you last read, as updated_at.")
		return
	}
	who := principal(request)
	macro, err := a.store.DeleteMacro(request.Context(), who.Workspace.ID, who.Agent.ID, id, seen)
	if err != nil {
		writeMacroError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, macro)
}

func (a *API) restoreMacro(response http.ResponseWriter, request *http.Request) {
	id, ok := macroID(response, request, "id", domain.ErrMacroNotFound)
	if !ok {
		return
	}
	who := principal(request)
	a.runOnce(response, request, http.StatusOK, restoreMacroOperation, []string{id}, func(ctx context.Context) (any, error) {
		macro, err := a.store.RestoreMacro(ctx, who.Workspace.ID, who.Agent.ID, id)
		if err != nil {
			writeMacroError(response, err)
			return nil, errAnswered
		}
		return macro, nil
	})
}

func (a *API) applyMacro(response http.ResponseWriter, request *http.Request) {
	conversationID, ok := macroID(response, request, "id", domain.ErrMacroConversationGone)
	if !ok {
		return
	}
	macroIDValue, ok := macroID(response, request, "macroId", domain.ErrMacroNotFound)
	if !ok {
		return
	}
	who := principal(request)
	a.runOnce(response, request, http.StatusOK, applyMacroOperation, []string{conversationID, macroIDValue}, func(ctx context.Context) (any, error) {
		result, err := a.store.ApplyMacro(ctx, who.Workspace.ID, who.Agent.ID, tokenOf(request), conversationID, macroIDValue)
		if err != nil {
			writeMacroError(response, err)
			return nil, errAnswered
		}
		return result, nil
	})
}

func (a *API) undoMacro(response http.ResponseWriter, request *http.Request) {
	conversationID, ok := macroID(response, request, "id", domain.ErrMacroConversationGone)
	if !ok {
		return
	}
	applicationID, ok := macroID(response, request, "applicationId", domain.ErrMacroApplicationGone)
	if !ok {
		return
	}
	who := principal(request)
	a.runOnce(response, request, http.StatusOK, undoMacroOperation, []string{conversationID, applicationID}, func(ctx context.Context) (any, error) {
		result, err := a.store.UndoMacro(ctx, who.Workspace.ID, who.Agent.ID, tokenOf(request), conversationID, applicationID)
		if err != nil {
			writeMacroError(response, err)
			return nil, errAnswered
		}
		return result, nil
	})
}

func mustJSON(value any) []byte {
	encoded, _ := json.Marshal(value)
	return encoded
}
