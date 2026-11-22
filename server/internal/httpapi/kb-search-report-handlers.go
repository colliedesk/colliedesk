package httpapi

import "net/http"

// getKBSearchReport answers what readers searched for on the help center in the last 7 days.
func (a *API) getKBSearchReport(response http.ResponseWriter, request *http.Request) {
	report, err := a.store.KBSearchReport(request.Context(), principal(request).Workspace.ID)
	if err != nil {
		writeKBError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, report)
}
