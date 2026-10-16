package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/polar"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

const (
	polarWebhookPath = "/api/v1/webhooks/polar"
	maxWebhookBytes  = 1 << 20
	// webhookIgnored is the answer for a verified event this instance has nothing to do with.
	webhookIgnored = "ignored"

	reasonNoWorkspaceID     = "customer has no workspace id as external id"
	reasonWorkspaceNotFound = "workspace not found"
)

var workspaceIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// receivePolarWebhook applies Polar subscription events. The signature is the only credential:
// nothing is read from the database until it verifies. Every verified event that is understood
// gets a 2xx so Polar stops retrying; the status says what happened. The workspace is the event's
// customer.external_id, which the server set to the workspace id when it created the checkout.
func (a *API) receivePolarWebhook(response http.ResponseWriter, request *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(response, request.Body, maxWebhookBytes))
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "The webhook body could not be read.")
		return
	}
	if err := polar.VerifyWebhook(a.config.Billing.WebhookSecret, request.Header, body, time.Now()); err != nil {
		writeError(response, http.StatusUnauthorized, "unauthorized", "A valid webhook signature is required.")
		return
	}
	event, err := polar.ParseEvent(body)
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "The webhook body is not a Polar event.")
		return
	}
	if event.Subscription == nil {
		writeJSON(response, http.StatusOK, map[string]string{"status": webhookIgnored})
		return
	}
	if a.isFoundingProduct(event.Subscription.ProductID) {
		// Whatever happens to the event, the slot count may have changed, including for a buyer
		// of the checkout link who has no workspace here.
		defer a.refreshFoundingAfterWebhook(request.Context())
	}
	incoming, workspaceID, reason := a.subscriptionFromEvent(event)
	webhookID := request.Header.Get("webhook-id")
	if reason != "" {
		warnIgnoredSubscription(event, webhookID, reason)
		if reason == reasonNoWorkspaceID {
			a.alertUnmatchedPayment(request.Context(), event, webhookID, reason)
		}
		writeJSON(response, http.StatusOK, map[string]string{"status": webhookIgnored})
		return
	}
	// A workspace becomes a paying customer once: Polar also sends subscription.active when a
	// past_due subscription is paid, and a workspace may later switch plans. Read before applying,
	// because applying records this webhook.
	firstActivation := false
	if event.Type == "subscription.active" && a.config.Funnel != nil {
		activated, err := a.store.HasBeenActivated(request.Context(), workspaceID)
		if err != nil {
			writeInternalError(response, err)
			return
		}
		firstActivation = !activated
	}
	result, err := a.store.ApplySubscription(request.Context(), workspaceID, webhookID, event.Type, incoming)
	if err != nil {
		// 500 makes Polar redeliver; nothing was recorded, so the retry is applied.
		writeInternalError(response, err)
		return
	}
	// A redelivery is a duplicate and a stale event changes nothing; neither is counted.
	if result == store.SubscriptionApplied && firstActivation {
		a.recordFunnel("subscription_activated", workspaceID, map[string]any{"plan": incoming.Plan})
	}
	status := string(result)
	if result == store.SubscriptionUnknownWorkspace {
		status = webhookIgnored
		warnIgnoredSubscription(event, webhookID, reasonWorkspaceNotFound)
		a.alertUnmatchedPayment(request.Context(), event, webhookID, reasonWorkspaceNotFound)
	}
	writeJSON(response, http.StatusOK, map[string]string{"status": status})
}

// warnIgnoredSubscription leaves a trace of a verified subscription event that changed nothing: it
// may be a payment nobody sees. The log carries ids only, never customer data.
func warnIgnoredSubscription(event polar.Event, webhookID, reason string) {
	subscriptionID := ""
	if event.Subscription != nil {
		subscriptionID = event.Subscription.ID
	}
	slog.Warn("polar subscription event ignored", "reason", reason, "event", event.Type, "subscription_id", subscriptionID, "webhook_id", webhookID)
}

// subscriptionFromEvent returns the stored form of a subscription event and the workspace it
// belongs to. reason is set, and nothing else, when the event cannot be attached to a workspace of
// this instance.
func (a *API) subscriptionFromEvent(event polar.Event) (domain.Subscription, string, string) {
	sub := event.Subscription
	if !workspaceIDPattern.MatchString(sub.ExternalCustomerID) {
		return domain.Subscription{}, "", reasonNoWorkspaceID
	}
	plan := a.planOfProduct(sub.ProductID)
	if plan == "" {
		return domain.Subscription{}, "", "product is not sold here"
	}
	return sub.Stored(plan), sub.ExternalCustomerID, ""
}

// planOfProduct is the plan a Polar product buys, or "" when the product is not sold here.
func (a *API) planOfProduct(productID string) string {
	return a.config.Billing.Products.PlanOf(productID)
}

// recordFunnel passes a sign-up or payment event to the configured funnel, if any.
func (a *API) recordFunnel(event, workspaceID string, properties map[string]any) {
	if a.config.Funnel != nil {
		a.config.Funnel.Record(event, workspaceID, properties)
	}
}
