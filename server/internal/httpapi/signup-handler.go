package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/telemetry"
)

const (
	cloudSignupPath  = "/api/v1/auth/cloud/signup"
	brandNameMessage = "This name is not available. Choose another name, or contact us if it is your own brand."
)

// refuseBrand answers 422 and reports true when name contains a denied brand on the hosted cloud,
// where a workspace's names end up in mail headers, its subdomain and its help center. A self-hosted
// server names things as its operator likes.
func (a *API) refuseBrand(response http.ResponseWriter, name string) bool {
	if _, denied := domain.DeniedBrand(name); denied && a.limits {
		writeError(response, http.StatusUnprocessableEntity, "name_not_allowed", brandNameMessage)
		return true
	}
	return false
}

// signUpCloud starts a trial: a person whose email the identity provider verified names a workspace
// and becomes its admin. A server without both cloud sign-in and billing answers 404. The response is the redeemed session, as for cloud sign-in.
func (a *API) signUpCloud(response http.ResponseWriter, request *http.Request) {
	// Only the hosted cloud, where cloud sign-in and billing are both configured, can start trials.
	if a.cloud == nil || a.polar == nil {
		writeError(response, http.StatusNotFound, "not_found", "Cloud sign-up is not configured.")
		return
	}
	if a.config.SignupDisabled {
		writeError(response, http.StatusServiceUnavailable, "signup_unavailable", "Sign-up is temporarily unavailable. Please try again later.")
		return
	}
	if request.Header.Get("Origin") != a.config.Origin {
		writeError(response, http.StatusForbidden, "csrf_rejected", "Origin does not match this Colliedesk instance.")
		return
	}
	token, ok := bearerToken(request.Header.Get("Authorization"))
	if !ok {
		writeError(response, http.StatusUnauthorized, "unauthorized", "A valid cloud session is required.")
		return
	}
	var input struct {
		WorkspaceName string  `json:"workspace_name"`
		Goal          *string `json:"goal"`
		Role          *string `json:"role"`
		Source        *string `json:"source"`
		Timezone      string  `json:"timezone"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "A workspace name is required.")
		return
	}
	onboarding := domain.Onboarding{Goal: input.Goal, Role: input.Role, Source: input.Source}
	verified, err := a.cloud.Verify(request.Context(), token)
	if err != nil {
		writeError(response, http.StatusUnauthorized, "unauthorized", "A valid cloud session is required.")
		return
	}
	ip := clientIP(request, a.config.TrustedProxies)
	workspace, err := a.auth.SignUpCloud(request.Context(), auth.SignUp{Email: verified, Name: input.WorkspaceName, ClientIP: ip, HostedMailDomain: a.config.Email.HostedMailDomain, Onboarding: onboarding})
	switch {
	case errors.Is(err, auth.ErrInvalidEmail):
		writeError(response, http.StatusBadRequest, "invalid_request", "The identity provider returned an email address this service cannot use.")
		return
	case errors.Is(err, auth.ErrInvalidOnboarding):
		writeError(response, http.StatusBadRequest, "invalid_request", "goal, role and source must each be one of their listed values.")
		return
	case errors.Is(err, auth.ErrInvalidName):
		writeError(response, http.StatusBadRequest, "invalid_request", "The workspace name must be 2 to 60 characters.")
		return
	case errors.Is(err, auth.ErrNameNotAllowed):
		writeError(response, http.StatusUnprocessableEntity, "name_not_allowed", brandNameMessage)
		return
	case errors.Is(err, auth.ErrTrialAlreadyUsed):
		writeError(response, http.StatusConflict, "trial_already_used", "This email already started a Colliedesk trial. Sign in instead, or contact us.")
		return
	case errors.Is(err, auth.ErrSignupRateLimited):
		writeError(response, http.StatusTooManyRequests, "rate_limited", "Too many sign-ups from this network. Try again tomorrow, or contact us.")
		return
	case errors.Is(err, auth.ErrSlugUnavailable):
		writeError(response, http.StatusConflict, "slug_unavailable", "No address was free for this name. Try a different name.")
		return
	case err != nil:
		writeInternalError(response, err)
		return
	}
	// Never the email, and the address only at debug level: the trial record keeps no raw address.
	slog.Info("cloud sign-up", "workspace", workspace.Slug)
	slog.Debug("cloud sign-up client", "client_ip", ip)
	result, _, err := a.auth.ExchangeCloud(request.Context(), verified, workspace.Slug, "Cloud sign-up")
	if err != nil {
		writeInternalError(response, err)
		return
	}
	// Counted only once the person has a session, so a failed sign-in is not a started trial. The
	// answers are fixed lists, never free text, and the event carries no email.
	properties := onboarding.Properties()
	if visitor := telemetry.WebsiteVisitorID(request); visitor != "" {
		if properties == nil {
			properties = map[string]any{}
		}
		properties["website_visitor_id"] = visitor
	}
	if a.hot().InternalSignups.Match(verified, ip) {
		if properties == nil {
			properties = map[string]any{}
		}
		properties["internal"] = true
	}
	a.recordFunnel("trial_started", workspace.ID, properties)
	a.startOnBrowserTimezone(request.Context(), &result, input.Timezone)
	setSessionCookie(response, result.Token)
	result.Token = ""
	writeJSON(response, http.StatusCreated, result)
}
