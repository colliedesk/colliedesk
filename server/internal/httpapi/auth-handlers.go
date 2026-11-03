package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
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
		// Timezone is the browser's IANA zone: a first sign-in starts the agent on it.
		Timezone string `json:"timezone"`
		// Next is the page a browser signing in on APP_ORIGIN opens on its workspace's host.
		Next string `json:"next"`
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
	result, err := a.auth.Redeem(request.Context(), input.Token, input.RequestedRole, input.Label, input.SessionType == "cookie")
	if writePlanLimit(response, err) {
		return
	}
	if err != nil {
		writeError(response, http.StatusUnauthorized, "invalid_link", "This link is invalid, expired, revoked, or already used.")
		return
	}
	// A person who joins takes over the imported replies and tickets of the Freshdesk agent with
	// their email. A failure never stops the sign-in; a person can link them in the import page.
	if _, err := a.imports.LinkByEmail(request.Context(), result.Workspace.ID, result.Agent.ID, result.Agent.Email); err != nil {
		slog.Error("link imported Freshdesk agent", "workspace_id", result.Workspace.ID, "error", err)
	}
	a.startOnBrowserTimezone(request.Context(), &result, input.Timezone)
	if input.SessionType == "cookie" && !a.finishBrowserSignIn(response, request, &result, input.Next) {
		return
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
	clearSessionCookie(response)
	response.WriteHeader(http.StatusNoContent)
}

func clearSessionCookie(response http.ResponseWriter) {
	http.SetCookie(response, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
}

// mePlan is the plan of GET /auth/me: the workspace's access, and for a non-admin person without a paid
// plan the admins to ask.
type mePlan struct {
	domain.Access
	Admins []string `json:"admins,omitempty"`
}

// maxPlanNames bounds the people listed to someone who must ask one of them for a plan.
const maxPlanNames = 5

func (a *API) me(response http.ResponseWriter, request *http.Request) {
	var err error
	identity := principal(request)
	effectiveTimezone := identity.Workspace.Timezone
	if identity.Agent.TimezoneOverride != nil {
		effectiveTimezone = *identity.Agent.TimezoneOverride
	}
	// plan is null without billing (self-hosted) so the client shows no trial or lock.
	var plan *mePlan
	if a.limits {
		plan = &mePlan{Access: identity.Access}
		// A person who cannot change the plan and cannot use the workspace for lack of one learns whom to ask.
		if !identity.IsToken() && !identity.Can(auth.PermBillingManage) && (identity.Access.Plan == domain.PlanTrial || identity.Access.Plan == domain.PlanNone) {
			if plan.Admins, err = a.store.NamesHolding(request.Context(), identity.Workspace.ID, auth.PermBillingManage); err != nil {
				writeStoreError(response, err)
				return
			}
			plan.Admins = plan.Admins[:min(len(plan.Admins), maxPlanNames)]
		}
	}
	goal, err := a.store.OnboardingGoal(request.Context(), identity.Workspace.ID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	features, err := a.ai.ActiveFeatures(request.Context(), identity.Workspace.ID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	// permissions is what this call may do: the person's roles, and for a token what it was given
	// limited to that. token is what a token was given; null for a person's session.
	writeJSON(response, http.StatusOK, map[string]any{
		"agent": identity.Agent, "workspace": identity.Workspace, "timezone": effectiveTimezone, "token": identity.Token, "permissions": identity.Effective(), "plan": plan,
		"onboarding_goal": goal, "ai_features": features, "setup_guide_hidden": identity.Agent.SetupGuideHidden,
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

// revokeSession signs out one session or revokes one token: the caller's own always, another person's
// with team:revoke when the caller outranks them.
func (a *API) revokeSession(response http.ResponseWriter, request *http.Request) {
	ok, err := a.auth.RevokeSession(request.Context(), principal(request), request.PathValue("id"))
	var missing *auth.MissingPermissionError
	if errors.As(err, &missing) {
		a.writePermissionMissing(response, request, principal(request), missing.Permission)
		return
	}
	if err != nil {
		writeStoreError(response, err)
		return
	}
	if !ok {
		writeError(response, http.StatusNotFound, "not_found", "The session was not found.")
		return
	}
	if who := principal(request); who.IsToken() {
		// The session list shows the revocation; the log also names the token that did it.
		slog.Info("agent token revoked a session", "workspace_id", who.Workspace.ID, "token_session_id", who.SessionID, "revoked_session_id", request.PathValue("id"))
	}
	response.WriteHeader(http.StatusNoContent)
}

func (a *API) createAgentToken(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Label       string   `json:"label"`
		RoleID      string   `json:"role_id"`
		Permissions []string `json:"permissions"`
		Scope       *string  `json:"scope"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "A label and a role_id (a role whose permissions you all hold) or a list of permissions are required.")
		return
	}
	if input.Scope != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Tiers were replaced by roles: send role_id (a role whose permissions you all hold) or a list of permissions.")
		return
	}
	// The raw token is in the body once; no cache may keep it, and a repeat of one press (the same
	// Idempotency-Key) answers the same session without it.
	response.Header().Set("Cache-Control", "no-store")
	permissions := slices.Sorted(slices.Values(input.Permissions))
	a.createOnce(response, request, createAgentTokenOperation, append([]string{input.Label, input.RoleID}, permissions...), func(ctx context.Context) (any, error) {
		created, err := a.auth.CreateAgentToken(ctx, principal(request), auth.TokenRequest{Label: input.Label, RoleID: input.RoleID, Permissions: input.Permissions})
		var invalid *auth.InvalidTokenError
		if errors.As(err, &invalid) {
			writeError(response, http.StatusBadRequest, "invalid_request", invalid.Reason)
			return nil, errAnswered
		}
		if writeRoleError(response, err) {
			return nil, errAnswered
		}
		if err != nil {
			return nil, err
		}
		return shownOnce{shown: created, kept: map[string]any{"session": created.Session}}, nil
	})
}
