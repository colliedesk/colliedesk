package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/serverconfig"
)

func (a *API) getConfig(response http.ResponseWriter, _ *http.Request) {
	enabled := a.featureOn(serverconfig.FeatureCloudSignIn)
	var publishableKey *string
	if enabled {
		publishableKey = &a.config.CloudAuth.PublishableKey
	}
	version := a.config.Version
	if version == "" {
		version = "dev"
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"version":       version,
		"cloud_sign_in": map[string]any{"enabled": enabled, "publishable_key": publishableKey},
		// switched_off: billing is set up but the operator switched it off; a workspace with a subscription
		// still sees its plan and the portal.
		"billing": map[string]any{"enabled": a.featureOn(serverconfig.FeatureBilling), "switched_off": a.polar != nil && a.switchedOff(serverconfig.FeatureBilling)},
		// Same condition as signUpCloud: 404 unless both are configured, 503 while its settings are missing,
		// 409 feature_off while an operator switched it (or cloud sign-in, or billing) off.
		"signup":            map[string]any{"enabled": a.featureOn(serverconfig.FeatureSignup)},
		"limits":            map[string]any{"outgoing_attachments_bytes": attachments.MaxOutgoingBytes, "outgoing_attachments_files": attachments.MaxOutgoingFiles},
		"product_analytics": a.productAnalyticsConfig(),
		"ai":                map[string]any{"server_provider": a.ai.ServerConfigured()},
		"autofix":           a.autofixConfig(),
		"features":          a.featuresConfig(),
	})
}

// productAnalyticsConfig tells the web app whether, and where, to report usage. With telemetry
// turned off the web app never loads the analytics library.
func (a *API) productAnalyticsConfig() map[string]any {
	analytics := a.config.ProductAnalytics
	if analytics == nil {
		return map[string]any{"enabled": false, "key": nil, "host": nil, "realm": nil}
	}
	return map[string]any{"enabled": true, "key": analytics.Key, "host": analytics.Host, "realm": analytics.Realm}
}

func (a *API) exchangeCloudSession(response http.ResponseWriter, request *http.Request) {
	if a.cloud == nil {
		writeError(response, http.StatusNotFound, "not_found", "Cloud sign-in is not configured.")
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
		WorkspaceSlug string `json:"workspace_slug"`
		Label         string `json:"label"`
		Timezone      string `json:"timezone"`
	}
	if request.ContentLength != 0 {
		if err := decodeJSON(request, &input); err != nil {
			writeError(response, http.StatusUnauthorized, "unauthorized", "A valid cloud session is required.")
			return
		}
	}
	email, err := a.cloud.Verify(request.Context(), token)
	a.countTrialSignIn(err == nil)
	if err != nil {
		writeError(response, http.StatusUnauthorized, "unauthorized", "A valid cloud session is required.")
		return
	}
	label := input.Label
	if label == "" {
		label = "Cloud sign-in"
	}
	result, choices, err := a.auth.ExchangeCloud(request.Context(), email, input.WorkspaceSlug, label)
	if errors.Is(err, auth.ErrCloudAgentNotFound) {
		writeError(response, http.StatusForbidden, "forbidden", "This email is not an agent of the requested workspace.")
		return
	}
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "Cloud sign-in could not create a session.")
		return
	}
	if len(choices) > 0 {
		writeJSON(response, http.StatusConflict, map[string]any{"workspaces": choices})
		return
	}
	a.startOnBrowserTimezone(request.Context(), &result, input.Timezone)
	setSessionCookie(response, result.Token)
	result.Token = ""
	writeJSON(response, http.StatusOK, result)
}

func bearerToken(header string) (string, bool) {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	return token, token != ""
}

// autofixConfig tells the web app whether Autofix can be used on this server (its GitHub App is set up)
// and, when not, which settings the operator still needs; the web app hides Autofix while it is off.
func (a *API) autofixConfig() map[string]any {
	// Public: never the names of the server's settings, only why Autofix is off (the console names them).
	off := a.offReason(serverconfig.FeatureAutofix, a.config.GitHub.Missing())
	return map[string]any{"available": off == nil, "missing_settings": []string{}, "off_reason": off}
}
