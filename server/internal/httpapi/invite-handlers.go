package httpapi

import "net/http"

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
	writeJSON(response, http.StatusCreated, map[string]any{"invite": invite, "url": link})
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
