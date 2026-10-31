package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// Chat threads (docs/user/channels.md "Chat threads"): everything said in a place of a chat channel,
// as people and agents read it. Every route needs a session or a token of the workspace; no public
// route reads a thread, so a customer never sees another customer's message.

// chatPaging reads cursor and limit (1 to 100, 100 when left out).
func chatPaging(response http.ResponseWriter, request *http.Request) (string, int, bool) {
	query := request.URL.Query()
	limit := 0
	if raw := query.Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > store.MaxChatPage {
			writeError(response, http.StatusBadRequest, "invalid_request", "limit is a number from 1 to 100.")
			return "", 0, false
		}
		limit = value
	}
	return query.Get("cursor"), limit, true
}

func writeChatError(response http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrBadCursor) {
		writeError(response, http.StatusBadRequest, "invalid_request", "The cursor is not one this server handed out.")
		return
	}
	writeStoreError(response, err)
}

func (a *API) getConversationChat(response http.ResponseWriter, request *http.Request) {
	cursor, limit, ok := chatPaging(response, request)
	if !ok {
		return
	}
	linked := request.URL.Query().Get("linked")
	if linked != "" && linked != "only" {
		writeError(response, http.StatusBadRequest, "invalid_request", "linked is only, or left out for the whole thread.")
		return
	}
	page, err := a.store.ConversationChat(request.Context(), principal(request).Workspace.ID, request.PathValue("id"), cursor, limit, linked == "only")
	if err != nil {
		writeChatError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, page)
}

func (a *API) listChannelThreads(response http.ResponseWriter, request *http.Request) {
	id, ok := channelID(response, request)
	if !ok {
		return
	}
	cursor, limit, ok := chatPaging(response, request)
	if !ok {
		return
	}
	kind := request.URL.Query().Get("kind")
	if kind != "" && kind != channels.PlaceDirect && kind != channels.PlaceGroup {
		writeError(response, http.StatusBadRequest, "invalid_request", "kind is direct or group.")
		return
	}
	page, err := a.store.ChannelThreads(request.Context(), principal(request).Workspace.ID, id, kind, cursor, limit)
	if err != nil {
		writeChannelError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, page)
}

func (a *API) listThreadItems(response http.ResponseWriter, request *http.Request) {
	id, ok := channelID(response, request)
	if !ok {
		return
	}
	cursor, limit, ok := chatPaging(response, request)
	if !ok {
		return
	}
	threadID := request.PathValue("threadId")
	if !uuidShape.MatchString(threadID) {
		writeError(response, http.StatusNotFound, "not_found", "No chat thread with that id on this channel.")
		return
	}
	page, err := a.store.ThreadItems(request.Context(), principal(request).Workspace.ID, id, threadID, cursor, limit)
	if err != nil {
		writeChatError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, page)
}

// listUnresolvedItems lists what customers said on chat channels that nobody concluded on.
func (a *API) listUnresolvedItems(response http.ResponseWriter, request *http.Request) {
	cursor, limit, ok := chatPaging(response, request)
	if !ok {
		return
	}
	olderThan := 0
	if raw := request.URL.Query().Get("older_than"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 || value > 30*24*3600 {
			writeError(response, http.StatusBadRequest, "invalid_request", "older_than is a number of seconds, at most 30 days.")
			return
		}
		olderThan = value
	}
	page, err := a.store.UnresolvedItems(request.Context(), principal(request).Workspace.ID, time.Duration(olderThan)*time.Second, cursor, limit, a.now())
	if err != nil {
		writeChatError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, page)
}
