package httpapi

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/microsoft"
	"github.com/colliedesk/colliedesk/server/internal/serverconfig"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
)

const (
	microsoftPath = "/api/v1/channels/email/microsoft"
	// channelMicrosoftPath connects Microsoft 365 to one email channel of several; microsoftPath to the default one.
	channelMicrosoftPath  = "/api/v1/channels/{id}/email/microsoft"
	microsoftCallbackPath = "/api/v1/channels/email/microsoft/callback"
	// microsoftAdminConsentPath is where an organization's admin returns after approving Colliedesk for
	// everyone; a second redirect URI of the app registration.
	microsoftAdminConsentPath = "/api/v1/channels/email/microsoft/admin-consent"
)

// microsoftSettings is what the email channel settings say about Microsoft 365.
type microsoftSettings struct {
	Available bool `json:"available"`
	// MissingSettings is always empty, as for Gmail; OffReason says why it is not offered.
	MissingSettings         []string                  `json:"missing_settings"`
	OffReason               *string                   `json:"off_reason"`
	RedirectURI             string                    `json:"redirect_uri"`
	AdminConsentRedirectURI string                    `json:"admin_consent_redirect_uri"`
	AdminConsentURL         *string                   `json:"admin_consent_url"`
	LargeAttachmentBytes    int                       `json:"large_attachment_bytes"`
	ServerCredentials       bool                      `json:"server_credentials_refused"`
	Connection              *domain.MailboxConnection `json:"connection"`
	AddressMatches          *bool                     `json:"address_matches,omitempty"`
}

// microsoftSettings describes Microsoft 365 for the channel (channelID "" for none): its admin consent
// link returns to that channel's settings.
func (a *API) microsoftSettings(channelID string, connection *domain.MailboxConnection) microsoftSettings {
	config := a.config.Microsoft
	off := a.offReason(serverconfig.FeatureMicrosoft, config.Missing())
	settings := microsoftSettings{Available: off == nil, MissingSettings: []string{}, OffReason: off, RedirectURI: a.redirectURI(a.microsoft()),
		AdminConsentRedirectURI: a.config.Origin + microsoftAdminConsentPath, LargeAttachmentBytes: microsoft.LargeAttachmentBytes, Connection: connection}
	if settings.Available {
		if address := config.AdminConsentURL(settings.AdminConsentRedirectURI, channelID); address != "" {
			settings.AdminConsentURL = &address
		}
	}
	return settings
}

// connectMicrosoft starts the consent: it records who started it and answers Microsoft's sign-in address.
func (a *API) connectMicrosoft(response http.ResponseWriter, request *http.Request) {
	a.connectMailbox(a.microsoft(), response, request)
}

// finishMicrosoftConnect is where Microsoft sends the person's browser back (finishMailboxConnect).
func (a *API) finishMicrosoftConnect(response http.ResponseWriter, request *http.Request) {
	a.finishMailboxConnect(a.microsoft(), response, request)
}

func (a *API) disconnectMicrosoft(response http.ResponseWriter, request *http.Request) {
	a.disconnectMailbox(a.microsoft(), response, request)
}

// finishMicrosoftAdminConsent is where an organization's admin returns after approving (or refusing)
// Colliedesk for every user. Microsoft sends no code: nothing is stored, and the tenant it names is not
// trusted for anything (v2-admin-consent). The admin is often not a Colliedesk user, so without a session
// this is a short page saying what to do next; with one it returns to the email channel the link was for
// (state), when that channel is in the session's workspace.
func (a *API) finishMicrosoftAdminConsent(response http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	if uuidShape.MatchString(query.Get("state")) && a.toWorkspaceHost(response, request, func(ctx context.Context) (string, error) {
		return a.store.ChannelSlug(ctx, query.Get("state"))
	}) {
		return
	}
	approved := query.Get("error") == "" && query.Get("admin_consent") == "True"
	if page, ok := a.adminConsentReturn(request, query.Get("state")); ok {
		outcome := "microsoft=admin_consent_done"
		if !approved {
			outcome = "microsoft_error=admin_consent_failed"
		}
		http.Redirect(response, request, page+"?tab=replies&"+outcome, http.StatusSeeOther)
		return
	}
	title, text := "Colliedesk is approved", "Colliedesk is approved for your organization. Tell your Colliedesk admin to select Connect Microsoft 365 again."
	if !approved {
		title, text = "Approval not completed", "The approval was not completed at Microsoft. Your Colliedesk admin can send you the approval link again."
	}
	header := response.Header()
	header.Set("Content-Type", "text/html; charset=utf-8")
	header.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("X-Frame-Options", "DENY")
	header.Set("Cache-Control", "no-store")
	header.Set("Referrer-Policy", "no-referrer")
	response.WriteHeader(http.StatusOK)
	// Light or dark as the admin's device is set (inline style only: the policy above allows no file).
	fmt.Fprintf(response, `<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="color-scheme" content="light dark"><title>%s · Colliedesk</title>
<style>body{font-family:system-ui,sans-serif;max-width:32rem;margin:15vh auto;padding:0 16px;line-height:1.5;color:#1f2933;background:#fff}
h1{font-size:1.4rem}@media (prefers-color-scheme:dark){body{color:#e4e7eb;background:#15181c}}</style>
<body><h1>%s</h1><p>%s</p></body></html>`, html.EscapeString(title), html.EscapeString(title), html.EscapeString(text))
}

// adminConsentReturn is the settings page an admin consent returns to for a signed-in person: the
// channel named by state when it is an email channel of their workspace, else the default email
// channel's. false without a session.
func (a *API) adminConsentReturn(request *http.Request, state string) (string, bool) {
	cookie, err := request.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		return "", false
	}
	who, err := a.auth.Authenticate(request.Context(), cookie.Value, true)
	if err != nil || who.IsToken() || !auth.HasPermission(who.Agent.Role, auth.PermissionAccessRead) {
		return "", false
	}
	page := "/settings/channels/type/email"
	if uuidShape.MatchString(state) {
		channel, err := a.store.GetEmailChannelByID(request.Context(), who.Workspace.ID, state)
		if err == nil && channel != nil {
			page = "/settings/channels/" + channel.ChannelID
		}
	}
	return page, true
}

// microsoftMailbox connects Microsoft 365 through the operator's app registration.
type microsoftMailbox struct{ config microsoft.Config }

func (a *API) microsoft() microsoftMailbox { return microsoftMailbox{config: a.config.Microsoft} }

func (microsoftMailbox) name() string              { return domain.MailboxMicrosoft }
func (microsoftMailbox) label() string             { return "Microsoft 365" }
func (microsoftMailbox) resultKey() string         { return "microsoft" }
func (microsoftMailbox) notConfiguredCode() string { return "microsoft_not_configured" }
func (microsoftMailbox) unavailable() string       { return "microsoft_unavailable" }
func (microsoftMailbox) callbackPath() string      { return microsoftCallbackPath }

// revokes is false: Microsoft has no API that revokes one app's grant for one user. A dropped token is
// useless without this server's client secret; the person removes the app in their account.
func (microsoftMailbox) revokes() bool                        { return false }
func (microsoftMailbox) revoke(context.Context, string) error { return nil }
func (m microsoftMailbox) missing() []string                  { return m.config.Missing() }
func (m microsoftMailbox) keys() tokenseal.Keys               { return m.config.Keys }
func (microsoftMailbox) grantContext(workspaceID string) string {
	return microsoft.GrantContext(workspaceID)
}

func (m microsoftMailbox) authorizationURL(redirectURI, state, verifier, hint string) string {
	return m.config.AuthorizationURL(redirectURI, state, verifier, hint)
}

func (m microsoftMailbox) exchange(ctx context.Context, code, verifier, redirectURI string) (mailboxGrant, string, error) {
	grant, err := m.config.Exchange(ctx, code, verifier, redirectURI)
	switch {
	case errors.Is(err, microsoft.ErrSendNotGranted):
		return mailboxGrant{}, "send_not_granted", nil
	case errors.Is(err, microsoft.ErrNoRefreshToken):
		return mailboxGrant{}, "no_refresh_token", nil
	case errors.Is(err, microsoft.ErrNoMailbox):
		return mailboxGrant{}, "mailbox_unavailable", nil
	case errors.Is(err, microsoft.ErrClientConfig):
		return mailboxGrant{}, "server_misconfigured", err
	case err != nil:
		return mailboxGrant{}, "", err
	}
	return mailboxGrant{RefreshToken: grant.RefreshToken, Address: grant.Address, AccountID: grant.AccountID, Scopes: grant.Scopes}, "", nil
}
