package httpapi

import (
	"errors"
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/colliedesk/colliedesk/server/internal/workspacehost"
)

// updateWorkspaceSlug renames the workspace slug. On the hosted cloud it is the subdomain of the
// forwarding address, so the old address stops receiving mail at once.
func (a *API) updateWorkspaceSlug(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Slug string `json:"slug"`
		// Next is the page the browser opens on the workspace's new host.
		Next string `json:"next"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "A slug is required.")
		return
	}
	if err := email.ValidateHostedSlug(input.Slug); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Invalid slug: "+err.Error()+".")
		return
	}
	if a.refuseBrand(response, input.Slug) {
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
	// On the workspace's own host the desk moves with the slug, and the cookie of the old host signs in
	// nowhere else: the browser continues on the new host, signed in by a one-time hand-off.
	who := principal(request)
	result := struct {
		domain.Workspace
		ContinueURL *string `json:"continue_url,omitempty"`
	}{Workspace: workspace}
	if _, onWorkspaceHost := workspacehost.Slug(request.Context()); onWorkspaceHost && who.ViaCookie {
		who.Workspace = workspace
		address, err := a.handoffTo(request.Context(), who, input.Next)
		if err != nil {
			writeInternalError(response, err)
			return
		}
		if address != "" {
			result.ContinueURL = &address
		}
	}
	writeJSON(response, http.StatusOK, result)
}
