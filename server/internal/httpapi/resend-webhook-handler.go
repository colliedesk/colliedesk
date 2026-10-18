package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/standardwebhooks"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

const (
	resendWebhookPath = "/api/v1/webhooks/resend"
	// reasonNotDeliveryFailure is an event the webhook does not act on (delivered, opened, a
	// temporary bounce); it is not logged.
	reasonNotDeliveryFailure = "not a delivery failure"
)

// resendEvent is the part of a Resend email event the bounce handling reads.
type resendEvent struct {
	Type string `json:"type"`
	Data struct {
		EmailID   string   `json:"email_id"`
		MessageID string   `json:"message_id"`
		From      string   `json:"from"`
		To        []string `json:"to"`
		Bounce    *struct {
			Type string `json:"type"`
		} `json:"bounce"`
	} `json:"data"`
}

// receiveResendWebhook marks a reply failed when Resend reports it bounced or was marked as spam.
// Resend owns the envelope sender and rewrites the Message-ID, so its bounces never reach
// MAIL_FROM_DOMAIN. The reply is the one named by its reply+<token>-<reply id>@MAIL_FROM_DOMAIN sender;
// a reply sent as a verified support address cannot be matched and is ignored. The Svix signature is
// the only credential: nothing is read from the database until it verifies.
func (a *API) receiveResendWebhook(response http.ResponseWriter, request *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(response, request.Body, maxWebhookBytes))
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "The webhook body could not be read.")
		return
	}
	header := request.Header
	webhookID := header.Get("svix-id")
	if err := standardwebhooks.Verify(a.config.Email.ResendWebhookSecret, webhookID, header.Get("svix-timestamp"),
		header.Get("svix-signature"), body, time.Now()); err != nil {
		writeError(response, http.StatusUnauthorized, "unauthorized", "A valid webhook signature is required.")
		return
	}
	var event resendEvent
	if err := json.Unmarshal(body, &event); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "The webhook body is not a Resend event.")
		return
	}
	status, reason, err := a.applyResendEvent(request, webhookID, event)
	if err != nil {
		// 500 makes Resend redeliver; nothing was recorded, so the retry is applied.
		writeInternalError(response, err)
		return
	}
	if reason != "" && reason != reasonNotDeliveryFailure {
		// A delivery failure nobody will see in the inbox. Ids only: the addresses are customer data.
		slog.Warn("resend event ignored", "reason", reason, "event", event.Type, "email_id", event.Data.EmailID, "webhook_id", webhookID)
	}
	writeJSON(response, http.StatusOK, map[string]string{"status": status})
}

// applyResendEvent records a bounce or complaint and returns the webhook status, with the reason when
// the event changed nothing.
func (a *API) applyResendEvent(request *http.Request, webhookID string, event resendEvent) (string, string, error) {
	data := event.Data
	complaint := event.Type == "email.complained"
	if event.Type != "email.bounced" && !complaint {
		return webhookIgnored, reasonNotDeliveryFailure, nil
	}
	if data.Bounce != nil && data.Bounce.Type != "" && !strings.EqualFold(data.Bounce.Type, "Permanent") {
		return webhookIgnored, reasonNotDeliveryFailure, nil
	}
	sender := senderAddress(data.From)
	token, replyID := email.PlatformReplyAddress(sender, a.config.Email.MailFromDomain)
	var workspaceIDs []string
	if token != "" {
		workspaceID, found, err := a.store.WorkspaceForReplyToken(request.Context(), token)
		if err != nil || !found {
			return webhookIgnored, "unknown reply token", err
		}
		workspaceIDs = []string{workspaceID}
	} else {
		// Mail sent as a verified support address carries no reply token; its workspace is the one that
		// holds that domain, verified through the email provider. If several hold it, the event counts
		// for each of them rather than for none.
		var err error
		if workspaceIDs, err = a.store.WorkspacesForVerifiedSender(request.Context(), email.Domain(sender)); err != nil || len(workspaceIDs) == 0 {
			return webhookIgnored, "not sent from a reply address or a verified support address", err
		}
	}
	status, reason := webhookIgnored, ""
	for _, workspaceID := range workspaceIDs {
		result, err := a.store.MarkRelayBounce(request.Context(), workspaceID, store.RelayBounce{
			WebhookID: webhookID, EventType: event.Type, ReplyToken: token, ReplyID: replyID,
			MessageID: strings.Trim(strings.TrimSpace(data.MessageID), "<>"), Recipients: bareAddresses(data.To), Complaint: complaint, SenderDomain: token == "",
		})
		if err != nil {
			return "", "", err
		}
		if complaint && (result == store.RelayBounceApplied || result == store.RelayBounceCopy || result == store.RelayBounceDuplicate || result == store.RelayBounceCounted) {
			// A duplicate is checked too: the first delivery may have recorded the complaint and then failed
			// before the pause, and the pause itself happens once.
			if err := a.pauseOnComplaints(request.Context(), workspaceID); err != nil {
				return "", "", err
			}
		}
		switch {
		case result == store.RelayBounceApplied, result == store.RelayBounceCopy, result == store.RelayBounceCounted:
			status, reason = "applied", ""
		case result == store.RelayBounceDuplicate && status != "applied":
			status, reason = "duplicate", ""
		case status == webhookIgnored:
			reason = string(result)
		}
	}
	return status, reason, nil
}

// senderAddress is the address of a From value. The value comes back as it was sent, and a display
// name with a comma or an @ that is not quoted (a workspace named "Acme, Inc.") is not valid RFC 5322,
// so the text inside the last angle brackets is used when the value does not parse.
func senderAddress(from string) string {
	if parsed, err := mail.ParseAddress(from); err == nil {
		return parsed.Address
	}
	open, end := strings.LastIndex(from, "<"), strings.LastIndex(from, ">")
	if open < 0 || end < open {
		return ""
	}
	return strings.TrimSpace(from[open+1 : end])
}

// bareAddresses drops display names; an entry that does not parse is kept as it is.
func bareAddresses(list []string) []string {
	result := make([]string, 0, len(list))
	for _, entry := range list {
		if parsed, err := mail.ParseAddress(entry); err == nil {
			entry = parsed.Address
		}
		result = append(result, entry)
	}
	return result
}
