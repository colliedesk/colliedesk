package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// helpCenterIDOf reads the help_center_id query parameter of a help center setting: "" (left out) is
// the primary help center. A value that is not an id is answered 400.
func helpCenterIDOf(response http.ResponseWriter, request *http.Request) (string, bool) {
	id := request.URL.Query().Get("help_center_id")
	if id != "" && !isUUID(id) {
		writeError(response, http.StatusBadRequest, "invalid_request",
			"help_center_id is the id of one of the workspace's help centers (GET /api/v1/help-centers lists them); leave it out for the primary one.")
		return "", false
	}
	return id, true
}

// writeHelpCenterError answers the help center errors; false when err is none of them.
func writeHelpCenterError(response http.ResponseWriter, err error) bool {
	var inUse *domain.HelpCenterInUseError
	switch {
	case errors.As(err, &inUse):
		names := make([]string, len(inUse.Channels))
		for i, channel := range inUse.Channels {
			names[i] = channel.Name
		}
		writeJSON(response, http.StatusConflict, map[string]any{"error": errorBody(response, map[string]any{"code": "help_center_in_use",
			"message": "This help center is served by " + strings.Join(names, ", ") + ". Move each channel to another help center " +
				"(PATCH /api/v1/channels/{id} with help_center_id) or remove it, then delete the help center. Nothing was changed.",
			"channels": inUse.Channels})})
	case errors.Is(err, domain.ErrHelpCenterNotFound):
		writeError(response, http.StatusNotFound, "not_found", "No help center of this workspace has that id. GET /api/v1/help-centers lists them.")
	case errors.Is(err, domain.ErrHelpCenterLimit):
		writeError(response, http.StatusConflict, "help_center_limit", "A workspace has at most "+strconv.Itoa(domain.MaxHelpCenters)+
			" help centers, the primary one included. Nothing was created: delete one you no longer need (DELETE /api/v1/help-centers/{id}) first.")
	case errors.Is(err, domain.ErrPrimaryHelpCenter):
		writeError(response, http.StatusConflict, "primary_help_center", "The primary help center cannot be deleted: it serves the workspace's own address. "+
			"Turn it off with PATCH /api/v1/help-centers/{id} and enabled false instead. Nothing was changed.")
	case errors.Is(err, domain.ErrHelpCenterHandleTaken):
		writeError(response, http.StatusConflict, "duplicate_handle", "Another help center of this workspace has this handle. Choose another one; nothing was saved.")
	default:
		return false
	}
	return true
}
