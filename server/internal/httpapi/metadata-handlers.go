package httpapi

import (
	"net/http"
	"regexp"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

func (a *API) listAgents(response http.ResponseWriter, request *http.Request) {
	items, err := a.store.ListAgents(request.Context(), principal(request).Workspace.ID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

var uuidShape = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// updateAgent changes the role or the active state of an agent. reassign_to only makes sense with
// active=false: it names the agent that takes over the open and pending conversations.
func (a *API) updateAgent(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Role       *string `json:"role"`
		Active     *bool   `json:"active"`
		ReassignTo *string `json:"reassign_to"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Agent changes are invalid.")
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
	if input.Role == nil && input.Active == nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send a role or an active flag.")
		return
	}
	if input.Role != nil && *input.Role != "admin" && *input.Role != "agent" {
		writeError(response, http.StatusBadRequest, "invalid_request", "Role must be admin or agent.")
		return
	}
	if input.ReassignTo != nil && (input.Active == nil || *input.Active) {
		writeError(response, http.StatusBadRequest, "invalid_request", "reassign_to only applies when active is false.")
		return
	}
	member, err := a.store.UpdateAgent(request.Context(), principal(request).Workspace.ID, request.PathValue("id"),
		store.AgentUpdate{Role: input.Role, Active: input.Active, ReassignTo: input.ReassignTo})
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
