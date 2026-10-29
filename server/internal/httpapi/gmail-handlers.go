package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/google"
	"github.com/colliedesk/colliedesk/server/internal/serverconfig"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
)

const (
	gmailPath = "/api/v1/channels/email/gmail"
	// channelGmailPath connects Gmail to one email channel of several; gmailPath to the default one.
	channelGmailPath  = "/api/v1/channels/{id}/email/gmail"
	gmailCallbackPath = "/api/v1/channels/email/gmail/callback"
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

func (a *API) gmailRedirectURI() string { return a.redirectURI(a.gmail()) }

func (a *API) gmailSettings(connection *domain.MailboxConnection) gmailSettings {
	missing := a.config.Google.Missing()
	if missing == nil {
		missing = []string{}
	}
	// Switched off with its settings complete: unavailable, and nothing is missing.
	return gmailSettings{Available: len(missing) == 0 && !a.switchedOff(serverconfig.FeatureGmail), MissingSettings: missing, RedirectURI: a.gmailRedirectURI(), Connection: connection}
}

// connectGmail starts the consent: it records who started it and answers Google's consent address.
func (a *API) connectGmail(response http.ResponseWriter, request *http.Request) {
	a.connectMailbox(a.gmail(), response, request)
}

// finishGmailConnect is where Google sends the person's browser back (finishMailboxConnect).
func (a *API) finishGmailConnect(response http.ResponseWriter, request *http.Request) {
	a.finishMailboxConnect(a.gmail(), response, request)
}

func (a *API) disconnectGmail(response http.ResponseWriter, request *http.Request) {
	a.disconnectMailbox(a.gmail(), response, request)
}

// gmailMailbox connects Gmail through the operator's Google OAuth client.
type gmailMailbox struct{ config google.Config }

func (a *API) gmail() gmailMailbox { return gmailMailbox{config: a.config.Google} }

func (gmailMailbox) name() string              { return domain.MailboxGoogle }
func (gmailMailbox) label() string             { return "Gmail" }
func (gmailMailbox) resultKey() string         { return "gmail" }
func (gmailMailbox) notConfiguredCode() string { return "gmail_not_configured" }
func (gmailMailbox) unavailable() string       { return "google_unavailable" }
func (gmailMailbox) callbackPath() string      { return gmailCallbackPath }
func (gmailMailbox) revokes() bool             { return true }
func (g gmailMailbox) missing() []string       { return g.config.Missing() }
func (g gmailMailbox) keys() tokenseal.Keys    { return g.config.Keys }
func (gmailMailbox) grantContext(workspaceID string) string {
	return google.GrantContext(workspaceID)
}

// authorizationURL ignores the hint: Google's consent asks which account every time (prompt=consent).
func (g gmailMailbox) authorizationURL(redirectURI, state, verifier, _ string) string {
	return g.config.AuthorizationURL(redirectURI, state, verifier)
}

func (g gmailMailbox) exchange(ctx context.Context, code, verifier, redirectURI string) (mailboxGrant, string, error) {
	grant, err := g.config.Exchange(ctx, code, verifier, redirectURI)
	switch {
	case errors.Is(err, google.ErrSendNotGranted):
		return mailboxGrant{}, "send_not_granted", nil
	case errors.Is(err, google.ErrNoRefreshToken):
		return mailboxGrant{}, "no_refresh_token", nil
	case errors.Is(err, google.ErrNoAddress):
		return mailboxGrant{}, "no_address", nil
	case err != nil:
		return mailboxGrant{}, "", err
	}
	return mailboxGrant{RefreshToken: grant.RefreshToken, Address: grant.Address, Scopes: grant.Scopes}, "", nil
}

func (g gmailMailbox) revoke(ctx context.Context, token string) error {
	return g.config.Revoke(ctx, token)
}
