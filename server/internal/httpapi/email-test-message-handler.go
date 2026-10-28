package httpapi

import (
	"context"
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
	// A repeat of one click (the same Idempotency-Key) finds the first test message and spends nothing.
	a.createOnce(response, request, sendTestMessageOperation, []string{target.ChannelID}, func(ctx context.Context) (any, error) {
		return a.deliverTestMessage(ctx, response, workspaceID, identity.Agent, target)
	})
}

// deliverTestMessage counts one test message against the workspace's hourly allowance and delivers it;
// a refusal is written to response and returned as errAnswered.
func (a *API) deliverTestMessage(ctx context.Context, response http.ResponseWriter, workspaceID string, agent domain.Agent, target *domain.EmailChannel) (any, error) {
	if allowed, wait := a.testMessages.allow(workspaceID, a.now()); !allowed {
		response.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		writeError(response, http.StatusTooManyRequests, "rate_limited",
			fmt.Sprintf("A workspace can send itself %d test messages an hour. Try again later.", testMessagesPerHour))
		return nil, errAnswered
	}
	raw, err := testMessage(agent, target.SupportAddress, a.now().Format("Mon, 02 Jan 2006 15:04:05 -0700"))
	if err != nil {
		return nil, err
	}
	parsed, err := email.Parse(raw, "")
	if err != nil {
		return nil, fmt.Errorf("parse test message: %w", err)
	}
	channel, err := a.store.GetChannel(ctx, workspaceID, target.ChannelID)
	if err != nil {
		return nil, err
	}
	results, err := a.pipeline.Receive(ctx, channel, []channels.Raw{{Parsed: parsed, Body: raw, ReceivedAt: a.now()}})
	if errors.Is(err, store.ErrRateLimited) {
		response.Header().Set("Retry-After", strconv.Itoa(int(store.InboundRateWindow.Seconds())))
		writeError(response, http.StatusTooManyRequests, "rate_limited", "Too many messages from your address arrived just now. Try again later.")
		return nil, errAnswered
	}
	if errors.Is(err, domain.ErrMailboxClosed) {
		writeError(response, http.StatusForbidden, "mailbox_closed", "This mailbox is not accepting new mail.")
		return nil, errAnswered
	}
	if writePlanLimit(response, err) {
		return nil, errAnswered
	}
	if err != nil {
		return nil, err
	}
	outcome := results[0]
	if outcome.ConversationID == nil || outcome.MessageID == nil {
		reason := outcome.Status
		if outcome.Reason != nil {
			reason += ": " + *outcome.Reason
		}
		return nil, fmt.Errorf("test message was not stored (%s)", reason)
	}
	return map[string]string{"conversation_id": *outcome.ConversationID, "message_id": *outcome.MessageID, "sent_to": agent.Email}, nil
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
