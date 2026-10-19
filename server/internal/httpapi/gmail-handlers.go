package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/google"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

const (
	gmailPath         = "/api/v1/channels/email/gmail"
	gmailCallbackPath = "/api/v1/channels/email/gmail/callback"
	// gmailConsentTTL is how long a started consent can be finished.
	gmailConsentTTL = 10 * time.Minute
	// googleCallTimeout bounds one call to Google made while a person waits.
	googleCallTimeout = 20 * time.Second
)

// gmailSettings is what the email channel settings say about Gmail.
type gmailSettings struct {
	Available       bool                      `json:"available"`
	MissingSettings []string                  `json:"missing_settings"`
	RedirectURI     string                    `json:"redirect_uri"`
	Connection      *domain.MailboxConnection `json:"connection"`
}

func (a *API) gmailRedirectURI() string { return a.config.Origin + gmailCallbackPath }

func (a *API) gmailSettings(connection *domain.MailboxConnection) gmailSettings {
	missing := a.config.Google.Missing()
	if missing == nil {
		missing = []string{}
	}
	return gmailSettings{Available: len(missing) == 0, MissingSettings: missing, RedirectURI: a.gmailRedirectURI(), Connection: connection}
}

// connectGmail starts the consent: it records who started it and answers Google's consent address.
func (a *API) connectGmail(response http.ResponseWriter, request *http.Request) {
	if missing := a.config.Google.Missing(); len(missing) > 0 {
		writeError(response, http.StatusConflict, "gmail_not_configured",
			"This server cannot connect Gmail yet. The administrator must set "+strings.Join(missing, ", ")+".")
		return
	}
	who := principal(request)
	now := a.now()
	started, err := a.store.CreateOAuthState(request.Context(), who.Workspace.ID, who.Agent.ID, now, now.Add(gmailConsentTTL))
	if errors.Is(err, store.ErrEmailChannelMissing) {
		writeError(response, http.StatusConflict, "email_channel_missing", "Connect the email channel with a support address before connecting Gmail.")
		return
	}
	if err != nil {
		writeInternalError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]string{"url": a.config.Google.AuthorizationURL(a.gmailRedirectURI(), started.State, started.Verifier)})
}

func gmailResultPath(outcome string) string {
	if outcome == "connected" {
		return "/settings?gmail=connected"
	}
	return "/settings?gmail_error=" + outcome
}

// finishGmailConnect is where Google sends the person's browser back. The route is public so that
// Google's redirect reaches it, but it finishes only for the signed-in admin who started this consent:
// otherwise anyone could send a victim a consent link and attach the victim's Gmail to the sender's
// workspace. It redoes here what authorize would do for a session route, and answers every outcome
// with a redirect the settings page explains; Google's own error text is never echoed.
func (a *API) finishGmailConnect(response http.ResponseWriter, request *http.Request) {
	outcome := a.gmailCallbackOutcome(request)
	http.Redirect(response, request, gmailResultPath(outcome), http.StatusSeeOther)
}

func (a *API) gmailCallbackOutcome(request *http.Request) string {
	cookie, err := request.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		return "signed_out"
	}
	who, err := a.auth.Authenticate(request.Context(), cookie.Value, true)
	if err != nil || who.Scope != "" {
		return "signed_out"
	}
	if !auth.HasPermission(who.Agent.Role, auth.PermissionAccessWrite) || (a.limits && who.Access.Locked) {
		return "forbidden"
	}
	query := request.URL.Query()
	state := query.Get("state")
	if state == "" {
		return "expired"
	}
	// The state is spent before anything else, so a refused or failed consent cannot be replayed.
	started, err := a.store.TakeOAuthState(request.Context(), who.Workspace.ID, state, a.now())
	if errors.Is(err, store.ErrOAuthStateNotFound) {
		return "expired"
	}
	if err != nil {
		slog.Error("gmail consent state", "error", err, "workspace", who.Workspace.ID)
		return "google_unavailable"
	}
	if started.AgentID != who.Agent.ID {
		return "wrong_person"
	}
	if query.Get("error") != "" || query.Get("code") == "" {
		return "denied"
	}
	ctx, cancel := context.WithTimeout(request.Context(), googleCallTimeout)
	defer cancel()
	grant, err := a.config.Google.Exchange(ctx, query.Get("code"), started.Verifier, a.gmailRedirectURI())
	switch {
	case errors.Is(err, google.ErrSendNotGranted):
		return "send_not_granted"
	case errors.Is(err, google.ErrNoRefreshToken):
		return "no_refresh_token"
	case errors.Is(err, google.ErrNoAddress):
		return "no_address"
	case err != nil:
		slog.Warn("gmail code exchange failed", "error", err, "workspace", who.Workspace.ID)
		return "google_unavailable"
	}
	keyID, sealed, err := a.config.Google.Keys.Seal([]byte(grant.RefreshToken), google.GrantContext(who.Workspace.ID))
	// A failure below does not revoke the new grant: Google revokes the whole grant of the account, which
	// would also cut a connection this workspace already has with the same account.
	if err != nil {
		slog.Error("seal gmail grant", "error", err, "workspace", who.Workspace.ID)
		return "google_unavailable"
	}
	_, replaced, err := a.store.SaveMailboxConnection(request.Context(), who.Workspace.ID, who.Agent.ID, grant.Address, strings.Join(grant.Scopes, " "),
		store.SealedGrant{KeyID: keyID, Sealed: sealed}, a.now())
	if errors.Is(err, store.ErrEmailChannelMissing) {
		// No channel means no earlier connection of this workspace, so the new grant is only ours to drop.
		a.revokeGrant(grant.RefreshToken, who.Workspace.ID)
		return "email_channel_missing"
	}
	if err != nil {
		slog.Error("save gmail connection", "error", err, "workspace", who.Workspace.ID)
		return "google_unavailable"
	}
	// Revoking the earlier grant of the same account would revoke the new one with it.
	if replaced != nil && replaced.Address != grant.Address {
		a.revokeSealed(replaced, who.Workspace.ID)
	}
	return "connected"
}

func (a *API) disconnectGmail(response http.ResponseWriter, request *http.Request) {
	workspaceID := principal(request).Workspace.ID
	removed, err := a.store.DeleteMailboxConnection(request.Context(), workspaceID)
	if err != nil {
		writeInternalError(response, err)
		return
	}
	if removed == nil {
		writeError(response, http.StatusNotFound, "not_found", "Gmail is not connected.")
		return
	}
	a.revokeSealed(removed, workspaceID)
	response.WriteHeader(http.StatusNoContent)
}

// revokeSealed revokes a grant this workspace no longer stores. Call it only after the row is gone and
// committed. Google revokes the account's whole grant to the project, so while another workspace still holds
// the same address nothing is revoked: that workspace keeps sending, and this workspace's token stays valid
// at Google until the last holder drops the address. Best effort: a failure is logged without the token.
func (a *API) revokeSealed(grant *store.SealedGrant, workspaceID string) {
	if grant == nil || !a.config.Google.Enabled() {
		return
	}
	held, err := a.store.MailboxAddressHeldElsewhere(context.Background(), workspaceID, grant.Address)
	if err != nil {
		slog.Warn("gmail grant not revoked: could not check other workspaces", "workspace", workspaceID, "error", err)
		return
	}
	if held {
		return
	}
	token, err := a.config.Google.Keys.Open(grant.KeyID, grant.Sealed, google.GrantContext(workspaceID))
	if err != nil {
		slog.Warn("gmail grant could not be opened to revoke it", "workspace", workspaceID, "error", err)
		return
	}
	a.revokeGrant(string(token), workspaceID)
}

func (a *API) revokeGrant(token, workspaceID string) {
	ctx, cancel := context.WithTimeout(context.Background(), googleCallTimeout)
	defer cancel()
	if err := a.config.Google.Revoke(ctx, token); err != nil {
		slog.Warn("gmail grant revoke failed", "workspace", workspaceID, "error", err)
	}
}
