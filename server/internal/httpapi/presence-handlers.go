package httpapi

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// presenceClientPattern is a tab's id: random per page load in the browser, a fixed name for a token.
var presenceClientPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

func writePresenceError(response http.ResponseWriter, err error) {
	if errors.Is(err, domain.ErrPresenceConversationNotFound) {
		writeError(response, http.StatusNotFound, "not_found", "The conversation was not found.")
		return
	}
	if errors.Is(err, domain.ErrPresenceTooSoon) {
		seconds := int(domain.PresenceMinInterval / time.Second)
		response.Header().Set("Retry-After", strconv.Itoa(seconds))
		writeError(response, http.StatusTooManyRequests, "too_many_requests",
			"This tab announced this conversation less than "+strconv.Itoa(seconds)+" seconds ago. Nothing changed; announce again after Retry-After.")
		return
	}
	writeStoreError(response, err)
}

// presenceConversation reads the conversation id of the path; anything that is not an id is a
// conversation that does not exist.
func presenceConversation(response http.ResponseWriter, request *http.Request) (string, bool) {
	id := strings.ToLower(request.PathValue("id"))
	if !contactIDPattern.MatchString(id) {
		writePresenceError(response, domain.ErrPresenceConversationNotFound)
		return "", false
	}
	return id, true
}

func presenceCaller(request *http.Request) store.PresenceCaller {
	who := principal(request)
	return store.PresenceCaller{AgentID: who.Agent.ID, SessionID: who.SessionID, Token: who.IsToken()}
}

// presenceTiming is how long the caller's row lives and when it should call again: a token is not
// on a timer; a reply not sent yet outlives the rare timers of a tab in the background.
func presenceTiming(token, writing, hidden bool) (ttl, heartbeat time.Duration) {
	switch {
	case token:
		return domain.PresenceTokenTTL, domain.PresenceTokenTTL
	case writing && hidden:
		return domain.PresenceWritingTTL, domain.PresenceHiddenHeartbeat
	case writing:
		return domain.PresenceWritingTTL, domain.PresenceHeartbeat
	case hidden:
		return domain.PresenceViewingTTL, domain.PresenceHiddenHeartbeat
	default:
		return domain.PresenceViewingTTL, domain.PresenceHeartbeat
	}
}

func (a *API) putConversationPresence(response http.ResponseWriter, request *http.Request) {
	conversationID, ok := presenceConversation(response, request)
	if !ok {
		return
	}
	var input struct {
		ClientID *string `json:"client_id"`
		Writing  *bool   `json:"writing"`
		Edited   bool    `json:"edited"`
		Hidden   bool    `json:"hidden"`
	}
	if err := decodeJSON(request, &input); err != nil || input.ClientID == nil || input.Writing == nil || !presenceClientPattern.MatchString(*input.ClientID) {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send client_id (8 to 64 letters, digits, - or _) and writing (true while the reply box holds a reply not sent yet).")
		return
	}
	caller := presenceCaller(request)
	ttl, heartbeat := presenceTiming(caller.Token, *input.Writing, input.Hidden)
	viewers, err := a.store.AnnouncePresence(request.Context(), principal(request).Workspace.ID, conversationID, caller,
		store.PresenceUpdate{ClientID: *input.ClientID, Writing: *input.Writing, Edited: input.Edited, TTL: ttl})
	if err != nil {
		writePresenceError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"viewers": viewers, "heartbeat_seconds": int(heartbeat / time.Second)})
}

func (a *API) deleteConversationPresence(response http.ResponseWriter, request *http.Request) {
	conversationID, ok := presenceConversation(response, request)
	if !ok {
		return
	}
	clientID := request.PathValue("clientId")
	if !presenceClientPattern.MatchString(clientID) {
		writePresenceError(response, domain.ErrPresenceConversationNotFound)
		return
	}
	if err := a.store.LeavePresence(request.Context(), principal(request).Workspace.ID, conversationID, principal(request).SessionID, clientID); err != nil {
		writePresenceError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}
