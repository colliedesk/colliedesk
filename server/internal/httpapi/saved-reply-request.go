package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// savedReplyRoutePrefix marks the routes that act on one saved reply; their requests keep it as it was.
const savedReplyRoutePrefix = "/api/v1/saved-replies/{id}"

// undeleteRoute is the one saved reply route that acts on a reply in the trash.
const undeleteRoute = savedReplyRoutePrefix + "/restore"

const versionRestoreRoute = savedReplyRoutePrefix + "/versions/{version}/restore"

// savedReplyBefore reads the saved reply a token asks to change, so the person deciding sees it as the
// token saw it. A reply that is not there (another workspace's, purged, or not in the state the call
// needs) is refused now with 404, not filed as a request that can only fail.
func (a *API) savedReplyBefore(response http.ResponseWriter, request *http.Request, route Route, who auth.Principal) (store.SavedReplySnapshot, bool) {
	if route.Method == http.MethodDelete {
		if _, err := time.Parse(time.RFC3339Nano, request.URL.Query().Get("updated_at")); err != nil {
			writeError(response, http.StatusBadRequest, "invalid_request", "Send the updated_at you last read as a query parameter: ?updated_at=<RFC 3339 time>.")
			return store.SavedReplySnapshot{}, false
		}
	}
	id := request.PathValue("id")
	state, err := a.store.SavedReplyStateOf(request.Context(), who.Workspace.ID, id)
	if err == nil && (state.DeletedAt != nil) != (route.Pattern == undeleteRoute) {
		err = domain.ErrSavedReplyNotFound
	}
	if err != nil {
		writeSavedReplyError(response, err)
		return store.SavedReplySnapshot{}, false
	}
	snapshot := store.SavedReplySnapshot{ID: id, Title: state.Title, Body: state.Body, UpdatedAt: state.UpdatedAt}
	if route.Pattern == versionRestoreRoute {
		// The person deciding sees the text the restore applies, as it was when the token asked.
		number, err := strconv.Atoi(request.PathValue("version"))
		if err != nil {
			number = 0
		}
		restores, err := a.store.SavedReplyVersionOf(request.Context(), who.Workspace.ID, id, number)
		if err != nil {
			writeSavedReplyError(response, err)
			return store.SavedReplySnapshot{}, false
		}
		snapshot.Restores = &restores
	}
	return snapshot, true
}

// summarizeSavedReplyRequest is the line a person reads before deciding on a saved reply change: which
// reply, and what the token wants it to become. The full before and after are in the request detail.
func summarizeSavedReplyRequest(route Route, body json.RawMessage, before *store.SavedReplySnapshot, version string) string {
	var fields struct {
		Title *string `json:"title"`
		Body  *string `json:"body"`
	}
	_ = json.Unmarshal(body, &fields)
	flat := func(text *string) string {
		if text == nil {
			return ""
		}
		return strings.Join(strings.Fields(*text), " ")
	}
	if before == nil {
		return fmt.Sprintf("Create a saved reply %q: %s", flat(fields.Title), flat(fields.Body))
	}
	name := fmt.Sprintf("saved reply %q", before.Title)
	switch {
	case route.Method == http.MethodDelete:
		return "Delete " + name + " (it stays in the trash for 30 days)"
	case route.Pattern == undeleteRoute:
		return "Restore " + name + " from the trash"
	case route.Method == http.MethodPost:
		return fmt.Sprintf("Restore version %s of %s", version, name)
	}
	var changes []string
	if fields.Title != nil && strings.TrimSpace(*fields.Title) != before.Title {
		changes = append(changes, fmt.Sprintf("rename to %q", strings.TrimSpace(*fields.Title)))
	}
	if fields.Body != nil && *fields.Body != before.Body {
		changes = append(changes, "new text: "+flat(fields.Body))
	}
	if len(changes) == 0 {
		return "Edit " + name + " (no change to the title or text)"
	}
	return "Edit " + name + ": " + strings.Join(changes, "; ")
}
