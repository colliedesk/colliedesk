package httpapi

import (
	"net/http"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels/channelemail"
)

// forwardingConfirmationWindow is how long Gmail's confirmation stays shown: Google's link expires, and an
// older one belongs to a setup already done or abandoned.
const forwardingConfirmationWindow = 24 * time.Hour

const noForwardingConfirmation = "No forwarding confirmation reached this channel in the last 24 hours."

type forwardingConfirmationResponse struct {
	Code           string    `json:"code"`
	ConfirmURL     *string   `json:"confirm_url"`
	ReceivedAt     time.Time `json:"received_at"`
	ConversationID string    `json:"conversation_id"`
}

// getForwardingConfirmation reads the newest Gmail forwarding confirmation the channel received, so the
// email setup can show its code and link where the person is. The email stays its own conversation.
func (a *API) getForwardingConfirmation(response http.ResponseWriter, request *http.Request) {
	target, ok := a.requestedEmailChannel(response, request)
	if !ok {
		return
	}
	found, err := a.store.LatestIncomingEmailFrom(request.Context(), principal(request).Workspace.ID, target.ChannelID,
		channelemail.GoogleForwardingSender, a.now().Add(-forwardingConfirmationWindow))
	if err != nil {
		writeInternalError(response, err)
		return
	}
	if found == nil {
		writeError(response, http.StatusNotFound, "not_found", noForwardingConfirmation)
		return
	}
	confirmation, recognised := channelemail.ParseForwardingConfirmation(found.From, found.Subject, found.Body, found.Authentication)
	if !recognised {
		writeError(response, http.StatusNotFound, "not_found", noForwardingConfirmation)
		return
	}
	result := forwardingConfirmationResponse{Code: confirmation.Code, ReceivedAt: found.ReceivedAt, ConversationID: found.ConversationID}
	if confirmation.ConfirmURL != "" {
		result.ConfirmURL = &confirmation.ConfirmURL
	}
	writeJSON(response, http.StatusOK, result)
}
