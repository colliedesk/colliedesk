package httpapi

import (
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"net/http"
	"time"
)

func (a *API) redeem(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Token         string `json:"token"`
		RequestedRole string `json:"requested_role"`
		Label         string `json:"label"`
		SessionType   string `json:"session_type"`
	}
	if err := decodeJSON(request, &input); err != nil || input.Token == "" {
		writeError(response, http.StatusBadRequest, "invalid_request", "A one-time token is required.")
		return
	}
	if input.SessionType == "" {
		input.SessionType = "bearer"
	}
	if input.SessionType != "bearer" && input.SessionType != "cookie" {
		writeError(response, http.StatusBadRequest, "invalid_request", "Session type must be cookie or bearer.")
		return
	}
	result, err := a.auth.Redeem(request.Context(), input.Token, input.RequestedRole, input.Label)
	if writePlanLimit(response, err) {
		return
	}
	if err != nil {
		writeError(response, http.StatusUnauthorized, "invalid_link", "This link is invalid, expired, revoked, or already used.")
		return
	}
	if input.SessionType == "cookie" {
		setSessionCookie(response, result.Token)
		result.Token = ""
	}
	writeJSON(response, http.StatusOK, result)
}

func setSessionCookie(response http.ResponseWriter, token string) {
	http.SetCookie(response, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", HttpOnly: true,
		Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: int((30 * 24 * time.Hour).Seconds()),
	})
}

func (a *API) logout(response http.ResponseWriter, request *http.Request) {
	if err := a.auth.Logout(request.Context(), principal(request)); err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "The session could not be revoked.")
		return
	}
	http.SetCookie(response, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	response.WriteHeader(http.StatusNoContent)
}

func (a *API) me(response http.ResponseWriter, request *http.Request) {
	identity := principal(request)
	effectiveTimezone := identity.Workspace.Timezone
	if identity.Agent.TimezoneOverride != nil {
		effectiveTimezone = *identity.Agent.TimezoneOverride
	}
	// plan is null without billing (self-hosted) so the client shows no trial or lock.
	var plan *domain.Access
	if a.limits {
		access, err := a.store.WorkspaceAccess(request.Context(), identity.Workspace.ID, identity.TrialEndsAt, a.now())
		if err != nil {
			writeInternalError(response, err)
			return
		}
		plan = &access
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"agent": identity.Agent, "workspace": identity.Workspace, "timezone": effectiveTimezone, "plan": plan,
	})
}

func (a *API) listSessions(response http.ResponseWriter, request *http.Request) {
	sessions, err := a.auth.ListSessions(request.Context(), principal(request))
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": sessions})
}

func (a *API) revokeSession(response http.ResponseWriter, request *http.Request) {
	ok, err := a.auth.RevokeSession(request.Context(), principal(request), request.PathValue("id"))
	if err != nil {
		writeStoreError(response, err)
		return
	}
	if !ok {
		writeError(response, http.StatusNotFound, "not_found", "The session was not found.")
		return
	}
	response.WriteHeader(http.StatusNoContent)
}
