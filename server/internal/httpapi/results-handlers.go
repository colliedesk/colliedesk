package httpapi

import "net/http"

func (a *API) getResults(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	page, err := a.store.ReadResults(request.Context(), who.Workspace.ID, who.Agent.Role == "admin")
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, page)
}
