package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
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
		plan = &identity.Access
	}
	// scope is the tier of an agent token and null for a person's session.
	var scope *string
	if identity.Scope != "" {
		scope = &identity.Scope
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"agent": identity.Agent, "workspace": identity.Workspace, "timezone": effectiveTimezone, "scope": scope, "plan": plan,
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

var sessionRevokeRoute = Route{http.MethodDelete, "/api/v1/auth/sessions/{id}", auth.PermissionAccessRead}

func (a *API) revokeSession(response http.ResponseWriter, request *http.Request) {
	// Signing another person out waits for a person whatever the token's tier; the creator's own
	// sign-ins and any agent token are revoked directly.
	if who := principal(request); who.Scope != "" {
		target, found, err := a.workspaceSession(request.Context(), who, request.PathValue("id"))
		if err != nil {
			writeStoreError(response, err)
			return
		}
		if found && target.Scope == nil && target.AgentID != who.Agent.ID {
			a.fileAgentRequest(response, request, sessionRevokeRoute, who, domain.AgentRequestAction, json.RawMessage{}, "")
			return
		}
	}
	ok, err := a.auth.RevokeSession(request.Context(), principal(request), request.PathValue("id"))
	if err != nil {
		writeStoreError(response, err)
		return
	}
	if !ok {
		writeError(response, http.StatusNotFound, "not_found", "The session was not found.")
		return
	}
	if who := principal(request); who.Scope != "" {
		// The session list shows the revocation; the log also names the token that did it.
		slog.Info("agent token revoked a session", "workspace_id", who.Workspace.ID, "token_session_id", who.SessionID, "revoked_session_id", request.PathValue("id"))
	}
	response.WriteHeader(http.StatusNoContent)
}

func (a *API) createAgentToken(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Label string `json:"label"`
		Scope string `json:"scope"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "A label and a scope are required.")
		return
	}
	created, err := a.auth.CreateAgentToken(request.Context(), principal(request), input.Label, input.Scope)
	var invalid *auth.InvalidTokenError
	if errors.As(err, &invalid) {
		writeError(response, http.StatusBadRequest, "invalid_request", invalid.Reason)
		return
	}
	if errors.Is(err, auth.ErrAdminTokenNeedsAdmin) {
		writeError(response, http.StatusForbidden, "admin_token_needs_admin", "Only a workspace admin can create an admin token. Choose read, draft or send.")
		return
	}
	if err != nil {
		writeInternalError(response, err)
		return
	}
	// The raw token is in the body once; no cache may keep it.
	response.Header().Set("Cache-Control", "no-store")
	writeJSON(response, http.StatusCreated, created)
}
