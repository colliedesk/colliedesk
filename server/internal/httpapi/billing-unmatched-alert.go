package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/polar"
)

const (
	polarStatusActive = "active"
	alertSendTimeout  = 8 * time.Second
	// maxAlertedSubscriptions bounds the memory of subscriptions already reported.
	maxAlertedSubscriptions = 4096
)

// alertedSubscriptions remembers which subscriptions the operator already heard about, so the
// several events Polar sends for one purchase (created, active, updated) and its redeliveries
// produce one email. It lives in memory: after a restart a later event for the same subscription
// may alert once more, which is cheaper than another table for a rare condition.
type alertedSubscriptions struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

// first reports true the first time it sees the id.
func (a *alertedSubscriptions) first(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.seen[id]; ok {
		return false
	}
	if a.seen == nil || len(a.seen) >= maxAlertedSubscriptions {
		a.seen = map[string]struct{}{}
	}
	a.seen[id] = struct{}{}
	return true
}

// forget lets a later event retry after a failed send.
func (a *alertedSubscriptions) forget(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.seen, id)
}

// alertUnmatchedPayment tells the operator that money was taken for a subscription that no
// workspace received, which is a customer who paid and got nothing. Only an active subscription is
// reported. The alert is best effort: a failed send is logged at ERROR and the webhook still
// answers 200, so Polar does not redeliver. A later event for the same subscription tries again;
// the reconciliation of Polar subscriptions against workspaces is the safety net for the rest.
func (a *API) alertUnmatchedPayment(ctx context.Context, event polar.Event, webhookID, reason string) {
	sub := event.Subscription
	if sub == nil || sub.Status != polarStatusActive {
		return
	}
	if !a.operatorAlertReady() {
		slog.Warn("paid subscription has no workspace and no operator alert is configured (set billing.alert_email, smtp.host and mail_from.domain with collie config or the operator console)", "subscription_id", sub.ID, "webhook_id", webhookID)
		return
	}
	if !a.alerted.first(sub.ID) {
		return
	}
	plan := a.planOfProduct(sub.ProductID)
	if plan == "" {
		plan = "unknown product"
	}
	body := fmt.Sprintf(`A paid Polar subscription reached Colliedesk but belongs to no workspace, so the buyer paid and received nothing.

Reason: %s
Buyer email: %s
Plan: %s
Polar product: %s
Polar subscription: %s
Polar customer: %s
Status: %s
Event: %s
Webhook: %s

Create or find the buyer's workspace and apply the plan (collie plan), or refund the subscription in Polar.
`, reason, valueOrDash(sub.CustomerEmail), plan, sub.ProductID, sub.ID, sub.CustomerID, sub.Status, event.Type, webhookID)
	if err := a.sendOperatorAlert(ctx, "Colliedesk: paid subscription without a workspace ("+plan+")", body); err != nil {
		slog.Error("send operator alert", "error", err, "subscription_id", sub.ID)
		a.alerted.forget(sub.ID)
	}
}

// operatorAlertReady reports whether an operator alert can be sent: an address, a relay and a platform
// domain to send from.
func (a *API) operatorAlertReady() bool {
	config := a.config
	return config.AlertEmail != "" && config.AlertMailer != nil && config.Email.MailFromDomain != ""
}

// sendOperatorAlert mails the operator address (BILLING_ALERT_EMAIL) from noreply@MAIL_FROM_DOMAIN. The
// send outlives the request that caused it, so a client that gives up does not drop the alert.
func (a *API) sendOperatorAlert(ctx context.Context, subject, body string) error {
	config := a.config
	from := "noreply@" + config.Email.MailFromDomain
	messageID, err := email.NewMessageID(config.Email.MailFromDomain)
	if err != nil {
		return fmt.Errorf("compose operator alert: %w", err)
	}
	message, err := email.Compose(email.Outgoing{FromName: "Colliedesk", FromAddress: from, ToAddress: config.AlertEmail,
		Subject: subject, Body: body, MessageID: messageID, Date: a.now(), Standalone: true})
	if err != nil {
		return fmt.Errorf("compose operator alert: %w", err)
	}
	sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), alertSendTimeout)
	defer cancel()
	return config.AlertMailer.Send(sendCtx, from, []string{config.AlertEmail}, message)
}

func valueOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
