package httpapi

import (
	"net/http"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// searchConsoleFields are the fields a search console call takes; an agent request carrying any other
// is refused before it is stored (settingsFields).
var searchConsoleFields = []string{"google_site_verification", "bing_site_verification"}

func (a *API) getHelpCenterSearchConsole(response http.ResponseWriter, request *http.Request) {
	view, err := a.store.GetHelpCenterSearchConsole(request.Context(), principal(request).Workspace.ID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, view)
}

// putHelpCenterSearchConsole sets or removes the verification tokens. A field left out stays as it is,
// so a client that knows only one token never removes the other; null or an empty string removes one.
// The tags appear on every public page, so settings access alone cannot publish them.
func (a *API) putHelpCenterSearchConsole(response http.ResponseWriter, request *http.Request) {
	raw := keepBody(request)
	var input struct {
		Google sentText `json:"google_site_verification"`
		Bing   sentText `json:"bing_site_verification"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send a JSON object with only these fields: "+strings.Join(searchConsoleFields, ", ")+".")
		return
	}
	var change domain.SearchConsoleChange
	if input.Google.Sent {
		change.Google = &input.Google.Value
	}
	if input.Bing.Sent {
		change.Bing = &input.Bing.Value
	}
	if err := domain.CheckSearchConsole(change); err != nil {
		writeThemeError(response, err)
		return
	}
	if a.askUnlessHeld(response, request, Route{http.MethodPut, "/api/v1/help-center/search-console", auth.PermissionAccessWrite}, principal(request), auth.PermKBArticlesPublish, raw, nil) {
		return
	}
	view, err := a.store.PutHelpCenterSearchConsole(request.Context(), principal(request).Workspace.ID, change)
	if err != nil {
		writeThemeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, view)
}
