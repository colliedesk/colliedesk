package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
)

const (
	// mailboxConsentTTL is how long a started consent can be finished.
	mailboxConsentTTL = 10 * time.Minute
	// mailboxCallTimeout bounds one call to a mailbox provider made while a person waits.
	mailboxCallTimeout = 20 * time.Second
)

// mailboxProvider is what connecting an email channel's mailbox needs of its provider (Gmail, Microsoft
// 365). An email channel has one mailbox connection: connecting through another provider replaces it.
type mailboxProvider interface {
	// name is the provider as stored (mailbox_connections.provider).
	name() string
	// label names the provider to people ("Gmail").
	label() string
	// resultKey is the query parameter the settings page reads the outcome from: <key>=connected,
	// <key>_error=<code>, <key>_channel=<id>.
	resultKey() string
	// notConfiguredCode is the API error code of a connect on a server without the provider's settings.
	notConfiguredCode() string
	// unavailable is the outcome when the provider cannot be reached or answers unexpectedly.
	unavailable() string
	// missing names the settings this server still needs before the provider can be connected.
	missing() []string
	keys() tokenseal.Keys
	grantContext(workspaceID string) string
	callbackPath() string
	// authorizationURL is where the person's browser consents; hint is the address to sign in as.
	authorizationURL(redirectURI, state, verifier, hint string) string
	// exchange trades the code for a grant. A refusal the settings page explains is returned as its
	// outcome code (with err when it is worth logging); err alone is anything else, and reads as unavailable.
	exchange(ctx context.Context, code, verifier, redirectURI string) (grant mailboxGrant, refused string, err error)
	// revokes reports whether the provider can revoke one grant; revoke does it.
	revokes() bool
	revoke(ctx context.Context, token string) error
}

// mailboxGrant is what a completed consent gave: the refresh token and the account it belongs to.
type mailboxGrant struct {
	RefreshToken string
	Address      string
	// AccountID is the provider's own id of the account, when it names one.
	AccountID string
	Scopes    []string
}

// mailboxProviders are the providers an email channel can connect, by name.
func (a *API) mailboxProviders() map[string]mailboxProvider {
	return map[string]mailboxProvider{a.gmail().name(): a.gmail(), a.microsoft().name(): a.microsoft()}
}

func (a *API) redirectURI(p mailboxProvider) string { return a.config.Origin + p.callbackPath() }

// connectMailbox starts the consent: it records who started it and answers the provider's consent address.
func (a *API) connectMailbox(p mailboxProvider, response http.ResponseWriter, request *http.Request) {
	if missing := p.missing(); len(missing) > 0 {
		writeError(response, http.StatusConflict, p.notConfiguredCode(),
			"This server cannot connect "+p.label()+" yet: it is not set up here, so ask whoever runs it.")
		return
	}
	channel, ok := a.requestedEmailChannel(response, request)
	if !ok {
		return
	}
	channelID, hint := "", ""
	if channel != nil {
		channelID, hint = channel.ChannelID, channel.SupportAddress
	}
	who := principal(request)
	now := a.now()
	started, err := a.store.CreateOAuthState(request.Context(), who.Workspace.ID, channelID, who.Agent.ID, p.name(), now, now.Add(mailboxConsentTTL))
	if errors.Is(err, store.ErrEmailChannelMissing) {
		writeError(response, http.StatusConflict, "email_channel_missing", "Connect the email channel with a support address before connecting "+p.label()+".")
		return
	}
	if err != nil {
		writeInternalError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]string{"url": p.authorizationURL(a.redirectURI(p), started.State, started.Verifier, hint)})
}

// mailboxResult is what a finished consent knows besides its outcome: the channel it was for, and the
// channel that already sends through the account when that is why it was refused.
type mailboxResult struct{ channelID, holderID string }

// mailboxResultPath is the Replies tab of the email channel the consent was for (of the default email
// channel when that is not known), where the mailbox block reads the outcome.
func mailboxResultPath(p mailboxProvider, result mailboxResult, outcome string) string {
	page := "/settings/channels/type/email"
	if result.channelID != "" {
		page = "/settings/channels/" + result.channelID
	}
	key := p.resultKey()
	if outcome == "connected" {
		return page + "?tab=replies&" + key + "=connected"
	}
	if result.holderID != "" {
		return page + "?tab=replies&" + key + "_error=" + outcome + "&" + key + "_channel=" + result.holderID
	}
	return page + "?tab=replies&" + key + "_error=" + outcome
}

// finishMailboxConnect is where the provider sends the person's browser back. The route is public so
// that the provider's redirect reaches it, but it finishes only for the signed-in admin who started this
// consent: otherwise anyone could send a victim a consent link and attach the victim's mailbox to the
// sender's workspace. It redoes here what authorize would do for a session route, and answers every
// outcome with a redirect the settings page explains; the provider's own error text is never echoed.
func (a *API) finishMailboxConnect(p mailboxProvider, response http.ResponseWriter, request *http.Request) {
	var result mailboxResult
	outcome := a.finishMailboxConsent(p, request, &result)
	http.Redirect(response, request, mailboxResultPath(p, result, outcome), http.StatusSeeOther)
}

// finishMailboxConsent finishes the consent and returns its outcome; result learns the channel it was
// for once the state is known.
func (a *API) finishMailboxConsent(p mailboxProvider, request *http.Request, result *mailboxResult) string {
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
	// The state is spent before anything else, so a refused or failed consent cannot be replayed. Only a
	// state started at this provider is found.
	started, err := a.store.TakeOAuthState(request.Context(), who.Workspace.ID, p.name(), state, a.now())
	if errors.Is(err, store.ErrOAuthStateNotFound) {
		return "expired"
	}
	if err != nil {
		slog.Error("mailbox consent state", "provider", p.name(), "error", err, "workspace", who.Workspace.ID)
		return p.unavailable()
	}
	if started.AgentID != who.Agent.ID {
		return "wrong_person"
	}
	result.channelID = started.ChannelID
	if query.Get("error") != "" || query.Get("code") == "" {
		return "denied"
	}
	ctx, cancel := context.WithTimeout(request.Context(), mailboxCallTimeout)
	defer cancel()
	grant, refused, err := p.exchange(ctx, query.Get("code"), started.Verifier, a.redirectURI(p))
	if refused != "" {
		if err != nil {
			slog.Error("mailbox code exchange refused", "provider", p.name(), "outcome", refused, "error", err, "workspace", who.Workspace.ID)
		}
		return refused
	}
	if err != nil {
		slog.Warn("mailbox code exchange failed", "provider", p.name(), "error", err, "workspace", who.Workspace.ID)
		return p.unavailable()
	}
	// A channel sends as its own address: the account must be its support address's mailbox, and one
	// account sends for one channel of the workspace. A refused grant is revoked unless a channel holds
	// the account already: Google would revoke that one with it.
	channel, err := a.store.GetEmailChannelByID(request.Context(), who.Workspace.ID, started.ChannelID)
	if err != nil {
		slog.Error("read the email channel of a mailbox consent", "provider", p.name(), "error", err, "workspace", who.Workspace.ID)
		return p.unavailable()
	}
	if channel == nil {
		a.revokeUnheld(p, grant.RefreshToken, grant.Address, who.Workspace.ID)
		return "email_channel_missing"
	}
	if !email.SameMailbox(grant.Address, channel.SupportAddress) {
		a.revokeUnheld(p, grant.RefreshToken, grant.Address, who.Workspace.ID)
		return "address_mismatch"
	}
	holder, err := a.store.MailboxChannelFor(request.Context(), who.Workspace.ID, grant.Address)
	if err != nil {
		slog.Error("check mailbox account", "provider", p.name(), "error", err, "workspace", who.Workspace.ID)
		return p.unavailable()
	}
	if holder != "" && holder != channel.ChannelID {
		result.holderID = holder
		return "account_in_use"
	}
	keyID, sealed, err := p.keys().Seal([]byte(grant.RefreshToken), p.grantContext(who.Workspace.ID))
	// A failure below does not revoke the new grant: Google revokes the whole grant of the account, which
	// would also cut a connection this workspace already has with the same account.
	if err != nil {
		slog.Error("seal mailbox grant", "provider", p.name(), "error", err, "workspace", who.Workspace.ID)
		return p.unavailable()
	}
	_, replaced, err := a.store.SaveMailboxConnection(request.Context(), who.Workspace.ID, started.ChannelID, who.Agent.ID, grant.Address, strings.Join(grant.Scopes, " "),
		store.SealedGrant{KeyID: keyID, Sealed: sealed, Provider: p.name(), AccountID: grant.AccountID}, a.now())
	if errors.Is(err, store.ErrEmailChannelMissing) {
		// No channel means no earlier connection of this workspace, so the new grant is only ours to drop.
		a.revokeGrant(p, grant.RefreshToken, who.Workspace.ID)
		return "email_channel_missing"
	}
	if err != nil {
		slog.Error("save mailbox connection", "provider", p.name(), "error", err, "workspace", who.Workspace.ID)
		return p.unavailable()
	}
	// Revoking the earlier grant of the same account at the same provider would revoke the new one with it.
	if replaced != nil && (replaced.Provider != p.name() || replaced.Address != grant.Address) {
		a.revokeSealed(replaced, who.Workspace.ID, started.ChannelID)
	}
	return "connected"
}

// disconnectMailbox removes the channel's connection through this provider; a connection through
// another provider is not this route's to remove.
func (a *API) disconnectMailbox(p mailboxProvider, response http.ResponseWriter, request *http.Request) {
	workspaceID := principal(request).Workspace.ID
	channel, ok := a.requestedEmailChannel(response, request)
	if !ok {
		return
	}
	if channel == nil {
		writeError(response, http.StatusNotFound, "not_found", p.label()+" is not connected.")
		return
	}
	removed, err := a.store.DeleteMailboxConnection(request.Context(), workspaceID, channel.ChannelID, p.name())
	if err != nil {
		writeInternalError(response, err)
		return
	}
	if removed == nil {
		writeError(response, http.StatusNotFound, "not_found", p.label()+" is not connected.")
		return
	}
	a.revokeSealed(removed, workspaceID, channel.ChannelID)
	response.WriteHeader(http.StatusNoContent)
}

// revokeSealed revokes a grant an email channel no longer stores, at the provider that issued it. Call it
// only after the row is gone and committed. Google revokes the account's whole grant to the project, so
// while another channel, of this workspace or another, still holds the same address at the same
// provider nothing is revoked: that channel keeps sending, and this channel's token stays valid until the
// last holder drops the address. Best effort: a failure is logged without the token.
func (a *API) revokeSealed(grant *store.SealedGrant, workspaceID, channelID string) {
	if grant == nil {
		return
	}
	p, ok := a.mailboxProviders()[grant.Provider]
	if !ok || !p.revokes() || len(p.missing()) > 0 {
		return
	}
	held, err := a.store.MailboxAddressHeldElsewhere(context.Background(), channelID, p.name(), grant.Address)
	if err != nil {
		slog.Warn("mailbox grant not revoked: could not check other workspaces", "provider", p.name(), "workspace", workspaceID, "error", err)
		return
	}
	if held {
		return
	}
	token, err := p.keys().Open(grant.KeyID, grant.Sealed, p.grantContext(workspaceID))
	if err != nil {
		slog.Warn("mailbox grant could not be opened to revoke it", "provider", p.name(), "workspace", workspaceID, "error", err)
		return
	}
	a.revokeGrant(p, string(token), workspaceID)
}

// revokeUnheld revokes a grant just received and not stored, unless a channel holds the same account.
func (a *API) revokeUnheld(p mailboxProvider, token, address, workspaceID string) {
	if !p.revokes() {
		return
	}
	held, err := a.store.MailboxAddressHeldElsewhere(context.Background(), "", p.name(), address)
	if err != nil {
		slog.Warn("mailbox grant not revoked: could not check other channels", "provider", p.name(), "workspace", workspaceID, "error", err)
		return
	}
	if !held {
		a.revokeGrant(p, token, workspaceID)
	}
}

func (a *API) revokeGrant(p mailboxProvider, token, workspaceID string) {
	if !p.revokes() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), mailboxCallTimeout)
	defer cancel()
	if err := p.revoke(ctx, token); err != nil {
		slog.Warn("mailbox grant revoke failed", "provider", p.name(), "workspace", workspaceID, "error", err)
	}
}
