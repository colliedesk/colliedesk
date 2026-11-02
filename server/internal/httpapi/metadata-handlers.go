package httpapi

import (
	"net/http"
	"regexp"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

func (a *API) listAgents(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	items, err := a.store.ListAgents(request.Context(), who.Workspace.ID, callerOf(who))
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

var uuidShape = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// callerOf is who changes roles or members: the person, and what this call may do.
func callerOf(who auth.Principal) store.Caller {
	return store.Caller{AgentID: who.Agent.ID, Permissions: who.Effective()}
}

// updateAgent changes the display name or the active state of a member; roles change with
// PUT /api/v1/agents/{id}/roles. reassign_to only makes sense with active=false: it names the member
// that takes over the open and pending conversations.
func (a *API) updateAgent(response http.ResponseWriter, request *http.Request) {
	var input struct {
		DisplayName *string `json:"display_name,omitempty"`
		Role        *string `json:"role,omitempty"`
		Active      *bool   `json:"active,omitempty"`
		ReassignTo  *string `json:"reassign_to,omitempty"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Agent changes are invalid.")
		return
	}
	if input.Role != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Roles change with PUT /api/v1/agents/{id}/roles (role_ids, or roles by name).")
		return
	}
	if !uuidShape.MatchString(request.PathValue("id")) {
		writeError(response, http.StatusNotFound, "not_found", "The requested resource was not found.")
		return
	}
	if input.ReassignTo != nil && !uuidShape.MatchString(*input.ReassignTo) {
		writeStoreError(response, domain.ErrInvalidReassignTarget)
		return
	}
	if input.DisplayName == nil && input.Active == nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send a display name or an active flag.")
		return
	}
	if input.DisplayName != nil {
		name, ok := normalizeDisplayName(*input.DisplayName)
		if !ok {
			writeError(response, http.StatusBadRequest, "invalid_request", displayNameInvalid)
			return
		}
		input.DisplayName = &name
	}
	if input.ReassignTo != nil && (input.Active == nil || *input.Active) {
		writeError(response, http.StatusBadRequest, "invalid_request", "reassign_to only applies when active is false.")
		return
	}
	who := principal(request)
	member, err := a.store.UpdateAgent(request.Context(), who.Workspace.ID, callerOf(who), request.PathValue("id"),
		store.AgentUpdate{DisplayName: input.DisplayName, Active: input.Active, ReassignTo: input.ReassignTo})
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, member)
}

func (a *API) listTags(response http.ResponseWriter, request *http.Request) {
	items, err := a.store.ListTags(request.Context(), principal(request).Workspace.ID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}
