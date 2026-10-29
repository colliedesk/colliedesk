package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/channels/helpchat"
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
	filter := ai.ChatSessionFilter{ChannelID: query.Get("channel_id"), Status: query.Get("status"), ConversationID: query.Get("conversation_id"), Cursor: query.Get("cursor")}
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

// previewAIChat answers a question as the help center's chat would, for an admin trying it (Settings →
// Built-in AI → AI chat → Try it). Nothing is shown to customers or stored but the call's usage.
func (a *API) previewAIChat(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Question  string `json:"question"`
		ChannelID string `json:"channel_id"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send question and, to try one channel's instructions, channel_id.")
		return
	}
	who := principal(request)
	ctx := request.Context()
	surface := ai.ChatSurface{WorkspaceName: who.Workspace.Name}
	channel, found, err := channelRouteKit{api: a, kind: helpchat.Type}.Channel(ctx, who.Workspace.ID, input.ChannelID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	if input.ChannelID != "" && !found {
		writeError(response, http.StatusNotFound, "not_found", "No help chat channel with that id in this workspace.")
		return
	}
	if found {
		config, err := a.store.ChannelConfigOf(ctx, channel)
		if err != nil {
			writeStoreError(response, err)
			return
		}
		if value, ok := config.(helpchat.Surface); ok {
			surface.ChannelID, surface.Instructions = channel.ID, value.Instructions
		}
	}
	// A press sent again (its Idempotency-Key) answers what the first one got: one paid answer per press.
	a.runOnce(response, request, http.StatusOK, previewAIChatOperation, []string{input.Question, input.ChannelID}, func(ctx context.Context) (any, error) {
		result, err := a.ai.ChatPreview(ctx, who.Workspace.ID, aiActor(request), surface, input.Question)
		var failed *ai.ChatPreviewError
		switch {
		case errors.Is(err, ai.ErrChatBusy):
			response.Header().Set("Retry-After", "5")
			writeError(response, http.StatusTooManyRequests, "rate_limited", "Too many chat answers at once. Try again in a moment.")
		case errors.As(err, &failed):
			writeError(response, http.StatusBadGateway, "ai_failed", "The AI did not answer ("+failed.Code+"). Try again.")
		case err != nil:
			writeAIError(response, err)
		default:
			return result, nil
		}
		return nil, errAnswered
	})
}
