package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/hcanalytics"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

func (a *API) getHelpCenterAnalytics(response http.ResponseWriter, request *http.Request) {
	settings, err := a.store.GetHelpCenterAnalytics(request.Context(), principal(request).Workspace.ID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, settings)
}

// putHelpCenterAnalytics saves where the page views go. ga4_api_secret left out or null keeps the
// stored secret, "" removes it; it is never answered. The sender reloads the workspace's target, so
// the next page view goes to the new place.
func (a *API) putHelpCenterAnalytics(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Provider      string  `json:"provider"`
		Enabled       bool    `json:"enabled"`
		MeasurementID *string `json:"ga4_measurement_id"`
		APISecret     *string `json:"ga4_api_secret"`
		Domain        *string `json:"plausible_domain"`
		BaseURL       *string `json:"plausible_base_url"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request",
			"Send a JSON object with provider, enabled, ga4_measurement_id, ga4_api_secret, plausible_domain and plausible_base_url.")
		return
	}
	text := func(value *string) string {
		if value == nil {
			return ""
		}
		return *value
	}
	change, err := a.config.Analytics.Clean(hcanalytics.Change{Provider: input.Provider, Enabled: input.Enabled,
		MeasurementID: text(input.MeasurementID), APISecret: input.APISecret, Domain: text(input.Domain), BaseURL: text(input.BaseURL)})
	if err != nil {
		writeThemeError(response, err)
		return
	}
	workspaceID := principal(request).Workspace.ID
	settings, err := a.store.PutHelpCenterAnalytics(request.Context(), workspaceID, principal(request).Agent.ID, change)
	if errors.Is(err, store.ErrSealKeysMissing) {
		writeError(response, http.StatusConflict, "secret_keys_missing",
			"This server has no MAILBOX_TOKEN_KEYS, so it cannot keep a GA4 API secret. Ask whoever runs it to set them, or use Plausible, which needs no secret. Nothing was saved.")
		return
	}
	if err != nil {
		writeThemeError(response, err)
		return
	}
	if a.config.Analytics != nil {
		if err := a.config.Analytics.Reload(request.Context(), workspaceID); err != nil {
			slog.Warn("help center analytics: reloading the saved target failed; the next reload applies it", "workspace", workspaceID, "error", err)
		}
	}
	writeJSON(response, http.StatusOK, settings)
}

// testHelpCenterAnalytics sends one test page view to the saved service, on or off, as the person
// asking (their browser and address), and answers what the service said.
func (a *API) testHelpCenterAnalytics(response http.ResponseWriter, request *http.Request) {
	if a.config.Analytics == nil {
		writeError(response, http.StatusServiceUnavailable, "analytics_unavailable", "This server does not send help center page views, so it cannot send a test.")
		return
	}
	workspaceID := principal(request).Workspace.ID
	target, err := a.store.HelpCenterAnalyticsTarget(request.Context(), workspaceID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	if target == nil {
		writeError(response, http.StatusConflict, "analytics_not_set",
			"Save Google Analytics 4 or Plausible first (PUT /api/v1/help-center/analytics), then send a test.")
		return
	}
	writeJSON(response, http.StatusOK, a.config.Analytics.Test(request.Context(), *target, request.UserAgent(), clientIP(request, a.config.TrustedProxies)))
}
