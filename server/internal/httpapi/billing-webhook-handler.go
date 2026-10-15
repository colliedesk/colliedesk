package httpapi

import (
	"io"
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
	incoming, workspaceID, ok := a.subscriptionFromEvent(event)
	if !ok {
		writeJSON(response, http.StatusOK, map[string]string{"status": webhookIgnored})
		return
	}
	result, err := a.store.ApplySubscription(request.Context(), workspaceID, request.Header.Get("webhook-id"), event.Type, incoming, time.Now().UTC())
	if err != nil {
		// 500 makes Polar redeliver; nothing was recorded, so the retry is applied.
		writeInternalError(response, err)
		return
	}
	status := string(result)
	if result == store.SubscriptionUnknownWorkspace {
		status = webhookIgnored
	}
	writeJSON(response, http.StatusOK, map[string]string{"status": status})
}

// subscriptionFromEvent returns the stored form of a subscription event and the workspace it
// belongs to. ok is false for events that carry no workspace of this instance or a product that is
// not sold here.
func (a *API) subscriptionFromEvent(event polar.Event) (domain.Subscription, string, bool) {
	sub := event.Subscription
	if sub == nil || !workspaceIDPattern.MatchString(sub.ExternalCustomerID) {
		return domain.Subscription{}, "", false
	}
	var plan string
	switch products := a.config.Billing.Products; sub.ProductID {
	case products.TeamMonthly, products.TeamYearly:
		plan = domain.PlanTeam
	case products.Founding:
		plan = domain.PlanFounding
	default:
		return domain.Subscription{}, "", false
	}
	return domain.Subscription{
		Plan: plan, SubscriptionID: sub.ID, CustomerID: sub.CustomerID, Status: sub.Status,
		CurrentPeriodEnd: sub.CurrentPeriodEnd, CancelAtPeriodEnd: sub.CancelAtPeriodEnd, EndedAt: sub.EndedAt, ModifiedAt: sub.ModifiedAt,
	}, sub.ExternalCustomerID, true
}
