package httpapi

import "net/http"

// getKBArticleStats answers an article's help center views and the sent replies that used it, per day.
func (a *API) getKBArticleStats(response http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	stats, err := a.store.KBArticleStats(request.Context(), principal(request).Workspace.ID, request.PathValue("id"), a.now(), query.Get("from"), query.Get("to"))
	if err != nil {
		writeKBError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, stats)
}
