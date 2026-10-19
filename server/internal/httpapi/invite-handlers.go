package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// createInvite runs for people only: a token's invite is an agent request (Approval), so its
// one-time link reaches the approving person once and never the token.
func (a *API) createInvite(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Email       string `json:"email"`
		RoleCeiling string `json:"role_ceiling"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Email and role ceiling are required.")
		return
	}
	invite, link, err := a.auth.CreateInvite(request.Context(), principal(request), input.Email, input.RoleCeiling)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	a.recordFunnel("teammate_invited", principal(request).Workspace.ID, nil)
	writeJSON(response, http.StatusCreated, map[string]any{"invite": invite, "url": link})
}

// fileGrantRequest files a token's call that a person must approve whatever the token's tier.
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
