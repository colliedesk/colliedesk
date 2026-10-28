// Package microsofttest is a local stand-in for the Microsoft endpoints Colliedesk calls to connect a
// Microsoft 365 or Outlook.com mailbox: the sign-in and consent page, the token endpoint (authorization
// code with PKCE, refresh that replaces the refresh token every time), the admin consent page, Graph's
// /me and sendMail. Tests and the e2e build point microsoft.Config.Base at it; no real Microsoft account
// or credential is involved. Answers follow Microsoft's documented shapes.
package microsofttest

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/colliedesk/colliedesk/server/internal/microsoft"
)

// Account kinds on the sign-in page.
const (
	Work     = "work"
	Personal = "personal"
)

// workTenant is the tenant id of the stand-in's organization.
const workTenant = "11111111-2222-3333-4444-555555555555"

// Sent is one message the stand-in accepted for sending.
type Sent struct {
	// Account is the mailbox the message was sent from.
	Account string
	Raw     []byte
	// MessageID is the Message-ID the recipients see (rewritten when RewriteMessageID is on).
	MessageID string
}

// Failure is a scripted Graph answer for the next sends: Status, the error code and Retry-After seconds.
type Failure struct {
	Status     int
	Code       string
	RetryAfter int
}

// TokenFailure is a scripted token endpoint answer: the OAuth error and its AADSTS numbers.
type TokenFailure struct {
	Status int
	Error  string
	Codes  []int
}

type user struct {
	address, kind, id string
}

type pendingCode struct {
	user                          user
	scope, challenge, redirectURI string
}

// Server is the stand-in. Fields under mu can be changed by tests between calls (Set).
type Server struct {
	ClientID, ClientSecret string

	mu      sync.Mutex
	codes   map[string]pendingCode
	refresh map[string]user // refresh token -> account
	access  map[string]user // access token -> account
	revoked map[string]int  // refresh token -> AADSTS code it is refused with
	sent    []Sent
	// GrantScope replaces the scope a code exchange answers.
	GrantScope string
	// OmitRefreshToken answers a code exchange without a refresh token.
	OmitRefreshToken bool
	// NoMailbox makes /me answer mail: null for work accounts (no Exchange Online mailbox).
	NoMailbox bool
	// RewriteMessageID makes delivered mail carry a Message-ID of Exchange's own instead of ours.
	RewriteMessageID bool
	// SendFailures are answered, in order, to the next sends.
	SendFailures []Failure
	// TokenFailures are answered, in order, to the next refresh token requests.
	TokenFailures []TokenFailure
	// MaxRequestBytes refuses larger sendMail bodies with 413 (0: no limit).
	MaxRequestBytes int
	// Refreshes counts refresh token requests.
	Refreshes int
}

// New returns a stand-in for the given app registration.
func New(clientID, clientSecret string) *Server {
	return &Server{ClientID: clientID, ClientSecret: clientSecret, codes: map[string]pendingCode{}, refresh: map[string]user{},
		access: map[string]user{}, revoked: map[string]int{}}
}

// Start serves the stand-in on a local port; close it with the returned server.
func (s *Server) Start() *httptest.Server { return httptest.NewServer(s) }

// ServeHTTP routes the Microsoft paths the client uses: /{tenant}/oauth2/v2.0/..., /{tenant}/v2.0/adminconsent
// and Graph's /v1.0/....
func (s *Server) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	path := request.URL.Path
	switch {
	case strings.HasSuffix(path, "/oauth2/v2.0/authorize") && request.Method == http.MethodGet:
		s.consentPage(response, request)
	case strings.HasSuffix(path, "/oauth2/v2.0/authorize") && request.Method == http.MethodPost:
		s.decide(response, request)
	case strings.HasSuffix(path, "/oauth2/v2.0/token") && request.Method == http.MethodPost:
		s.token(response, request)
	case strings.HasSuffix(path, "/v2.0/adminconsent") && request.Method == http.MethodGet:
		s.adminConsentPage(response, request)
	case strings.HasSuffix(path, "/v2.0/adminconsent") && request.Method == http.MethodPost:
		s.adminDecide(response, request)
	case path == "/v1.0/me" && request.Method == http.MethodGet:
		s.me(response, request)
	case path == "/v1.0/me/sendMail" && request.Method == http.MethodPost:
		s.send(response, request)
	default:
		http.NotFound(response, request)
	}
}

func randomToken(prefix string) string {
	raw := make([]byte, 16)
	_, _ = rand.Read(raw)
	return prefix + hex.EncodeToString(raw)
}

func idFor(address string) string {
	sum := sha256.Sum256([]byte(address))
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}

// Consent plays the person on Microsoft's sign-in and consent pages: it reads an authorization URL and
// returns the redirect Microsoft would send the browser to, approving as account of kind (Work or
// Personal), or denying when account is empty.
func (s *Server) Consent(authorizationURL, account, kind string) (string, error) {
	parsed, err := url.Parse(authorizationURL)
	if err != nil {
		return "", err
	}
	return s.redirectFor(parsed.Query(), account, kind)
}

func (s *Server) redirectFor(query url.Values, account, kind string) (string, error) {
	if query.Get("client_id") != s.ClientID || query.Get("response_type") != "code" || query.Get("code_challenge_method") != "S256" ||
		query.Get("response_mode") != "query" || query.Get("prompt") != "select_account" || !strings.Contains(query.Get("scope"), "offline_access") {
		return "", fmt.Errorf("authorization request is not what Microsoft expects: %s", query.Encode())
	}
	redirect, err := url.Parse(query.Get("redirect_uri"))
	if err != nil || redirect.Scheme == "" || redirect.RawQuery != "" {
		return "", fmt.Errorf("bad redirect_uri %q (Microsoft refuses a query string with personal accounts)", query.Get("redirect_uri"))
	}
	answer := url.Values{"state": {query.Get("state")}}
	if account == "" {
		answer.Set("error", "access_denied")
		answer.Set("error_description", "AADSTS65004: User declined to consent to access the app.")
	} else {
		if kind != Personal {
			kind = Work
		}
		address := strings.ToLower(account)
		code := randomToken("code-")
		s.mu.Lock()
		s.codes[code] = pendingCode{user: user{address: address, kind: kind, id: idFor(address)}, scope: query.Get("scope"),
			challenge: query.Get("code_challenge"), redirectURI: query.Get("redirect_uri")}
		s.mu.Unlock()
		answer.Set("code", code)
	}
	redirect.RawQuery = answer.Encode()
	return redirect.String(), nil
}

func hiddenFields(query url.Values) string {
	var hidden strings.Builder
	for key, values := range query {
		for _, value := range values {
			fmt.Fprintf(&hidden, `<input type="hidden" name="%s" value="%s">`, html.EscapeString(key), html.EscapeString(value))
		}
	}
	return hidden.String()
}

func (s *Server) consentPage(response http.ResponseWriter, request *http.Request) {
	hint := request.URL.Query().Get("login_hint")
	if hint == "" {
		hint = "support@example.com"
	}
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(response, `<!doctype html><meta charset="utf-8"><title>Microsoft stand-in: sign in</title>
<body style="font-family:system-ui;max-width:32rem;margin:3rem auto">
<h1>Microsoft stand-in</h1><p>Local stand-in for Microsoft's sign-in and consent pages. <strong>%s</strong> asks to send email as you and read your profile.</p>
<form method="post">%s<p><label>Account <input name="account" type="email" value="%s" required></label></p>
<p><label><input type="radio" name="kind" value="work" checked> Work or school account</label>
<label><input type="radio" name="kind" value="personal"> Personal account</label></p>
<p><button name="decision" value="allow">Accept</button> <button name="decision" value="deny" formnovalidate>Cancel</button></p></form>`,
		html.EscapeString(request.URL.Query().Get("client_id")), hiddenFields(request.URL.Query()), html.EscapeString(hint))
}

func (s *Server) decide(response http.ResponseWriter, request *http.Request) {
	if err := request.ParseForm(); err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}
	account := request.PostForm.Get("account")
	if request.PostForm.Get("decision") != "allow" {
		account = ""
	}
	target, err := s.redirectFor(request.PostForm, account, request.PostForm.Get("kind"))
	if err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(response, request, target, http.StatusSeeOther)
}

func (s *Server) adminConsentPage(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(response, `<!doctype html><meta charset="utf-8"><title>Microsoft stand-in: admin consent</title>
<body style="font-family:system-ui;max-width:32rem;margin:3rem auto">
<h1>Microsoft stand-in</h1><p>An admin approves <strong>%s</strong> for the whole organization.</p>
<form method="post">%s<p><button name="decision" value="allow">Accept</button> <button name="decision" value="deny">Cancel</button></p></form>`,
		html.EscapeString(request.URL.Query().Get("client_id")), hiddenFields(request.URL.Query()))
}

func (s *Server) adminDecide(response http.ResponseWriter, request *http.Request) {
	if err := request.ParseForm(); err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}
	target, err := s.AdminConsent(request.PostForm, request.PostForm.Get("decision") == "allow")
	if err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(response, request, target, http.StatusSeeOther)
}

// AdminConsent returns the redirect after an admin approves (or refuses) the app: admin_consent=True with
// the tenant, never a code (v2-admin-consent).
func (s *Server) AdminConsent(query url.Values, approve bool) (string, error) {
	if query.Get("client_id") != s.ClientID {
		return "", fmt.Errorf("unknown client %q", query.Get("client_id"))
	}
	redirect, err := url.Parse(query.Get("redirect_uri"))
	if err != nil || redirect.Scheme == "" {
		return "", fmt.Errorf("bad redirect_uri %q", query.Get("redirect_uri"))
	}
	answer := url.Values{}
	if query.Get("state") != "" {
		answer.Set("state", query.Get("state"))
	}
	if approve {
		answer.Set("admin_consent", "True")
		answer.Set("tenant", workTenant)
		answer.Set("scope", query.Get("scope"))
	} else {
		answer.Set("error", "access_denied")
		answer.Set("error_description", "AADSTS65004: The admin declined.")
	}
	redirect.RawQuery = answer.Encode()
	return redirect.String(), nil
}

func writeJSON(response http.ResponseWriter, status int, body any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(body)
}

func tokenError(response http.ResponseWriter, status int, code string, codes ...int) {
	writeJSON(response, status, map[string]any{"error": code, "error_description": "AADSTS" + fmt.Sprint(codes) + ": stand-in",
		"error_codes": codes, "trace_id": randomToken(""), "correlation_id": randomToken("")})
}

func (s *Server) token(response http.ResponseWriter, request *http.Request) {
	if err := request.ParseForm(); err != nil {
		tokenError(response, http.StatusBadRequest, "invalid_request", 900144)
		return
	}
	form := request.PostForm
	s.mu.Lock()
	defer s.mu.Unlock()
	if form.Get("client_id") != s.ClientID || form.Get("client_secret") != s.ClientSecret {
		tokenError(response, http.StatusUnauthorized, "invalid_client", 7000215)
		return
	}
	switch form.Get("grant_type") {
	case "authorization_code":
		pending, ok := s.codes[form.Get("code")]
		delete(s.codes, form.Get("code"))
		sum := sha256.Sum256([]byte(form.Get("code_verifier")))
		if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != pending.challenge || form.Get("redirect_uri") != pending.redirectURI {
			tokenError(response, http.StatusBadRequest, "invalid_grant", 70008)
			return
		}
		scope := pending.scope
		if s.GrantScope != "" {
			scope = s.GrantScope
		}
		writeJSON(response, http.StatusOK, s.issue(pending.user, scope, !s.OmitRefreshToken))
	case "refresh_token":
		s.Refreshes++
		if len(s.TokenFailures) > 0 {
			failure := s.TokenFailures[0]
			s.TokenFailures = s.TokenFailures[1:]
			tokenError(response, failure.Status, failure.Error, failure.Codes...)
			return
		}
		account, ok := s.refresh[form.Get("refresh_token")]
		if code := s.revoked[form.Get("refresh_token")]; !ok || code != 0 {
			if code == 0 {
				code = 50173
			}
			tokenError(response, http.StatusBadRequest, "invalid_grant", code)
			return
		}
		// Like Microsoft: every refresh answers a new refresh token; the old one keeps working.
		writeJSON(response, http.StatusOK, s.issue(account, "openid offline_access User.Read Mail.Send profile", true))
	default:
		tokenError(response, http.StatusBadRequest, "unsupported_grant_type", 70003)
	}
}

// issue answers tokens for the account; call with mu held.
func (s *Server) issue(account user, scope string, refresh bool) map[string]any {
	access := randomToken("access-")
	s.access[access] = account
	answer := map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": 3599, "scope": scope, "id_token": s.idToken(account)}
	if refresh {
		token := randomToken("refresh-")
		s.refresh[token] = account
		answer["refresh_token"] = token
	}
	return answer
}

// idToken is an unsigned JWT with Microsoft's claims; the client reads it from a direct token answer.
func (s *Server) idToken(account user) string {
	tenant := workTenant
	if account.kind == Personal {
		tenant = microsoft.PersonalTenant
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	claims, _ := json.Marshal(map[string]any{"iss": "https://login.microsoftonline.com/" + tenant + "/v2.0", "aud": s.ClientID, "tid": tenant,
		"oid": account.id, "preferred_username": account.address, "email": account.address})
	return header + "." + base64.RawURLEncoding.EncodeToString(claims) + ".stand-in"
}

func graphFailure(response http.ResponseWriter, status int, code string, retryAfter int) {
	if retryAfter > 0 {
		response.Header().Set("Retry-After", strconv.Itoa(retryAfter))
	}
	writeJSON(response, status, map[string]any{"error": map[string]any{"code": code, "message": "stand-in",
		"innerError": map[string]any{"request-id": randomToken(""), "date": "2026-10-25T00:00:00"}}})
}

func (s *Server) caller(request *http.Request) (user, bool) {
	account, ok := s.access[strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")]
	return account, ok
}

func (s *Server) me(response http.ResponseWriter, request *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	account, ok := s.caller(request)
	if !ok {
		graphFailure(response, http.StatusUnauthorized, "InvalidAuthenticationToken", 0)
		return
	}
	body := map[string]any{"id": account.id, "userPrincipalName": account.address, "mail": account.address}
	// A personal account's /me has no mail; a work account without a mailbox neither.
	if account.kind == Personal || s.NoMailbox {
		body["mail"] = nil
	}
	writeJSON(response, http.StatusOK, body)
}

func (s *Server) send(response http.ResponseWriter, request *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	account, ok := s.caller(request)
	if !ok {
		graphFailure(response, http.StatusUnauthorized, "InvalidAuthenticationToken", 0)
		return
	}
	if len(s.SendFailures) > 0 {
		failure := s.SendFailures[0]
		s.SendFailures = s.SendFailures[1:]
		graphFailure(response, failure.Status, failure.Code, failure.RetryAfter)
		return
	}
	if !strings.HasPrefix(request.Header.Get("Content-Type"), "text/plain") {
		graphFailure(response, http.StatusBadRequest, "RequestBodyRead", 0)
		return
	}
	encoded, _ := io.ReadAll(io.LimitReader(request.Body, 200<<20))
	if s.MaxRequestBytes > 0 && len(encoded) > s.MaxRequestBytes {
		graphFailure(response, http.StatusRequestEntityTooLarge, "RequestEntityTooLarge", 0)
		return
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil {
		graphFailure(response, http.StatusBadRequest, "ErrorMimeContentInvalidBase64String", 0)
		return
	}
	parsed, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		graphFailure(response, http.StatusBadRequest, "ErrorMimeContentInvalid", 0)
		return
	}
	messageID := strings.Trim(parsed.Header.Get("Message-ID"), "<>")
	if s.RewriteMessageID || messageID == "" {
		messageID = randomToken("") + "@namprd00.prod.outlook.com"
	}
	s.sent = append(s.sent, Sent{Account: account.address, Raw: raw, MessageID: messageID})
	response.WriteHeader(http.StatusAccepted)
}

// Messages returns the messages accepted so far.
func (s *Server) Messages() []Sent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Sent(nil), s.sent...)
}

// RevokeAll plays the person removing Colliedesk in their Microsoft account (My Apps).
func (s *Server) RevokeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for token := range s.refresh {
		s.revoked[token] = 50173
	}
}

// ExpireAll plays 90 days without a refresh: every refresh token answers invalid_grant AADSTS700082.
func (s *Server) ExpireAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for token := range s.refresh {
		s.revoked[token] = 700082
	}
}

// AccountOf names the account a refresh token was issued to.
func (s *Server) AccountOf(token string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refresh[token].address
}

// Set changes the stand-in's scripted behaviour under its lock.
func (s *Server) Set(change func(*Server)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	change(s)
}
