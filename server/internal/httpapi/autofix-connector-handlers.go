package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/autofix"
	"github.com/colliedesk/colliedesk/server/internal/cursor"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// cursorCallTimeout bounds what a person waits for Cursor in a settings call; cursorCallsPerMinute is
// how many such calls a workspace makes in a minute (Cursor allows a key 20, the worker keeps 10).
const (
	cursorCallTimeout    = 15 * time.Second
	cursorCallsPerMinute = 5
)

// cursorLimited answers 429 when the workspace already called Cursor from the settings this minute.
func (a *API) cursorLimited(response http.ResponseWriter, request *http.Request) bool {
	allowed, wait := a.cursorCalls.allow(principal(request).Workspace.ID, a.now())
	if allowed {
		return false
	}
	response.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
	writeError(response, http.StatusTooManyRequests, "rate_limited", "Cursor was called from the settings several times this minute. Try again in a moment.")
	return true
}

// autofixActor is who changed an Autofix setting: the person, or the agent token's owner.
func autofixActor(request *http.Request) autofix.Actor {
	who := principal(request)
	actor := autofix.Actor{Kind: autofix.ActorPerson, ID: who.Agent.ID, Name: strings.TrimSpace(who.Agent.DisplayName)}
	if actor.Name == "" {
		actor.Name = who.Agent.Email
	}
	if who.IsToken() {
		actor.Kind = autofix.ActorToken
	}
	return actor
}

// autofixOff answers 503 autofix_unavailable while this server's GitHub App is not set up.
func (a *API) autofixOff(response http.ResponseWriter) bool {
	if a.config.GitHub.Enabled() {
		return false
	}
	writeError(response, http.StatusServiceUnavailable, "autofix_unavailable",
		"Autofix is off on this server: its GitHub App is not set up (GET /api/v1/config names the missing settings).")
	return true
}

// fixConnectorKind reads the {kind} of the path, or answers 400.
func fixConnectorKind(response http.ResponseWriter, request *http.Request) (autofix.Connector, bool) {
	kind := autofix.Connector(request.PathValue("kind"))
	if !kind.Valid() {
		writeError(response, http.StatusBadRequest, "invalid_request", "kind is claude_issue, copilot_issue or cursor_api.")
		return "", false
	}
	return kind, true
}

func writeConnectorError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, autofix.ErrConsentOutdated):
		writeError(response, http.StatusConflict, "consent_outdated",
			"The consent text changed since it was read: read it again (GET /api/v1/autofix/settings, consent_version) and agree to the current one.")
	case errors.Is(err, autofix.ErrCursorKeyMissing):
		writeError(response, http.StatusConflict, "cursor_key_missing", "No Cursor key is set for this workspace. Set one first.")
	case errors.Is(err, store.ErrSealKeysMissing):
		writeError(response, http.StatusConflict, "secret_keys_missing", "This server has no MAILBOX_TOKEN_KEYS, so it cannot keep a Cursor key.")
	default:
		writeStoreError(response, err)
	}
}

func (a *API) enableFixConnector(response http.ResponseWriter, request *http.Request) {
	kind, ok := fixConnectorKind(response, request)
	if !ok {
		return
	}
	var input struct {
		ConsentVersion string `json:"consent_version"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send consent_version, the version of the consent text you agree to.")
		return
	}
	// No consent, no coding agent: an approved agent request without one is refused too, so the person
	// approving always agreed to a stated text (the consent is recorded in their name).
	if strings.TrimSpace(input.ConsentVersion) == "" {
		writeError(response, http.StatusUnprocessableEntity, "consent_required",
			"consent_version is required: the version of the consent text agreed to (consent_version in GET /api/v1/autofix/settings).")
		return
	}
	setting, err := a.store.EnableFixConnector(request.Context(), principal(request).Workspace.ID, kind, input.ConsentVersion, autofixActor(request))
	if err != nil {
		writeConnectorError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, setting)
}

func (a *API) disableFixConnector(response http.ResponseWriter, request *http.Request) {
	kind, ok := fixConnectorKind(response, request)
	if !ok {
		return
	}
	workspaceID := principal(request).Workspace.ID
	setting, err := a.store.DisableFixConnector(request.Context(), workspaceID, kind)
	if err != nil {
		writeConnectorError(response, err)
		return
	}
	// Turning an agent off withdraws the consent: fixes whose content has not left yet end now (the
	// worker and the issue connectors check again right before anything leaves).
	sending, err := a.store.SendingFixIDs(request.Context(), workspaceID, kind)
	if err != nil {
		writeConnectorError(response, err)
		return
	}
	actor := autofixActor(request)
	actor.Kind = autofix.ActorSystem
	for _, id := range sending {
		if _, _, err := a.store.ApplyFixObservation(request.Context(), workspaceID, id, autofix.Observation{State: autofix.Cancelled,
			Reason: "connector_disabled", Summary: "The coding agent was turned off before the fix was sent; nothing was sent."}, actor); err != nil {
			writeConnectorError(response, err)
			return
		}
	}
	writeJSON(response, http.StatusOK, setting)
}

func (a *API) checkFixConnector(response http.ResponseWriter, request *http.Request) {
	kind, ok := fixConnectorKind(response, request)
	if !ok || a.autofixOff(response) {
		return
	}
	workspaceID := principal(request).Workspace.ID
	if kind == autofix.CursorAPI {
		if a.cursorLimited(response, request) {
			return
		}
		ctx, cancel := context.WithTimeout(request.Context(), 2*cursorCallTimeout)
		defer cancel()
		setting, err := a.cursor.CheckKey(ctx, workspaceID)
		if err != nil {
			writeConnectorError(response, err)
			return
		}
		writeJSON(response, http.StatusOK, setting)
		return
	}
	// The issue connectors: what Colliedesk knows without GitHub. The vendor's side (the Claude workflow
	// and its secret, Copilot turned on) is a checklist the admin ticks.
	facts, err := a.store.AutofixIssueFacts(request.Context(), workspaceID)
	if err != nil {
		writeConnectorError(response, err)
		return
	}
	result, message := autofix.CheckOK, "The GitHub App is connected and every connected repository is private. The setup on GitHub's side is the checklist above."
	switch {
	case facts.ActiveInstallations == 0:
		result, message = autofix.CheckInstallationInactive, "No active installation of the Colliedesk GitHub App: connect GitHub, or narrow an installation to selected repositories."
	case len(facts.NotPrivate) > 0:
		result, message = autofix.CheckRepoNotPrivate, strings.Join(facts.NotPrivate, ", ")+" is not private; fixes are only sent to private repositories."
	case facts.Repos == 0:
		message = "The GitHub App is connected. Connect a private repository to send fixes to it."
	}
	setting, err := a.store.RecordConnectorCheck(request.Context(), workspaceID, kind, result, message)
	if err != nil {
		writeConnectorError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, setting)
}

func (a *API) setCursorKey(response http.ResponseWriter, request *http.Request) {
	if a.autofixOff(response) {
		return
	}
	var input struct {
		Key string `json:"key"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send key, the Cursor API key.")
		return
	}
	key := strings.TrimSpace(input.Key)
	if len(key) < 8 || len(key) > 500 || strings.ContainsAny(key, " \t\r\n") {
		writeError(response, http.StatusBadRequest, "invalid_request", "key must be a Cursor API key: 8 to 500 characters, no spaces.")
		return
	}
	if a.cursorLimited(response, request) {
		return
	}
	// The key is tried before it is kept, so a wrong one never replaces a working one.
	ctx, cancel := context.WithTimeout(request.Context(), cursorCallTimeout)
	defer cancel()
	if err := (cursor.Client{Base: a.cursor.Base, Key: key, HTTPClient: a.cursor.HTTPClient}).Me(ctx); err != nil {
		if status := cursor.StatusOf(err); status == http.StatusUnauthorized || status == http.StatusForbidden {
			writeError(response, http.StatusBadRequest, "cursor_key_invalid", "Cursor does not accept this key. Nothing was changed.")
			return
		}
		writeError(response, http.StatusServiceUnavailable, "cursor_unavailable", "Cursor did not answer, so the key could not be tried. Nothing was changed; try again.")
		return
	}
	workspaceID := principal(request).Workspace.ID
	if _, err := a.store.SetCursorKey(request.Context(), workspaceID, key, autofixActor(request)); err != nil {
		writeConnectorError(response, err)
		return
	}
	a.cursor.ForgetListing(workspaceID)
	setting, err := a.store.RecordConnectorCheck(request.Context(), workspaceID, autofix.CursorAPI, autofix.CheckOK, "Cursor accepts the key.")
	if err != nil {
		writeConnectorError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, setting)
}

func (a *API) removeCursorKey(response http.ResponseWriter, request *http.Request) {
	setting, err := a.store.RemoveCursorKey(request.Context(), principal(request).Workspace.ID)
	if err != nil {
		writeConnectorError(response, err)
		return
	}
	a.cursor.ForgetListing(principal(request).Workspace.ID)
	writeJSON(response, http.StatusOK, setting)
}
