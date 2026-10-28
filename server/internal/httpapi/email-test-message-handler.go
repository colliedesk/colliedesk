package httpapi

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/mail"
	"strconv"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// testMessagesPerHour bounds the test messages one workspace sends itself: enough to try the loop a few
// times, too few to fill an inbox.
const testMessagesPerHour = 5

const testMessageSubject = "Test message from Colliedesk"

// sendTestMessage delivers one email from the signed-in agent's own address to an email channel, through
// the pipeline real inbound mail takes, so a new team sees a conversation arrive and can answer it
// without a forwarder or a terminal. The reply goes to that agent (see the trial rule in the store).
func (a *API) sendTestMessage(response http.ResponseWriter, request *http.Request) {
	identity := principal(request)
	workspaceID := identity.Workspace.ID
	target, ok := a.requestedEmailChannel(response, request)
	if !ok {
		return
	}
	if target == nil {
		writeError(response, http.StatusNotFound, "not_found", "The email channel is not connected.")
		return
	}
	// Mail from the support address itself never opens a conversation, so there would be nothing to answer.
	if email.MailboxKey(identity.Agent.Email) == email.MailboxKey(target.SupportAddress) {
		writeError(response, http.StatusConflict, "own_address",
			"You sign in with the support address itself, and mail from it never opens a conversation. Send the test from an agent with another address.")
		return
	}
	if a.config.Attachments == nil {
		writeInternalError(response, errors.New("attachment storage is not configured; inbound mail cannot be kept"))
		return
	}
	if allowed, wait := a.testMessages.allow(workspaceID, a.now()); !allowed {
		response.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		writeError(response, http.StatusTooManyRequests, "rate_limited",
			fmt.Sprintf("A workspace can send itself %d test messages an hour. Try again later.", testMessagesPerHour))
		return
	}
	raw, err := testMessage(identity.Agent, target.SupportAddress, a.now().Format("Mon, 02 Jan 2006 15:04:05 -0700"))
	if err != nil {
		writeInternalError(response, err)
		return
	}
	parsed, err := email.Parse(raw, "")
	if err != nil {
		writeInternalError(response, fmt.Errorf("parse test message: %w", err))
		return
	}
	channel, err := a.store.GetChannel(request.Context(), workspaceID, target.ChannelID)
	if err != nil {
		writeInternalError(response, err)
		return
	}
	results, err := a.pipeline.Receive(request.Context(), channel, []channels.Raw{{Parsed: parsed, Body: raw, ReceivedAt: a.now()}})
	if errors.Is(err, store.ErrRateLimited) {
		response.Header().Set("Retry-After", strconv.Itoa(int(store.InboundRateWindow.Seconds())))
		writeError(response, http.StatusTooManyRequests, "rate_limited", "Too many messages from your address arrived just now. Try again later.")
		return
	}
	if errors.Is(err, domain.ErrMailboxClosed) {
		writeError(response, http.StatusForbidden, "mailbox_closed", "This mailbox is not accepting new mail.")
		return
	}
	if writePlanLimit(response, err) {
		return
	}
	if err != nil {
		writeInternalError(response, err)
		return
	}
	outcome := results[0]
	if outcome.ConversationID == nil || outcome.MessageID == nil {
		reason := outcome.Status
		if outcome.Reason != nil {
			reason += ": " + *outcome.Reason
		}
		writeInternalError(response, fmt.Errorf("test message was not stored (%s)", reason))
		return
	}
	writeJSON(response, http.StatusCreated, map[string]string{"conversation_id": *outcome.ConversationID,
		"message_id": *outcome.MessageID, "sent_to": identity.Agent.Email})
}

// testMessage writes the RFC 5322 message an agent sends itself: from their own address to the
// channel's support address, with a Message-ID of its own so it never threads into another conversation.
func testMessage(agent domain.Agent, supportAddress, date string) ([]byte, error) {
	messageID, err := email.NewMessageID(email.Domain(supportAddress))
	if err != nil {
		return nil, err
	}
	from := (&mail.Address{Name: agent.DisplayName, Address: agent.Email}).String()
	body := strings.Join([]string{
		"This is a test message you sent yourself from Colliedesk.",
		"",
		"It arrived the way a customer's email does. Write a reply and send it: the reply is emailed to " + agent.Email +
			", so you see exactly what your customers receive.",
	}, "\r\n")
	headers := []string{
		"From: " + from,
		"To: " + supportAddress,
		"Subject: " + mime.QEncoding.Encode("utf-8", testMessageSubject),
		"Date: " + date,
		"Message-ID: <" + messageID + ">",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
	}
	return []byte(strings.Join(headers, "\r\n") + "\r\n\r\n" + body + "\r\n"), nil
}
