package httpapi

import (
	"net/http"
	"time"
)

func (a *API) inboxActivity(response http.ResponseWriter, request *http.Request) {
	var since *time.Time
	if raw := request.URL.Query().Get("since"); raw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			writeError(response, http.StatusBadRequest, "invalid_request", "since must be an RFC 3339 timestamp.")
			return
		}
		since = &parsed
	}
	activity, err := a.store.InboxActivity(request.Context(), principal(request).Workspace.ID, presenceCaller(request), since)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, activity)
}
