package httpapi

import "net/http"

// getSetupProgress tells where the workspace stands on getting started.
func (a *API) getSetupProgress(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	progress, err := a.store.SetupProgress(request.Context(), who.Workspace.ID, a.now())
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
