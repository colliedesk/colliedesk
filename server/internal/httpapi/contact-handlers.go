package httpapi

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

func (a *API) createContact(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := decodeJSON(request, &input); err != nil || strings.TrimSpace(input.Name) == "" || !strings.Contains(input.Email, "@") {
		writeError(response, http.StatusBadRequest, "invalid_request", "Name and a valid email are required.")
		return
	}
	contact, err := a.store.CreateContact(request.Context(), principal(request).Workspace.ID, input.Name, input.Email)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, contact)
}

func (a *API) getContact(response http.ResponseWriter, request *http.Request) {
	contact, err := a.store.GetContact(request.Context(), principal(request).Workspace.ID, request.PathValue("id"))
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, contact)
}

// maxContactIDs bounds the ids= filter of the contact list.
const maxContactIDs = 100

var contactIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func (a *API) listContacts(response http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	filter := store.ContactFilter{Search: query.Get("search")}
	if query.Has("ids") {
		ids, problem := parseContactIDs(query.Get("ids"))
		if problem != "" {
			writeError(response, http.StatusBadRequest, "invalid_request", problem)
			return
		}
		filter.IDs = ids
	}
	contacts, err := a.store.ListContacts(request.Context(), principal(request).Workspace.ID, filter)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": contacts})
}

// parseContactIDs reads a comma-separated list of contact ids as uuids. The first bad entry makes the
// whole list invalid; problem then says which and why, for the client.
func parseContactIDs(raw string) (ids []pgtype.UUID, problem string) {
	entries := strings.Split(strings.ToLower(raw), ",")
	if len(entries) > maxContactIDs {
		return nil, fmt.Sprintf("ids takes at most %d contact ids, got %d.", maxContactIDs, len(entries))
	}
	ids = make([]pgtype.UUID, len(entries))
	for index, entry := range entries {
		if entry == "" {
			return nil, "ids has an empty entry. Give contact ids (UUIDs) separated by single commas."
		}
		if !contactIDPattern.MatchString(entry) || ids[index].Scan(entry) != nil {
			return nil, fmt.Sprintf("ids must be contact ids (UUIDs) separated by commas; %q is not one.", shorten(entry, 40))
		}
	}
	return ids, ""
}

// shorten cuts text to at most limit bytes (at a character boundary) for an error message.
func shorten(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	for limit > 0 && !utf8.RuneStart(text[limit]) {
		limit--
	}
	return text[:limit] + "..."
}
