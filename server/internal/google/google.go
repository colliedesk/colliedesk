// Package google connects a workspace's Gmail account for sending: the OAuth 2.0 authorization code
// flow with PKCE (consent, code exchange, refresh, revoke) and the Gmail API messages.send call. Only
// the gmail.send scope is asked for besides openid email, which names the address that connected.
// Plain net/http against Google's documented endpoints; no SDK.
package google

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
)

// Scopes asked for at consent. gmail.send is a sensitive scope (no CASA); openid email only identify
// the account, because gmail.send cannot read the address it sends from.
const (
	ScopeGmailSend = "https://www.googleapis.com/auth/gmail.send"
	scopes         = "openid email " + ScopeGmailSend
)

const (
	authPath       = "/o/oauth2/v2/auth"
	tokenPath      = "/token"
	revokePath     = "/revoke"
	sendPath       = "/upload/gmail/v1/users/me/messages/send?uploadType=media"
	requestTimeout = 60 * time.Second
	maxAnswerBytes = 1 << 20
)

var (
	defaultAuthURL   = "https://accounts.google.com" + authPath
	defaultTokenURL  = "https://oauth2.googleapis.com" + tokenPath
	defaultRevokeURL = "https://oauth2.googleapis.com" + revokePath
	defaultGmailURL  = "https://gmail.googleapis.com"
)

// Config is the operator's own OAuth client (Google Cloud console, Web application) and the keys that
// seal refresh tokens. All three settings are needed; with any missing, Gmail cannot be connected.
type Config struct {
	ClientID     string
	ClientSecret string
	Keys         tokenseal.Keys
	// Base replaces every Google address with one local server (tests and the e2e stand-in). Empty is Google.
	Base string
	HTTP *http.Client
}

// Environment variables read by ConfigFromEnv.
const (
	EnvClientID     = "GOOGLE_OAUTH_CLIENT_ID"
	EnvClientSecret = "GOOGLE_OAUTH_CLIENT_SECRET"
	EnvTokenKeys    = "MAILBOX_TOKEN_KEYS"
)

// ConfigFromEnv reads GOOGLE_OAUTH_CLIENT_ID, GOOGLE_OAUTH_CLIENT_SECRET and MAILBOX_TOKEN_KEYS. A
// malformed key ring is an error; missing settings are not (Missing names them).
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	keys, err := tokenseal.ParseKeys(getenv(EnvTokenKeys))
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", EnvTokenKeys, err)
	}
	return Config{ClientID: strings.TrimSpace(getenv(EnvClientID)), ClientSecret: strings.TrimSpace(getenv(EnvClientSecret)), Keys: keys}, nil
}

// Missing names the settings still needed before Gmail can be connected, empty when none.
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

// Enabled reports whether Gmail can be connected on this server.
func (c Config) Enabled() bool { return len(c.Missing()) == 0 }

func (c Config) url(path, fallback string) string {
	if c.Base != "" {
		return strings.TrimRight(c.Base, "/") + path
	}
	return fallback
}

func (c Config) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: requestTimeout}
}

// GrantContext is the additional data a workspace's sealed grant is bound to, so a sealed token copied
// into another workspace's row does not open.
func GrantContext(workspaceID string) string { return "colliedesk/mailbox/google/" + workspaceID }

// Challenge is the PKCE S256 challenge of a verifier.
func Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// AuthorizationURL is where the person's browser goes to consent. access_type=offline and
// prompt=consent make Google return a refresh token every time, also when the account consented before.
func (c Config) AuthorizationURL(redirectURI, state, verifier string) string {
	query := url.Values{
		"client_id": {c.ClientID}, "redirect_uri": {redirectURI}, "response_type": {"code"}, "scope": {scopes},
		"state": {state}, "code_challenge": {Challenge(verifier)}, "code_challenge_method": {"S256"},
		"access_type": {"offline"}, "prompt": {"consent"},
	}
	return c.url(authPath, defaultAuthURL) + "?" + query.Encode()
}

// Grant is what a completed consent gave: the refresh token and the account it belongs to.
type Grant struct {
	RefreshToken string
	Address      string
	Scopes       []string
}

// Errors of the consent step, each a fixed code the settings page explains.
var (
	ErrNoRefreshToken = errors.New("google returned no refresh token")
	ErrSendNotGranted = errors.New("the gmail.send permission was not granted")
	ErrNoAddress      = errors.New("google did not name a verified email address")
	// ErrRevoked means the refresh token no longer works (revoked, expired, password reset): reconnect.
	ErrRevoked = errors.New("the Gmail connection was revoked or expired")
)

type tokenAnswer struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	IDToken      string `json:"id_token"`
	Error        string `json:"error"`
}

// Exchange trades the authorization code for tokens and checks what was granted. The ID token comes
// straight from Google's token endpoint over TLS, which OpenID Connect accepts in place of checking its
// signature; its audience must still be this client.
func (c Config) Exchange(ctx context.Context, code, verifier, redirectURI string) (Grant, error) {
	answer, err := c.token(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {redirectURI}})
	if err != nil {
		return Grant{}, err
	}
	granted := strings.Fields(answer.Scope)
	if !slices.Contains(granted, ScopeGmailSend) {
		return Grant{}, ErrSendNotGranted
	}
	if answer.RefreshToken == "" {
		return Grant{}, ErrNoRefreshToken
	}
	address, err := c.verifiedAddress(answer.IDToken)
	if err != nil {
		return Grant{}, err
	}
	return Grant{RefreshToken: answer.RefreshToken, Address: address, Scopes: granted}, nil
}

func (c Config) verifiedAddress(idToken string) (string, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return "", ErrNoAddress
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", ErrNoAddress
	}
	var claims struct {
		Issuer        string          `json:"iss"`
		Audience      json.RawMessage `json:"aud"`
		Email         string          `json:"email"`
		EmailVerified any             `json:"email_verified"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", ErrNoAddress
	}
	var audiences []string
	if json.Unmarshal(claims.Audience, &audiences) != nil {
		var single string
		_ = json.Unmarshal(claims.Audience, &single)
		audiences = []string{single}
	}
	verified := claims.EmailVerified == true || claims.EmailVerified == "true"
	issuerOK := claims.Issuer == "https://accounts.google.com" || claims.Issuer == "accounts.google.com"
	if !issuerOK || !slices.Contains(audiences, c.ClientID) || !verified || claims.Email == "" {
		return "", ErrNoAddress
	}
	return strings.ToLower(claims.Email), nil
}

// AccessToken gets a fresh access token. ErrRevoked means the grant is gone; any other error is
// temporary. A rotated refresh token, when Google sends one, is returned so the caller can store it.
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
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url(tokenPath, defaultTokenURL), strings.NewReader(form.Encode()))
	if err != nil {
		return tokenAnswer{}, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := c.client().Do(request)
	if err != nil {
		return tokenAnswer{}, fmt.Errorf("google token endpoint: %w", err)
	}
	defer response.Body.Close()
	var answer tokenAnswer
	body, _ := io.ReadAll(io.LimitReader(response.Body, maxAnswerBytes))
	_ = json.Unmarshal(body, &answer)
	switch {
	case answer.Error == "invalid_grant":
		return tokenAnswer{}, ErrRevoked
	case response.StatusCode != http.StatusOK:
		// Never the body: it can echo the client id. The error code alone is enough to act on.
		return tokenAnswer{}, &APIError{Status: response.StatusCode, Reason: answer.Error}
	case answer.AccessToken == "":
		return tokenAnswer{}, errors.New("google token endpoint answered without an access token")
	}
	return answer, nil
}

// Revoke asks Google to forget the grant; the token stops working at once.
func (c Config) Revoke(ctx context.Context, token string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url(revokePath, defaultRevokeURL), strings.NewReader(url.Values{"token": {token}}.Encode()))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := c.client().Do(request)
	if err != nil {
		return fmt.Errorf("google revoke: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxAnswerBytes))
	// 400 invalid_token: already revoked or expired, which is the state asked for.
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusBadRequest {
		return &APIError{Status: response.StatusCode}
	}
	return nil
}

// APIError is an error answer from Google. Reason is the first errors[].reason of a Gmail API error
// (or the ErrorInfo reason), or the OAuth error code of the token endpoint.
type APIError struct {
	Status  int
	Reason  string
	Message string
}

func (e *APIError) Error() string {
	text := fmt.Sprintf("Gmail answered HTTP %d", e.Status)
	if e.Reason != "" {
		text += " " + e.Reason
	}
	if e.Message != "" {
		text += ": " + e.Message
	}
	return text
}

// rateReasons are the 403 reasons Gmail uses for quota and rate limits: wait and try again.
var rateReasons = []string{"rateLimitExceeded", "userRateLimitExceeded", "dailyLimitExceeded", "quotaExceeded", "RATE_LIMIT_EXCEEDED"}

// Temporary reports an answer worth retrying later: rate limits and server errors.
func (e *APIError) Temporary() bool {
	return e.Status == http.StatusTooManyRequests || e.Status >= 500 || (e.Status == http.StatusForbidden && slices.Contains(rateReasons, e.Reason))
}

// ConnectionBroken reports an answer that says the grant no longer allows sending (a 403 that is not
// a rate limit, such as insufficientPermissions): the admin must reconnect.
func (e *APIError) ConnectionBroken() bool {
	return e.Status == http.StatusForbidden && !e.Temporary()
}

// ErrUnauthorized means Gmail refused the access token (401).
var ErrUnauthorized = errors.New("gmail refused the access token")

// Send hands one composed RFC 5322 message to Gmail, which sends it from the connected account and keeps
// a copy in Sent. Every To and Cc in the message receives it.
func (c Config) Send(ctx context.Context, accessToken string, message []byte) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url(sendPath, defaultGmailURL+sendPath), bytes.NewReader(message))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "message/rfc822")
	request.Header.Set("Authorization", "Bearer "+accessToken)
	response, err := c.client().Do(request)
	if err != nil {
		return fmt.Errorf("gmail send: %w", err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, maxAnswerBytes))
	if response.StatusCode == http.StatusOK {
		return nil
	}
	if response.StatusCode == http.StatusUnauthorized {
		return ErrUnauthorized
	}
	return gmailError(response.StatusCode, body)
}

func gmailError(status int, body []byte) *APIError {
	var answer struct {
		Error struct {
			Message string `json:"message"`
			Errors  []struct {
				Reason string `json:"reason"`
			} `json:"errors"`
			Details []struct {
				Reason string `json:"reason"`
			} `json:"details"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &answer)
	failure := &APIError{Status: status, Message: answer.Error.Message}
	if len(failure.Message) > 200 {
		failure.Message = failure.Message[:200]
	}
	for _, item := range answer.Error.Errors {
		if item.Reason != "" {
			failure.Reason = item.Reason
			return failure
		}
	}
	for _, item := range answer.Error.Details {
		if item.Reason != "" {
			failure.Reason = item.Reason
			return failure
		}
	}
	return failure
}
