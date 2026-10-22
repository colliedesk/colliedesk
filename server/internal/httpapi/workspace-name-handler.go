package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

const workspaceNameInvalid = "name must be 2 to 60 characters without line breaks or other control characters."

// updateWorkspaceName renames the workspace. Customers see the name as the help center title while the
// help center has no title of its own; replies never carry it, each email channel sends with its own
// sender name. The rules are those of sign-up: 2 to 60 characters, and no brand on the hosted cloud.
func (a *API) updateWorkspaceName(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Name json.RawMessage `json:"name"`
	}
	var raw string
	if err := decodeJSON(request, &input); err != nil || len(input.Name) == 0 || json.Unmarshal(input.Name, &raw) != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", workspaceNameInvalid)
		return
	}
	name, ok := domain.NormalizeWorkspaceName(raw)
	if !ok {
		writeError(response, http.StatusBadRequest, "invalid_request", workspaceNameInvalid)
		return
	}
	if a.refuseBrand(response, name) {
		return
	}
	workspace, err := a.store.UpdateWorkspaceName(request.Context(), principal(request).Workspace.ID, name)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, workspace)
}
