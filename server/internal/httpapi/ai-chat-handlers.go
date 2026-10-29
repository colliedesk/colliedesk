package httpapi

import (
	"errors"
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/ai"
)

// The team's side of the help center's AI chat: the month's use, the chats and their transcripts.
// The chat's own pages and calls live below the help center (channels.HelpCenterRoute).

func (a *API) getAIChatUsage(response http.ResponseWriter, request *http.Request) {
	month, err := a.ai.ParseMonth(request.URL.Query().Get("month"))
	if err != nil {
		writeAIError(response, err)
		return
	}
	usage, err := a.ai.ChatUsage(request.Context(), principal(request).Workspace.ID, month)
	if err != nil {
		writeAIError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, usage)
}

func (a *API) listAIChatSessions(response http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	filter := ai.ChatSessionFilter{ChannelID: query.Get("channel_id"), Status: query.Get("status"), Cursor: query.Get("cursor")}
	switch filter.Status {
	case "", ai.ChatOpen, ai.ChatEnded, ai.ChatHandedOff:
	default:
		writeError(response, http.StatusBadRequest, "invalid_request", "status is open, ended or handed_off.")
		return
	}
	switch query.Get("feedback") {
	case "":
	case "not_helpful":
		filter.NotHelpful = true
	default:
		writeError(response, http.StatusBadRequest, "invalid_request", "feedback is not_helpful.")
		return
	}
	page, err := a.ai.ChatSessions(request.Context(), principal(request).Workspace.ID, filter)
	if err != nil {
		writeAIError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, page)
}

func (a *API) getAIChatSession(response http.ResponseWriter, request *http.Request) {
	chat, err := a.ai.ChatSession(request.Context(), principal(request).Workspace.ID, request.PathValue("id"))
	if errors.Is(err, ai.ErrChatNotFound) {
		writeError(response, http.StatusNotFound, "not_found", "No chat with that id in this workspace.")
		return
	}
	if err != nil {
		writeAIError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, chat)
}

func (a *API) deleteAIChatSession(response http.ResponseWriter, request *http.Request) {
	err := a.ai.DeleteChatSession(request.Context(), principal(request).Workspace.ID, aiActor(request), request.PathValue("id"))
	if errors.Is(err, ai.ErrChatNotFound) {
		writeError(response, http.StatusNotFound, "not_found", "No chat with that id in this workspace.")
		return
	}
	if err != nil {
		writeAIError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}
