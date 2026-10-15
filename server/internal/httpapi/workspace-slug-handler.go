package httpapi

import (
	"errors"
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// updateWorkspaceSlug renames the workspace slug. On the hosted cloud it is the subdomain of the
// forwarding address, so the old address stops receiving mail at once.
func (a *API) updateWorkspaceSlug(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Slug string `json:"slug"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "A slug is required.")
		return
	}
	if err := email.ValidateHostedSlug(input.Slug); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Invalid slug: "+err.Error()+".")
		return
	}
	if _, denied := domain.DeniedBrand(input.Slug); denied && a.limits {
		writeError(response, http.StatusUnprocessableEntity, "name_not_allowed", brandNameMessage)
		return
	}
	workspace, err := a.store.UpdateWorkspaceSlug(request.Context(), principal(request).Workspace.ID, input.Slug)
	if errors.Is(err, store.ErrSlugTaken) {
		writeError(response, http.StatusConflict, "slug_taken", "This name is already used by another workspace.")
		return
	}
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, workspace)
}
