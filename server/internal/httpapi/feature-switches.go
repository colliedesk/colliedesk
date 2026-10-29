package httpapi

import (
	"net/http"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/serverconfig"
)

// switchedOff reports whether the operator switched the feature off. Sign-up runs on cloud sign-in and
// billing, so it is off with either.
func (a *API) switchedOff(feature string) bool {
	off := a.config.SwitchedOff
	if feature == serverconfig.FeatureSignup && (off[serverconfig.FeatureCloudSignIn] || off[serverconfig.FeatureBilling]) {
		return true
	}
	return off[feature]
}

// writeFeatureOff answers a use of a feature the operator switched off. It never names a setting: someone
// using a workspace cannot change one.
func writeFeatureOff(response http.ResponseWriter) {
	writeError(response, http.StatusConflict, "feature_off", "This feature is turned off on this server. Whoever runs it can turn it on again; nothing it holds was removed.")
}

// featureOfRoute is the feature a route uses, for the routes a switch closes. Reading what a feature holds
// stays open (a way to see its state), and so do the routes that keep its data whole: webhooks, consent
// callbacks, disconnecting a mailbox.
func featureOfRoute(route Route) string {
	if route.Permission == auth.PermissionPublic && route.Pattern != "/api/v1/auth/cloud/exchange" && route.Pattern != cloudSignupPath {
		return ""
	}
	switch {
	case route.Method == http.MethodPost && (route.Pattern == gmailPath || route.Pattern == channelGmailPath):
		return serverconfig.FeatureGmail
	case route.Method == http.MethodPost && (route.Pattern == microsoftPath || route.Pattern == channelMicrosoftPath):
		return serverconfig.FeatureMicrosoft
	case route.Method != http.MethodGet && (strings.HasPrefix(route.Pattern, "/api/v1/autofix/") || strings.HasPrefix(route.Pattern, "/api/v1/conversations/{id}/fixes")):
		return serverconfig.FeatureAutofix
	case billingRoute(route) && route.Pattern != polarWebhookPath:
		return serverconfig.FeatureBilling
	case route.Pattern == "/api/v1/auth/cloud/exchange":
		return serverconfig.FeatureCloudSignIn
	case route.Pattern == cloudSignupPath:
		return serverconfig.FeatureSignup
	}
	return ""
}

// featureGate answers feature_off in place of a route whose feature is switched off.
func (a *API) featureGate(route Route, handler http.HandlerFunc) http.HandlerFunc {
	feature := featureOfRoute(route)
	if feature == "" || !a.switchedOff(feature) {
		return handler
	}
	return func(response http.ResponseWriter, _ *http.Request) { writeFeatureOff(response) }
}

// featuresConfig tells the web app which features run here: on, and with their settings complete.
func (a *API) featuresConfig() map[string]any {
	on := map[string]bool{
		serverconfig.FeatureGmail:       a.config.Google.Enabled(),
		serverconfig.FeatureMicrosoft:   a.config.Microsoft.Enabled(),
		serverconfig.FeatureAutofix:     a.config.GitHub.Enabled(),
		serverconfig.FeatureAIServer:    a.ai.ServerConfigured(),
		serverconfig.FeatureHostedMail:  a.config.Email.HostedMailDomain != "" && a.config.Email.PlatformInboundSecret != "",
		serverconfig.FeatureCloudSignIn: a.cloud != nil,
		serverconfig.FeatureBilling:     a.polar != nil,
		serverconfig.FeatureSignup:      a.cloud != nil && a.polar != nil && !a.config.SignupDisabled,
	}
	features := map[string]any{}
	for _, feature := range serverconfig.Features() {
		features[feature.Name] = map[string]bool{"enabled": on[feature.Name] && !a.switchedOff(feature.Name)}
	}
	return features
}

// featureOn is featuresConfig for one feature.
func (a *API) featureOn(feature string) bool {
	state, _ := a.featuresConfig()[feature].(map[string]bool)
	return state["enabled"]
}

// hostedMailDomain is the domain new workspaces get their hosted address on; empty while hosted addresses
// are switched off. Inbound mail and the isolation checks read the configured domain, never this.
func (a *API) hostedMailDomain() string {
	if a.switchedOff(serverconfig.FeatureHostedMail) {
		return ""
	}
	return a.config.Email.HostedMailDomain
}
