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
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/google"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

const (
	gmailPath = "/api/v1/channels/email/gmail"
	// channelGmailPath connects Gmail to one email channel of several; gmailPath to the default one.
	channelGmailPath  = "/api/v1/channels/{id}/email/gmail"
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
	// AddressMatches is false for a connection whose account is not the channel's support address;
	// absent without a connection.
	AddressMatches *bool `json:"address_matches,omitempty"`
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
	channel, ok := a.requestedEmailChannel(response, request)
	if !ok {
		return
	}
	channelID := ""
	if channel != nil {
		channelID = channel.ChannelID
	}
	who := principal(request)
	now := a.now()
	started, err := a.store.CreateOAuthState(request.Context(), who.Workspace.ID, channelID, who.Agent.ID, now, now.Add(gmailConsentTTL))
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

// gmailResult is what a finished consent knows besides its outcome: the channel it was for, and the
// channel that already sends through the account when that is why it was refused.
type gmailResult struct{ channelID, holderID string }

// gmailResultPath is the Replies tab of the email channel the consent was for (of the default email
// channel when that is not known), where the "Send from Gmail" block reads the outcome.
func gmailResultPath(result gmailResult, outcome string) string {
	page := "/settings/channels/type/email"
	if result.channelID != "" {
		page = "/settings/channels/" + result.channelID
	}
	if outcome == "connected" {
		return page + "?tab=replies&gmail=connected"
	}
	if result.holderID != "" {
		return page + "?tab=replies&gmail_error=" + outcome + "&gmail_channel=" + result.holderID
	}
	return page + "?tab=replies&gmail_error=" + outcome
}

// finishGmailConnect is where Google sends the person's browser back. The route is public so that
// Google's redirect reaches it, but it finishes only for the signed-in admin who started this consent:
// otherwise anyone could send a victim a consent link and attach the victim's Gmail to the sender's
// workspace. It redoes here what authorize would do for a session route, and answers every outcome
// with a redirect the settings page explains; Google's own error text is never echoed.
func (a *API) finishGmailConnect(response http.ResponseWriter, request *http.Request) {
	var result gmailResult
	outcome := a.finishGmailConsent(request, &result)
	http.Redirect(response, request, gmailResultPath(result, outcome), http.StatusSeeOther)
}

// finishGmailConsent finishes the consent and returns its outcome; result learns the channel it was
// for once the state is known.
func (a *API) finishGmailConsent(request *http.Request, result *gmailResult) string {
	cookie, err := request.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		return "signed_out"
	}
	who, err := a.auth.Authenticate(request.Context(), cookie.Value, true)
	if err != nil || who.IsToken() {
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
	result.channelID = started.ChannelID
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
	// A channel sends as its own address: the account must be its support address's mailbox, and one
	// account sends for one channel of the workspace. A refused grant is revoked unless a channel holds
	// the account already: Google would revoke that one with it.
	channel, err := a.store.GetEmailChannelByID(request.Context(), who.Workspace.ID, started.ChannelID)
	if err != nil {
		slog.Error("read the email channel of a gmail consent", "error", err, "workspace", who.Workspace.ID)
		return "google_unavailable"
	}
	if channel == nil {
		a.revokeUnheld(grant.RefreshToken, grant.Address, who.Workspace.ID)
		return "email_channel_missing"
	}
	if !email.SameMailbox(grant.Address, channel.SupportAddress) {
		a.revokeUnheld(grant.RefreshToken, grant.Address, who.Workspace.ID)
		return "address_mismatch"
	}
	holder, err := a.store.MailboxChannelFor(request.Context(), who.Workspace.ID, grant.Address)
	if err != nil {
		slog.Error("check gmail account", "error", err, "workspace", who.Workspace.ID)
		return "google_unavailable"
	}
	if holder != "" && holder != channel.ChannelID {
		result.holderID = holder
		return "account_in_use"
	}
	keyID, sealed, err := a.config.Google.Keys.Seal([]byte(grant.RefreshToken), google.GrantContext(who.Workspace.ID))
	// A failure below does not revoke the new grant: Google revokes the whole grant of the account, which
	// would also cut a connection this workspace already has with the same account.
	if err != nil {
		slog.Error("seal gmail grant", "error", err, "workspace", who.Workspace.ID)
		return "google_unavailable"
	}
	_, replaced, err := a.store.SaveMailboxConnection(request.Context(), who.Workspace.ID, started.ChannelID, who.Agent.ID, grant.Address, strings.Join(grant.Scopes, " "),
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
		a.revokeSealed(replaced, who.Workspace.ID, started.ChannelID)
	}
	return "connected"
}

func (a *API) disconnectGmail(response http.ResponseWriter, request *http.Request) {
	workspaceID := principal(request).Workspace.ID
	channel, ok := a.requestedEmailChannel(response, request)
	if !ok {
		return
	}
	if channel == nil {
		writeError(response, http.StatusNotFound, "not_found", "Gmail is not connected.")
		return
	}
	removed, err := a.store.DeleteMailboxConnection(request.Context(), workspaceID, channel.ChannelID)
	if err != nil {
		writeInternalError(response, err)
		return
	}
	if removed == nil {
		writeError(response, http.StatusNotFound, "not_found", "Gmail is not connected.")
		return
	}
	a.revokeSealed(removed, workspaceID, channel.ChannelID)
	response.WriteHeader(http.StatusNoContent)
}

// revokeSealed revokes a grant an email channel no longer stores. Call it only after the row is gone and
// committed. Google revokes the account's whole grant to the project, so while another channel, of this
// workspace or another, still holds the same address nothing is revoked: that channel keeps sending, and
// this channel's token stays valid at Google until the last holder drops the address. Best effort: a
// failure is logged without the token.
func (a *API) revokeSealed(grant *store.SealedGrant, workspaceID, channelID string) {
	if grant == nil || !a.config.Google.Enabled() {
		return
	}
	held, err := a.store.MailboxAddressHeldElsewhere(context.Background(), channelID, grant.Address)
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

// revokeUnheld revokes a grant just received and not stored, unless a channel holds the same account.
func (a *API) revokeUnheld(token, address, workspaceID string) {
	held, err := a.store.MailboxAddressHeldElsewhere(context.Background(), "", address)
	if err != nil {
		slog.Warn("gmail grant not revoked: could not check other channels", "workspace", workspaceID, "error", err)
		return
	}
	if !held {
		a.revokeGrant(token, workspaceID)
	}
}

func (a *API) revokeGrant(token, workspaceID string) {
	ctx, cancel := context.WithTimeout(context.Background(), googleCallTimeout)
	defer cancel()
	if err := a.config.Google.Revoke(ctx, token); err != nil {
		slog.Warn("gmail grant revoke failed", "workspace", workspaceID, "error", err)
	}
}
