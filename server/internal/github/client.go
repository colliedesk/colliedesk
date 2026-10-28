package github

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultWebURL = "https://github.com"
	defaultAPIURL = "https://api.github.com"
	apiVersion    = "2022-11-28"
	// requestTimeout bounds one call to GitHub.
	requestTimeout = 20 * time.Second
	maxAnswerBytes = 8 << 20
	// maxPages bounds a paginated read (100 per page).
	maxPages = 20
	// tokenMargin renews an installation token this long before GitHub expires it (after one hour).
	tokenMargin = 10 * time.Minute
)

// Errors a caller acts on.
var (
	// ErrNotFound: GitHub does not show it to this App or person (missing, or not theirs to see).
	ErrNotFound = errors.New("github: not found or not visible")
	// ErrCodeRefused: the authorization code was wrong, used or expired (bad_verification_code), or the
	// PKCE verifier did not match.
	ErrCodeRefused = errors.New("github refused the authorization code")
	// ErrSuspended: the installation is suspended; GitHub gives it no token.
	ErrSuspended = errors.New("github: the installation is suspended")
	// ErrTooMany: a list longer than Colliedesk reads (maxPages of 100); never read as a shorter list.
	ErrTooMany = errors.New("github: the list is longer than Colliedesk reads")
)

// APIError is any other answer GitHub gave. The body is never kept: only the status and GitHub's
// message, cut.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	text := fmt.Sprintf("GitHub answered HTTP %d", e.Status)
	if e.Message != "" {
		text += ": " + e.Message
	}
	return text
}

// Temporary reports an answer worth trying again later: rate limits and server errors.
func (e *APIError) Temporary() bool {
	return e.Status == http.StatusTooManyRequests || e.Status >= 500 || e.Status == http.StatusForbidden && strings.Contains(strings.ToLower(e.Message), "rate limit")
}

// App is the Colliedesk GitHub App of this server: its JWT, its installation tokens (cached in memory,
// never stored) and the calls Autofix makes. Safe for concurrent use.
type App struct {
	config Config
	key    *rsa.PrivateKey
	now    func() time.Time

	mu     sync.Mutex
	tokens map[int64]installationToken
}

type installationToken struct {
	token   string
	expires time.Time
}

// NewApp builds the App from complete settings; a key that does not parse is an error.
func NewApp(config Config) (*App, error) {
	if !config.Enabled() {
		return nil, fmt.Errorf("github app settings missing: %s", strings.Join(config.Missing(), ", "))
	}
	key, err := config.SigningKey()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", EnvPrivateKey, err)
	}
	return &App{config: config, key: key, now: time.Now, tokens: map[int64]installationToken{}}, nil
}

// Config is the App's settings.
func (a *App) Config() Config { return a.config }

func (a *App) webURL(path string) string {
	if a.config.Base != "" {
		return strings.TrimRight(a.config.Base, "/") + path
	}
	return defaultWebURL + path
}

func (a *App) apiURL(path string) string {
	if a.config.Base != "" {
		return strings.TrimRight(a.config.Base, "/") + path
	}
	return defaultAPIURL + path
}

func (a *App) client() *http.Client {
	if a.config.HTTP != nil {
		return a.config.HTTP
	}
	return &http.Client{Timeout: requestTimeout}
}

// Challenge is the PKCE S256 challenge of a verifier.
func Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// InstallURL is where an admin's browser installs the App (choosing its repositories). With "Request
// user authorization (OAuth) during installation" on, GitHub sends the browser back to the App's
// callback URL with code, installation_id, setup_action and this state.
func (a *App) InstallURL(state string) string {
	return a.webURL("/apps/"+url.PathEscape(a.config.Slug)+"/installations/new") + "?" + url.Values{"state": {state}}.Encode()
}

// InstallationSettingsURL is the installation's page on GitHub, where its repositories are changed.
func InstallationSettingsURL(accountLogin, accountType string, installationID int64) string {
	id := strconv.FormatInt(installationID, 10)
	if accountType == "Organization" {
		return defaultWebURL + "/organizations/" + url.PathEscape(accountLogin) + "/settings/installations/" + id
	}
	return defaultWebURL + "/settings/installations/" + id
}

// AuthorizeURL is where a person's browser authorizes the App to act as them (PKCE S256).
func (a *App) AuthorizeURL(redirectURI, state, verifier string) string {
	query := url.Values{"client_id": {a.config.ClientID}, "redirect_uri": {redirectURI}, "state": {state},
		"code_challenge": {Challenge(verifier)}, "code_challenge_method": {"S256"}}
	return a.webURL("/login/oauth/authorize") + "?" + query.Encode()
}

// Exchange trades an authorization code for the person's user token (ghu_). verifier is empty for the
// authorization made during installation, which GitHub starts without PKCE. The token is the caller's to
// keep in memory and revoke; it is never stored.
func (a *App) Exchange(ctx context.Context, code, verifier, redirectURI string) (string, error) {
	form := url.Values{"client_id": {a.config.ClientID}, "client_secret": {a.config.ClientSecret}, "code": {code}, "redirect_uri": {redirectURI}}
	if verifier != "" {
		form.Set("code_verifier", verifier)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, a.webURL("/login/oauth/access_token"), strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := a.client().Do(request)
	if err != nil {
		return "", fmt.Errorf("github token exchange: %w", err)
	}
	defer response.Body.Close()
	var answer struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, maxAnswerBytes)).Decode(&answer); err != nil || response.StatusCode != http.StatusOK {
		return "", &APIError{Status: response.StatusCode}
	}
	// GitHub answers 200 with an error code; the code alone says enough (never its description).
	switch {
	case answer.Error == "bad_verification_code" || answer.Error == "invalid_grant":
		return "", ErrCodeRefused
	case answer.Error != "":
		return "", &APIError{Status: http.StatusBadRequest, Message: cut(answer.Error)}
	case answer.AccessToken == "":
		return "", errors.New("github token exchange answered without a token")
	}
	return answer.AccessToken, nil
}

// Revoke deletes a user token at GitHub (DELETE /applications/{client_id}/token): it stops working at
// once. A token already gone (404) is the state asked for.
func (a *App) Revoke(ctx context.Context, token string) error {
	body, _ := json.Marshal(map[string]string{"access_token": token})
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, a.apiURL("/applications/"+url.PathEscape(a.config.ClientID)+"/token"), bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.SetBasicAuth(a.config.ClientID, a.config.ClientSecret)
	request.Header.Set("Content-Type", "application/json")
	err = a.send(request, nil)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

// jwt is the App's own token (RS256, ten minutes at most, back-dated a minute for clock drift).
func (a *App) jwt() (string, error) {
	now := a.now()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, err := json.Marshal(map[string]any{"iat": now.Add(-time.Minute).Unix(), "exp": now.Add(9 * time.Minute).Unix(), "iss": a.config.AppID})
	if err != nil {
		return "", err
	}
	signed := header + "." + base64.RawURLEncoding.EncodeToString(claims)
	sum := sha256.Sum256([]byte(signed))
	signature, err := rsa.SignPKCS1v15(rand.Reader, a.key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signed + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

// Account is the user or organization an installation belongs to.
type Account struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

// Installation is one installation of the App.
type Installation struct {
	ID                  int64      `json:"id"`
	Account             Account    `json:"account"`
	RepositorySelection string     `json:"repository_selection"`
	SuspendedAt         *time.Time `json:"suspended_at"`
}

// UserInstallations lists the installations of the App the person behind userToken can reach. It is
// how Colliedesk knows an installation_id from a redirect is theirs: GitHub's redirect can be forged.
func (a *App) UserInstallations(ctx context.Context, userToken string) ([]Installation, error) {
	return paginate(func(page int) ([]Installation, int, error) {
		var answer struct {
			TotalCount    int            `json:"total_count"`
			Installations []Installation `json:"installations"`
		}
		err := a.get(ctx, "/user/installations?per_page=100&page="+strconv.Itoa(page), "Bearer "+userToken, &answer)
		return answer.Installations, answer.TotalCount, err
	})
}

// UserRepository is a repository of an installation the person reaches, and whether they administer it.
type UserRepository struct {
	ID          int64 `json:"id"`
	Permissions struct {
		Admin bool `json:"admin"`
	} `json:"permissions"`
}

// UserInstallationRepositories lists the repositories of an installation the person behind userToken
// reaches, with their permissions on each (only the ones they reach: compare with Repositories).
func (a *App) UserInstallationRepositories(ctx context.Context, userToken string, installationID int64) ([]UserRepository, error) {
	return paginate(func(page int) ([]UserRepository, int, error) {
		var answer struct {
			TotalCount   int              `json:"total_count"`
			Repositories []UserRepository `json:"repositories"`
		}
		err := a.get(ctx, "/user/installations/"+strconv.FormatInt(installationID, 10)+"/repositories?per_page=100&page="+strconv.Itoa(page),
			"Bearer "+userToken, &answer)
		return answer.Repositories, answer.TotalCount, err
	})
}

// paginate reads pages of 100 until total_count, up to maxPages: a longer list is ErrTooMany.
func paginate[T any](read func(page int) ([]T, int, error)) ([]T, error) {
	all := []T{}
	for page := 1; page <= maxPages; page++ {
		items, total, err := read(page)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if len(all) >= total || len(items) == 0 {
			if len(all) < total {
				return nil, fmt.Errorf("%w: GitHub counted %d, answered %d", ErrTooMany, total, len(all))
			}
			return all, nil
		}
	}
	return nil, ErrTooMany
}

// Installation reads one installation as the App; ErrNotFound when it was uninstalled.
func (a *App) Installation(ctx context.Context, id int64) (Installation, error) {
	token, err := a.jwt()
	if err != nil {
		return Installation{}, err
	}
	var installation Installation
	err = a.get(ctx, "/app/installations/"+strconv.FormatInt(id, 10), "Bearer "+token, &installation)
	return installation, err
}

// Uninstall removes the App from the installation's account. One already gone is the state asked for.
func (a *App) Uninstall(ctx context.Context, id int64) error {
	token, err := a.jwt()
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, a.apiURL("/app/installations/"+strconv.FormatInt(id, 10)), nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	err = a.send(request, nil)
	a.forget(id)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

// installationToken returns a token of the installation, from memory while it has ten minutes left.
func (a *App) installationToken(ctx context.Context, id int64) (string, error) {
	a.mu.Lock()
	cached, ok := a.tokens[id]
	a.mu.Unlock()
	if ok && a.now().Before(cached.expires.Add(-tokenMargin)) {
		return cached.token, nil
	}
	jwt, err := a.jwt()
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, a.apiURL("/app/installations/"+strconv.FormatInt(id, 10)+"/access_tokens"), nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+jwt)
	var answer struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := a.send(request, &answer); err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusForbidden && strings.Contains(strings.ToLower(apiErr.Message), "suspend") {
			return "", ErrSuspended
		}
		return "", err
	}
	if answer.Token == "" {
		return "", errors.New("github answered an installation token without a token")
	}
	a.mu.Lock()
	a.tokens[id] = installationToken{token: answer.Token, expires: answer.ExpiresAt}
	a.mu.Unlock()
	return answer.Token, nil
}

func (a *App) forget(id int64) {
	a.mu.Lock()
	delete(a.tokens, id)
	a.mu.Unlock()
}

// asInstallation makes a GET as the installation.
func (a *App) asInstallation(ctx context.Context, installationID int64, path string, into any) error {
	token, err := a.installationToken(ctx, installationID)
	if err != nil {
		return err
	}
	err = a.get(ctx, path, "Bearer "+token, into)
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusUnauthorized {
		// The token was revoked early (the installation was suspended or removed): forget it.
		a.forget(installationID)
	}
	return err
}

// Repository is a repository as Autofix needs it.
type Repository struct {
	ID            int64  `json:"id"`
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	Visibility    string `json:"visibility"`
	DefaultBranch string `json:"default_branch"`
}

// Kind is the repository's visibility: private, internal or public.
func (r Repository) Kind() string {
	switch {
	case r.Visibility == "internal":
		return "internal"
	case r.Private || r.Visibility == "private":
		return "private"
	}
	return "public"
}

// Repositories lists every repository the installation can reach.
func (a *App) Repositories(ctx context.Context, installationID int64) ([]Repository, error) {
	return paginate(func(page int) ([]Repository, int, error) {
		var answer struct {
			TotalCount   int          `json:"total_count"`
			Repositories []Repository `json:"repositories"`
		}
		err := a.asInstallation(ctx, installationID, "/installation/repositories?per_page=100&page="+strconv.Itoa(page), &answer)
		return answer.Repositories, answer.TotalCount, err
	})
}

// Repository reads one repository by its id as the installation, now (never cached): ErrNotFound when
// the installation cannot reach it.
func (a *App) Repository(ctx context.Context, installationID, repoID int64) (Repository, error) {
	var repo Repository
	err := a.asInstallation(ctx, installationID, "/repositories/"+strconv.FormatInt(repoID, 10), &repo)
	return repo, err
}

// Issue is the state of an issue.
type Issue struct {
	Number      int    `json:"number"`
	State       string `json:"state"`
	StateReason string `json:"state_reason"`
}

// Issue reads one issue of a repository as the installation.
func (a *App) Issue(ctx context.Context, installationID int64, fullName string, number int) (Issue, error) {
	var issue Issue
	err := a.asInstallation(ctx, installationID, "/repos/"+fullName+"/issues/"+strconv.Itoa(number), &issue)
	return issue, err
}

// PullRequest is the state of a pull request.
type PullRequest struct {
	Number         int    `json:"number"`
	HTMLURL        string `json:"html_url"`
	State          string `json:"state"`
	Merged         bool   `json:"merged"`
	MergeCommitSHA string `json:"merge_commit_sha"`
	MergedBy       *struct {
		Login string `json:"login"`
	} `json:"merged_by"`
	Head struct {
		Ref string `json:"ref"`
	} `json:"head"`
}

// MergedByLogin is who merged it, "" when unknown.
func (p PullRequest) MergedByLogin() string {
	if p.MergedBy == nil {
		return ""
	}
	return p.MergedBy.Login
}

// PullRequest reads one pull request of a repository as the installation.
func (a *App) PullRequest(ctx context.Context, installationID int64, fullName string, number int) (PullRequest, error) {
	var pull PullRequest
	err := a.asInstallation(ctx, installationID, "/repos/"+fullName+"/pulls/"+strconv.Itoa(number), &pull)
	return pull, err
}

// ClosingIssues are the issue numbers a pull request closes when merged ("Fixes #N" or linked by hand),
// in the same repository.
func (a *App) ClosingIssues(ctx context.Context, installationID int64, fullName string, number int) ([]int, error) {
	owner, name, _ := strings.Cut(fullName, "/")
	var answer struct {
		Repository *struct {
			PullRequest *struct {
				ClosingIssuesReferences struct {
					Nodes []struct {
						Number     int `json:"number"`
						Repository struct {
							NameWithOwner string `json:"nameWithOwner"`
						} `json:"repository"`
					} `json:"nodes"`
				} `json:"closingIssuesReferences"`
			} `json:"pullRequest"`
		} `json:"repository"`
	}
	query := `query($owner:String!,$name:String!,$number:Int!){repository(owner:$owner,name:$name){pullRequest(number:$number){` +
		`closingIssuesReferences(first:10){nodes{number repository{nameWithOwner}}}}}}`
	if err := a.graphql(ctx, installationID, query, map[string]any{"owner": owner, "name": name, "number": number}, &answer); err != nil {
		return nil, err
	}
	if answer.Repository == nil || answer.Repository.PullRequest == nil {
		return nil, ErrNotFound
	}
	var numbers []int
	for _, node := range answer.Repository.PullRequest.ClosingIssuesReferences.Nodes {
		if strings.EqualFold(node.Repository.NameWithOwner, fullName) {
			numbers = append(numbers, node.Number)
		}
	}
	return numbers, nil
}

// LinkedPullRequest is a pull request that closes (or closed) an issue.
type LinkedPullRequest struct {
	Number         int
	URL            string
	State          string // OPEN, CLOSED or MERGED
	MergeCommitSHA string
	MergedBy       string
	HeadRef        string
}

// IssuePullRequests are the pull requests of the same repository that close the issue, open, closed or
// merged: how a merge is found when its own webhook was missed.
func (a *App) IssuePullRequests(ctx context.Context, installationID int64, fullName string, number int) ([]LinkedPullRequest, error) {
	owner, name, _ := strings.Cut(fullName, "/")
	var answer struct {
		Repository *struct {
			Issue *struct {
				ClosedByPullRequestsReferences struct {
					Nodes []struct {
						Number      int    `json:"number"`
						URL         string `json:"url"`
						State       string `json:"state"`
						HeadRefName string `json:"headRefName"`
						MergeCommit *struct {
							OID string `json:"oid"`
						} `json:"mergeCommit"`
						MergedBy *struct {
							Login string `json:"login"`
						} `json:"mergedBy"`
						Repository struct {
							NameWithOwner string `json:"nameWithOwner"`
						} `json:"repository"`
					} `json:"nodes"`
				} `json:"closedByPullRequestsReferences"`
			} `json:"issue"`
		} `json:"repository"`
	}
	query := `query($owner:String!,$name:String!,$number:Int!){repository(owner:$owner,name:$name){issue(number:$number){` +
		`closedByPullRequestsReferences(first:10,includeClosedPrs:true){nodes{number url state headRefName mergeCommit{oid} mergedBy{login} repository{nameWithOwner}}}}}}`
	if err := a.graphql(ctx, installationID, query, map[string]any{"owner": owner, "name": name, "number": number}, &answer); err != nil {
		return nil, err
	}
	if answer.Repository == nil || answer.Repository.Issue == nil {
		return nil, ErrNotFound
	}
	var pulls []LinkedPullRequest
	for _, node := range answer.Repository.Issue.ClosedByPullRequestsReferences.Nodes {
		if !strings.EqualFold(node.Repository.NameWithOwner, fullName) {
			continue
		}
		pull := LinkedPullRequest{Number: node.Number, URL: node.URL, State: node.State, HeadRef: node.HeadRefName}
		if node.MergeCommit != nil {
			pull.MergeCommitSHA = node.MergeCommit.OID
		}
		if node.MergedBy != nil {
			pull.MergedBy = node.MergedBy.Login
		}
		pulls = append(pulls, pull)
	}
	return pulls, nil
}

func (a *App) graphql(ctx context.Context, installationID int64, query string, variables map[string]any, into any) error {
	token, err := a.installationToken(ctx, installationID)
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, a.apiURL("/graphql"), bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	var answer struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := a.send(request, &answer); err != nil {
		return err
	}
	if len(answer.Errors) > 0 {
		if answer.Errors[0].Type == "NOT_FOUND" {
			return ErrNotFound
		}
		return &APIError{Status: http.StatusOK, Message: cut(answer.Errors[0].Message)}
	}
	return json.Unmarshal(answer.Data, into)
}

func (a *App) get(ctx context.Context, path, authorization string, into any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, a.apiURL(path), nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", authorization)
	return a.send(request, into)
}

// send makes one REST call: 2xx decodes into into (when given), 404 is ErrNotFound, anything else an
// APIError with GitHub's message, cut.
func (a *App) send(request *http.Request, into any) error {
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", apiVersion)
	request.Header.Set("User-Agent", "Colliedesk")
	response, err := a.client().Do(request)
	if err != nil {
		return fmt.Errorf("github %s: %w", request.URL.Path, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxAnswerBytes))
	if err != nil {
		return fmt.Errorf("github %s: %w", request.URL.Path, err)
	}
	switch {
	case response.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case response.StatusCode < 200 || response.StatusCode > 299:
		var answer struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(body, &answer)
		return &APIError{Status: response.StatusCode, Message: cut(answer.Message)}
	case into == nil || len(body) == 0:
		return nil
	}
	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("github %s: unexpected answer: %w", request.URL.Path, err)
	}
	return nil
}

func cut(text string) string {
	if len(text) > 200 {
		return text[:200]
	}
	return text
}
