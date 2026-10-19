package httpapi

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
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

const (
	// maxContactIDs bounds the ids= filter of the contact list.
	maxContactIDs = 100
	// maxContactLimit bounds the limit= parameter of the contact list.
	maxContactLimit = 200
)

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
	if query.Has("limit") {
		limit, err := strconv.Atoi(query.Get("limit"))
		if err != nil || limit < 1 || limit > maxContactLimit {
			writeError(response, http.StatusBadRequest, "invalid_request", fmt.Sprintf("limit must be a whole number from 1 to %d.", maxContactLimit))
			return
		}
		filter.Limit = limit
	}
	contacts, hasMore, err := a.store.ListContacts(request.Context(), principal(request).Workspace.ID, filter)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	body := map[string]any{"items": contacts}
	if filter.Limit > 0 {
		body["has_more"] = hasMore
	}
	writeJSON(response, http.StatusOK, body)
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

// maxContactName bounds a contact's name, as the MCP tools do.
const maxContactName = 200

func (a *API) updateContact(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Name string `json:"name"`
	}
	name := ""
	if err := decodeJSON(request, &input); err == nil {
		name = strings.TrimSpace(input.Name)
	}
	if name == "" || utf8.RuneCountInString(name) > maxContactName {
		writeError(response, http.StatusBadRequest, "invalid_request", fmt.Sprintf("A name of 1 to %d characters is required.", maxContactName))
		return
	}
	contact, err := a.store.RenameContact(request.Context(), principal(request).Workspace.ID, request.PathValue("id"), name)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, contact)
}

func (a *API) mergeContact(response http.ResponseWriter, request *http.Request) {
	var input struct {
		ContactID string `json:"contact_id"`
	}
	if err := decodeJSON(request, &input); err != nil || !contactIDPattern.MatchString(strings.ToLower(input.ContactID)) {
		writeError(response, http.StatusBadRequest, "invalid_request", "contact_id must name the duplicate contact (a UUID).")
		return
	}
	identity := principal(request)
	actor := store.Actor{AgentID: identity.Agent.ID}
	if identity.Scope != "" {
		actor.TokenID = identity.SessionID
	}
	merge, err := a.store.MergeContacts(request.Context(), identity.Workspace.ID, strings.ToLower(request.PathValue("id")), strings.ToLower(input.ContactID), actor)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, merge)
}

func (a *API) unmergeContact(response http.ResponseWriter, request *http.Request) {
	identity := principal(request)
	merge, err := a.store.UndoContactMerge(request.Context(), identity.Workspace.ID, request.PathValue("id"), request.PathValue("mergeId"), identity.Agent.ID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, merge)
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
