package httpapi

import (
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// getSetupProgress tells where the workspace stands on getting started.
func (a *API) getSetupProgress(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	// The AI drafts step is unavailable on a server without built-in AI for drafts (and no provider of the
	// workspace); the reply targets suggestion is read only for someone who may change the targets.
	progress, err := a.store.SetupProgress(request.Context(), who.Workspace.ID, a.now(), store.SetupOptions{
		ServerDrafts: a.ai.ServerOffers(ai.SlotDraft), CanSetTargets: who.Can(auth.PermWorkspaceUpdate)})
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, progress)
}

// setSetupGuideHidden hides the setup guide for the caller, or shows it again.
func (a *API) setSetupGuideHidden(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Hidden *bool `json:"hidden"`
	}
	if err := decodeJSON(request, &input); err != nil || input.Hidden == nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send hidden: true or false.")
		return
	}
	who := principal(request)
	if err := a.store.SetSetupGuideHidden(request.Context(), who.Workspace.ID, who.Agent.ID, *input.Hidden); err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"hidden": *input.Hidden})
}
