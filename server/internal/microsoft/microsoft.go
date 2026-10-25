// Package microsoft connects a Microsoft 365 or Outlook.com mailbox for sending: the Microsoft identity
// platform's OAuth 2.0 authorization code flow with PKCE for a confidential client (consent, code
// exchange, refresh), the Microsoft Graph user the grant belongs to (GET /me) and Graph's sendMail with a
// MIME message. Only Mail.Send and User.Read are asked for besides openid and offline_access; nothing
// reads mail. Plain net/http against Microsoft's documented endpoints; no SDK.
//
// Unlike Google, Microsoft has no API that revokes one app's grant for one user (revokeSignInSessions
// signs the user out of every app and needs an admin permission), so there is no Revoke: disconnecting
// deletes the token, which is useless without this server's client secret, and the person removes the app
// in their Microsoft account.
//
//	https://learn.microsoft.com/en-us/entra/identity-platform/v2-oauth2-auth-code-flow
//	https://learn.microsoft.com/en-us/graph/api/user-sendmail?view=graph-rest-1.0
package microsoft

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
)

// Scopes asked for at consent. Mail.Send and User.Read need no admin consent (Graph permissions
// reference); openid only gives the ID token whose tid tells a personal account from a work one, and
// offline_access the refresh token.
const (
	ScopeMailSend = "https://graph.microsoft.com/Mail.Send"
	ScopeUserRead = "https://graph.microsoft.com/User.Read"
	scopes        = "openid offline_access " + ScopeUserRead + " " + ScopeMailSend
	// adminConsentScopes are the Graph permissions an organization's admin approves for everyone.
	adminConsentScopes = ScopeMailSend + " " + ScopeUserRead
)

// PersonalTenant is the tenant id of every personal Microsoft account (Outlook.com, Hotmail, Live): the
// tid claim of their ID tokens (ID token claims reference).
const PersonalTenant = "9188040d-6c67-4c5b-b112-36a304b66dad"

// LargeAttachmentBytes is the attachment size from which Graph says to upload files in a session
// instead of one request (outlook-large-attachments). Microsoft documents no limit for a MIME sendMail,
// so a larger reply is still sent; the web app only warns that Microsoft may refuse it.
const LargeAttachmentBytes = 3 << 20

const (
	loginBase      = "https://login.microsoftonline.com"
	graphBase      = "https://graph.microsoft.com"
	mePath         = "/v1.0/me?$select=id,mail,userPrincipalName"
	sendPath       = "/v1.0/me/sendMail"
	requestTimeout = 60 * time.Second
	maxAnswerBytes = 1 << 20
	// DefaultTenant accepts work and school accounts of any organization and personal accounts.
	DefaultTenant = "common"
)

// Config is the operator's own app registration in Microsoft Entra (a Web platform redirect URI and a
// client secret) and the keys that seal refresh tokens.
type Config struct {
	ClientID     string
	ClientSecret string
	// Tenant is the {tenant} of the endpoints: common (default), organizations, consumers, or one
	// tenant's id or domain for a single-tenant app.
	Tenant string
	Keys   tokenseal.Keys
	// Base replaces both login.microsoftonline.com and graph.microsoft.com with one local server (tests
	// and the e2e stand-in). Empty is Microsoft.
	Base string
	HTTP *http.Client
}

// Environment variables read by ConfigFromEnv. The token keys are the ones Gmail uses (MAILBOX_TOKEN_KEYS).
const (
	EnvClientID     = "MICROSOFT_OAUTH_CLIENT_ID"
	EnvClientSecret = "MICROSOFT_OAUTH_CLIENT_SECRET"
	EnvTenant       = "MICROSOFT_OAUTH_TENANT"
	EnvTokenKeys    = "MAILBOX_TOKEN_KEYS"
)

// tenantShape is what a tenant may look like in a URL path: a GUID, a domain or one of the three names.
var tenantShape = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]{0,99}$`)

// ConfigFromEnv reads MICROSOFT_OAUTH_CLIENT_ID, MICROSOFT_OAUTH_CLIENT_SECRET and MICROSOFT_OAUTH_TENANT,
// with the token keys the server already parsed from MAILBOX_TOKEN_KEYS. A malformed tenant is an error;
// missing settings are not (Missing names them).
func ConfigFromEnv(getenv func(string) string, keys tokenseal.Keys) (Config, error) {
	tenant := strings.TrimSpace(getenv(EnvTenant))
	if tenant == "" {
		tenant = DefaultTenant
	}
	if !tenantShape.MatchString(tenant) {
		return Config{}, fmt.Errorf("%s: %q is not common, organizations, consumers, a tenant id or a domain", EnvTenant, tenant)
	}
	return Config{ClientID: strings.TrimSpace(getenv(EnvClientID)), ClientSecret: strings.TrimSpace(getenv(EnvClientSecret)), Tenant: tenant, Keys: keys}, nil
}

// Missing names the settings still needed before Microsoft 365 can be connected, empty when none.
func (c Config) Missing() []string {
	var missing []string
	if c.ClientID == "" {
		missing = append(missing, EnvClientID)
	}
	if c.ClientSecret == "" {
		missing = append(missing, EnvClientSecret)
	}
	if !c.Keys.Configured() {
		missing = append(missing, EnvTokenKeys)
	}
	return missing
}

// Enabled reports whether Microsoft 365 can be connected on this server.
func (c Config) Enabled() bool { return len(c.Missing()) == 0 }

func (c Config) tenant() string {
	if c.Tenant == "" {
		return DefaultTenant
	}
	return c.Tenant
}

func (c Config) login(path string) string {
	if c.Base != "" {
		return strings.TrimRight(c.Base, "/") + path
	}
	return loginBase + path
}

func (c Config) graph(path string) string {
	if c.Base != "" {
		return strings.TrimRight(c.Base, "/") + path
	}
	return graphBase + path
}

func (c Config) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: requestTimeout}
}

// GrantContext is the additional data a workspace's sealed grant is bound to, so a sealed token copied
// into another workspace's row (or read as a Google grant) does not open.
func GrantContext(workspaceID string) string { return "colliedesk/mailbox/microsoft/" + workspaceID }

// Challenge is the PKCE S256 challenge of a verifier.
func Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// AuthorizationURL is where the person's browser goes to sign in and consent. response_mode=query keeps
// the redirect a top-level GET, which carries the SameSite=Lax session cookie the callback needs (a
// form_post would arrive without it). prompt=select_account lets the person pick the support mailbox
// instead of the account the browser is signed in to; loginHint pre-fills it.
func (c Config) AuthorizationURL(redirectURI, state, verifier, loginHint string) string {
	query := url.Values{
		"client_id": {c.ClientID}, "redirect_uri": {redirectURI}, "response_type": {"code"}, "response_mode": {"query"},
		"scope": {scopes}, "state": {state}, "code_challenge": {Challenge(verifier)}, "code_challenge_method": {"S256"},
		"prompt": {"select_account"},
	}
	if loginHint != "" {
		query.Set("login_hint", loginHint)
	}
	return c.login("/"+url.PathEscape(c.tenant())+"/oauth2/v2.0/authorize") + "?" + query.Encode()
}

// AdminConsentURL is the address an organization's admin opens to approve Colliedesk for every user of
// the organization, returning to redirectURI with admin_consent=True or an error and never a code. It
// takes organizations or one tenant, never common, and personal accounts have no admin: "" with the
// consumers tenant (v2-admin-consent).
func (c Config) AdminConsentURL(redirectURI, state string) string {
	tenant := c.tenant()
	switch strings.ToLower(tenant) {
	case "consumers":
		return ""
	case "common", "organizations":
		tenant = "organizations"
	}
	query := url.Values{"client_id": {c.ClientID}, "scope": {adminConsentScopes}, "redirect_uri": {redirectURI}}
	if state != "" {
		query.Set("state", state)
	}
	return c.login("/"+url.PathEscape(tenant)+"/v2.0/adminconsent") + "?" + query.Encode()
}

// Grant is what a completed consent gave: the refresh token, the mailbox address it sends from and the
// account's own id.
type Grant struct {
	RefreshToken string
	Address      string
	AccountID    string
	Scopes       []string
}

// Errors of the consent step, each a fixed code the settings page explains, and of sending.
var (
	ErrNoRefreshToken = errors.New("microsoft returned no refresh token")
	ErrSendNotGranted = errors.New("the Mail.Send permission was not granted")
	// ErrNoMailbox means the account has no Exchange Online mailbox to send from.
	ErrNoMailbox = errors.New("the Microsoft account has no mailbox")
	// ErrRevoked means the refresh token no longer works (removed, expired after 90 days unused, password
	// reset, a sign-in policy that now needs the person): reconnect.
	ErrRevoked = errors.New("the Microsoft 365 connection was revoked or expired")
	// ErrClientConfig means Microsoft refused this server's own app credentials (invalid_client: a wrong or
	// expired client secret): the operator must fix the server; the connection itself is fine.
	ErrClientConfig = errors.New("microsoft refused this server's client credentials")
	// ErrUnauthorized means Graph refused the access token (401).
	ErrUnauthorized = errors.New("microsoft graph refused the access token")
)

type tokenAnswer struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	IDToken      string `json:"id_token"`
	Error        string `json:"error"`
	ErrorCodes   []int  `json:"error_codes"`
}

// Exchange trades the authorization code for tokens, checks what was granted, and reads the mailbox
// address from Graph (GET /me): the ID token's email claim is not guaranteed to be correct and must not
// be used to authorize (ID token claims reference).
func (c Config) Exchange(ctx context.Context, code, verifier, redirectURI string) (Grant, error) {
	answer, err := c.token(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {redirectURI}})
	if err != nil {
		return Grant{}, err
	}
	granted := grantedScopes(answer.Scope)
	// The answer's scope is optional: without it the token holds the scopes asked for.
	if answer.Scope != "" && !hasScope(granted, ScopeMailSend) {
		return Grant{}, ErrSendNotGranted
	}
	if len(granted) == 0 {
		granted = strings.Fields(scopes)
	}
	if answer.RefreshToken == "" {
		return Grant{}, ErrNoRefreshToken
	}
	tenant := c.tenantOf(answer.IDToken)
	user, err := c.me(ctx, answer.AccessToken)
	if err != nil {
		return Grant{}, err
	}
	address := strings.ToLower(strings.TrimSpace(user.Mail))
	// An Outlook.com account's /me often has no mail (a community answer, not documented; checked on a
	// real account before Cloud offers it): its sign-in name is its address. A work account without mail
	// has no Exchange Online mailbox.
	if address == "" && tenant == PersonalTenant {
		address = strings.ToLower(strings.TrimSpace(user.UserPrincipalName))
	}
	if address == "" || !strings.Contains(address, "@") || len(address) > 254 {
		return Grant{}, ErrNoMailbox
	}
	return Grant{RefreshToken: answer.RefreshToken, Address: address, AccountID: user.ID, Scopes: granted}, nil
}

// grantedScopes reads the space-separated scope of a token answer, which Microsoft's own examples show
// URL-encoded.
func grantedScopes(scope string) []string {
	if decoded, err := url.QueryUnescape(scope); err == nil {
		scope = decoded
	}
	return strings.Fields(scope)
}

// hasScope finds a Graph scope with or without its resource prefix, in any case: Microsoft answers Mail.Send
// for https://graph.microsoft.com/Mail.Send.
func hasScope(granted []string, want string) bool {
	short := strings.TrimPrefix(want, graphBase+"/")
	return slices.ContainsFunc(granted, func(scope string) bool {
		return strings.EqualFold(scope, want) || strings.EqualFold(scope, short)
	})
}

// tenantOf reads the tid claim of the ID token that came straight from the token endpoint over TLS
// (OpenID Connect accepts that in place of checking its signature); its audience must be this client. ""
// when it cannot be read: the account then counts as a work account.
func (c Config) tenantOf(idToken string) string {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Audience string `json:"aud"`
		Tenant   string `json:"tid"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Audience != c.ClientID {
		return ""
	}
	return claims.Tenant
}

type graphUser struct {
	ID                string `json:"id"`
	Mail              string `json:"mail"`
	UserPrincipalName string `json:"userPrincipalName"`
}

func (c Config) me(ctx context.Context, accessToken string) (graphUser, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.graph(mePath), nil)
	if err != nil {
		return graphUser{}, err
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	response, err := c.client().Do(request)
	if err != nil {
		return graphUser{}, fmt.Errorf("microsoft graph /me: %w", err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, maxAnswerBytes))
	if response.StatusCode != http.StatusOK {
		failure := graphError(response, body)
		if failure.MailboxUnavailable() {
			return graphUser{}, ErrNoMailbox
		}
		return graphUser{}, failure
	}
	var user graphUser
	if err := json.Unmarshal(body, &user); err != nil || user.ID == "" {
		return graphUser{}, errors.New("microsoft graph /me answered without a user")
	}
	return user, nil
}

// AccessToken gets a fresh access token. Microsoft replaces the refresh token on every use and the new
// one slides the 90-day lifetime, so rotated is the token to store from now on ("" when none came back).
// ErrRevoked means the grant is gone; ErrClientConfig that this server's credentials are wrong; any other
// error is temporary.
func (c Config) AccessToken(ctx context.Context, refreshToken string) (access, rotated string, err error) {
	answer, err := c.token(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}})
	if err != nil {
		return "", "", err
	}
	if answer.RefreshToken != refreshToken {
		rotated = answer.RefreshToken
	}
	return answer.AccessToken, rotated, nil
}

func (c Config) token(ctx context.Context, form url.Values) (tokenAnswer, error) {
	form.Set("client_id", c.ClientID)
	form.Set("client_secret", c.ClientSecret)
	form.Set("scope", scopes)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.login("/"+url.PathEscape(c.tenant())+"/oauth2/v2.0/token"), strings.NewReader(form.Encode()))
	if err != nil {
		return tokenAnswer{}, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := c.client().Do(request)
	if err != nil {
		return tokenAnswer{}, fmt.Errorf("microsoft token endpoint: %w", err)
	}
	defer response.Body.Close()
	var answer tokenAnswer
	body, _ := io.ReadAll(io.LimitReader(response.Body, maxAnswerBytes))
	_ = json.Unmarshal(body, &answer)
	// React to error, never to error_description (reference-error-codes); the AADSTS numbers only go to logs.
	switch {
	// unauthorized_client is the account's organization: the app is not (any more) added to its tenant, or it
	// was disabled there (AADSTS700016, 7000112). That grant is gone; the server's credentials are fine.
	case answer.Error == "invalid_grant", answer.Error == "interaction_required", answer.Error == "consent_required", answer.Error == "unauthorized_client":
		return tokenAnswer{}, fmt.Errorf("%w (%s %s)", ErrRevoked, answer.Error, stsCodes(answer.ErrorCodes))
	case answer.Error == "invalid_client":
		return tokenAnswer{}, fmt.Errorf("%w (%s %s)", ErrClientConfig, answer.Error, stsCodes(answer.ErrorCodes))
	case response.StatusCode != http.StatusOK:
		// Never the body: it can echo the client id. The error code alone is enough to act on.
		return tokenAnswer{}, &APIError{Status: response.StatusCode, Code: answer.Error, RetryAfter: retryAfter(response)}
	case answer.AccessToken == "":
		return tokenAnswer{}, errors.New("microsoft token endpoint answered without an access token")
	}
	return answer, nil
}

func stsCodes(codes []int) string {
	parts := make([]string, len(codes))
	for index, code := range codes {
		parts[index] = "AADSTS" + strconv.Itoa(code)
	}
	return strings.Join(parts, ",")
}

// APIError is an error answer from Microsoft: Code is the innermost Graph error code it carries (or the
// OAuth error of the token endpoint), RetryAfter the wait it asked for. Graph's message is meant for
// developers only and is not kept.
type APIError struct {
	Status     int
	Code       string
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	text := fmt.Sprintf("Microsoft answered HTTP %d", e.Status)
	if e.Code != "" {
		text += " " + e.Code
	}
	return text
}

// Temporary reports an answer worth retrying later: throttling and server errors (throttling).
func (e *APIError) Temporary() bool {
	return e.Status == http.StatusTooManyRequests || e.Status >= 500
}

// mailboxCodes are the Graph codes that say the account has no Exchange Online mailbox. Microsoft does
// not list them in one place; checked against a real account before Cloud offers Microsoft 365.
var mailboxCodes = []string{"MailboxNotEnabledForRESTAPI", "MailboxNotHostedInExchangeOnline", "ErrorNonExistentMailbox", "ErrorMailboxNotFound"}

// MailboxUnavailable reports an answer that says the account has no mailbox to send from (for example a
// licence removed).
func (e *APIError) MailboxUnavailable() bool {
	return slices.ContainsFunc(mailboxCodes, func(code string) bool { return strings.EqualFold(code, e.Code) })
}

// TooLarge reports a message bigger than Microsoft accepts. Graph documents no limit for a MIME sendMail.
func (e *APIError) TooLarge() bool {
	return e.Status == http.StatusRequestEntityTooLarge || strings.EqualFold(e.Code, "ErrorMessageSizeExceeded")
}

// PermissionDenied reports a refusal of this grant to send from the mailbox (403, or a 404 of the
// mailbox): the admin must reconnect.
func (e *APIError) PermissionDenied() bool {
	return !e.Temporary() && !e.MailboxUnavailable() && (e.Status == http.StatusForbidden || e.Status == http.StatusNotFound)
}

// retryAfter reads Retry-After in seconds (or as an HTTP date); 0 without one.
func retryAfter(response *http.Response) time.Duration {
	value := strings.TrimSpace(response.Header.Get("Retry-After"))
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil {
		if wait := time.Until(at); wait > 0 {
			return wait
		}
	}
	return 0
}

// Send hands one composed RFC 5322 message to Graph as MIME (base64 in a text/plain body), which keeps
// our Message-ID, In-Reply-To and References headers: the JSON form only takes custom x- headers. Graph
// sends it from the signed-in mailbox, keeps a copy in Sent Items and answers 202 once it accepted it.
func (c Config) Send(ctx context.Context, accessToken string, message []byte) error {
	encoded := base64.StdEncoding.EncodeToString(message)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.graph(sendPath), strings.NewReader(encoded))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "text/plain")
	request.Header.Set("Authorization", "Bearer "+accessToken)
	response, err := c.client().Do(request)
	if err != nil {
		return fmt.Errorf("microsoft graph sendMail: %w", err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, maxAnswerBytes))
	if response.StatusCode == http.StatusAccepted {
		return nil
	}
	failure := graphError(response, body)
	if response.StatusCode == http.StatusUnauthorized && !failure.MailboxUnavailable() {
		return ErrUnauthorized
	}
	return failure
}

// graphError reads a Graph error answer. Graph nests more specific codes in innererror and says to use
// the most specific one the client understands (Graph errors reference): a nested mailbox or size code
// wins, else the top code stays.
func graphError(response *http.Response, body []byte) *APIError {
	type nested struct {
		Code       string          `json:"code"`
		InnerError json.RawMessage `json:"innererror"`
	}
	var answer struct {
		Error nested `json:"error"`
	}
	_ = json.Unmarshal(body, &answer)
	failure := &APIError{Status: response.StatusCode, Code: answer.Error.Code, RetryAfter: retryAfter(response)}
	current := answer.Error
	for range 5 {
		var next nested
		if len(current.InnerError) == 0 || json.Unmarshal(current.InnerError, &next) != nil || next.Code == "" {
			break
		}
		if known := (&APIError{Status: failure.Status, Code: next.Code}); known.MailboxUnavailable() || strings.EqualFold(next.Code, "ErrorMessageSizeExceeded") {
			failure.Code = next.Code
		}
		current = next
	}
	if len(failure.Code) > 100 {
		failure.Code = failure.Code[:100]
	}
	return failure
}
