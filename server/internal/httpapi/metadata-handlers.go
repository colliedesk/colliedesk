package httpapi

import (
	"net/http"
	"regexp"

	"github.com/colliedesk/colliedesk/server/internal/auth"
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

var agentUpdateRoute = Route{http.MethodPatch, "/api/v1/agents/{id}", auth.PermissionAccessWrite}

// touchesAdmin reports whether a team change is about the admin role: it asks for it, or it changes
// the role or the active state of a target that has it now. A rename alone is not, and an unknown
// target is not (the update answers 404).
func (a *API) touchesAdmin(request *http.Request, role *string, active *bool) (bool, error) {
	if role == nil && active == nil {
		return false, nil
	}
	if role != nil && *role == "admin" {
		return true, nil
	}
	members, err := a.store.ListAgents(request.Context(), principal(request).Workspace.ID)
	if err != nil {
		return false, err
	}
	for _, member := range members {
		if member.ID == request.PathValue("id") {
			return member.Role == "admin", nil
		}
	}
	return false, nil
}

// updateAgent changes the display name, the role or the active state of an agent. reassign_to only makes sense with
// active=false: it names the agent that takes over the open and pending conversations.
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
	if !uuidShape.MatchString(request.PathValue("id")) {
		writeError(response, http.StatusNotFound, "not_found", "The requested resource was not found.")
		return
	}
	if input.ReassignTo != nil && !uuidShape.MatchString(*input.ReassignTo) {
		writeStoreError(response, domain.ErrInvalidReassignTarget)
		return
	}
	if input.DisplayName == nil && input.Role == nil && input.Active == nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send a display name, a role or an active flag.")
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
	if input.Role != nil && *input.Role != "admin" && *input.Role != "agent" {
		writeError(response, http.StatusBadRequest, "invalid_request", "Role must be admin or agent.")
		return
	}
	if input.ReassignTo != nil && (input.Active == nil || *input.Active) {
		writeError(response, http.StatusBadRequest, "invalid_request", "reassign_to only applies when active is false.")
		return
	}
	// A change that grants, keeps or takes the admin role waits for a person whatever the token's tier:
	// promoting, reactivating an admin (it keeps the role), demoting or deactivating an admin.
	if who := principal(request); who.Scope != "" {
		touchesAdmin, err := a.touchesAdmin(request, input.Role, input.Active)
		if err != nil {
			writeStoreError(response, err)
			return
		}
		if touchesAdmin {
			a.fileGrantRequest(response, request, agentUpdateRoute, who, input)
			return
		}
	}
	member, err := a.store.UpdateAgent(request.Context(), principal(request).Workspace.ID, request.PathValue("id"),
		store.AgentUpdate{DisplayName: input.DisplayName, Role: input.Role, Active: input.Active, ReassignTo: input.ReassignTo})
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
