package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
)

var inviteRoute = Route{http.MethodPost, "/api/v1/auth/invites", auth.PermissionAccessWrite}

// tokenInviteNote tells an agent token why its invite answer has no link.
const tokenInviteNote = "The invite exists, but its one-time link is a credential and is never shown to an agent token. " +
	"A person shares the link: approving an invite requested through Colliedesk shows it to them once."

func (a *API) createInvite(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Email       string `json:"email"`
		RoleCeiling string `json:"role_ceiling"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Email and role ceiling are required.")
		return
	}
	who := principal(request)
	// Granting the admin role waits for a person whatever the token's tier.
	if who.Scope != "" && input.RoleCeiling == "admin" {
		a.fileGrantRequest(response, request, inviteRoute, who, input)
		return
	}
	invite, link, err := a.auth.CreateInvite(request.Context(), who, input.Email, input.RoleCeiling)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	if who.Scope != "" {
		writeJSON(response, http.StatusCreated, map[string]any{"invite": invite, "note": tokenInviteNote})
		return
	}
	writeJSON(response, http.StatusCreated, map[string]any{"invite": invite, "url": link})
}

// fileGrantRequest files a token's call that would grant the admin role as an agent request.
func (a *API) fileGrantRequest(response http.ResponseWriter, request *http.Request, route Route, who auth.Principal, call any) {
	encoded, err := json.Marshal(call)
	if err != nil {
		writeInternalError(response, err)
		return
	}
	a.fileAgentRequest(response, request, route, who, domain.AgentRequestAction, encoded, "")
}

func (a *API) listInvites(response http.ResponseWriter, request *http.Request) {
	items, err := a.auth.ListInvites(request.Context(), principal(request).Workspace.ID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

func (a *API) revokeInvite(response http.ResponseWriter, request *http.Request) {
	ok, err := a.auth.RevokeInvite(request.Context(), principal(request).Workspace.ID, request.PathValue("id"))
	if err != nil {
		writeStoreError(response, err)
		return
	}
	if !ok {
		writeError(response, http.StatusNotFound, "not_found", "The invite was not found.")
		return
	}
	response.WriteHeader(http.StatusNoContent)
}
