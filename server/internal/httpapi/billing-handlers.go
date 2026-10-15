package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/polar"
)

// billingRoute reports whether a route exists only when Polar billing is configured.
func billingRoute(route Route) bool {
	return strings.HasPrefix(route.Pattern, "/api/v1/billing") || route.Pattern == polarWebhookPath
}

func (a *API) getBilling(response http.ResponseWriter, request *http.Request) {
	billing, err := a.store.GetBilling(request.Context(), principal(request).Workspace.ID, a.now())
	if err != nil {
		writeInternalError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, billing)
}

// checkoutProduct maps a plan name from the client to the configured Polar product.
func (a *API) checkoutProduct(plan string) (string, bool) {
	products := a.config.Billing.Products
	switch plan {
	case "team_monthly":
		return products.TeamMonthly, true
	case "team_yearly":
		return products.TeamYearly, true
	case "founding":
		return products.Founding, true
	}
	return "", false
}

// billingEmail is the fallback address for a workspace's Polar customer when the admin's own address
// already belongs to another customer (Polar keeps one customer per email, and one person can
// administer several workspaces). It carries a workspace tag after "+", which providers that support
// subaddressing deliver to the same mailbox. The workspace id, not this address, is the identity.
func billingEmail(email, workspaceID string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 || len(workspaceID) < 8 {
		return email
	}
	return email[:at] + "+cd" + strings.ReplaceAll(workspaceID, "-", "")[:8] + email[at:]
}

// createCheckout starts a Polar checkout for the signed-in workspace. The workspace id comes from
// the session and is the external id of its Polar customer; nothing about the customer is read from
// the request, so one workspace can never pay for, or be linked to, another.
func (a *API) createCheckout(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Plan string `json:"plan"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "plan must be team_monthly, team_yearly or founding.")
		return
	}
	product, ok := a.checkoutProduct(input.Plan)
	if !ok {
		writeError(response, http.StatusBadRequest, "invalid_request", "plan must be team_monthly, team_yearly or founding.")
		return
	}
	who := principal(request)
	current, err := a.store.GetBilling(request.Context(), who.Workspace.ID, a.now())
	if err != nil {
		writeInternalError(response, err)
		return
	}
	if current.Plan != domain.PlanFree {
		writeError(response, http.StatusConflict, "already_subscribed", "This workspace already has a paid plan. Change or cancel it from the billing portal.")
		return
	}
	customerID, err := a.polar.EnsureCustomer(request.Context(), who.Workspace.ID, who.Agent.Email, who.Workspace.Name)
	if errors.Is(err, polar.ErrCustomerConflict) {
		// The admin's address belongs to another Polar customer: the same person pays for another
		// workspace. Receipts matter, so the real address is tried first and the tag only here.
		slog.Warn("polar customer email already in use; retrying with a workspace-tagged address", "workspace_id", who.Workspace.ID)
		customerID, err = a.polar.EnsureCustomer(request.Context(), who.Workspace.ID, billingEmail(who.Agent.Email, who.Workspace.ID), who.Workspace.Name)
	}
	if err != nil {
		writeBillingProviderError(response, err)
		return
	}
	url, err := a.polar.CreateCheckout(request.Context(), polar.CheckoutRequest{
		ProductID: product, CustomerID: customerID, SuccessURL: a.config.Origin + "/settings/billing?checkout=success",
	})
	if err != nil {
		writeBillingProviderError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]string{"url": url})
}

// createPortal opens the Polar customer portal, where the customer changes the card, downloads
// invoices or cancels. It stays available after a subscription ends.
func (a *API) createPortal(response http.ResponseWriter, request *http.Request) {
	workspaceID := principal(request).Workspace.ID
	known, err := a.store.HasBillingCustomer(request.Context(), workspaceID)
	if err != nil {
		writeInternalError(response, err)
		return
	}
	if !known {
		writeError(response, http.StatusNotFound, "not_found", "This workspace has no billing account yet.")
		return
	}
	url, err := a.polar.CreatePortalSession(request.Context(), workspaceID, a.config.Origin+"/settings/billing")
	if errors.Is(err, polar.ErrCustomerNotFound) {
		writeError(response, http.StatusNotFound, "not_found", "This workspace has no billing account yet.")
		return
	}
	if err != nil {
		writeBillingProviderError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]string{"url": url})
}

// writeBillingProviderError keeps the cause in the server log and gives the client nothing from Polar.
func writeBillingProviderError(response http.ResponseWriter, err error) {
	slog.Error("billing provider request failed", "error", err)
	writeError(response, http.StatusBadGateway, "billing_provider_error", "The billing provider could not be reached. Try again.")
}
