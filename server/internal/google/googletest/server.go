// Package googletest is a local stand-in for the Google endpoints Colliedesk calls to connect Gmail:
// the consent page, the token endpoint (authorization code with PKCE, refresh), revoke, and Gmail's
// messages.send media upload. Tests and the e2e build point google.Config.Base at it; no real Google
// account or credential is involved. Answers follow Google's documented shapes.
package googletest

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
	"strings"
	"sync"
)

// Sent is one message the stand-in accepted for sending.
type Sent struct {
	// Account is the connected address the message was sent from.
	Account string
	Raw     []byte
	// MessageID is the Message-ID the recipients see (rewritten when RewriteMessageID is on).
	MessageID string
}

// Failure is a scripted answer for the next sends.
type Failure struct {
	Status int
	Reason string
}

type pendingCode struct {
	account, scope, challenge, redirectURI string
}

// Server is the stand-in. Fields under mu can be changed by tests between calls.
type Server struct {
	ClientID, ClientSecret string

	mu      sync.Mutex
	codes   map[string]pendingCode
	refresh map[string]string // refresh token -> account
	access  map[string]string // access token -> account
	revoked map[string]bool
	sent    []Sent
	// GrantScope replaces the scope the consent grants (a person can untick gmail.send).
	GrantScope string
	// OmitRefreshToken answers a code exchange without a refresh token.
	OmitRefreshToken bool
	// RewriteMessageID makes delivered mail carry a Message-ID of Gmail's own instead of ours.
	RewriteMessageID bool
	// SendFailures are answered, in order, to the next sends.
	SendFailures []Failure
	// IDTokenAudience replaces the aud claim of ID tokens.
	IDTokenAudience string
	// TokenDown makes the token endpoint answer 503.
	TokenDown bool
	tokens    int
}

// New returns a stand-in for the given OAuth client.
func New(clientID, clientSecret string) *Server {
	return &Server{ClientID: clientID, ClientSecret: clientSecret, codes: map[string]pendingCode{}, refresh: map[string]string{},
		access: map[string]string{}, revoked: map[string]bool{}}
}

// Start serves the stand-in on a local port; close it with the returned server.
func (s *Server) Start() *httptest.Server { return httptest.NewServer(s) }

// ServeHTTP routes the Google paths the client uses.
func (s *Server) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	switch {
	case request.URL.Path == "/o/oauth2/v2/auth" && request.Method == http.MethodGet:
		s.consentPage(response, request)
	case request.URL.Path == "/o/oauth2/v2/auth" && request.Method == http.MethodPost:
		s.decide(response, request)
	case request.URL.Path == "/token" && request.Method == http.MethodPost:
		s.token(response, request)
	case request.URL.Path == "/revoke" && request.Method == http.MethodPost:
		s.revoke(response, request)
	case request.URL.Path == "/upload/gmail/v1/users/me/messages/send" && request.Method == http.MethodPost:
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

// Consent plays the person on Google's consent screen: it reads an authorization URL and returns the
// redirect Google would send the browser to, approving as account (or denying when account is empty).
func (s *Server) Consent(authorizationURL, account string) (string, error) {
	parsed, err := url.Parse(authorizationURL)
	if err != nil {
		return "", err
	}
	return s.redirectFor(parsed.Query(), account)
}

func (s *Server) redirectFor(query url.Values, account string) (string, error) {
	if query.Get("client_id") != s.ClientID || query.Get("response_type") != "code" || query.Get("code_challenge_method") != "S256" ||
		query.Get("access_type") != "offline" || query.Get("prompt") != "consent" {
		return "", fmt.Errorf("authorization request is not what Google expects: %s", query.Encode())
	}
	redirect, err := url.Parse(query.Get("redirect_uri"))
	if err != nil || redirect.Scheme == "" {
		return "", fmt.Errorf("bad redirect_uri %q", query.Get("redirect_uri"))
	}
	answer := url.Values{"state": {query.Get("state")}}
	if account == "" {
		answer.Set("error", "access_denied")
	} else {
		code := randomToken("code-")
		scope := query.Get("scope")
		s.mu.Lock()
		if s.GrantScope != "" {
			scope = s.GrantScope
		}
		s.codes[code] = pendingCode{account: strings.ToLower(account), scope: scope, challenge: query.Get("code_challenge"), redirectURI: query.Get("redirect_uri")}
		s.mu.Unlock()
		answer.Set("code", code)
	}
	redirect.RawQuery = answer.Encode()
	return redirect.String(), nil
}

func (s *Server) consentPage(response http.ResponseWriter, request *http.Request) {
	var hidden strings.Builder
	for key, values := range request.URL.Query() {
		for _, value := range values {
			fmt.Fprintf(&hidden, `<input type="hidden" name="%s" value="%s">`, html.EscapeString(key), html.EscapeString(value))
		}
	}
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(response, `<!doctype html><meta charset="utf-8"><title>Google stand-in: consent</title>
<body style="font-family:system-ui;max-width:32rem;margin:3rem auto">
<h1>Google stand-in</h1><p>Local stand-in for Google's consent screen. <strong>%s</strong> asks to send email as you.</p>
<form method="post">%s<label>Google account <input name="account" type="email" value="support@example.com" required></label>
<p><button name="decision" value="allow">Allow</button> <button name="decision" value="deny" formnovalidate>Cancel</button></p></form>`,
		html.EscapeString(request.URL.Query().Get("client_id")), hidden.String())
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
	target, err := s.redirectFor(request.PostForm, account)
	if err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(response, request, target, http.StatusSeeOther)
}

func writeJSON(response http.ResponseWriter, status int, body any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(body)
}

func (s *Server) token(response http.ResponseWriter, request *http.Request) {
	if err := request.ParseForm(); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	form := request.PostForm
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens++
	if s.TokenDown {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{"error": "temporarily_unavailable"})
		return
	}
	if form.Get("client_id") != s.ClientID || form.Get("client_secret") != s.ClientSecret {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "invalid_client"})
		return
	}
	switch form.Get("grant_type") {
	case "authorization_code":
		pending, ok := s.codes[form.Get("code")]
		delete(s.codes, form.Get("code"))
		sum := sha256.Sum256([]byte(form.Get("code_verifier")))
		if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != pending.challenge || form.Get("redirect_uri") != pending.redirectURI {
			writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
			return
		}
		access := randomToken("access-")
		s.access[access] = pending.account
		answer := map[string]any{"access_token": access, "expires_in": 3599, "token_type": "Bearer", "scope": pending.scope,
			"id_token": s.idToken(pending.account)}
		if !s.OmitRefreshToken {
			refresh := randomToken("refresh-")
			s.refresh[refresh] = pending.account
			answer["refresh_token"] = refresh
		}
		writeJSON(response, http.StatusOK, answer)
	case "refresh_token":
		account, ok := s.refresh[form.Get("refresh_token")]
		if !ok || s.revoked[form.Get("refresh_token")] {
			writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid_grant", "error_description": "Token has been expired or revoked."})
			return
		}
		access := randomToken("access-")
		s.access[access] = account
		writeJSON(response, http.StatusOK, map[string]any{"access_token": access, "expires_in": 3599, "token_type": "Bearer", "scope": "openid email https://www.googleapis.com/auth/gmail.send"})
	default:
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "unsupported_grant_type"})
	}
}

// idToken is an unsigned JWT with Google's claims; the client reads it from a direct token answer.
func (s *Server) idToken(account string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	audience := s.ClientID
	if s.IDTokenAudience != "" {
		audience = s.IDTokenAudience
	}
	claims, _ := json.Marshal(map[string]any{"iss": "https://accounts.google.com", "aud": audience, "sub": "sub-" + account,
		"email": account, "email_verified": true})
	return header + "." + base64.RawURLEncoding.EncodeToString(claims) + ".stand-in"
}

func (s *Server) revoke(response http.ResponseWriter, request *http.Request) {
	_ = request.ParseForm()
	token := request.PostForm.Get("token")
	s.mu.Lock()
	defer s.mu.Unlock()
	account, ok := s.refresh[token]
	if !ok {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid_token"})
		return
	}
	// Like Google: revoking one token removes the account's whole grant to the project, so every refresh
	// token issued to that account stops working, not only the one revoked.
	for other, owner := range s.refresh {
		if owner == account {
			s.revoked[other] = true
		}
	}
	writeJSON(response, http.StatusOK, map[string]string{})
}

// AccountOf names the account a refresh token was issued to.
func (s *Server) AccountOf(token string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refresh[token]
}

// RevokeAll plays the person removing Colliedesk's access in their Google account.
func (s *Server) RevokeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for token := range s.refresh {
		s.revoked[token] = true
	}
}

func (s *Server) send(response http.ResponseWriter, request *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	account, ok := s.access[strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")]
	if !ok {
		writeJSON(response, http.StatusUnauthorized, map[string]any{"error": map[string]any{"code": 401, "message": "Request had invalid authentication credentials."}})
		return
	}
	if len(s.SendFailures) > 0 {
		failure := s.SendFailures[0]
		s.SendFailures = s.SendFailures[1:]
		writeJSON(response, failure.Status, map[string]any{"error": map[string]any{"code": failure.Status, "message": "scripted failure",
			"errors": []map[string]string{{"reason": failure.Reason}}}})
		return
	}
	if request.URL.Query().Get("uploadType") != "media" || !strings.HasPrefix(request.Header.Get("Content-Type"), "message/rfc822") {
		writeJSON(response, http.StatusBadRequest, map[string]any{"error": map[string]any{"code": 400, "message": "expected a media upload of message/rfc822"}})
		return
	}
	raw, _ := io.ReadAll(io.LimitReader(request.Body, 36<<20))
	parsed, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]any{"error": map[string]any{"code": 400, "message": "Invalid RFC 5322 message"}})
		return
	}
	messageID := strings.Trim(parsed.Header.Get("Message-ID"), "<>")
	if s.RewriteMessageID || messageID == "" {
		messageID = randomToken("gmail-") + "@mail.gmail.com"
	}
	s.sent = append(s.sent, Sent{Account: account, Raw: raw, MessageID: messageID})
	writeJSON(response, http.StatusOK, map[string]any{"id": randomToken(""), "threadId": randomToken(""), "labelIds": []string{"SENT"}})
}

// Messages returns the messages accepted so far.
func (s *Server) Messages() []Sent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Sent(nil), s.sent...)
}

// Revoked reports whether a refresh token was revoked.
func (s *Server) Revoked(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.revoked[token]
}

// Set changes the stand-in's scripted behaviour under its lock.
func (s *Server) Set(change func(*Server)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	change(s)
}

// RefreshTokens lists every refresh token issued (for tests that check revocation).
func (s *Server) RefreshTokens() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	tokens := make([]string, 0, len(s.refresh))
	for token := range s.refresh {
		tokens = append(tokens, token)
	}
	return tokens
}
