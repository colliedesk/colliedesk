package httpapi

import "net/http"

func (a *API) listAgents(response http.ResponseWriter, request *http.Request) {
	items, err := a.store.ListAgents(request.Context(), principal(request).Workspace.ID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

func (a *API) listTags(response http.ResponseWriter, request *http.Request) {
	items, err := a.store.ListTags(request.Context(), principal(request).Workspace.ID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}
