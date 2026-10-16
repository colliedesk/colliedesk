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
	// relayClockSkew covers the gap between the relay creating its record of an email and the outbox
	// recording the reply as sent, which happens after the relay answered.
	relayClockSkew = 2 * time.Minute
	// reasonNotDeliveryFailure is an event the webhook does not act on (delivered, opened, a
	// temporary bounce); it is not logged.
	reasonNotDeliveryFailure = "not a delivery failure"
)

// resendEvent is the part of a Resend email event the bounce handling reads.
type resendEvent struct {
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
	Data      struct {
		EmailID   string    `json:"email_id"`
		MessageID string    `json:"message_id"`
		From      string    `json:"from"`
		To        []string  `json:"to"`
		CreatedAt time.Time `json:"created_at"`
		Bounce    *struct {
			Type string `json:"type"`
		} `json:"bounce"`
	} `json:"data"`
}

// receiveResendWebhook marks a reply failed when Resend reports it bounced or was marked as spam.
// Resend owns the envelope sender and rewrites the Message-ID, so its bounces never reach
// MAIL_FROM_DOMAIN. The reply is found from the reply+<token>@MAIL_FROM_DOMAIN address it was sent
// from; a reply sent as a verified support address cannot be matched and is ignored. The Svix
// signature is the only credential: nothing is read from the database until it verifies.
func (a *API) receiveResendWebhook(response http.ResponseWriter, request *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(response, request.Body, maxWebhookBytes))
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "The webhook body could not be read.")
		return
	}
	header := request.Header
	if err := standardwebhooks.Verify(a.config.Email.ResendWebhookSecret, header.Get("svix-id"), header.Get("svix-timestamp"),
		header.Get("svix-signature"), body, time.Now()); err != nil {
		writeError(response, http.StatusUnauthorized, "unauthorized", "A valid webhook signature is required.")
		return
	}
	var event resendEvent
	if err := json.Unmarshal(body, &event); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "The webhook body is not a Resend event.")
		return
	}
	bounce, workspaceID, reason, err := a.relayBounceFromEvent(request, event)
	if err == nil && reason == "" {
		var matched bool
		matched, err = a.store.MarkRelayBounce(request.Context(), workspaceID, bounce)
		if !matched {
			reason = "no sent reply to that recipient"
		}
	}
	if err != nil {
		// 500 makes Resend redeliver; nothing was recorded, so the retry is applied.
		writeInternalError(response, err)
		return
	}
	if reason == "" {
		writeJSON(response, http.StatusOK, map[string]string{"status": "applied"})
		return
	}
	if reason != reasonNotDeliveryFailure {
		// Ids only: the addresses are customer data.
		slog.Info("resend event ignored", "reason", reason, "event", event.Type, "email_id", event.Data.EmailID, "webhook_id", header.Get("svix-id"))
	}
	writeJSON(response, http.StatusOK, map[string]string{"status": webhookIgnored})
}

// relayBounceFromEvent returns the bounce to record and its workspace, or the reason the event changes
// nothing.
func (a *API) relayBounceFromEvent(request *http.Request, event resendEvent) (store.RelayBounce, string, string, error) {
	data := event.Data
	complaint := event.Type == "email.complained"
	if event.Type != "email.bounced" && !complaint {
		return store.RelayBounce{}, "", reasonNotDeliveryFailure, nil
	}
	if data.Bounce != nil && data.Bounce.Type != "" && !strings.EqualFold(data.Bounce.Type, "Permanent") {
		return store.RelayBounce{}, "", reasonNotDeliveryFailure, nil
	}
	from, err := mail.ParseAddress(data.From)
	if err != nil {
		return store.RelayBounce{}, "", "sender is not an address", nil
	}
	token := email.PlatformReplyToken([]string{from.Address}, a.config.Email.MailFromDomain)
	if token == "" {
		return store.RelayBounce{}, "", "not sent from a reply address", nil
	}
	workspaceID, found, err := a.store.WorkspaceForReplyToken(request.Context(), token)
	if err != nil || !found {
		return store.RelayBounce{}, "", "unknown reply token", err
	}
	createdAt := data.CreatedAt
	if createdAt.IsZero() {
		createdAt = event.CreatedAt
	}
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	return store.RelayBounce{
		ReplyToken: token, MessageID: strings.Trim(strings.TrimSpace(data.MessageID), "<>"), Recipients: data.To,
		SentBefore: createdAt.Add(relayClockSkew), Complaint: complaint,
	}, workspaceID, "", nil
}
