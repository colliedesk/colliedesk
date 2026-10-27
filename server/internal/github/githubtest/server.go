// Package githubtest is a local stand-in for the GitHub endpoints the Colliedesk GitHub App calls: the
// installation and authorization redirects, the token endpoint (with PKCE), token revocation, the App's
// JWT and installation tokens, /user/installations, installations and their repositories, issues, pull
// requests and the two GraphQL reads Autofix makes; and it signs the webhooks it delivers. Tests and the
// e2e build point github.Config.Base at it; no real GitHub account or credential is involved. Answers
// follow GitHub's documented shapes.
package githubtest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/github"
	"github.com/colliedesk/colliedesk/server/internal/github/githubsig"
)

// DefaultLogin is who the authorize page signs in as when the browser names nobody.
const DefaultLogin = "octocat"

// Repo is a repository of the stand-in.
type Repo struct {
	ID            int64
	FullName      string
	Visibility    string // private, internal or public
	DefaultBranch string
	// Owner is the account (installation account login) it belongs to.
	Owner string
}

// Installation is an installation of the App.
type Installation struct {
	ID          int64
	Account     string
	AccountType string // User or Organization
	// Selection is selected or all; with all, every repository of the account is reachable.
	Selection string
	// Repos are the selected repositories (ignored with all).
	Repos     []int64
	Suspended bool
	// Users are the logins that can reach the installation (its admins).
	Users []string
}

// Issue is an issue of a repository.
type Issue struct {
	Number      int
	State       string // open or closed
	StateReason string
	// ClosedBy are the pull requests (numbers, same repository) that close it.
	ClosedBy []int
}

// Pull is a pull request of a repository.
type Pull struct {
	Number   int
	State    string // open or closed
	Merged   bool
	MergeSHA string
	MergedBy string
	HeadRef  string
	// Closes are the issues it closes (Fixes #N).
	Closes []int
}

type pendingCode struct {
	login, challenge, redirectURI string
}

// Server is the stand-in. Use its methods to change it between calls.
type Server struct {
	AppID, Slug, ClientID, ClientSecret, WebhookSecret string
	// CallbackURL is the App's callback URL, where installing and authorizing send the browser back.
	CallbackURL string
	// URL is the stand-in's address once started.
	URL string

	key *rsa.PrivateKey

	mu            sync.Mutex
	installations map[int64]*Installation
	repos         map[int64]*Repo
	issues        map[int64]map[int]*Issue
	pulls         map[int64]map[int]*Pull
	codes         map[string]pendingCode
	userTokens    map[string]string // ghu_ token -> login
	revoked       map[string]bool
	appTokens     map[string]int64 // ghs_ token -> installation
	uninstalled   map[int64]bool
	// Down makes every API call answer 503 (GitHub unavailable).
	down bool
	// calls counts the requests per "METHOD path".
	calls map[string]int
}

// New makes a stand-in with its own App key.
func New() *Server {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return NewWithKey(key)
}

// NewWithKey makes a stand-in whose App signs with key (the e2e server is configured with the same one).
func NewWithKey(key *rsa.PrivateKey) *Server {
	return &Server{AppID: "4242", Slug: "colliedesk-test", ClientID: "Iv1.test", ClientSecret: "not-a-secret", WebhookSecret: "webhook-secret",
		key: key, installations: map[int64]*Installation{}, repos: map[int64]*Repo{}, issues: map[int64]map[int]*Issue{}, pulls: map[int64]map[int]*Pull{},
		codes: map[string]pendingCode{}, userTokens: map[string]string{}, revoked: map[string]bool{}, appTokens: map[string]int64{},
		uninstalled: map[int64]bool{}, calls: map[string]int{}}
}

// PrivateKeyPEM is the App's private key as GitHub hands it out (PKCS#1 PEM).
func (s *Server) PrivateKeyPEM() string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(s.key)}))
}

// Start serves the stand-in on a local port.
func (s *Server) Start() *httptest.Server {
	server := httptest.NewServer(s)
	s.URL = server.URL
	return server
}

// Config is the App's settings pointing at the stand-in.
func (s *Server) Config() github.Config {
	return github.Config{AppID: s.AppID, Slug: s.Slug, PrivateKey: s.PrivateKeyPEM(), ClientID: s.ClientID, ClientSecret: s.ClientSecret,
		WebhookSecret: s.WebhookSecret, Base: s.URL}
}

// AddInstallation adds (or replaces) an installation.
func (s *Server) AddInstallation(installation Installation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	copied := installation
	copied.Repos = append([]int64(nil), installation.Repos...)
	copied.Users = append([]string(nil), installation.Users...)
	s.installations[installation.ID] = &copied
	delete(s.uninstalled, installation.ID)
}

// AddRepo adds a repository and, with a non-zero installation of selected repositories, selects it there.
func (s *Server) AddRepo(installationID int64, repo Repo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if repo.DefaultBranch == "" {
		repo.DefaultBranch = "main"
	}
	if repo.Owner == "" {
		repo.Owner, _, _ = strings.Cut(repo.FullName, "/")
	}
	copied := repo
	s.repos[repo.ID] = &copied
	if installation := s.installations[installationID]; installation != nil {
		installation.Repos = append(installation.Repos, repo.ID)
	}
}

// Update changes an installation under the lock (selection, suspension, repositories).
func (s *Server) Update(id int64, change func(*Installation)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if installation := s.installations[id]; installation != nil {
		change(installation)
	}
}

// SetVisibility changes a repository's visibility.
func (s *Server) SetVisibility(repoID int64, visibility string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if repo := s.repos[repoID]; repo != nil {
		repo.Visibility = visibility
	}
}

// SetIssue adds or replaces an issue of a repository.
func (s *Server) SetIssue(repoID int64, issue Issue) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.issues[repoID] == nil {
		s.issues[repoID] = map[int]*Issue{}
	}
	copied := issue
	s.issues[repoID][issue.Number] = &copied
}

// SetPull adds or replaces a pull request of a repository.
func (s *Server) SetPull(repoID int64, pull Pull) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pulls[repoID] == nil {
		s.pulls[repoID] = map[int]*Pull{}
	}
	copied := pull
	s.pulls[repoID][pull.Number] = &copied
}

// SetDown makes every call answer 503 (true) or work again (false).
func (s *Server) SetDown(down bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.down = down
}

// Uninstalled reports whether the App removed the installation.
func (s *Server) Uninstalled(id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.uninstalled[id]
}

// UserTokens lists every user token the stand-in issued.
func (s *Server) UserTokens() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var tokens []string
	for token := range s.userTokens {
		tokens = append(tokens, token)
	}
	return tokens
}

// Revoked reports whether a user token was revoked.
func (s *Server) Revoked(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.revoked[token]
}

// Calls counts the calls of "METHOD path" (the path without its query).
func (s *Server) Calls(route string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[route]
}

// Install plays an admin (login) installing the App on installation id from Colliedesk's install
// address: it returns where GitHub sends the browser (the callback with code, installation_id,
// setup_action=install and the state).
func (s *Server) Install(installURL, login string, installationID int64) (string, error) {
	parsed, err := url.Parse(installURL)
	if err != nil {
		return "", err
	}
	if parsed.Path != "/apps/"+s.Slug+"/installations/new" {
		return "", fmt.Errorf("not the App's install page: %s", parsed.Path)
	}
	code := s.issueCode(pendingCode{login: login})
	query := url.Values{"code": {code}, "installation_id": {strconv.FormatInt(installationID, 10)}, "setup_action": {"install"}}
	if state := parsed.Query().Get("state"); state != "" {
		query.Set("state", state)
	}
	return s.CallbackURL + "?" + query.Encode(), nil
}

// Authorize plays login authorizing the App from Colliedesk's authorize address (PKCE S256): it returns
// where GitHub sends the browser.
func (s *Server) Authorize(authorizeURL, login string) (string, error) {
	parsed, err := url.Parse(authorizeURL)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	if parsed.Path != "/login/oauth/authorize" || query.Get("client_id") != s.ClientID {
		return "", fmt.Errorf("not the App's authorize page: %s", authorizeURL)
	}
	if query.Get("code_challenge_method") != "S256" || query.Get("code_challenge") == "" {
		return "", errors.New("authorize without PKCE S256")
	}
	redirect := query.Get("redirect_uri")
	if redirect == "" {
		redirect = s.CallbackURL
	}
	code := s.issueCode(pendingCode{login: login, challenge: query.Get("code_challenge"), redirectURI: redirect})
	return redirect + "?" + url.Values{"code": {code}, "state": {query.Get("state")}}.Encode(), nil
}

func (s *Server) issueCode(code pendingCode) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	value := random(10)
	s.codes[value] = code
	return value
}

// RevokedTokens counts the user tokens revoked so far.
func (s *Server) RevokedTokens() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.revoked)
}

// Webhook is a delivery as GitHub would send it: the body and its headers, signed with WebhookSecret.
func (s *Server) Webhook(event string, payload any) ([]byte, http.Header) {
	return s.SignedWebhook(event, random(16), payload)
}

// SignedWebhook is Webhook with a given delivery id (to replay one).
func (s *Server) SignedWebhook(event, delivery string, payload any) ([]byte, http.Header) {
	body, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	header := http.Header{}
	header.Set("Content-Type", "application/json")
	header.Set("X-GitHub-Event", event)
	header.Set("X-GitHub-Delivery", delivery)
	header.Set(githubsig.Header, githubsig.Sign(s.WebhookSecret, body))
	return body, header
}

func random(n int) string {
	buffer := make([]byte, n)
	if _, err := rand.Read(buffer); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buffer)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.calls[r.Method+" "+r.URL.Path]++
	down := s.down
	s.mu.Unlock()
	if down {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "Service unavailable"})
		return
	}
	path := r.URL.Path
	switch {
	case r.Method == http.MethodGet && path == "/apps/"+s.Slug+"/installations/new":
		s.installPage(w, r)
	case r.Method == http.MethodGet && path == "/login/oauth/authorize":
		s.authorizePage(w, r)
	case r.Method == http.MethodPost && path == "/login/oauth/access_token":
		s.token(w, r)
	case r.Method == http.MethodDelete && path == "/applications/"+s.ClientID+"/token":
		s.revoke(w, r)
	case r.Method == http.MethodGet && path == "/user/installations":
		s.userInstallations(w, r)
	case strings.HasPrefix(path, "/app/installations/"):
		s.app(w, r, strings.TrimPrefix(path, "/app/installations/"))
	case r.Method == http.MethodGet && path == "/installation/repositories":
		s.installationRepositories(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/repositories/"):
		s.repository(w, r, strings.TrimPrefix(path, "/repositories/"))
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/repos/"):
		s.repoItem(w, r, strings.TrimPrefix(path, "/repos/"))
	case r.Method == http.MethodPost && path == "/graphql":
		s.graphql(w, r)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
	}
}

// installPage is GitHub's install page for a browser: with ?as=<login>&installation_id=<id> it installs at
// once and redirects to the callback; without, it lists one link per installation and admin to choose.
func (s *Server) installPage(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	if id, err := strconv.ParseInt(query.Get("installation_id"), 10, 64); err == nil && query.Get("as") != "" {
		target, err := s.Install(r.URL.String(), query.Get("as"), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Redirect(w, r, target, http.StatusFound)
		return
	}
	s.mu.Lock()
	var links strings.Builder
	for _, installation := range s.installations {
		for _, login := range installation.Users {
			choice := url.Values{"state": {query.Get("state")}, "as": {login}, "installation_id": {strconv.FormatInt(installation.ID, 10)}}
			fmt.Fprintf(&links, `<li><a href="?%s">Install on %s as %s</a></li>`, html.EscapeString(choice.Encode()), html.EscapeString(installation.Account), html.EscapeString(login))
		}
	}
	s.mu.Unlock()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><title>Install %s</title><h1>Install %s</h1><ul>%s</ul>`, html.EscapeString(s.Slug), html.EscapeString(s.Slug), links.String())
}

// authorizePage is GitHub's authorize page for a browser: it authorizes ?as=<login> (DefaultLogin when
// the browser names nobody) and redirects back.
func (s *Server) authorizePage(w http.ResponseWriter, r *http.Request) {
	login := r.URL.Query().Get("as")
	if login == "" {
		login = DefaultLogin
	}
	target, err := s.Authorize(r.URL.String(), login)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	if r.Form.Get("client_id") != s.ClientID || r.Form.Get("client_secret") != s.ClientSecret {
		writeJSON(w, http.StatusOK, map[string]string{"error": "incorrect_client_credentials"})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	code, ok := s.codes[r.Form.Get("code")]
	delete(s.codes, r.Form.Get("code"))
	if !ok || (code.challenge != "" && github.Challenge(r.Form.Get("code_verifier")) != code.challenge) ||
		(code.redirectURI != "" && r.Form.Get("redirect_uri") != "" && r.Form.Get("redirect_uri") != code.redirectURI) {
		writeJSON(w, http.StatusOK, map[string]string{"error": "bad_verification_code"})
		return
	}
	token := "ghu_" + random(18)
	s.userTokens[token] = code.login
	writeJSON(w, http.StatusOK, map[string]string{"access_token": token, "token_type": "bearer", "scope": ""})
}

func (s *Server) revoke(w http.ResponseWriter, r *http.Request) {
	id, secret, ok := r.BasicAuth()
	if !ok || id != s.ClientID || secret != s.ClientSecret {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
		return
	}
	var body struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, known := s.userTokens[body.AccessToken]; !known || s.revoked[body.AccessToken] {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	s.revoked[body.AccessToken] = true
	w.WriteHeader(http.StatusNoContent)
}

// userOf is the login of a live user token, "" for none.
func (s *Server) userOf(r *http.Request) string {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if s.revoked[token] {
		return ""
	}
	return s.userTokens[token]
}

func (s *Server) installationJSON(installation *Installation) map[string]any {
	answer := map[string]any{"id": installation.ID, "account": map[string]any{"login": installation.Account, "type": installation.AccountType},
		"repository_selection": installation.Selection, "suspended_at": nil}
	if installation.Suspended {
		answer["suspended_at"] = time.Now().UTC().Format(time.RFC3339)
	}
	return answer
}

func (s *Server) userInstallations(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	login := s.userOf(r)
	if login == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
		return
	}
	items := []map[string]any{}
	for _, installation := range s.installations {
		for _, user := range installation.Users {
			if strings.EqualFold(user, login) {
				items = append(items, s.installationJSON(installation))
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"total_count": len(items), "installations": items})
}

// verifyJWT checks the App's JWT: RS256 with the App's key, issued by the App, not expired.
func (s *Server) verifyJWT(r *http.Request) bool {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(&s.key.PublicKey, crypto.SHA256, sum[:], signature) != nil {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var claims struct {
		Iss string `json:"iss"`
		Exp int64  `json:"exp"`
		Iat int64  `json:"iat"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Iss != s.AppID {
		return false
	}
	now := time.Now().Unix()
	return claims.Exp > now && claims.Exp-claims.Iat <= 600
}

func (s *Server) app(w http.ResponseWriter, r *http.Request, rest string) {
	if !s.verifyJWT(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "A JSON web token could not be decoded"})
		return
	}
	idText, action, _ := strings.Cut(rest, "/")
	id, err := strconv.ParseInt(idText, 10, 64)
	s.mu.Lock()
	defer s.mu.Unlock()
	installation := s.installations[id]
	if err != nil || installation == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	switch {
	case r.Method == http.MethodGet && action == "":
		writeJSON(w, http.StatusOK, s.installationJSON(installation))
	case r.Method == http.MethodDelete && action == "":
		delete(s.installations, id)
		s.uninstalled[id] = true
		for token, owner := range s.appTokens {
			if owner == id {
				delete(s.appTokens, token)
			}
		}
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && action == "access_tokens":
		if installation.Suspended {
			writeJSON(w, http.StatusForbidden, map[string]string{"message": "This installation has been suspended"})
			return
		}
		token := "ghs_" + random(18)
		s.appTokens[token] = id
		writeJSON(w, http.StatusCreated, map[string]any{"token": token, "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
	}
}

// installationOf is the live installation of an installation token, nil for none.
func (s *Server) installationOf(r *http.Request) *Installation {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	id, ok := s.appTokens[token]
	if !ok {
		return nil
	}
	installation := s.installations[id]
	if installation == nil || installation.Suspended {
		return nil
	}
	return installation
}

func (s *Server) reaches(installation *Installation, repo *Repo) bool {
	if repo == nil || !strings.EqualFold(repo.Owner, installation.Account) {
		return false
	}
	if installation.Selection == "all" {
		return true
	}
	for _, id := range installation.Repos {
		if id == repo.ID {
			return true
		}
	}
	return false
}

func repoJSON(repo *Repo) map[string]any {
	return map[string]any{"id": repo.ID, "full_name": repo.FullName, "name": repo.FullName[strings.Index(repo.FullName, "/")+1:],
		"private": repo.Visibility != "public", "visibility": repo.Visibility, "default_branch": repo.DefaultBranch}
}

func (s *Server) installationRepositories(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	installation := s.installationOf(r)
	if installation == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
		return
	}
	items := []map[string]any{}
	for _, repo := range s.repos {
		if s.reaches(installation, repo) {
			items = append(items, repoJSON(repo))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"total_count": len(items), "repositories": items})
}

func (s *Server) repository(w http.ResponseWriter, r *http.Request, idText string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	installation := s.installationOf(r)
	if installation == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
		return
	}
	id, _ := strconv.ParseInt(idText, 10, 64)
	repo := s.repos[id]
	if !s.reaches(installation, repo) {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	writeJSON(w, http.StatusOK, repoJSON(repo))
}

// repoByName finds a repository the installation reaches by its full name.
func (s *Server) repoByName(installation *Installation, fullName string) *Repo {
	for _, repo := range s.repos {
		if strings.EqualFold(repo.FullName, fullName) && s.reaches(installation, repo) {
			return repo
		}
	}
	return nil
}

func (s *Server) repoItem(w http.ResponseWriter, r *http.Request, rest string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	installation := s.installationOf(r)
	if installation == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
		return
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 4 {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	repo := s.repoByName(installation, parts[0]+"/"+parts[1])
	number, _ := strconv.Atoi(parts[3])
	if repo == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	switch parts[2] {
	case "issues":
		if issue := s.issues[repo.ID][number]; issue != nil {
			writeJSON(w, http.StatusOK, map[string]any{"number": issue.Number, "state": issue.State, "state_reason": nullable(issue.StateReason)})
			return
		}
	case "pulls":
		if pull := s.pulls[repo.ID][number]; pull != nil {
			writeJSON(w, http.StatusOK, pullJSON(repo, pull))
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
}

func nullable(text string) any {
	if text == "" {
		return nil
	}
	return text
}

// PullURL is a pull request's address on GitHub.
func PullURL(fullName string, number int) string {
	return "https://github.com/" + fullName + "/pull/" + strconv.Itoa(number)
}

func pullJSON(repo *Repo, pull *Pull) map[string]any {
	answer := map[string]any{"number": pull.Number, "html_url": PullURL(repo.FullName, pull.Number), "state": pull.State, "merged": pull.Merged,
		"merge_commit_sha": nullable(pull.MergeSHA), "merged_by": nil, "head": map[string]any{"ref": pull.HeadRef}}
	if pull.MergedBy != "" {
		answer["merged_by"] = map[string]any{"login": pull.MergedBy}
	}
	return answer
}

// PullPayload is the pull_request object of a webhook for this pull request.
func (s *Server) PullPayload(repoID int64, number int) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	repo, pull := s.repos[repoID], s.pulls[repoID][number]
	if repo == nil || pull == nil {
		return nil
	}
	return pullJSON(repo, pull)
}

// RepoPayload is the repository object of a webhook for this repository.
func (s *Server) RepoPayload(repoID int64) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if repo := s.repos[repoID]; repo != nil {
		return repoJSON(repo)
	}
	return nil
}

func (s *Server) graphql(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	installation := s.installationOf(r)
	if installation == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
		return
	}
	var call struct {
		Query     string `json:"query"`
		Variables struct {
			Owner  string `json:"owner"`
			Name   string `json:"name"`
			Number int    `json:"number"`
		} `json:"variables"`
	}
	body, _ := io.ReadAll(r.Body)
	if json.Unmarshal(body, &call) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "Problems parsing JSON"})
		return
	}
	repo := s.repoByName(installation, call.Variables.Owner+"/"+call.Variables.Name)
	notFound := map[string]any{"data": map[string]any{"repository": nil}, "errors": []map[string]any{{"type": "NOT_FOUND", "message": "Could not resolve to a Repository"}}}
	if repo == nil {
		writeJSON(w, http.StatusOK, notFound)
		return
	}
	reference := map[string]any{"nameWithOwner": repo.FullName}
	switch {
	case strings.Contains(call.Query, "closingIssuesReferences"):
		pull := s.pulls[repo.ID][call.Variables.Number]
		if pull == nil {
			writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": nil}},
				"errors": []map[string]any{{"type": "NOT_FOUND", "message": "Could not resolve to a PullRequest"}}})
			return
		}
		nodes := []map[string]any{}
		for _, number := range pull.Closes {
			nodes = append(nodes, map[string]any{"number": number, "repository": reference})
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": map[string]any{
			"closingIssuesReferences": map[string]any{"nodes": nodes}}}}})
	case strings.Contains(call.Query, "closedByPullRequestsReferences"):
		issue := s.issues[repo.ID][call.Variables.Number]
		if issue == nil {
			writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"repository": map[string]any{"issue": nil}},
				"errors": []map[string]any{{"type": "NOT_FOUND", "message": "Could not resolve to an Issue"}}})
			return
		}
		nodes := []map[string]any{}
		for _, number := range issue.ClosedBy {
			pull := s.pulls[repo.ID][number]
			if pull == nil {
				continue
			}
			state := "OPEN"
			switch {
			case pull.Merged:
				state = "MERGED"
			case pull.State == "closed":
				state = "CLOSED"
			}
			node := map[string]any{"number": number, "url": PullURL(repo.FullName, number), "state": state, "headRefName": pull.HeadRef,
				"mergeCommit": nil, "mergedBy": nil, "repository": reference}
			if pull.MergeSHA != "" {
				node["mergeCommit"] = map[string]any{"oid": pull.MergeSHA}
			}
			if pull.MergedBy != "" {
				node["mergedBy"] = map[string]any{"login": pull.MergedBy}
			}
			nodes = append(nodes, node)
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"repository": map[string]any{"issue": map[string]any{
			"closedByPullRequestsReferences": map[string]any{"nodes": nodes}}}}})
	default:
		writeJSON(w, http.StatusOK, map[string]any{"errors": []map[string]any{{"message": "the stand-in does not know this query"}}})
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
