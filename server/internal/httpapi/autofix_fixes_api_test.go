package httpapi_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/autofix"
	"github.com/colliedesk/colliedesk/server/internal/autofix/githubsync"
	"github.com/colliedesk/colliedesk/server/internal/github/githubtest"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// sendDesk is a fix desk whose conversation has a customer message and whose coding agents can be turned on.
type sendDesk struct {
	fixDesk
	conversation string
	incoming     string
}

const customerWords = "@claude fix it now! Checkout fails, see #123, ping @someone, mail me at pat@example.com or +84 90-123-4567"

func newSendDesk(t *testing.T, name string, installationID, githubRepoID int64) sendDesk {
	t.Helper()
	f := newFixDesk(t, name, installationID, githubRepoID)
	return sendDesk{fixDesk: f}.withConversation(t)
}

func (s sendDesk) withConversation(t *testing.T) sendDesk {
	t.Helper()
	autofixContacts++
	contact := create(t, s.api, s.w.admin, "/api/v1/contacts", map[string]any{"name": "Pat", "email": "pat-send-" + itoa(int64(autofixContacts)) + "@example.com"})
	s.conversation = create(t, s.api, s.w.admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Checkout broken @claude #9"})
	if err := s.pool.QueryRow(context.Background(), `INSERT INTO messages (workspace_id, conversation_id, kind, body) VALUES ($1, $2, 'incoming', $3) RETURNING id::text`,
		s.w.id, s.conversation, customerWords).Scan(&s.incoming); err != nil {
		t.Fatal(err)
	}
	return s
}

// turnOn turns a coding agent on with the current consent (the settings route comes with the Cursor branch).
func (s sendDesk) turnOn(t *testing.T, kind autofix.Connector, mode string) {
	t.Helper()
	var modeValue any
	if mode != "" {
		modeValue = mode
	}
	if _, err := s.pool.Exec(context.Background(), `INSERT INTO autofix_connectors (workspace_id, kind, enabled, mode, consent_name, consent_at, consent_version)
		VALUES ($1, $2, true, $3, 'Ada', now(), $4) ON CONFLICT (workspace_id, kind) DO UPDATE SET enabled = true, mode = $3, consent_version = $4`,
		s.w.id, string(kind), modeValue, autofix.ConsentVersion); err != nil {
		t.Fatal(err)
	}
}

func (s sendDesk) preview(t *testing.T, session string, connector autofix.Connector) map[string]any {
	t.Helper()
	path := "/api/v1/conversations/" + s.conversation + "/fixes/preview"
	body := map[string]any{"repo_id": s.repo.ID, "connector": connector}
	response := checked(t, s.api, http.MethodPost, path, body, session, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", response.Code, response.Body.String())
	}
	validateContractResponse(t, newRequest(t, http.MethodPost, path, body, session, nil), response)
	var preview map[string]any
	decode(t, response, &preview)
	return preview
}

type fixSentView struct {
	Fix          autofix.Fix `json:"fix"`
	AuthorizeURL *string     `json:"authorize_url"`
}

func (s sendDesk) sendBody(connector autofix.Connector, key string) map[string]any {
	return map[string]any{"repo_id": s.repo.ID, "connector": connector, "title": "Checkout broken", "body": "The checkout button does nothing.\n\n" + customerWords,
		"message_ids": []string{s.incoming}, "client_key": key}
}

func (s sendDesk) send(t *testing.T, session string, connector autofix.Connector, key string, want int) fixSentView {
	t.Helper()
	path := "/api/v1/conversations/" + s.conversation + "/fixes"
	response := checked(t, s.api, http.MethodPost, path, s.sendBody(connector, key), session, nil)
	if response.Code != want {
		t.Fatalf("send status=%d body=%s, want %d", response.Code, response.Body.String(), want)
	}
	validateContractResponse(t, newRequest(t, http.MethodPost, path, s.sendBody(connector, key), session, nil), response)
	var sent fixSentView
	decode(t, response, &sent)
	return sent
}

// authorize plays login confirming on GitHub and the browser coming back with session's cookie; it waits
// for the send to finish and returns where the browser went.
func (s sendDesk) authorize(t *testing.T, address, login, session string) string {
	t.Helper()
	target, err := s.fake.Authorize(address, login)
	if err != nil {
		t.Fatal(err)
	}
	location := s.callback(t, strings.TrimPrefix(target, testOrigin), sessionCookieOf(session))
	s.server.WaitForAutofixSends()
	return location
}

// noTokenLeft checks that every user token GitHub issued was revoked and none is in the database.
func (s sendDesk) noTokenLeft(t *testing.T) {
	t.Helper()
	for _, token := range s.fake.UserTokens() {
		if !s.fake.Revoked(token) {
			t.Fatalf("a GitHub user token was not revoked")
		}
	}
	var leaked int
	if err := s.pool.QueryRow(context.Background(), `SELECT count(*) FROM (
			SELECT to_jsonb(f)::text AS row FROM fix_attempts f UNION ALL SELECT to_jsonb(e)::text FROM fix_attempt_events e
			UNION ALL SELECT to_jsonb(o)::text FROM autofix_oauth_states o UNION ALL SELECT to_jsonb(r)::text FROM agent_requests r) rows
		WHERE row LIKE '%ghu\_%'`).Scan(&leaked); err != nil {
		t.Fatal(err)
	}
	if leaked != 0 {
		t.Fatalf("%d rows hold a GitHub user token", leaked)
	}
}

func liveMention(text string) string {
	for _, live := range []string{"@claude", "@someone", "#123", "#9", "pat@example.com", "90-123-4567"} {
		if strings.Contains(text, live) {
			return live
		}
	}
	return ""
}

func TestAClaudeFixIsCreatedAsThePersonWithOnlyColliedesksMentionLive(t *testing.T) {
	s := newSendDesk(t, "Send Claude", 201, 2001)
	s.turnOn(t, autofix.ClaudeIssue, "")

	preview := s.preview(t, s.w.admin, autofix.ClaudeIssue)
	if live := liveMention(preview["title"].(string) + preview["body"].(string)); live != "" {
		t.Fatalf("the preview keeps %q live: %v", live, preview)
	}
	if !strings.Contains(preview["body"].(string), `<customer-message untrusted="true" from="Pat"`) {
		t.Fatalf("the customer's words are not wrapped: %s", preview["body"])
	}

	sent := s.send(t, s.w.admin, autofix.ClaudeIssue, "key-1", http.StatusCreated)
	if sent.Fix.State != autofix.Sending || sent.AuthorizeURL == nil || !strings.Contains(*sent.AuthorizeURL, "code_challenge_method=S256") {
		t.Fatalf("send = %+v", sent)
	}
	if live := liveMention(sent.Fix.SentTitle); live != "" {
		t.Fatalf("the stored title keeps %q live", live)
	}
	// The same key while it waits answers the fix and a new confirmation address.
	again := s.send(t, s.w.admin, autofix.ClaudeIssue, "key-1", http.StatusOK)
	if again.Fix.ID != sent.Fix.ID || again.AuthorizeURL == nil {
		t.Fatalf("same key = %+v", again)
	}

	location := s.authorize(t, *again.AuthorizeURL, "ada", s.w.admin)
	if location != "/conversations/"+s.conversation+"?fix="+sent.Fix.ID {
		t.Fatalf("back to %s", location)
	}
	fix := s.fix(t, sent.Fix.ID)
	if fix.State != autofix.Sent || fix.IssueNumber == nil || fix.GitHubLogin == nil || *fix.GitHubLogin != "ada" {
		t.Fatalf("fix = %+v", fix.Fix)
	}
	issues := s.fake.CreatedIssues(s.repo.GitHubRepoID)
	if len(issues) != 1 || issues[0].Author != "ada" || !strings.HasSuffix(issues[0].Body, githubsync.Marker(fix.ID)) {
		t.Fatalf("issues = %+v", issues)
	}
	if live := liveMention(issues[0].Title + issues[0].Body); live != "" {
		t.Fatalf("the issue keeps %q live: %s", live, issues[0].Body)
	}
	if len(issues[0].Comments) != 1 || issues[0].Comments[0].Author != "ada" || issues[0].Comments[0].Body != githubsync.ClaudeTrigger {
		t.Fatalf("comments = %+v", issues[0].Comments)
	}
	s.noTokenLeft(t)

	// Sent: the same key answers the fix, with nothing to confirm.
	if done := s.send(t, s.w.admin, autofix.ClaudeIssue, "key-1", http.StatusOK); done.AuthorizeURL != nil || done.Fix.State != autofix.Sent {
		t.Fatalf("same key once sent = %+v", done)
	}
	// The state was used: the same redirect again changes nothing.
	if second := s.authorize(t, *again.AuthorizeURL, "ada", s.w.admin); !strings.Contains(second, "github_error=expired") {
		t.Fatalf("second use of the state: %s", second)
	}
	if issues := s.fake.CreatedIssues(s.repo.GitHubRepoID); len(issues) != 1 {
		t.Fatalf("%d issues", len(issues))
	}

	// Cancel: the issue is closed with a comment by the App.
	response := checked(t, s.api, http.MethodPost, "/api/v1/autofix/fixes/"+fix.ID+"/cancel", map[string]any{"row_version": fix.RowVersion}, s.w.admin, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("cancel %d %s", response.Code, response.Body.String())
	}
	closed := s.fake.CreatedIssues(s.repo.GitHubRepoID)[0]
	if closed.State != "closed" || len(closed.Comments) != 2 || closed.Comments[1].Author != "app" {
		t.Fatalf("closed issue = %+v", closed)
	}
}

func TestCopilotIsAssignedOrLeftToAssignOnGitHub(t *testing.T) {
	s := newSendDesk(t, "Send Copilot", 202, 2002)
	s.turnOn(t, autofix.CopilotIssue, "")

	// Copilot cannot be assigned on this repository: nothing is created.
	sent := s.send(t, s.w.admin, autofix.CopilotIssue, "c-1", http.StatusCreated)
	s.authorize(t, *sent.AuthorizeURL, "ada", s.w.admin)
	if fix := s.fix(t, sent.Fix.ID); fix.State != autofix.Failed || *fix.OutcomeReason != "copilot_unavailable" {
		t.Fatalf("no Copilot: %s %v", fix.State, fix.OutcomeReason)
	}
	if issues := s.fake.CreatedIssues(s.repo.GitHubRepoID); len(issues) != 0 {
		t.Fatalf("an issue was left: %+v", issues)
	}

	s.fake.SetCopilot(s.repo.GitHubRepoID, true)
	retried := s.retry(t, sent.Fix.ID, autofix.CopilotIssue, "c-2", http.StatusCreated)
	if retried.Fix.PreviousAttemptID == nil || *retried.Fix.PreviousAttemptID != sent.Fix.ID || retried.Fix.AttemptNo != 2 {
		t.Fatalf("retry = %+v", retried.Fix)
	}
	s.authorize(t, *retried.AuthorizeURL, "ada", s.w.admin)
	fix := s.fix(t, retried.Fix.ID)
	if fix.State != autofix.Sent || fix.CopilotAssignment == nil || *fix.CopilotAssignment != "assigned" {
		t.Fatalf("assigned: %+v", fix.Fix)
	}
	issues := s.fake.CreatedIssues(s.repo.GitHubRepoID)
	if len(issues) != 1 || len(issues[0].Assignees) != 1 || len(issues[0].Comments) != 0 {
		t.Fatalf("issues = %+v", issues)
	}
	// Retrying a fix in progress is refused.
	if response := checked(t, s.api, http.MethodPost, "/api/v1/autofix/fixes/"+fix.ID+"/retry", s.sendBody(autofix.CopilotIssue, "c-3"), s.w.admin, nil); response.Code != http.StatusConflict ||
		!strings.Contains(response.Body.String(), "fix_not_ended") {
		t.Fatalf("retry in progress: %d %s", response.Code, response.Body.String())
	}

	// Assigned by a person on GitHub: the issue is created and left for them.
	checked(t, s.api, http.MethodPost, "/api/v1/autofix/fixes/"+fix.ID+"/cancel", nil, s.w.admin, nil)
	s.turnOn(t, autofix.CopilotIssue, "assign_on_github")
	manual := s.retry(t, fix.ID, autofix.CopilotIssue, "c-4", http.StatusCreated)
	s.authorize(t, *manual.AuthorizeURL, "ada", s.w.admin)
	if got := s.fix(t, manual.Fix.ID); got.State != autofix.Sent || *got.CopilotAssignment != "on_github" {
		t.Fatalf("on GitHub: %+v", got.Fix)
	}
	if issues := s.fake.CreatedIssues(s.repo.GitHubRepoID); len(issues) != 2 || len(issues[1].Assignees) != 0 {
		t.Fatalf("issues = %+v", issues)
	}
	// Assigned through the API but GitHub leaves Copilot out: the issue stays, for a person to assign.
	checked(t, s.api, http.MethodPost, "/api/v1/autofix/fixes/"+manual.Fix.ID+"/cancel", nil, s.w.admin, nil)
	s.turnOn(t, autofix.CopilotIssue, "assign_api")
	s.fake.Fail("POST /repos/"+s.repo.FullName+"/issues/3/assignees", http.StatusForbidden)
	refused := s.retry(t, manual.Fix.ID, autofix.CopilotIssue, "c-5", http.StatusCreated)
	s.authorize(t, *refused.AuthorizeURL, "ada", s.w.admin)
	if got := s.fix(t, refused.Fix.ID); got.State != autofix.Sent || *got.CopilotAssignment != "on_github" {
		t.Fatalf("refused assignment: %+v", got.Fix)
	}
	s.noTokenLeft(t)
}

func (s sendDesk) retry(t *testing.T, fixID string, connector autofix.Connector, key string, want int) fixSentView {
	t.Helper()
	path := "/api/v1/autofix/fixes/" + fixID + "/retry"
	response := checked(t, s.api, http.MethodPost, path, s.sendBody(connector, key), s.w.admin, nil)
	if response.Code != want {
		t.Fatalf("retry status=%d body=%s", response.Code, response.Body.String())
	}
	validateContractResponse(t, newRequest(t, http.MethodPost, path, s.sendBody(connector, key), s.w.admin, nil), response)
	var sent fixSentView
	decode(t, response, &sent)
	return sent
}

func TestAFixIsCheckedBeforeAnythingIsWrittenOnGitHub(t *testing.T) {
	s := newSendDesk(t, "Send Checks", 203, 2003)
	s.turnOn(t, autofix.ClaudeIssue, "")
	cases := []struct {
		name, login, reason string
		state               autofix.State
		before              func()
	}{
		{"cannot write", "bo", "no_repo_access", autofix.Failed, func() {
			s.fake.Update(203, func(i *githubtest.Installation) { i.Users = append(i.Users, "bo") })
			s.fake.SetAccess(s.repo.GitHubRepoID, "bo", "read")
		}},
		{"not visible to the person", "cy", "no_repo_access", autofix.Failed, func() {}},
		{"turned public", "ada", "repo_public", autofix.Failed, func() { s.fake.SetVisibility(s.repo.GitHubRepoID, "public") }},
		{"agent turned off", "ada", "connector_disabled", autofix.Cancelled, func() {
			if _, err := s.pool.Exec(context.Background(), `UPDATE autofix_connectors SET enabled = false WHERE workspace_id = $1`, s.w.id); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for i, c := range cases {
		s.fake.SetVisibility(s.repo.GitHubRepoID, "private")
		if _, err := s.pool.Exec(context.Background(), `UPDATE autofix_repos SET paused_reason = NULL, paused_at = NULL, visibility = 'private' WHERE workspace_id = $1`, s.w.id); err != nil {
			t.Fatal(err)
		}
		s.turnOn(t, autofix.ClaudeIssue, "")
		sent := s.send(t, s.w.admin, autofix.ClaudeIssue, "check-"+itoa(int64(i)), http.StatusCreated)
		c.before()
		s.authorize(t, *sent.AuthorizeURL, c.login, s.w.admin)
		fix := s.fix(t, sent.Fix.ID)
		if fix.State != c.state || fix.OutcomeReason == nil || *fix.OutcomeReason != c.reason {
			t.Fatalf("%s: %s %v", c.name, fix.State, fix.OutcomeReason)
		}
		if issues := s.fake.CreatedIssues(s.repo.GitHubRepoID); len(issues) != 0 {
			t.Fatalf("%s: an issue was created", c.name)
		}
	}
	s.noTokenLeft(t)

	// Saying no on GitHub cancels it; nothing left Colliedesk.
	s.turnOn(t, autofix.ClaudeIssue, "")
	sent := s.send(t, s.w.admin, autofix.ClaudeIssue, "denied", http.StatusCreated)
	state := stateOf(t, *sent.AuthorizeURL)
	if location := s.callback(t, "/api/v1/autofix/github/callback?error=access_denied&state="+state, sessionCookieOf(s.w.admin)); location != "/conversations/"+s.conversation+"?fix_error=denied" {
		t.Fatalf("denied: %s", location)
	}
	if fix := s.fix(t, sent.Fix.ID); fix.State != autofix.Cancelled || *fix.OutcomeReason != "authorization_denied" {
		t.Fatalf("denied: %s %v", fix.State, fix.OutcomeReason)
	}

	// Another person of the workspace cannot finish someone's send.
	second := s.send(t, s.w.admin, autofix.ClaudeIssue, "other-person", http.StatusCreated)
	colleague := joinAsAgent(t, s.api, s.w.admin, "colleague@send-checks.example.com")
	target, err := s.fake.Authorize(*second.AuthorizeURL, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if location := s.callback(t, strings.TrimPrefix(target, testOrigin), sessionCookieOf(colleague)); !strings.Contains(location, "fix_error=wrong_person") {
		t.Fatalf("another person: %s", location)
	}
	s.server.WaitForAutofixSends()
	if fix := s.fix(t, second.Fix.ID); fix.State != autofix.Sending {
		t.Fatalf("another person moved it: %s", fix.State)
	}

	// Cursor is not turned on in this workspace.
	if response := checked(t, s.api, http.MethodPost, "/api/v1/conversations/"+s.conversation+"/fixes", s.sendBody(autofix.CursorAPI, "cursor"), s.w.admin, nil); response.Code != http.StatusConflict ||
		!strings.Contains(response.Body.String(), "connector_off") {
		t.Fatalf("cursor: %d %s", response.Code, response.Body.String())
	}
}

func stateOf(t *testing.T, address string) string {
	t.Helper()
	parsed, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	return parsed.Query().Get("state")
}

func TestASendLeftWaitingEndsAndAnInterruptedOneFindsItsIssue(t *testing.T) {
	s := newSendDesk(t, "Send Expiry", 204, 2004)
	s.turnOn(t, autofix.ClaudeIssue, "")
	ctx := context.Background()
	reconcile := func() {
		t.Helper()
		if err := s.server.GitHubSync().Reconcile(ctx); err != nil {
			t.Fatal(err)
		}
	}
	age := func(id, dispatch string) {
		t.Helper()
		if _, err := s.pool.Exec(ctx, `UPDATE fix_attempts SET created_at = now() - interval '20 minutes', dispatch_started_at = `+dispatch+` WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
	}

	// Never confirmed on GitHub.
	abandoned := s.send(t, s.w.admin, autofix.ClaudeIssue, "abandoned", http.StatusCreated).Fix
	reconcile()
	if fix := s.fix(t, abandoned.ID); fix.State != autofix.Sending {
		t.Fatalf("fresh: %s", fix.State)
	}
	age(abandoned.ID, "NULL")
	reconcile()
	if fix := s.fix(t, abandoned.ID); fix.State != autofix.Cancelled || *fix.OutcomeReason != "authorization_abandoned" {
		t.Fatalf("abandoned: %s %v", fix.State, fix.OutcomeReason)
	}

	// The process died after creating the issue: the marked issue is found, never created again.
	found := s.send(t, s.w.admin, autofix.ClaudeIssue, "found", http.StatusCreated).Fix
	number := s.fake.AddCreatedIssue(s.repo.GitHubRepoID, "ada", "Checkout broken", "body\n\n"+githubsync.Marker(found.ID))
	age(found.ID, "now() - interval '19 minutes'")
	reconcile()
	fix := s.fix(t, found.ID)
	if fix.State != autofix.Sent || fix.IssueNumber == nil || *fix.IssueNumber != number || *fix.GitHubLogin != "ada" {
		t.Fatalf("found: %+v", fix.Fix)
	}
	if issues := s.fake.CreatedIssues(s.repo.GitHubRepoID); len(issues) != 1 {
		t.Fatalf("%d issues", len(issues))
	}
	checked(t, s.api, http.MethodPost, "/api/v1/autofix/fixes/"+found.ID+"/cancel", nil, s.w.admin, nil)

	// A send still running when the fix is 15 minutes old is left to finish.
	running := s.send(t, s.w.admin, autofix.ClaudeIssue, "running", http.StatusCreated).Fix
	age(running.ID, "now()")
	reconcile()
	if fix := s.fix(t, running.ID); fix.State != autofix.Sending {
		t.Fatalf("a send still running: %s %v", fix.State, fix.OutcomeReason)
	}
	checked(t, s.api, http.MethodPost, "/api/v1/autofix/fixes/"+running.ID+"/cancel", nil, s.w.admin, nil)

	// It died before: no issue, the fix failed interrupted.
	lost := s.send(t, s.w.admin, autofix.ClaudeIssue, "lost", http.StatusCreated).Fix
	age(lost.ID, "now() - interval '19 minutes'")
	reconcile()
	if fix := s.fix(t, lost.ID); fix.State != autofix.Failed || *fix.OutcomeReason != "interrupted" {
		t.Fatalf("lost: %s %v", fix.State, fix.OutcomeReason)
	}
	if issues := s.fake.CreatedIssues(s.repo.GitHubRepoID); len(issues) != 1 {
		t.Fatalf("%d issues", len(issues))
	}
}

func TestAnAgentTokensSendWaitsForAPersonWhoConfirmsAsThemselves(t *testing.T) {
	s := newSendDesk(t, "Send Token", 205, 2005)
	s.turnOn(t, autofix.ClaudeIssue, "")
	token := issueToken(t, s.api, s.w.admin, "Triage agent", "send").Token

	// Reading and previewing are the token's; sending is a request.
	s.preview(t, token, autofix.ClaudeIssue)
	asked := accepted(t, s.api, http.MethodPost, "/api/v1/conversations/"+s.conversation+"/fixes", s.sendBody(autofix.ClaudeIssue, "token-1"), token)
	approved := approve(t, s.api, s.w.admin, asked.RequestID, nil)
	if approved.Code != http.StatusOK || !strings.Contains(approved.Body.String(), "/login/oauth/authorize") {
		t.Fatalf("approve: %d %s", approved.Code, approved.Body.String())
	}
	if stored := getRequest(t, s.api, token, asked.RequestID); strings.Contains(string(stored.Result), "oauth") || strings.Contains(string(stored.Result), "state=") {
		t.Fatalf("the token can read the address: %s", stored.Result)
	}
	var answer struct {
		Result struct {
			Fix          autofix.Fix `json:"fix"`
			AuthorizeURL string      `json:"authorize_url"`
		} `json:"result"`
	}
	decode(t, approved, &answer)
	if answer.Result.Fix.CreatedByKind != "token" || answer.Result.Fix.ApprovedRequestID == nil || *answer.Result.Fix.ApprovedRequestID != asked.RequestID ||
		answer.Result.Fix.CreatedByName == "" {
		t.Fatalf("fix = %+v", answer.Result.Fix)
	}
	s.authorize(t, answer.Result.AuthorizeURL, "ada", s.w.admin)
	if fix := s.fix(t, answer.Result.Fix.ID); fix.State != autofix.Sent || *fix.GitHubLogin != "ada" {
		t.Fatalf("approved send: %+v", fix.Fix)
	}
	s.noTokenLeft(t)

	// A draft token cancels and marks; a read token asks.
	draft := issueToken(t, s.api, s.w.admin, "Drafter", "draft").Token
	if response := checked(t, s.api, http.MethodPost, "/api/v1/autofix/fixes/"+answer.Result.Fix.ID+"/cancel", nil, draft, nil); response.Code != http.StatusOK {
		t.Fatalf("draft cancel: %d %s", response.Code, response.Body.String())
	}
	read := issueToken(t, s.api, s.w.admin, "Reader", "read").Token
	accepted(t, s.api, http.MethodPut, "/api/v1/autofix/fixes/"+answer.Result.Fix.ID+"/deployed", nil, read)
}

func TestAPersonsFixChangesReleaseAndPullRequests(t *testing.T) {
	s := newSendDesk(t, "Send Changes", 206, 2006)
	fix := s.sent(t, 7)
	path := "/api/v1/autofix/fixes/" + fix.ID
	change := func(method, rest string, body any, want int) autofix.Fix {
		t.Helper()
		response := checked(t, s.api, method, path+rest, body, s.w.admin, nil)
		if response.Code != want {
			t.Fatalf("%s %s: %d %s", method, rest, response.Code, response.Body.String())
		}
		if want == http.StatusOK {
			validateContractResponse(t, newRequest(t, method, path+rest, body, s.w.admin, nil), response)
		}
		var got autofix.Fix
		decode(t, response, &got)
		return got
	}
	change(http.MethodPut, "/pull-request", map[string]any{"url": "https://github.com/other/repo/pull/2"}, http.StatusBadRequest)
	linked := change(http.MethodPut, "/pull-request", map[string]any{"url": "https://github.com/" + s.repo.FullName + "/pull/2"}, http.StatusOK)
	if linked.State != autofix.PROpen || *linked.PRLinkedBy != "person" {
		t.Fatalf("linked: %+v", linked)
	}
	change(http.MethodPut, "/deployed", map[string]any{"row_version": linked.RowVersion}, http.StatusConflict)
	if unlinked := change(http.MethodDelete, "/pull-request", nil, http.StatusOK); unlinked.State != autofix.Sent || unlinked.PRURL != nil {
		t.Fatalf("unlinked: %+v", unlinked)
	}
	observe(t, s.pool, s.w.id, fix.ID, autofix.Observation{State: autofix.Merged, PRNumber: 2, PRURL: "https://github.com/" + s.repo.FullName + "/pull/2"})
	stale := s.fix(t, fix.ID).RowVersion - 1
	change(http.MethodPut, "/deployed", map[string]any{"row_version": stale}, http.StatusConflict)
	if deployed := change(http.MethodPut, "/deployed", nil, http.StatusOK); deployed.State != autofix.Deployed || deployed.DeployedByName == nil {
		t.Fatalf("deployed: %+v", deployed)
	}
	if back := change(http.MethodDelete, "/deployed", nil, http.StatusOK); back.State != autofix.Merged || back.DeployedAt != nil {
		t.Fatalf("taken back: %+v", back)
	}
	change(http.MethodPost, "/cancel", nil, http.StatusConflict)
}

func TestNoWorkspaceSendsOrChangesAnotherWorkspacesFix(t *testing.T) {
	a := newSendDesk(t, "Send Iso A", 207, 2007)
	a.turnOn(t, autofix.ClaudeIssue, "")
	fix := a.sent(t, 4)
	pending := a.withConversation(t).send(t, a.w.admin, autofix.ClaudeIssue, "iso", http.StatusCreated)
	b := a.workspace(t, "Send Iso B")
	for _, c := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/api/v1/conversations/" + a.conversation + "/fixes/preview", map[string]any{"repo_id": a.repo.ID, "connector": "claude_issue"}},
		{http.MethodPost, "/api/v1/conversations/" + a.conversation + "/fixes", a.sendBody(autofix.ClaudeIssue, "b-1")},
		{http.MethodPost, "/api/v1/autofix/fixes/" + fix.ID + "/retry", a.sendBody(autofix.ClaudeIssue, "b-2")},
		{http.MethodPost, "/api/v1/autofix/fixes/" + fix.ID + "/cancel", nil},
		{http.MethodPut, "/api/v1/autofix/fixes/" + fix.ID + "/deployed", nil},
		{http.MethodDelete, "/api/v1/autofix/fixes/" + fix.ID + "/deployed", nil},
		{http.MethodPut, "/api/v1/autofix/fixes/" + fix.ID + "/pull-request", map[string]any{"url": "https://github.com/" + a.repo.FullName + "/pull/1"}},
		{http.MethodDelete, "/api/v1/autofix/fixes/" + fix.ID + "/pull-request", nil},
	} {
		if response := checked(t, a.api, c.method, c.path, c.body, b.admin, nil); response.Code != http.StatusNotFound {
			t.Fatalf("%s %s from B: %d %s", c.method, c.path, response.Code, response.Body.String())
		}
	}
	// B's own conversation cannot use A's repository.
	contact := create(t, a.api, b.admin, "/api/v1/contacts", map[string]any{"name": "Kim", "email": "kim@send-iso-b.example.com"})
	conversation := create(t, a.api, b.admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "B"})
	if response := checked(t, a.api, http.MethodPost, "/api/v1/conversations/"+conversation+"/fixes/preview", map[string]any{"repo_id": a.repo.ID, "connector": "claude_issue"}, b.admin, nil); response.Code != http.StatusNotFound {
		t.Fatalf("A's repository from B: %d %s", response.Code, response.Body.String())
	}
	// A's confirmation state finishes nothing for B's person.
	target, err := a.fake.Authorize(*pending.AuthorizeURL, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if location := a.callback(t, strings.TrimPrefix(target, testOrigin), sessionCookieOf(b.admin)); !strings.Contains(location, "github_error=expired") {
		t.Fatalf("B finishing A's send: %s", location)
	}
	a.server.WaitForAutofixSends()
	if got := a.fix(t, pending.Fix.ID); got.State != autofix.Sending {
		t.Fatalf("B moved A's fix: %s", got.State)
	}
}

// A person who cancels while the issue is being created keeps the cancel; the issue is still recorded,
// so its address shows (the sender closes it).
func TestAnIssueCreatedAfterACancelKeepsTheCancel(t *testing.T) {
	s := newSendDesk(t, "Send Race", 208, 2008)
	s.turnOn(t, autofix.ClaudeIssue, "")
	sent := s.send(t, s.w.admin, autofix.ClaudeIssue, "race", http.StatusCreated)
	checked(t, s.api, http.MethodPost, "/api/v1/autofix/fixes/"+sent.Fix.ID+"/cancel", nil, s.w.admin, nil)
	fix, result, err := store.New(s.pool).RecordFixIssue(context.Background(), s.w.id, sent.Fix.ID, store.IssueCreated{Number: 9, URL: "https://github.com/" + s.repo.FullName + "/issues/9",
		GitHubUserID: 1001, GitHubLogin: "ada"}, autofix.Actor{Kind: autofix.ActorSystem, Name: "Colliedesk"})
	if err != nil {
		t.Fatal(err)
	}
	if result != autofix.Note || fix.State != autofix.Cancelled || *fix.OutcomeReason != "cancelled_by_person" || fix.IssueNumber == nil || *fix.IssueNumber != 9 {
		t.Fatalf("after the cancel: %v %+v", result, fix)
	}
	// The callback of a cancelled fix sends nothing.
	if location := s.authorize(t, *sent.AuthorizeURL, "ada", s.w.admin); !strings.Contains(location, "fix_error=expired") {
		t.Fatalf("callback after cancel: %s", location)
	}
	if issues := s.fake.CreatedIssues(s.repo.GitHubRepoID); len(issues) != 0 {
		t.Fatalf("issues = %+v", issues)
	}
}

// An issue GitHub would not close leaves the cancel standing and says so on the fix's timeline.
func TestACancelWhoseIssueStaysOpenSaysSo(t *testing.T) {
	s := newSendDesk(t, "Send Close", 209, 2009)
	fix := s.sent(t, 4)
	s.fake.Fail("POST /repos/"+s.repo.FullName+"/issues/4/comments", http.StatusServiceUnavailable)
	if response := checked(t, s.api, http.MethodPost, "/api/v1/autofix/fixes/"+fix.ID+"/cancel", nil, s.w.admin, nil); response.Code != http.StatusOK {
		t.Fatalf("cancel: %d %s", response.Code, response.Body.String())
	}
	detail := s.fix(t, fix.ID)
	last := detail.Events[len(detail.Events)-1]
	if detail.State != autofix.Cancelled || last.Kind != "issue_close_failed" || last.Data["issue_url"] != "https://github.com/"+s.repo.FullName+"/issues/4" {
		t.Fatalf("timeline: %s %+v", detail.State, last)
	}
}
