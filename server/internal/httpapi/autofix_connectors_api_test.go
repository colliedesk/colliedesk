package httpapi_test

import (
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/autofix"
	"github.com/colliedesk/colliedesk/server/internal/autofixworker"
	"github.com/colliedesk/colliedesk/server/internal/cursor/cursortest"
	"github.com/colliedesk/colliedesk/server/internal/github"
	"github.com/colliedesk/colliedesk/server/internal/github/githubtest"
	"github.com/colliedesk/colliedesk/server/internal/google"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
)

const cursorCanaryKey = "crsr_canary-api-DO-NOT-PRINT-4b8e"

type codingAgent struct {
	Kind    string  `json:"kind"`
	Enabled bool    `json:"enabled"`
	Mode    *string `json:"mode"`
	Consent *struct {
		Version string `json:"version"`
		Name    string `json:"name"`
	} `json:"consent"`
	Credential *struct {
		SetByName *string `json:"set_by_name"`
	} `json:"credential"`
	LastCheck *struct {
		Result  string  `json:"result"`
		Message *string `json:"message"`
	} `json:"last_check"`
}

// autofixConfig turns the GitHub App on (stand-in values), gives the server a seal ring and points
// Cursor at the stand-in.
func autofixConfig(standIn *cursortest.Server) func(*httpapi.Config) {
	return func(config *httpapi.Config) {
		config.GitHub = github.ConfigFromEnv(func(name string) string { return "set-" + name })
		keys, _ := tokenseal.ParseKeys("k1:AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM=")
		config.Google = google.Config{Keys: keys}
		config.Cursor = &autofixworker.Cursor{Base: standIn.URL()}
	}
}

func TestAnAdminTurnsACodingAgentOnWithTheCurrentConsentAndOffAgain(t *testing.T) {
	standIn := cursortest.New(cursorCanaryKey)
	defer standIn.Close()
	api, service, _, _ := testAPIWithConfig(t, autofixConfig(standIn))
	admin := createSession(t, api, service, "Agents On", "admin@agents-on.example.com")
	other := createSession(t, api, service, "Agents Other", "admin@agents-other.example.com")
	path := "/api/v1/autofix/connectors/claude_issue"

	if response := request(t, api, http.MethodPut, path, map[string]any{"consent_version": "2020-01-01"}, admin, nil); response.Code != http.StatusConflict ||
		!strings.Contains(response.Body.String(), "consent_outdated") {
		t.Fatalf("an old consent: %d %s", response.Code, response.Body)
	}
	if response := request(t, api, http.MethodPut, "/api/v1/autofix/connectors/codex", map[string]any{"consent_version": autofix.ConsentVersion}, admin, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("an unknown agent: %d", response.Code)
	}
	response := request(t, api, http.MethodPut, path, map[string]any{"consent_version": autofix.ConsentVersion}, admin, nil)
	var on codingAgent
	decode(t, response, &on)
	if response.Code != http.StatusOK || !on.Enabled || on.Consent == nil || on.Consent.Version != autofix.ConsentVersion || on.Consent.Name == "" || on.Mode != nil {
		t.Fatalf("turned on: %d %s", response.Code, response.Body)
	}
	response = request(t, api, http.MethodDelete, path, nil, admin, nil)
	var off codingAgent
	decode(t, response, &off)
	if response.Code != http.StatusOK || off.Enabled || off.Consent == nil {
		t.Fatalf("turned off (the consent on record stays): %d %s", response.Code, response.Body)
	}
	// Another workspace's agents are its own.
	response = request(t, api, http.MethodPost, "/api/v1/autofix/connectors/claude_issue/check", nil, other, nil)
	var theirs codingAgent
	decode(t, response, &theirs)
	if response.Code != http.StatusOK || theirs.Consent != nil || theirs.Enabled {
		t.Fatalf("B sees A's consent: %d %s", response.Code, response.Body)
	}
}

func TestOnlyAdminsChangeCodingAgentsAndTokensAskFirst(t *testing.T) {
	standIn := cursortest.New(cursorCanaryKey)
	defer standIn.Close()
	api, service, pool, _ := testAPIWithConfig(t, autofixConfig(standIn))
	admin := createSession(t, api, service, "Agents Ask", "admin@agents-ask.example.com")
	_, inviteURL := createContractInvite(t, api, admin, "member@agents-ask.example.com", "agent")
	member := redeemLabelled(t, api, inviteURL, "Member laptop")
	for _, call := range []struct{ method, path string }{
		{http.MethodPut, "/api/v1/autofix/connectors/cursor_api"},
		{http.MethodDelete, "/api/v1/autofix/connectors/cursor_api"},
		{http.MethodPut, "/api/v1/autofix/connectors/cursor_api/key"},
		{http.MethodDelete, "/api/v1/autofix/connectors/cursor_api/key"},
	} {
		body := map[string]any{"consent_version": autofix.ConsentVersion}
		if strings.HasSuffix(call.path, "/key") {
			body = map[string]any{"key": cursorCanaryKey}
		}
		if call.method == http.MethodDelete {
			body = nil
		}
		if response := request(t, api, call.method, call.path, body, member, nil); response.Code != http.StatusForbidden {
			t.Errorf("a member %s %s: %d", call.method, call.path, response.Code)
		}
	}
	// Turning an agent on waits for a person whatever the token holds.
	adminToken := issueToken(t, api, admin, "Admin agent", "admin").Token
	if response := request(t, api, http.MethodPut, "/api/v1/autofix/connectors/cursor_api", map[string]any{"consent_version": autofix.ConsentVersion}, adminToken, nil); response.Code != http.StatusAccepted {
		t.Fatalf("an admin token turns an agent on: %d %s", response.Code, response.Body)
	}
	// A token below admin is refused the key, and the key never waits in a request.
	sendToken := issueToken(t, api, admin, "Send agent", "send").Token
	response := request(t, api, http.MethodPut, "/api/v1/autofix/connectors/cursor_api/key", map[string]any{"key": cursorCanaryKey}, sendToken, nil)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "credentials_need_admin_token") {
		t.Fatalf("a send token sets the key: %d %s", response.Code, response.Body)
	}
	if n := queryString(t, pool, `SELECT count(*)::text FROM agent_requests WHERE body::text LIKE '%'||$1||'%'`, cursorCanaryKey); n != "0" {
		t.Fatalf("the key waits in %s agent requests", n)
	}
	// An admin token sets it directly.
	if response := request(t, api, http.MethodPut, "/api/v1/autofix/connectors/cursor_api/key", map[string]any{"key": cursorCanaryKey}, adminToken, nil); response.Code != http.StatusOK {
		t.Fatalf("an admin token sets the key: %d %s", response.Code, response.Body)
	}
}

func TestTheCursorKeyIsTriedSealedNeverShownAndRemoved(t *testing.T) {
	standIn := cursortest.New(cursorCanaryKey)
	defer standIn.Close()
	api, service, pool, _ := testAPIWithConfig(t, autofixConfig(standIn))
	admin := createSession(t, api, service, "Cursor Key", "admin@cursor-key.example.com")
	other := createSession(t, api, service, "Cursor Other", "admin@cursor-other.example.com")
	path := "/api/v1/autofix/connectors/cursor_api/key"

	if response := request(t, api, http.MethodPut, path, map[string]any{"key": "crsr_not-a-key-cursor-knows"}, admin, nil); response.Code != http.StatusBadRequest ||
		!strings.Contains(response.Body.String(), "cursor_key_invalid") {
		t.Fatalf("a key Cursor refuses: %d %s", response.Code, response.Body)
	}
	if n := queryString(t, pool, `SELECT count(*)::text FROM autofix_connectors WHERE credential_sealed IS NOT NULL`); n != "0" {
		t.Fatalf("a refused key was kept")
	}
	response := request(t, api, http.MethodPut, path, map[string]any{"key": cursorCanaryKey}, admin, nil)
	var set codingAgent
	decode(t, response, &set)
	if response.Code != http.StatusOK || set.Credential == nil || set.Credential.SetByName == nil || set.LastCheck == nil || set.LastCheck.Result != "ok" {
		t.Fatalf("set: %d %s", response.Code, response.Body)
	}
	if strings.Contains(response.Body.String(), cursorCanaryKey) || strings.Contains(response.Body.String(), cursorCanaryKey[len(cursorCanaryKey)-4:]) {
		t.Fatal("the answer shows the key")
	}
	if n := queryString(t, pool, `SELECT count(*)::text FROM autofix_connectors c WHERE position($1 in c::text) > 0`, cursorCanaryKey); n != "0" {
		t.Fatal("the key is stored in clear")
	}
	// The export says a key is set, never which.
	export := request(t, api, http.MethodGet, "/api/v1/export", nil, admin, nil)
	if export.Code != http.StatusOK || strings.Contains(export.Body.String(), cursorCanaryKey) {
		t.Fatalf("export: %d, carries the key: %v", export.Code, strings.Contains(export.Body.String(), cursorCanaryKey))
	}
	// Another workspace has no key.
	if response := request(t, api, http.MethodPost, "/api/v1/autofix/connectors/cursor_api/check", nil, other, nil); response.Code != http.StatusConflict ||
		!strings.Contains(response.Body.String(), "cursor_key_missing") {
		t.Fatalf("B checks A's key: %d %s", response.Code, response.Body)
	}
	response = request(t, api, http.MethodDelete, path, nil, admin, nil)
	var removed codingAgent
	decode(t, response, &removed)
	if response.Code != http.StatusOK || removed.Credential != nil || removed.LastCheck != nil {
		t.Fatalf("removed: %d %s", response.Code, response.Body)
	}
	if n := queryString(t, pool, `SELECT count(*)::text FROM autofix_connectors WHERE credential_sealed IS NOT NULL`); n != "0" {
		t.Fatal("the removed key is still stored")
	}
}

func TestCheckingCursorTriesTheKeyAndLooksForTheConnectedRepositories(t *testing.T) {
	standIn := cursortest.New(cursorCanaryKey)
	defer standIn.Close()
	var mu sync.Mutex
	now := time.Now().UTC()
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	later := func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }
	handler, auth, db, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		autofixConfig(standIn)(config)
		config.Now = clock
	})
	admin := createSession(t, handler, auth, "Cursor Check", "admin@cursor-check.example.com")
	newAutofixDesk(t, handler, db, admin)
	if response := request(t, handler, http.MethodPut, "/api/v1/autofix/connectors/cursor_api/key", map[string]any{"key": cursorCanaryKey}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("set: %d", response.Code)
	}
	standIn.Repositories("https://github.com/acme/other")
	check := func() codingAgent {
		response := request(t, handler, http.MethodPost, "/api/v1/autofix/connectors/cursor_api/check", nil, admin, nil)
		var agent codingAgent
		decode(t, response, &agent)
		if response.Code != http.StatusOK || agent.LastCheck == nil {
			t.Fatalf("check: %d %s", response.Code, response.Body)
		}
		return agent
	}
	// Right after the key is set, the first check lists the repositories.
	missing := check()
	if missing.LastCheck.Result != "no_repo_access" || missing.LastCheck.Message == nil || !strings.Contains(*missing.LastCheck.Message, "acme/shop-web") {
		t.Fatalf("a repository Cursor does not reach: %+v", missing.LastCheck)
	}
	// Within the minute Cursor allows, the listing is not repeated and its result stands.
	standIn.Repositories("https://github.com/Acme/Shop-Web.git")
	if again := check(); again.LastCheck.Result != "no_repo_access" || standIn.Calls("repositories") != 1 {
		t.Fatalf("within a minute: %+v, %d listings", again.LastCheck, standIn.Calls("repositories"))
	}
	// A new key is another listing: the check right after it lists again.
	if response := request(t, handler, http.MethodPut, "/api/v1/autofix/connectors/cursor_api/key", map[string]any{"key": cursorCanaryKey}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("set again: %d", response.Code)
	}
	if reached := check(); reached.LastCheck.Result != "ok" || standIn.Calls("repositories") != 2 {
		t.Fatalf("after a new key: %+v, %d listings", reached.LastCheck, standIn.Calls("repositories"))
	}
	// A key Cursor stopped accepting shows as invalid.
	later(61 * time.Second)
	standIn.Fail("me", cursortest.Failure{Status: 401, Message: "Invalid API key"})
	if refused := check(); refused.LastCheck.Result != "key_invalid" {
		t.Fatalf("refused: %+v", refused.LastCheck)
	}
}

func TestCheckingAnIssueAgentSaysWhatTheGitHubConnectionLacks(t *testing.T) {
	standIn := cursortest.New(cursorCanaryKey)
	defer standIn.Close()
	handler, auth, db, _ := testAPIWithConfig(t, autofixConfig(standIn))
	admin := createSession(t, handler, auth, "Issue Check", "admin@issue-check.example.com")
	check := func() codingAgent {
		response := request(t, handler, http.MethodPost, "/api/v1/autofix/connectors/copilot_issue/check", nil, admin, nil)
		var agent codingAgent
		decode(t, response, &agent)
		if response.Code != http.StatusOK || agent.LastCheck == nil {
			t.Fatalf("check: %d %s", response.Code, response.Body)
		}
		return agent
	}
	if none := check(); none.LastCheck.Result != "installation_inactive" {
		t.Fatalf("no installation: %+v", none.LastCheck)
	}
	desk := newAutofixDesk(t, handler, db, admin)
	if _, err := db.Exec(t.Context(), `INSERT INTO autofix_github_installations (workspace_id, installation_id, account_login, account_type, repository_selection, status, connected_by_name)
		VALUES ($1, 99001, 'acme', 'Organization', 'selected', 'active', 'Ada')`, desk.workspaceID); err != nil {
		t.Fatal(err)
	}
	if ok := check(); ok.LastCheck.Result != "ok" {
		t.Fatalf("connected: %+v", ok.LastCheck)
	}
	if _, err := db.Exec(t.Context(), `UPDATE autofix_repos SET visibility = 'internal' WHERE id = $1`, desk.repoID); err != nil {
		t.Fatal(err)
	}
	if internal := check(); internal.LastCheck.Result != "repo_not_private" {
		t.Fatalf("internal: %+v", internal.LastCheck)
	}
}

func TestWithoutTheGitHubAppCursorCannotBeSetOrChecked(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "No App", "admin@no-app.example.com")
	for _, call := range []struct {
		method, path string
		body         map[string]any
	}{
		{http.MethodPut, "/api/v1/autofix/connectors/cursor_api/key", map[string]any{"key": cursorCanaryKey}},
		{http.MethodPost, "/api/v1/autofix/connectors/cursor_api/check", nil},
	} {
		if response := request(t, api, call.method, call.path, call.body, admin, nil); response.Code != http.StatusServiceUnavailable ||
			!strings.Contains(response.Body.String(), "autofix_unavailable") {
			t.Errorf("%s %s: %d %s", call.method, call.path, response.Code, response.Body)
		}
	}
}

func TestTheSettingsCallCursorAFewTimesAMinuteAtMost(t *testing.T) {
	standIn := cursortest.New(cursorCanaryKey)
	defer standIn.Close()
	api, service, _, _ := testAPIWithConfig(t, autofixConfig(standIn))
	admin := createSession(t, api, service, "Cursor Calls", "admin@cursor-calls.example.com")
	readToken := issueToken(t, api, admin, "Read agent", "read").Token
	if response := request(t, api, http.MethodPut, "/api/v1/autofix/connectors/cursor_api/key", map[string]any{"key": cursorCanaryKey}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("set: %d", response.Code)
	}
	limited := 0
	for range 8 {
		response := request(t, api, http.MethodPost, "/api/v1/autofix/connectors/cursor_api/check", nil, readToken, nil)
		if response.Code == http.StatusTooManyRequests && response.Header().Get("Retry-After") != "" {
			limited++
		}
	}
	if limited != 4 || standIn.Calls("me") != 5 {
		t.Fatalf("limited %d, Cursor called %d times", limited, standIn.Calls("me"))
	}
}

func TestAnApprovedRequestTurnsAnAgentOnOnlyWithAConsentInTheApproversName(t *testing.T) {
	standIn := cursortest.New(cursorCanaryKey)
	defer standIn.Close()
	api, service, _, _ := testAPIWithConfig(t, autofixConfig(standIn))
	admin := createSession(t, api, service, "Agents Approve", "admin@agents-approve.example.com")
	if response := request(t, api, http.MethodPatch, "/api/v1/agents/me/display-name", map[string]any{"display_name": "Ada Approver"}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("name: %d", response.Code)
	}
	path := "/api/v1/autofix/connectors/claude_issue"
	for _, body := range []map[string]any{{}, {"consent_version": "  "}} {
		if response := request(t, api, http.MethodPut, path, body, admin, nil); response.Code != http.StatusUnprocessableEntity ||
			!strings.Contains(response.Body.String(), "consent_required") {
			t.Fatalf("no consent %v: %d %s", body, response.Code, response.Body)
		}
	}
	token := issueToken(t, api, admin, "Setup agent", "admin").Token
	file := func(body map[string]any) string {
		response := request(t, api, http.MethodPut, path, body, token, nil)
		var filed struct {
			RequestID string `json:"request_id"`
		}
		decode(t, response, &filed)
		if response.Code != http.StatusAccepted || filed.RequestID == "" {
			t.Fatalf("filed: %d %s", response.Code, response.Body)
		}
		return filed.RequestID
	}
	// A request without a consent is refused when approved, and nothing turns on.
	if response := approve(t, api, admin, file(map[string]any{}), nil); !strings.Contains(response.Body.String(), "consent_required") {
		t.Fatalf("approved without a consent: %d %s", response.Code, response.Body)
	}
	if response := request(t, api, http.MethodPost, path+"/check", nil, admin, nil); strings.Contains(response.Body.String(), `"enabled":true`) {
		t.Fatalf("turned on without a consent: %s", response.Body)
	}
	response := approve(t, api, admin, file(map[string]any{"consent_version": autofix.ConsentVersion}), nil)
	if response.Code != http.StatusOK {
		t.Fatalf("approved: %d %s", response.Code, response.Body)
	}
	check := request(t, api, http.MethodPost, path+"/check", nil, admin, nil)
	var agent codingAgent
	decode(t, check, &agent)
	if !agent.Enabled || agent.Consent == nil || agent.Consent.Name != "Ada Approver" || agent.Consent.Version != autofix.ConsentVersion {
		t.Fatalf("the consent is not the approver's: %s", check.Body)
	}
}

func TestTurningAnAgentOffCancelsItsFixesNotSentYetAndOnlyThose(t *testing.T) {
	standIn := cursortest.New(cursorCanaryKey)
	defer standIn.Close()
	api, service, pool, _ := testAPIWithConfig(t, autofixConfig(standIn))
	a := newAutofixDesk(t, api, pool, createSession(t, api, service, "Off A", "admin@off-a.example.com"))
	b := newAutofixDesk(t, api, pool, createSession(t, api, service, "Off B", "admin@off-b.example.com"))
	sending := a.send(t, pool, a.conversation(t, api), "off-1")
	sent := a.send(t, pool, a.conversation(t, api), "off-2")
	observe(t, pool, a.workspaceID, sent.ID, autofix.Observation{State: autofix.Sent, IssueNumber: 5})
	theirs := b.send(t, pool, b.conversation(t, api), "off-3")
	if response := request(t, api, http.MethodDelete, "/api/v1/autofix/connectors/claude_issue", nil, a.token, nil); response.Code != http.StatusOK {
		t.Fatalf("off: %d %s", response.Code, response.Body)
	}
	state := func(d autofixDesk, id string) string {
		return queryString(t, pool, `SELECT state || '/' || COALESCE(outcome_reason, '') FROM fix_attempts WHERE workspace_id = $1 AND id = $2`, d.workspaceID, id)
	}
	if got := state(a, sending.ID); got != "cancelled/connector_disabled" {
		t.Errorf("not sent yet: %s", got)
	}
	if got := state(a, sent.ID); got != "sent/" {
		t.Errorf("already sent: %s", got)
	}
	if got := state(b, theirs.ID); got != "sending/" {
		t.Errorf("another workspace's fix: %s", got)
	}
}

// cursorDesk is a workspace with a private repository connected through the GitHub stand-in, Cursor's
// key set and Cursor turned on, and the worker of the same process.
type cursorDesk struct {
	fixDesk
	cursor *cursortest.Server
	worker *autofixworker.Worker
}

func newCursorDesk(t *testing.T, name string, installationID, githubRepoID int64) cursorDesk {
	t.Helper()
	fake := githubtest.New()
	server := fake.Start()
	t.Cleanup(server.Close)
	fake.CallbackURL = autofixCallback
	standIn := cursortest.New(cursorCanaryKey)
	t.Cleanup(standIn.Close)
	shared := &autofixworker.Cursor{Base: standIn.URL()}
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		autofixConfig(standIn)(config)
		config.GitHub = fake.Config()
		config.Cursor = shared
	})
	d := githubDesk{api: api, server: api.(*httpapi.Server), service: service, pool: pool, fake: fake}
	w := d.workspace(t, name)
	installation := d.connected(t, w, installationID, "selected")
	fake.AddRepo(installationID, githubtest.Repo{ID: githubRepoID, FullName: account(installationID) + "/shop", Visibility: "private"})
	var repo autofix.Repo
	decode(t, d.connectRepo(t, w.admin, installation, githubRepoID), &repo)
	if response := request(t, api, http.MethodPut, "/api/v1/autofix/connectors/cursor_api/key", map[string]any{"key": cursorCanaryKey}, w.admin, nil); response.Code != http.StatusOK {
		t.Fatalf("key: %d %s", response.Code, response.Body)
	}
	if response := request(t, api, http.MethodPut, "/api/v1/autofix/connectors/cursor_api", map[string]any{"consent_version": autofix.ConsentVersion}, w.admin, nil); response.Code != http.StatusOK {
		t.Fatalf("on: %d %s", response.Code, response.Body)
	}
	return cursorDesk{fixDesk: fixDesk{githubDesk: d, w: w, installation: installation, repo: repo}, cursor: standIn,
		worker: &autofixworker.Worker{Cursor: shared, Repos: d.server.GitHubSync()}}
}

func TestAFixSentToCursorLeavesWithTheWorkerAndACancelStopsItsRun(t *testing.T) {
	c := newCursorDesk(t, "Cursor Send", 61001, 62001)
	s := sendDesk{fixDesk: c.fixDesk}.withConversation(t)
	sent := s.send(t, c.w.admin, autofix.CursorAPI, "cursor-send-1", http.StatusCreated)
	if sent.AuthorizeURL != nil || sent.Fix.State != autofix.Sending {
		t.Fatalf("sent: %+v", sent)
	}
	// The same key answers the same fix, without a second send.
	if again := s.send(t, c.w.admin, autofix.CursorAPI, "cursor-send-1", http.StatusOK); again.Fix.ID != sent.Fix.ID || again.AuthorizeURL != nil {
		t.Fatalf("again: %+v", again)
	}
	if _, err := c.worker.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if fix := c.fix(t, sent.Fix.ID); fix.State != autofix.Sent || fix.VendorURL == nil {
		t.Fatalf("after the worker: %s %v", fix.State, fix.VendorURL)
	}
	creates := c.cursor.Creates()
	if len(creates) != 1 {
		t.Fatalf("creates: %d", len(creates))
	}
	repos, _ := creates[0].Body["repos"].([]any)
	repo, _ := repos[0].(map[string]any)
	if repo["url"] != "https://github.com/"+c.repo.FullName {
		t.Fatalf("repository sent: %v", repo["url"])
	}
	if response := request(t, c.api, http.MethodPost, "/api/v1/autofix/fixes/"+sent.Fix.ID+"/cancel", nil, c.w.admin, nil); response.Code != http.StatusOK {
		t.Fatalf("cancel: %d %s", response.Code, response.Body)
	}
	if cancels := c.cursor.Cancels(); len(cancels) != 1 || cancels[0] != "run-"+sent.Fix.ID {
		t.Fatalf("the run was not cancelled: %v", cancels)
	}
	if fix := c.fix(t, sent.Fix.ID); fix.State != autofix.Cancelled || *fix.OutcomeReason != "cancelled_by_person" {
		t.Fatalf("after the cancel: %s", fix.State)
	}
}

func TestCursorTakesNoFixWithoutItsKey(t *testing.T) {
	c := newCursorDesk(t, "Cursor Keyless", 61002, 62002)
	if response := request(t, c.api, http.MethodDelete, "/api/v1/autofix/connectors/cursor_api/key", nil, c.w.admin, nil); response.Code != http.StatusOK {
		t.Fatalf("remove: %d", response.Code)
	}
	s := sendDesk{fixDesk: c.fixDesk}.withConversation(t)
	response := request(t, c.api, http.MethodPost, "/api/v1/conversations/"+s.conversation+"/fixes", s.sendBody(autofix.CursorAPI, "keyless-1"), c.w.admin, nil)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "cursor_key_missing") {
		t.Fatalf("send without a key: %d %s", response.Code, response.Body)
	}
}

func TestSendingTheSameKeyAgainPutsBackACursorFixWhoseScheduleWasLost(t *testing.T) {
	c := newCursorDesk(t, "Cursor Lost", 61003, 62003)
	s := sendDesk{fixDesk: c.fixDesk}.withConversation(t)
	sent := s.send(t, c.w.admin, autofix.CursorAPI, "cursor-lost-1", http.StatusCreated)
	// The process died between storing the fix and scheduling it.
	if _, err := c.pool.Exec(t.Context(), `UPDATE fix_attempts SET next_check_at = NULL WHERE id = $1`, sent.Fix.ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := c.worker.Tick(t.Context()); n != 0 {
		t.Fatal("an unscheduled fix was leased")
	}
	s.send(t, c.w.admin, autofix.CursorAPI, "cursor-lost-1", http.StatusOK)
	if _, err := c.worker.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if fix := c.fix(t, sent.Fix.ID); fix.State != autofix.Sent || len(c.cursor.Creates()) != 1 {
		t.Fatalf("after the same key: %s, %d creates", fix.State, len(c.cursor.Creates()))
	}
}
