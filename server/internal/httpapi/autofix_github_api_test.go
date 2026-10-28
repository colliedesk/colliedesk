package httpapi_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/autofix"
	"github.com/colliedesk/colliedesk/server/internal/github/githubsig"
	"github.com/colliedesk/colliedesk/server/internal/github/githubtest"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

const autofixCallback = testOrigin + "/api/v1/autofix/github/callback"

// githubDesk is a server whose GitHub App is the stand-in.
type githubDesk struct {
	api     http.Handler
	server  *httpapi.Server
	service *auth.Service
	pool    *pgxpool.Pool
	fake    *githubtest.Server
}

func newGitHubDesk(t *testing.T) githubDesk {
	t.Helper()
	fake := githubtest.New()
	server := fake.Start()
	t.Cleanup(server.Close)
	fake.CallbackURL = autofixCallback
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.GitHub = fake.Config() })
	return githubDesk{api: api, server: api.(*httpapi.Server), service: service, pool: pool, fake: fake}
}

// workspace is one workspace of the desk: its admin's session and id.
type githubWorkspace struct {
	admin, id string
}

func (d githubDesk) workspace(t *testing.T, name string) githubWorkspace {
	t.Helper()
	_, pairing, err := d.service.CreateWorkspace(context.Background(), name, "admin@"+slugOf(name)+".example.com")
	if err != nil {
		t.Fatal(err)
	}
	response := checked(t, d.api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, pairing)}, "", nil)
	var session struct {
		Token string `json:"token"`
	}
	decode(t, response, &session)
	return githubWorkspace{admin: session.Token, id: workspaceID(t, d.api, session.Token)}
}

// startInstall asks for the install address as the session's person.
func (d githubDesk) startInstall(t *testing.T, session string) string {
	t.Helper()
	response := checked(t, d.api, http.MethodPost, "/api/v1/autofix/github/installations", nil, session, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("start install status=%d body=%s", response.Code, response.Body.String())
	}
	validateContractResponse(t, newRequest(t, http.MethodPost, "/api/v1/autofix/github/installations", nil, session, nil), response)
	var body struct {
		AuthorizeURL string `json:"authorize_url"`
	}
	decode(t, response, &body)
	return body.AuthorizeURL
}

// install plays login installing the App as installation id from the address, and GitHub's redirect of
// the browser holding cookie; it returns where Colliedesk sends the browser.
func (d githubDesk) install(t *testing.T, address, login string, installationID int64, cookie *http.Cookie) string {
	t.Helper()
	target, err := d.fake.Install(address, login, installationID)
	if err != nil {
		t.Fatal(err)
	}
	return d.callback(t, strings.TrimPrefix(target, testOrigin), cookie)
}

func (d githubDesk) callback(t *testing.T, path string, cookie *http.Cookie) string {
	t.Helper()
	response := checked(t, d.api, http.MethodGet, path, nil, "", cookie)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("callback status=%d body=%s, want 303", response.Code, response.Body.String())
	}
	return response.Header().Get("Location")
}

func (d githubDesk) settings(t *testing.T, session string) autofix.Settings {
	t.Helper()
	response := checked(t, d.api, http.MethodGet, "/api/v1/autofix/settings", nil, session, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("settings status=%d body=%s", response.Code, response.Body.String())
	}
	validateContractResponse(t, newRequest(t, http.MethodGet, "/api/v1/autofix/settings", nil, session, nil), response)
	var settings autofix.Settings
	decode(t, response, &settings)
	return settings
}

// connected installs the App for the workspace (installation id, account acme-<id>) and returns its id in
// Colliedesk.
func (d githubDesk) connected(t *testing.T, w githubWorkspace, installationID int64, selection string) string {
	t.Helper()
	d.fake.AddInstallation(githubtest.Installation{ID: installationID, Account: account(installationID), AccountType: "Organization", Selection: selection, Users: []string{"ada"}})
	// Connecting proves the person administers every repository the installation reaches: it has one.
	d.fake.AddRepo(installationID, githubtest.Repo{ID: seedRepo(installationID), FullName: account(installationID) + "/seed", Visibility: "private"})
	if location := d.install(t, d.startInstall(t, w.admin), "ada", installationID, sessionCookieOf(w.admin)); location != "/settings/autofix?github=connected" {
		t.Fatalf("install: Location = %s", location)
	}
	for _, installation := range d.settings(t, w.admin).Installations {
		if installation.InstallationID == installationID {
			return installation.ID
		}
	}
	t.Fatal("the installation is not in the settings")
	return ""
}

// seedRepo is the repository every installation of the tests starts with.
func seedRepo(installationID int64) int64 { return 900000 + installationID }

func account(installationID int64) string { return "acme-" + itoa(installationID) }

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func (d githubDesk) connectRepo(t *testing.T, session, installation string, githubRepoID int64) *httptest.ResponseRecorder {
	t.Helper()
	return checked(t, d.api, http.MethodPost, "/api/v1/autofix/repos", map[string]any{"installation_id": installation, "github_repo_id": githubRepoID}, session, nil)
}

// webhook delivers a signed event from the stand-in and returns the answer.
func (d githubDesk) webhook(t *testing.T, event string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	body, header := d.fake.Webhook(event, payload)
	return d.deliver(t, body, header)
}

// deliver posts a delivery and checks the answer against the contract.
func (d githubDesk) deliver(t *testing.T, body []byte, header http.Header) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(body))
	req.Header = header
	response := httptest.NewRecorder()
	d.api.ServeHTTP(response, req)
	validateContractResponse(t, httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(body)), copyOf(response))
	return response
}

// checked is request with its answer checked against the contract, errors included.
func checked(t *testing.T, api http.Handler, method, path string, body any, token string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	response := request(t, api, method, path, body, token, cookie)
	validateContractResponse(t, newRequest(t, method, path, body, token, cookie), copyOf(response))
	return response
}

// copyOf is a recorder with the same answer, so checking one leaves the other's body to read.
func copyOf(response *httptest.ResponseRecorder) *httptest.ResponseRecorder {
	copied := httptest.NewRecorder()
	for key, values := range response.Header() {
		copied.Header()[key] = values
	}
	copied.WriteHeader(response.Code)
	copied.Body.Write(response.Body.Bytes())
	return copied
}

func webhookStatus(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("webhook status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Status string `json:"status"`
	}
	decode(t, response, &body)
	return body.Status
}

func installationOf(id int64) map[string]any {
	return map[string]any{"id": id, "repository_selection": "selected"}
}

func TestAnAdminInstallsTheAppAndConnectsOnlyAPrivateRepository(t *testing.T) {
	d := newGitHubDesk(t)
	w := d.workspace(t, "GitHub Desk")
	before := d.settings(t, w.admin)
	if !before.Available || before.AppSlug == nil || *before.AppSlug != d.fake.Slug || len(before.Installations) != 0 || len(before.Connectors) != 3 ||
		before.ConsentVersion != autofix.ConsentVersion {
		t.Fatalf("before = %+v", before)
	}
	installation := d.connected(t, w, 11, "selected")
	d.fake.AddRepo(11, githubtest.Repo{ID: 501, FullName: "acme-11/shop-web", Visibility: "private"})
	d.fake.AddRepo(11, githubtest.Repo{ID: 502, FullName: "acme-11/docs", Visibility: "public"})
	d.fake.AddRepo(11, githubtest.Repo{ID: 503, FullName: "acme-11/intranet", Visibility: "internal"})

	settings := d.settings(t, w.admin)
	if len(settings.Installations) != 1 || settings.Installations[0].Status != autofix.InstallationActive || settings.Installations[0].AccountLogin != "acme-11" ||
		!strings.HasSuffix(settings.Installations[0].SettingsURL, "/organizations/acme-11/settings/installations/11") {
		t.Fatalf("installations = %+v", settings.Installations)
	}
	// The person's token proved the installation and was revoked; it is stored nowhere.
	tokens := d.fake.UserTokens()
	if len(tokens) != 1 || !d.fake.Revoked(tokens[0]) {
		t.Fatalf("user tokens = %v, want one, revoked", tokens)
	}
	for _, table := range []string{"autofix_github_installations", "autofix_repos", "autofix_oauth_states", "agent_requests", "sessions"} {
		var found bool
		if err := d.pool.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM `+table+` t WHERE position($1 in t::text) > 0)`, tokens[0]).Scan(&found); err != nil || found {
			t.Fatalf("the user token is in %s (or the query failed: %v)", table, err)
		}
	}

	path := "/api/v1/autofix/github/installations/" + installation + "/repositories"
	listed := checked(t, d.api, http.MethodGet, path, nil, w.admin, nil)
	validateContractResponse(t, newRequest(t, http.MethodGet, path, nil, w.admin, nil), listed)
	var repos struct {
		Items []autofix.GitHubRepository `json:"items"`
	}
	decode(t, listed, &repos)
	if len(repos.Items) != 4 {
		t.Fatalf("repositories = %+v", repos.Items)
	}
	for _, id := range []int64{502, 503} {
		if response := d.connectRepo(t, w.admin, installation, id); response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "repo_not_private") {
			t.Fatalf("connect %d: %d %s", id, response.Code, response.Body.String())
		}
	}
	// A repository of another account is not the installation's to reach.
	d.fake.AddInstallation(githubtest.Installation{ID: 12, Account: "other", AccountType: "User", Selection: "selected"})
	d.fake.AddRepo(12, githubtest.Repo{ID: 601, FullName: "other/secret", Visibility: "private"})
	if response := d.connectRepo(t, w.admin, installation, 601); response.Code != http.StatusNotFound {
		t.Fatalf("connect another account's repository: %d %s", response.Code, response.Body.String())
	}
	connected := d.connectRepo(t, w.admin, installation, 501)
	if connected.Code != http.StatusCreated {
		t.Fatalf("connect: %d %s", connected.Code, connected.Body.String())
	}
	validateContractResponse(t, newRequest(t, http.MethodPost, "/api/v1/autofix/repos", map[string]any{"installation_id": installation, "github_repo_id": 501}, w.admin, nil), connected)
	var repo autofix.Repo
	decode(t, connected, &repo)
	if repo.FullName != "acme-11/shop-web" || repo.Visibility != "private" || repo.IncludeNotes || repo.PausedReason != nil || repo.InstallationID == nil || *repo.InstallationID != 11 {
		t.Fatalf("repo = %+v", repo)
	}

	patched := checked(t, d.api, http.MethodPatch, "/api/v1/autofix/repos/"+repo.ID, map[string]any{"default_connector": "claude_issue"}, w.admin, nil)
	validateContractResponse(t, newRequest(t, http.MethodPatch, "/api/v1/autofix/repos/"+repo.ID, map[string]any{"default_connector": "claude_issue"}, w.admin, nil), patched)
	decode(t, patched, &repo)
	if repo.DefaultConnector == nil || *repo.DefaultConnector != autofix.ClaudeIssue {
		t.Fatalf("default connector = %v", repo.DefaultConnector)
	}
	if response := checked(t, d.api, http.MethodPatch, "/api/v1/autofix/repos/"+repo.ID, map[string]any{"default_connector": "codex"}, w.admin, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("unknown connector: %d", response.Code)
	}
	if response := checked(t, d.api, http.MethodPatch, "/api/v1/autofix/repos/"+repo.ID, map[string]any{"default_connector": nil}, w.admin, nil); response.Code != http.StatusOK {
		t.Fatalf("no connector: %d %s", response.Code, response.Body.String())
	}
	notes := checked(t, d.api, http.MethodPut, "/api/v1/autofix/repos/"+repo.ID+"/include-notes", nil, w.admin, nil)
	validateContractResponse(t, newRequest(t, http.MethodPut, "/api/v1/autofix/repos/"+repo.ID+"/include-notes", nil, w.admin, nil), notes)
	decode(t, notes, &repo)
	if !repo.IncludeNotes {
		t.Fatal("notes not included")
	}
	decode(t, checked(t, d.api, http.MethodDelete, "/api/v1/autofix/repos/"+repo.ID+"/include-notes", nil, w.admin, nil), &repo)
	if repo.IncludeNotes {
		t.Fatal("notes still included")
	}
	decode(t, checked(t, d.api, http.MethodGet, path, nil, w.admin, nil), &repos)
	for _, item := range repos.Items {
		if (item.GitHubRepoID == 501) != (item.ConnectedRepoID != nil && *item.ConnectedRepoID == repo.ID) {
			t.Fatalf("connected_repo_id of %d = %v", item.GitHubRepoID, item.ConnectedRepoID)
		}
	}
	if response := checked(t, d.api, http.MethodDelete, "/api/v1/autofix/repos/"+repo.ID, nil, w.admin, nil); response.Code != http.StatusNoContent {
		t.Fatalf("disconnect: %d %s", response.Code, response.Body.String())
	}
	if after := d.settings(t, w.admin); len(after.Repos) != 0 {
		t.Fatalf("repos after disconnect = %+v", after.Repos)
	}
}

// GitHub's redirect names the installation, and anyone can type another one: only an installation the
// person's own GitHub authorization reaches joins the workspace, and one installation joins one workspace.
func TestOnlyThePersonsOwnInstallationJoinsTheWorkspaceItStartedIn(t *testing.T) {
	d := newGitHubDesk(t)
	a := d.workspace(t, "Install A")
	b := d.workspace(t, "Install B")
	d.fake.AddInstallation(githubtest.Installation{ID: 21, Account: "acme", AccountType: "Organization", Selection: "selected", Users: []string{"ada"}})
	d.fake.AddInstallation(githubtest.Installation{ID: 22, Account: "victim", AccountType: "Organization", Selection: "selected", Users: []string{"vic"}})
	d.fake.AddRepo(21, githubtest.Repo{ID: 2101, FullName: "acme/app", Visibility: "private"})
	d.fake.AddRepo(22, githubtest.Repo{ID: 2201, FullName: "victim/app", Visibility: "private"})

	if location := d.install(t, d.startInstall(t, a.admin), "ada", 22, sessionCookieOf(a.admin)); location != "/settings/autofix?github_error=installation_not_yours" {
		t.Fatalf("a forged installation_id: Location = %s", location)
	}
	for _, token := range d.fake.UserTokens() {
		if !d.fake.Revoked(token) {
			t.Fatal("the token of a refused installation is still alive")
		}
	}
	if len(d.settings(t, a.admin).Installations) != 0 {
		t.Fatal("a forged installation joined")
	}
	address := d.startInstall(t, a.admin)
	if location := d.install(t, address, "ada", 21, nil); location != "/settings/autofix?github_error=signed_out" {
		t.Fatalf("no session: Location = %s", location)
	}
	// B's admin finishing A's start: the state is looked up in B's workspace only.
	if location := d.install(t, address, "ada", 21, sessionCookieOf(b.admin)); location != "/settings/autofix?github_error=expired" {
		t.Fatalf("another workspace's session: Location = %s", location)
	}
	second := joinWithRole(t, d.api, a.admin, "second@install-a.example.com", "admin")
	if location := d.install(t, address, "ada", 21, sessionCookieOf(second)); location != "/settings/autofix?github_error=wrong_person" {
		t.Fatalf("another admin of the workspace: Location = %s", location)
	}
	target, err := d.fake.Install(d.startInstall(t, a.admin), "ada", 21)
	if err != nil {
		t.Fatal(err)
	}
	if location := d.callback(t, strings.TrimPrefix(target, testOrigin), sessionCookieOf(a.admin)); location != "/settings/autofix?github=connected" {
		t.Fatalf("install: Location = %s", location)
	}
	if location := d.callback(t, strings.TrimPrefix(target, testOrigin), sessionCookieOf(a.admin)); location != "/settings/autofix?github_error=expired" {
		t.Fatalf("a replayed redirect: Location = %s", location)
	}
	// B's admin is also an admin of installation 21 on GitHub: it stays A's.
	d.fake.Update(21, func(i *githubtest.Installation) { i.Users = append(i.Users, "bo") })
	if location := d.install(t, d.startInstall(t, b.admin), "bo", 21, sessionCookieOf(b.admin)); location != "/settings/autofix?github_error=installation_in_use" {
		t.Fatalf("an installation of another workspace: Location = %s", location)
	}
	if len(d.settings(t, b.admin).Installations) != 0 || len(d.settings(t, a.admin).Installations) != 1 {
		t.Fatal("the installation moved")
	}
	// An agent (not admin) cannot start, nor finish one.
	agent := joinWithRole(t, d.api, a.admin, "agent@install-a.example.com", "agent")
	if response := checked(t, d.api, http.MethodPost, "/api/v1/autofix/github/installations", nil, agent, nil); response.Code != http.StatusForbidden {
		t.Fatalf("an agent starts installing: %d", response.Code)
	}
	if location := d.callback(t, "/api/v1/autofix/github/callback?setup_action=request", sessionCookieOf(a.admin)); location != "/settings/autofix?github=requested" {
		t.Fatalf("an installation requested of the organization's owners: Location = %s", location)
	}
}

func TestNoWorkspaceReachesAnotherWorkspacesInstallationOrRepository(t *testing.T) {
	d := newGitHubDesk(t)
	a := d.workspace(t, "Cross A")
	b := d.workspace(t, "Cross B")
	installation := d.connected(t, a, 31, "selected")
	d.fake.AddRepo(31, githubtest.Repo{ID: 701, FullName: "acme-31/app", Visibility: "private"})
	var repo autofix.Repo
	decode(t, d.connectRepo(t, a.admin, installation, 701), &repo)

	for _, c := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/v1/autofix/github/installations/" + installation + "/repositories", nil},
		{http.MethodDelete, "/api/v1/autofix/github/installations/" + installation, nil},
		{http.MethodPost, "/api/v1/autofix/repos", map[string]any{"installation_id": installation, "github_repo_id": 701}},
		{http.MethodPatch, "/api/v1/autofix/repos/" + repo.ID, map[string]any{"default_connector": "cursor_api"}},
		{http.MethodPut, "/api/v1/autofix/repos/" + repo.ID + "/include-notes", nil},
		{http.MethodDelete, "/api/v1/autofix/repos/" + repo.ID + "/include-notes", nil},
		{http.MethodDelete, "/api/v1/autofix/repos/" + repo.ID, nil},
		{http.MethodDelete, "/api/v1/autofix/repos/not-a-uuid", nil},
	} {
		if response := checked(t, d.api, c.method, c.path, c.body, b.admin, nil); response.Code != http.StatusNotFound {
			t.Errorf("B %s %s: %d %s, want 404", c.method, c.path, response.Code, response.Body.String())
		}
	}
	if settings := d.settings(t, b.admin); len(settings.Installations) != 0 || len(settings.Repos) != 0 {
		t.Fatalf("B sees %+v", settings)
	}
	if settings := d.settings(t, a.admin); len(settings.Repos) != 1 || settings.Repos[0].IncludeNotes || d.fake.Uninstalled(31) {
		t.Fatalf("A's settings changed: %+v", settings)
	}
}

// GitHub cannot make an admin pick "Only select repositories": an installation reaching every repository
// connects, blocked, and unblocks by itself when narrowed on GitHub.
func TestAnInstallationOfEveryRepositoryIsBlockedUntilNarrowed(t *testing.T) {
	d := newGitHubDesk(t)
	w := d.workspace(t, "All Repos")
	installation := d.connected(t, w, 41, "all")
	d.fake.AddRepo(0, githubtest.Repo{ID: 801, FullName: "acme-41/app", Visibility: "private"})
	if status := d.settings(t, w.admin).Installations[0].Status; status != autofix.InstallationBlocked {
		t.Fatalf("status = %s", status)
	}
	if response := d.connectRepo(t, w.admin, installation, 801); response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "installation_blocked") {
		t.Fatalf("connect while blocked: %d %s", response.Code, response.Body.String())
	}
	d.fake.Update(41, func(i *githubtest.Installation) { i.Selection, i.Repos = "selected", []int64{801} })
	status := webhookStatus(t, d.webhook(t, "installation_repositories", map[string]any{"action": "added", "installation": installationOf(41),
		"repository_selection": "selected", "repositories_added": []any{map[string]any{"id": 801, "full_name": "acme-41/app", "private": true}}, "repositories_removed": []any{}}))
	if status != "applied" || d.settings(t, w.admin).Installations[0].Status != autofix.InstallationActive {
		t.Fatalf("narrowed: %s %+v", status, d.settings(t, w.admin).Installations)
	}
	if response := d.connectRepo(t, w.admin, installation, 801); response.Code != http.StatusCreated {
		t.Fatalf("connect once narrowed: %d %s", response.Code, response.Body.String())
	}
}

func TestTheWebhookTakesOnlySignedNewDeliveriesOfItsOwnWorkspace(t *testing.T) {
	d := newGitHubDesk(t)
	a := d.workspace(t, "Hook A")
	b := d.workspace(t, "Hook B")
	installationA := d.connected(t, a, 51, "selected")
	installationB := d.connected(t, b, 52, "selected")
	d.fake.AddRepo(51, githubtest.Repo{ID: 901, FullName: "acme-51/app", Visibility: "private"})
	d.fake.AddRepo(52, githubtest.Repo{ID: 902, FullName: "acme-52/app", Visibility: "private"})
	d.connectRepo(t, a.admin, installationA, 901)
	d.connectRepo(t, b.admin, installationB, 902)

	payload := map[string]any{"action": "publicized", "installation": installationOf(52), "repository": map[string]any{"id": 902, "full_name": "acme-52/app", "private": false, "visibility": "public"}}
	body, header := d.fake.Webhook("repository", payload)
	for name, change := range map[string]func(http.Header){
		"no signature":    func(h http.Header) { h.Del("X-Hub-Signature-256") },
		"wrong signature": func(h http.Header) { h.Set("X-Hub-Signature-256", "sha256="+strings.Repeat("0", 64)) },
	} {
		forged := header.Clone()
		change(forged)
		if response := d.deliver(t, body, forged); response.Code != http.StatusUnauthorized {
			t.Errorf("%s: %d", name, response.Code)
		}
	}
	if response := d.deliver(t, append(bytes.Clone(body), ' '), header); response.Code != http.StatusUnauthorized {
		t.Errorf("a changed body: %d", response.Code)
	}
	big, bigHeader := d.fake.Webhook("repository", map[string]any{"padding": strings.Repeat("x", 5<<20)})
	if response := d.deliver(t, big, bigHeader); response.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("a 5 MB body: %d", response.Code)
	}
	if repo := d.settings(t, b.admin).Repos[0]; repo.PausedReason != nil {
		t.Fatalf("an unsigned delivery paused B's repository: %+v", repo)
	}

	// A's installation naming B's repository changes nothing anywhere.
	cross := map[string]any{"action": "publicized", "installation": installationOf(51), "repository": map[string]any{"id": 902, "full_name": "acme-52/app", "private": false, "visibility": "public"}}
	if status := webhookStatus(t, d.webhook(t, "repository", cross)); status != "ignored" {
		t.Fatalf("A's installation with B's repository: %s", status)
	}
	if repo := d.settings(t, b.admin).Repos[0]; repo.PausedReason != nil || repo.Visibility != "private" {
		t.Fatalf("B's repository moved: %+v", repo)
	}

	if status := webhookStatus(t, d.deliver(t, body, header)); status != "applied" {
		t.Fatalf("signed delivery: %s", status)
	}
	repo := d.settings(t, b.admin).Repos[0]
	if repo.PausedReason == nil || *repo.PausedReason != autofix.PausedBecamePublic || repo.Visibility != "public" {
		t.Fatalf("publicized: %+v", repo)
	}
	if status := webhookStatus(t, d.deliver(t, body, header)); status != "duplicate" {
		t.Fatalf("the same delivery again: %s", status)
	}
	if status := webhookStatus(t, d.webhook(t, "repository", map[string]any{"action": "privatized", "installation": installationOf(52),
		"repository": map[string]any{"id": 902, "full_name": "acme-52/app", "private": true, "visibility": "private"}})); status != "applied" {
		t.Fatalf("privatized: %s", status)
	}
	if repo := d.settings(t, b.admin).Repos[0]; repo.PausedReason != nil || repo.Visibility != "private" {
		t.Fatalf("privatized: %+v", repo)
	}
	// A second installation of the same workspace speaks only for its own repositories.
	d.connected(t, b, 53, "selected")
	other := map[string]any{"action": "publicized", "installation": installationOf(53), "repository": map[string]any{"id": 902, "full_name": "acme-52/app", "private": false, "visibility": "public"}}
	if status := webhookStatus(t, d.webhook(t, "repository", other)); status != "ignored" {
		t.Fatalf("another installation of the workspace: %s", status)
	}
	if repo := d.settings(t, b.admin).Repos[0]; repo.PausedReason != nil {
		t.Fatalf("another installation paused the repository: %+v", repo)
	}
	if status := webhookStatus(t, d.webhook(t, "ping", map[string]any{"zen": "Keep it logically awesome."})); status != "ignored" {
		t.Fatalf("ping: %s", status)
	}
}

// fixDesk is a workspace with a connected repository and a conversation to send.
type fixDesk struct {
	githubDesk
	w            githubWorkspace
	installation string
	repo         autofix.Repo
}

func newFixDesk(t *testing.T, name string, installationID, githubRepoID int64) fixDesk {
	t.Helper()
	d := newGitHubDesk(t)
	w := d.workspace(t, name)
	installation := d.connected(t, w, installationID, "selected")
	d.fake.AddRepo(installationID, githubtest.Repo{ID: githubRepoID, FullName: account(installationID) + "/shop", Visibility: "private"})
	var repo autofix.Repo
	decode(t, d.connectRepo(t, w.admin, installation, githubRepoID), &repo)
	return fixDesk{githubDesk: d, w: w, installation: installation, repo: repo}
}

// sent makes a fix whose issue number exists on GitHub.
func (f fixDesk) sent(t *testing.T, issue int) autofix.Fix {
	t.Helper()
	contact := create(t, f.api, f.w.admin, "/api/v1/contacts", map[string]any{"name": "Pat", "email": "pat-" + itoa(int64(issue)) + "@example.com"})
	conversation := create(t, f.api, f.w.admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Checkout broken"})
	st := store.New(f.pool)
	fix, _, err := st.CreateFix(context.Background(), f.w.id, store.NewFix{ConversationID: conversation, RepoID: f.repo.ID, Connector: autofix.ClaudeIssue,
		SentTitle: "Checkout broken", SentBody: "The button does nothing.", CreatedByName: "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	fix, _, err = st.ApplyFixObservation(context.Background(), f.w.id, fix.ID, autofix.Observation{State: autofix.Sent, IssueNumber: issue}, githubActor)
	if err != nil {
		t.Fatal(err)
	}
	f.fake.SetIssue(f.repo.GitHubRepoID, githubtest.Issue{Number: issue, State: "open"})
	return fix
}

func (f fixDesk) fix(t *testing.T, id string) autofix.FixDetail {
	t.Helper()
	var fix autofix.FixDetail
	decode(t, checked(t, f.api, http.MethodGet, "/api/v1/autofix/fixes/"+id, nil, f.w.admin, nil), &fix)
	return fix
}

func (f fixDesk) repoPayload() map[string]any { return f.fake.RepoPayload(f.repo.GitHubRepoID) }

func (f fixDesk) installationID() int64 { return *f.repo.InstallationID }

func (f fixDesk) pullEvent(t *testing.T, action string, number int) string {
	t.Helper()
	return webhookStatus(t, f.webhook(t, "pull_request", map[string]any{"action": action, "installation": installationOf(f.installationID()),
		"repository": f.repoPayload(), "pull_request": f.fake.PullPayload(f.repo.GitHubRepoID, number), "sender": map[string]any{"login": "ada", "type": "User"}}))
}

func (f fixDesk) issueEvent(t *testing.T, action string, number int, extra map[string]any) string {
	t.Helper()
	payload := map[string]any{"action": action, "installation": installationOf(f.installationID()), "repository": f.repoPayload(),
		"issue": map[string]any{"number": number}, "sender": map[string]any{"login": "ada", "type": "User"}}
	for key, value := range extra {
		payload[key] = value
	}
	return webhookStatus(t, f.webhook(t, "issues", payload))
}

func TestPullRequestEventsCarryAFixToItsMerge(t *testing.T) {
	f := newFixDesk(t, "PR Flow", 61, 1001)
	fix := f.sent(t, 3)
	repoID := f.repo.GitHubRepoID

	if status := webhookStatus(t, f.webhook(t, "issue_comment", map[string]any{"action": "created", "installation": installationOf(61), "repository": f.repoPayload(),
		"issue": map[string]any{"number": 3}, "comment": map[string]any{"body": "\nClaude is working on it…\nmore", "user": map[string]any{"login": "claude[bot]", "type": "Bot"}}})); status != "applied" {
		t.Fatalf("Claude's comment: %s", status)
	}
	if got := f.fix(t, fix.ID); got.State != autofix.Working {
		t.Fatalf("after Claude's comment: %s", got.State)
	}
	// Someone else's comment is not a sign of the agent.
	if status := webhookStatus(t, f.webhook(t, "issue_comment", map[string]any{"action": "created", "installation": installationOf(61), "repository": f.repoPayload(),
		"issue": map[string]any{"number": 3}, "comment": map[string]any{"body": "@claude[bot] hi", "user": map[string]any{"login": "claude[bot]", "type": "User"}}})); status != "ignored" {
		t.Fatalf("a person named like the bot: %s", status)
	}
	// Claude's pull request names the issue on a branch of its own; the link comes from GitHub.
	f.fake.SetPull(repoID, githubtest.Pull{Number: 9, State: "open", HeadRef: "claude/issue-3-20261027", Closes: []int{3}})
	if status := f.pullEvent(t, "opened", 9); status != "applied" {
		t.Fatalf("opened: %s", status)
	}
	got := f.fix(t, fix.ID)
	if got.State != autofix.PROpen || got.PRNumber == nil || *got.PRNumber != 9 || got.PRURL == nil || *got.PRURL != "https://github.com/acme-61/shop/pull/9" ||
		got.PRLinkedBy == nil || *got.PRLinkedBy != "auto" {
		t.Fatalf("after opened: %+v", got.Fix)
	}
	sha := strings.Repeat("ab", 20)
	f.fake.SetPull(repoID, githubtest.Pull{Number: 9, State: "closed", Merged: true, MergeSHA: sha, MergedBy: "ada", HeadRef: "claude/issue-3-20261027", Closes: []int{3}})
	if status := f.pullEvent(t, "closed", 9); status != "applied" {
		t.Fatalf("merged: %s", status)
	}
	got = f.fix(t, fix.ID)
	if got.State != autofix.Merged || got.MergeSHA == nil || *got.MergeSHA != sha || got.MergedByLogin == nil || *got.MergedByLogin != "ada" {
		t.Fatalf("after merge: %+v", got.Fix)
	}
	// The issue closing after the merge is only noted; an old opened event is stale.
	f.fake.SetIssue(repoID, githubtest.Issue{Number: 3, State: "closed", StateReason: "completed", ClosedBy: []int{9}})
	if status := f.issueEvent(t, "closed", 3, nil); status != "applied" {
		t.Fatalf("issue closed after merge: %s", status)
	}
	f.fake.SetPull(repoID, githubtest.Pull{Number: 9, State: "open", HeadRef: "claude/issue-3-20261027"})
	if status := f.pullEvent(t, "reopened", 9); status == "applied" && f.fix(t, fix.ID).State != autofix.Merged {
		t.Fatal("a merged fix went back")
	}
	if got := f.fix(t, fix.ID); got.State != autofix.Merged {
		t.Fatalf("final state %s", got.State)
	}

	// A pull request of an unrelated branch closing no fix's issue is ignored.
	f.fake.SetPull(repoID, githubtest.Pull{Number: 10, State: "open", HeadRef: "feature/x"})
	if status := f.pullEvent(t, "opened", 10); status != "ignored" {
		t.Fatalf("unrelated pull request: %s", status)
	}
}

func TestAClosedIssueIsAMergeWhenItsPullRequestMergedElseACancel(t *testing.T) {
	f := newFixDesk(t, "Issue Close", 71, 1101)
	repoID := f.repo.GitHubRepoID
	merged := f.sent(t, 5)
	// The pull request's own webhook was missed: GitHub says a merged pull request closed the issue.
	f.fake.SetPull(repoID, githubtest.Pull{Number: 6, State: "closed", Merged: true, MergeSHA: strings.Repeat("c", 40), MergedBy: "ada", HeadRef: "copilot/fix-5"})
	f.fake.SetIssue(repoID, githubtest.Issue{Number: 5, State: "closed", StateReason: "completed", ClosedBy: []int{6}})
	f.issueEvent(t, "closed", 5, nil)
	if got := f.fix(t, merged.ID); got.State != autofix.Merged || got.PRNumber == nil || *got.PRNumber != 6 {
		t.Fatalf("closed by a merged pull request: %+v", got.Fix)
	}

	byHand := f.sent(t, 7)
	f.fake.SetIssue(repoID, githubtest.Issue{Number: 7, State: "closed", StateReason: "not_planned"})
	f.issueEvent(t, "closed", 7, nil)
	got := f.fix(t, byHand.ID)
	if got.State != autofix.Cancelled || got.OutcomeReason == nil || *got.OutcomeReason != "issue_closed" {
		t.Fatalf("closed by hand: %+v", got.Fix)
	}
	f.issueEvent(t, "reopened", 7, nil)
	if got := f.fix(t, byHand.ID); got.State != autofix.Sent {
		t.Fatalf("reopened: %s", got.State)
	}

	copilot := f.sent(t, 8)
	f.issueEvent(t, "assigned", 8, map[string]any{"assignee": map[string]any{"login": "Copilot", "type": "Bot"}})
	if got := f.fix(t, copilot.ID); got.State != autofix.Working {
		t.Fatalf("assigned to Copilot: %s", got.State)
	}
}

// Removing the App on GitHub, or a repository from it, pauses the repositories and cancels the fixes in
// progress on them; the repositories come back when the App reaches them again.
func TestRepositoriesLeavingTheAppPauseAndCancelTheirFixes(t *testing.T) {
	f := newFixDesk(t, "Leave App", 81, 1201)
	fix := f.sent(t, 2)
	if status := webhookStatus(t, f.webhook(t, "installation_repositories", map[string]any{"action": "removed", "installation": installationOf(81),
		"repository_selection": "selected", "repositories_added": []any{}, "repositories_removed": []any{map[string]any{"id": 1201, "full_name": "acme-81/shop"}}})); status != "applied" {
		t.Fatalf("removed: %s", status)
	}
	repo := f.settings(t, f.w.admin).Repos[0]
	if repo.PausedReason == nil || *repo.PausedReason != autofix.PausedRemovedFromApp {
		t.Fatalf("repo after removal: %+v", repo)
	}
	if got := f.fix(t, fix.ID); got.State != autofix.Cancelled || *got.OutcomeReason != "repo_disconnected" {
		t.Fatalf("fix after removal: %+v", got.Fix)
	}
	webhookStatus(t, f.webhook(t, "installation_repositories", map[string]any{"action": "added", "installation": installationOf(81),
		"repository_selection": "selected", "repositories_added": []any{map[string]any{"id": 1201, "full_name": "acme-81/shop", "private": true}}, "repositories_removed": []any{}}))
	if repo := f.settings(t, f.w.admin).Repos[0]; repo.PausedReason != nil {
		t.Fatalf("repo added back: %+v", repo)
	}

	second := f.sent(t, 4)
	if status := webhookStatus(t, f.webhook(t, "installation", map[string]any{"action": "deleted", "installation": installationOf(81)})); status != "applied" {
		t.Fatalf("deleted: %s", status)
	}
	settings := f.settings(t, f.w.admin)
	if len(settings.Installations) != 0 || len(settings.Repos) != 1 || settings.Repos[0].InstallationID != nil || *settings.Repos[0].PausedReason != autofix.PausedRemovedFromApp {
		t.Fatalf("after the App was removed: %+v", settings)
	}
	if got := f.fix(t, second.ID); got.State != autofix.Cancelled {
		t.Fatalf("fix after the App was removed: %s", got.State)
	}
	// Installing again brings the repository back.
	f.fake.AddInstallation(githubtest.Installation{ID: 81, Account: "acme-81", AccountType: "Organization", Selection: "selected", Repos: []int64{1201}, Users: []string{"ada"}})
	if location := f.install(t, f.startInstall(t, f.w.admin), "ada", 81, sessionCookieOf(f.w.admin)); location != "/settings/autofix?github=connected" {
		t.Fatalf("install again: %s", location)
	}
	f.server.GitHubSync().ResumeRepos(context.Background(), f.w.id, 81)
	if repo := f.settings(t, f.w.admin).Repos[0]; repo.PausedReason != nil || repo.InstallationID == nil || *repo.InstallationID != 81 {
		t.Fatalf("repo after installing again: %+v", repo)
	}
}

func TestDisconnectingGitHubUninstallsTheAppAndKeepsTheRepositoriesPaused(t *testing.T) {
	f := newFixDesk(t, "Disconnect", 91, 1301)
	fix := f.sent(t, 1)
	f.fake.SetDown(true)
	if response := checked(t, f.api, http.MethodDelete, "/api/v1/autofix/github/installations/"+f.installation, nil, f.w.admin, nil); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("GitHub down: %d %s", response.Code, response.Body.String())
	}
	if len(f.settings(t, f.w.admin).Installations) != 1 {
		t.Fatal("removed although GitHub did not uninstall")
	}
	f.fake.SetDown(false)
	response := checked(t, f.api, http.MethodDelete, "/api/v1/autofix/github/installations/"+f.installation, nil, f.w.admin, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("disconnect: %d %s", response.Code, response.Body.String())
	}
	validateContractResponse(t, newRequest(t, http.MethodDelete, "/api/v1/autofix/github/installations/"+f.installation, nil, f.w.admin, nil), response)
	if !f.fake.Uninstalled(91) {
		t.Fatal("the App is still installed on GitHub")
	}
	if got := f.fix(t, fix.ID); got.State != autofix.Cancelled || *got.OutcomeReason != "repo_disconnected" {
		t.Fatalf("fix: %+v", got.Fix)
	}
	if repo := f.settings(t, f.w.admin).Repos[0]; *repo.PausedReason != autofix.PausedRemovedFromApp {
		t.Fatalf("repo: %+v", repo)
	}
}

// The reconcile pass follows a self-hosted server that gets no webhook: a merge, a repository that left
// the App or turned public, an installation removed.
func TestTheReconcilePassFollowsGitHubWithoutWebhooks(t *testing.T) {
	f := newFixDesk(t, "Reconcile", 95, 1401)
	ctx := context.Background()
	repoID := f.repo.GitHubRepoID
	open := f.sent(t, 11)
	f.fake.SetPull(repoID, githubtest.Pull{Number: 12, State: "open", HeadRef: "copilot/fix-11"})
	f.fake.SetIssue(repoID, githubtest.Issue{Number: 11, State: "open", ClosedBy: []int{12}})
	if err := f.server.GitHubSync().Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if got := f.fix(t, open.ID); got.State != autofix.PROpen || *got.PRNumber != 12 {
		t.Fatalf("an open pull request: %+v", got.Fix)
	}
	f.fake.SetPull(repoID, githubtest.Pull{Number: 12, State: "closed", Merged: true, MergeSHA: strings.Repeat("d", 40), MergedBy: "ada"})
	if err := f.server.GitHubSync().Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if got := f.fix(t, open.ID); got.State != autofix.Merged {
		t.Fatalf("merged: %s", got.State)
	}

	waiting := f.sent(t, 13)
	f.fake.SetVisibility(repoID, "public")
	if err := f.server.GitHubSync().Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if repo := f.settings(t, f.w.admin).Repos[0]; repo.PausedReason == nil || *repo.PausedReason != autofix.PausedBecamePublic {
		t.Fatalf("turned public: %+v", repo)
	}
	if got := f.fix(t, waiting.ID); got.State != autofix.Sent {
		t.Fatalf("a fix already sent stays: %s", got.State)
	}
	f.fake.SetVisibility(repoID, "private")
	f.fake.Update(95, func(i *githubtest.Installation) { i.Repos = nil })
	if err := f.server.GitHubSync().Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if got := f.fix(t, waiting.ID); got.State != autofix.Cancelled || *got.OutcomeReason != "repo_disconnected" {
		t.Fatalf("repository left the App: %+v", got.Fix)
	}
	d := f.fake
	d.Update(95, func(i *githubtest.Installation) { i.Suspended = true })
	if err := f.server.GitHubSync().Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if status := f.settings(t, f.w.admin).Installations[0].Status; status != autofix.InstallationSuspended {
		t.Fatalf("suspended: %s", status)
	}
	if err := f.server.GitHubSync().App().Uninstall(ctx, 95); err != nil {
		t.Fatal(err)
	}
	if err := f.server.GitHubSync().Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if settings := f.settings(t, f.w.admin); len(settings.Installations) != 0 {
		t.Fatalf("an installation removed on GitHub stays: %+v", settings.Installations)
	}
}

// The repository check Cursor runs before it sends: GitHub now, a public repository paused.
func TestTheRepositoryCheckReadsGitHubNow(t *testing.T) {
	f := newFixDesk(t, "Check Repo", 97, 1501)
	sync := f.server.GitHubSync()
	check, err := sync.CheckRepo(context.Background(), f.w.id, f.repo.ID)
	if err != nil || check.Visibility != "private" || check.FullName != "acme-97/shop" {
		t.Fatalf("check = %+v, %v", check, err)
	}
	f.fake.SetVisibility(f.repo.GitHubRepoID, "public")
	if check, err := sync.CheckRepo(context.Background(), f.w.id, f.repo.ID); err != nil || check.Visibility != "public" {
		t.Fatalf("public: %+v, %v", check, err)
	}
	if repo := f.settings(t, f.w.admin).Repos[0]; repo.PausedReason == nil || *repo.PausedReason != autofix.PausedBecamePublic {
		t.Fatalf("not paused: %+v", repo)
	}
	f.fake.Update(97, func(i *githubtest.Installation) { i.Repos = nil })
	if _, err := sync.CheckRepo(context.Background(), f.w.id, f.repo.ID); err != autofix.ErrRepoUnreachable {
		t.Fatalf("removed from the App: %v", err)
	}
	other := f.workspace(t, "Check Other")
	if _, err := sync.CheckRepo(context.Background(), other.id, f.repo.ID); err != autofix.ErrRepoNotFound {
		t.Fatalf("another workspace's repository: %v", err)
	}
}

func TestAnAgentTokenAsksAPersonToInstallTheApp(t *testing.T) {
	d := newGitHubDesk(t)
	w := d.workspace(t, "Token GitHub")
	admin := issueToken(t, d.api, w.admin, "Ops agent", "admin").Token
	asked := accepted(t, d.api, http.MethodPost, "/api/v1/autofix/github/installations", nil, admin)
	approved := approve(t, d.api, w.admin, asked.RequestID, nil)
	if approved.Code != http.StatusOK || !strings.Contains(approved.Body.String(), "/installations/new") {
		t.Fatalf("approver gets the install address: %d %s", approved.Code, approved.Body.String())
	}
	if stored := getRequest(t, d.api, admin, asked.RequestID); strings.Contains(string(stored.Result), "installations/new") {
		t.Fatalf("the token can read the install address: %s", stored.Result)
	}
	read := issueToken(t, d.api, w.admin, "Reader", "read").Token
	if response := checked(t, d.api, http.MethodGet, "/api/v1/autofix/settings", nil, read, nil); response.Code != http.StatusOK {
		t.Fatalf("a read token reads the settings: %d", response.Code)
	}
	accepted(t, d.api, http.MethodPost, "/api/v1/autofix/repos", map[string]any{"installation_id": "00000000-0000-4000-8000-000000000000", "github_repo_id": 1}, read)
}

func TestAServerWithoutItsGitHubAppSaysSo(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, nil)
	admin := createSession(t, api, service, "No GitHub", "admin@no-github.example.com")
	var settings autofix.Settings
	decode(t, request(t, api, http.MethodGet, "/api/v1/autofix/settings", nil, admin, nil), &settings)
	if settings.Available || settings.AppSlug != nil {
		t.Fatalf("settings = %+v", settings)
	}
	response := request(t, api, http.MethodPost, "/api/v1/autofix/github/installations", nil, admin, nil)
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "autofix_unavailable") {
		t.Fatalf("install: %d %s", response.Code, response.Body.String())
	}
	if response := request(t, api, http.MethodPost, "/api/v1/webhooks/github", map[string]any{}, "", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("webhook: %d", response.Code)
	}
}

// joinWithRole invites email with the role and returns the session of the person who redeems it.
func joinWithRole(t *testing.T, api http.Handler, admin, email, role string) string {
	t.Helper()
	_, link := createContractInvite(t, api, admin, email, role)
	response := request(t, api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, link)}, "", nil)
	var session struct {
		Token string `json:"token"`
	}
	decode(t, response, &session)
	if session.Token == "" {
		t.Fatalf("redeem %s invite status=%d body=%s", role, response.Code, response.Body.String())
	}
	return session.Token
}

const (
	githubWebhookPath = "/api/v1/webhooks/github"
	// The contract fixture's installation and the repository its connect case connects.
	contractInstallation     = 4001
	contractInstallationText = "4001"
	contractNewRepo          = 4502
	// contractWebhookSecret is the webhook secret of githubtest.New.
	contractWebhookSecret = "webhook-secret"
)

// contractGitHub is what the contract fixture connected through the GitHub stand-in: an installation
// with a connected repository, a second repository to disconnect, and a second installation to remove.
type contractGitHub struct {
	installation, removableInstallation string
	repo, removableRepo                 string
}

func startContractGitHub(t *testing.T) *githubtest.Server {
	t.Helper()
	fake := githubtest.New()
	server := fake.Start()
	t.Cleanup(server.Close)
	fake.CallbackURL = autofixCallback
	return fake
}

func seedContractGitHub(t *testing.T, api http.Handler, fake *githubtest.Server, admin string) contractGitHub {
	t.Helper()
	d := githubDesk{api: api, fake: fake}
	w := githubWorkspace{admin: admin}
	var seeded contractGitHub
	seeded.installation = d.connected(t, w, contractInstallation, "selected")
	seeded.removableInstallation = d.connected(t, w, contractInstallation+1, "selected")
	for _, id := range []int64{4501, contractNewRepo, 4503} {
		fake.AddRepo(contractInstallation, githubtest.Repo{ID: id, FullName: account(contractInstallation) + "/repo-" + itoa(id), Visibility: "private"})
	}
	seeded.repo = create(t, api, admin, "/api/v1/autofix/repos", map[string]any{"installation_id": seeded.installation, "github_repo_id": 4501})
	seeded.removableRepo = create(t, api, admin, "/api/v1/autofix/repos", map[string]any{"installation_id": seeded.installation, "github_repo_id": 4503})
	return seeded
}

// signedGitHubRequest is a GitHub delivery signed with the stand-in's webhook secret.
func signedGitHubRequest(body []byte) *http.Request {
	req := httptest.NewRequest(http.MethodPost, githubWebhookPath, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "ping")
	req.Header.Set("X-GitHub-Delivery", "contract-"+strconv.FormatInt(time.Now().UnixNano(), 10))
	req.Header.Set(githubsig.Header, githubsig.Sign(contractWebhookSecret, body))
	return req
}

// /user/installations also lists an organization's installation to a member who reaches one of its
// repositories: only a person who administers every repository the installation reaches connects it.
func TestOnlyAnAdministratorOfEveryRepositoryConnectsTheInstallation(t *testing.T) {
	d := newGitHubDesk(t)
	w := d.workspace(t, "Repo Admins")
	d.fake.AddInstallation(githubtest.Installation{ID: 23, Account: "org23", AccountType: "Organization", Selection: "selected", Users: []string{"olga", "mem", "half"}})
	d.fake.AddRepo(23, githubtest.Repo{ID: 2301, FullName: "org23/app", Visibility: "private", Access: map[string]string{"mem": "write", "half": "admin"}})
	d.fake.AddRepo(23, githubtest.Repo{ID: 2302, FullName: "org23/secret", Visibility: "private", Access: map[string]string{"mem": "none", "half": "none"}})
	for _, login := range []string{"mem", "half"} {
		before := len(d.fake.UserTokens())
		if location := d.install(t, d.startInstall(t, w.admin), login, 23, sessionCookieOf(w.admin)); location != "/settings/autofix?github_error=installation_not_yours" {
			t.Fatalf("%s: Location = %s", login, location)
		}
		tokens := d.fake.UserTokens()
		if len(tokens) != before+1 || d.fake.RevokedTokens() != len(tokens) {
			t.Fatalf("%s: tokens %d, revoked %d", login, len(tokens), d.fake.RevokedTokens())
		}
		if len(d.settings(t, w.admin).Installations) != 0 {
			t.Fatalf("%s connected the organization's installation", login)
		}
	}
	// An installation with no repository to administer proves nothing either.
	d.fake.AddInstallation(githubtest.Installation{ID: 24, Account: "empty24", AccountType: "Organization", Selection: "selected", Users: []string{"olga"}})
	if location := d.install(t, d.startInstall(t, w.admin), "olga", 24, sessionCookieOf(w.admin)); location != "/settings/autofix?github_error=installation_not_yours" {
		t.Fatalf("an installation with no repository: Location = %s", location)
	}
	if location := d.install(t, d.startInstall(t, w.admin), "olga", 23, sessionCookieOf(w.admin)); location != "/settings/autofix?github=connected" {
		t.Fatalf("the owner: Location = %s", location)
	}
	// Removing it uninstalls the App on the customer's GitHub: an agent token asks a person.
	installation := d.settings(t, w.admin).Installations[0].ID
	admin := issueToken(t, d.api, w.admin, "Ops agent", "admin").Token
	accepted(t, d.api, http.MethodDelete, "/api/v1/autofix/github/installations/"+installation, nil, admin)
	if d.fake.Uninstalled(23) {
		t.Fatal("an agent token uninstalled the App")
	}
}
