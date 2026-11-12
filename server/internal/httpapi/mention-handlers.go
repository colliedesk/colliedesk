package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

// maxMentions is how many teammates one call may ask on a note.
const maxMentions = 20

// checkMentions refuses mentions on anything but a note, and a list that is too long or has a blank item;
// it reports whether the request may go on.
func checkMentions(response http.ResponseWriter, kind string, mentions []string) bool {
	if len(mentions) == 0 {
		return true
	}
	if kind != "note" {
		writeError(response, http.StatusBadRequest, "invalid_request", "Only a private note can mention teammates: send the reply without mentions, or write a note.")
		return false
	}
	if len(mentions) > maxMentions {
		writeError(response, http.StatusBadRequest, "invalid_request", "A note can mention at most 20 teammates.")
		return false
	}
	for index, mention := range mentions {
		if strings.TrimSpace(mention) == "" {
			writeMentionField(response, index, "Name a teammate by their id or email address.")
			return false
		}
	}
	return true
}

func writeMentionField(response http.ResponseWriter, index int, message string) {
	writeErrorWith(response, http.StatusBadRequest, "validation_failed", message+" Nothing was stored.",
		map[string]any{"fields": map[string]string{"mentions." + strconv.Itoa(index): message}})
}

// writeMentionRefused answers a mention of someone who is not an active member of the workspace.
func writeMentionRefused(response http.ResponseWriter, err error) bool {
	var refused *store.MentionRefusedError
	switch {
	case errors.As(err, &refused):
		writeMentionField(response, refused.Index, strconv.Quote(refused.Given)+" is not an active teammate of this workspace: mention a person from GET /api/v1/agents by id or email.")
	case errors.Is(err, store.ErrMentionsOnlyOnNotes):
		writeError(response, http.StatusBadRequest, "invalid_request", "Only a private note can mention teammates.")
	case errors.Is(err, store.ErrNotYourNote):
		writeError(response, http.StatusForbidden, "not_your_note", "Only the person who wrote this note can ask more teammates on it. Write a new note that mentions them.")
	default:
		return false
	}
	return true
}

func (a *API) addNoteMentions(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Mentions []string `json:"mentions"`
	}
	if err := decodeJSON(request, &input); err != nil || len(input.Mentions) == 0 {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send mentions: the teammates to ask, by id or email.")
		return
	}
	if !checkMentions(response, "note", input.Mentions) {
		return
	}
	identity := principal(request)
	item, err := a.store.AddMentions(request.Context(), identity.Workspace.ID, request.PathValue("id"), request.PathValue("messageId"), identity.Agent.ID, input.Mentions)
	if writeMentionRefused(response, err) {
		return
	}
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, item)
}

func (a *API) listMentions(response http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	status, limit := query.Get("status"), 50
	if status == "" {
		status = "unread"
	}
	if raw := query.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			writeError(response, http.StatusBadRequest, "invalid_request", "limit is a number from 1 to 100.")
			return
		}
		limit = parsed
	}
	before := query.Get("before")
	if (status != "unread" && status != "all") || (before != "" && !isUUID(before)) {
		writeError(response, http.StatusBadRequest, "invalid_request", "status is unread or all; before is the next_before of the previous page.")
		return
	}
	identity := principal(request)
	page, err := a.store.ListMentions(request.Context(), identity.Workspace.ID, identity.Agent.ID, status == "all", limit, before)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, page)
}

func (a *API) markMentionsRead(response http.ResponseWriter, request *http.Request) {
	var input struct {
		MentionIDs     []string `json:"mention_ids"`
		ConversationID string   `json:"conversation_id"`
		All            bool     `json:"all"`
		Read           *bool    `json:"read"`
	}
	chosen := 0
	err := decodeJSON(request, &input)
	for _, set := range []bool{len(input.MentionIDs) > 0, input.ConversationID != "", input.All} {
		if set {
			chosen++
		}
	}
	valid := err == nil && chosen == 1 && len(input.MentionIDs) <= 100 && (input.ConversationID == "" || isUUID(input.ConversationID))
	for _, id := range input.MentionIDs {
		valid = valid && isUUID(id)
	}
	if !valid {
		writeError(response, http.StatusBadRequest, "invalid_request", "Name exactly one of mention_ids (up to 100), conversation_id or all: true.")
		return
	}
	read := input.Read == nil || *input.Read
	identity := principal(request)
	changed, unread, err := a.store.MarkMentions(request.Context(), identity.Workspace.ID, identity.Agent.ID,
		store.MentionSelection{IDs: input.MentionIDs, ConversationID: input.ConversationID, All: input.All}, read)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]int{"changed": changed, "unread": unread})
}

func (a *API) setMentionEmail(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Email *bool `json:"email"`
	}
	if err := decodeJSON(request, &input); err != nil || input.Email == nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send email: true or false.")
		return
	}
	identity := principal(request)
	if err := a.store.SetMentionEmail(request.Context(), identity.Workspace.ID, identity.Agent.ID, *input.Email); err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]bool{"email": *input.Email})
}

// isUUID reports whether value is a lowercase id as the API writes them.
func isUUID(value string) bool { return contactIDPattern.MatchString(strings.ToLower(value)) }
