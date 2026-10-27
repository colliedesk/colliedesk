package httpapi_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/autofix"
	"github.com/colliedesk/colliedesk/server/internal/github"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// autofixDesk is one workspace with a connected repository; the routes that connect repositories and send
// fixes come with the GitHub connection, so the tests seed the repository and send through the store.
type autofixDesk struct {
	token, workspaceID, agentID, repoID string
}

var autofixRepoNumber int64

var autofixContacts int

func newAutofixDesk(t *testing.T, api http.Handler, pool *pgxpool.Pool, token string) autofixDesk {
	t.Helper()
	desk := autofixDesk{token: token, workspaceID: workspaceID(t, api, token)}
	var me struct {
		Agent struct {
			ID string `json:"id"`
		} `json:"agent"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/auth/me", nil, token, nil), &me)
	desk.agentID = me.Agent.ID
	autofixRepoNumber++
	if err := pool.QueryRow(context.Background(), `INSERT INTO autofix_repos (workspace_id, github_repo_id, full_name, visibility, default_branch, connected_by_name)
		VALUES ($1, $2, 'acme/shop-web', 'private', 'main', 'Ada') RETURNING id::text`, desk.workspaceID, 70000+autofixRepoNumber).Scan(&desk.repoID); err != nil {
		t.Fatal(err)
	}
	return desk
}

func (d autofixDesk) conversation(t *testing.T, api http.Handler) string {
	t.Helper()
	autofixContacts++
	contact := create(t, api, d.token, "/api/v1/contacts", map[string]any{"name": "Pat", "email": fmt.Sprintf("pat-%d@example.com", autofixContacts)})
	return create(t, api, d.token, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Checkout broken"})
}

func (d autofixDesk) send(t *testing.T, pool *pgxpool.Pool, conversationID, clientKey string) autofix.Fix {
	t.Helper()
	fix, _, err := store.New(pool).CreateFix(context.Background(), d.workspaceID, store.NewFix{
		ConversationID: conversationID, RepoID: d.repoID, Connector: autofix.ClaudeIssue, SentTitle: "Checkout broken",
		SentBody: "The checkout button does nothing.", ClientKey: clientKey, CreatedByAgentID: d.agentID, CreatedByName: "Ada",
	})
	if err != nil {
		t.Fatal(err)
	}
	return fix
}

var githubActor = autofix.Actor{Kind: autofix.ActorGitHub, Name: "octocat"}

func observe(t *testing.T, pool *pgxpool.Pool, workspace, id string, obs autofix.Observation) (autofix.Fix, autofix.Result) {
	t.Helper()
	fix, result, err := store.New(pool).ApplyFixObservation(context.Background(), workspace, id, obs, githubActor)
	if err != nil {
		t.Fatal(err)
	}
	return fix, result
}

func TestFixesAreReadOnlyInTheirOwnWorkspace(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	a := newAutofixDesk(t, api, pool, createSession(t, api, service, "Fix A", "admin@fix-a.example.com"))
	b := newAutofixDesk(t, api, pool, createSession(t, api, service, "Fix B", "admin@fix-b.example.com"))
	conversation := a.conversation(t, api)
	fix := a.send(t, pool, conversation, "key-a")

	for _, path := range []string{"/api/v1/autofix/fixes/" + fix.ID, "/api/v1/conversations/" + conversation + "/fixes", "/api/v1/autofix/fixes/not-a-uuid"} {
		if response := request(t, api, http.MethodGet, path, nil, b.token, nil); response.Code != http.StatusNotFound {
			t.Errorf("B reads %s: status=%d body=%s, want 404", path, response.Code, response.Body.String())
		}
	}
	var page struct {
		Items []autofix.Fix `json:"items"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/autofix/fixes", nil, b.token, nil), &page)
	if len(page.Items) != 0 {
		t.Errorf("B lists %d fixes of A", len(page.Items))
	}
	var status autofix.Status
	decode(t, request(t, api, http.MethodGet, "/api/v1/autofix/status", nil, b.token, nil), &status)
	if status != (autofix.Status{ConnectedRepos: 1}) {
		t.Errorf("B status = %+v, want only its own repository", status)
	}
	// B cannot move A's fix through the store either, nor send for A's conversation on its repository.
	if _, _, err := store.New(pool).ApplyFixObservation(context.Background(), b.workspaceID, fix.ID, autofix.Observation{State: autofix.Sent}, githubActor); !errors.Is(err, autofix.ErrFixNotFound) {
		t.Errorf("B moves A's fix: %v", err)
	}
	if _, _, err := store.New(pool).CreateFix(context.Background(), b.workspaceID, store.NewFix{ConversationID: conversation, RepoID: b.repoID,
		Connector: autofix.ClaudeIssue, SentTitle: "x", SentBody: "x", CreatedByName: "Bo"}); err == nil {
		t.Error("B sent a fix of A's conversation")
	}

	var detail autofix.FixDetail
	decode(t, request(t, api, http.MethodGet, "/api/v1/autofix/fixes/"+fix.ID, nil, a.token, nil), &detail)
	if detail.SentBody != "The checkout button does nothing." || len(detail.Events) != 1 || detail.Events[0].Kind != "created" || detail.State != autofix.Sending {
		t.Errorf("A reads its fix: %+v", detail)
	}
}

// The rules of plan §7.1a, through the one function every source uses, read back over the API.
func TestFixMovesFollowTheRulesAndLeaveATimeline(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	d := newAutofixDesk(t, api, pool, createSession(t, api, service, "Fix Rules", "admin@fix-rules.example.com"))
	ctx := context.Background()
	data := store.New(pool)

	// One fix in progress per conversation; the same client key answers the same fix.
	conversation := d.conversation(t, api)
	first := d.send(t, pool, conversation, "key-1")
	again, created, err := data.CreateFix(ctx, d.workspaceID, store.NewFix{ConversationID: conversation, RepoID: d.repoID, Connector: autofix.CursorAPI,
		SentTitle: "other", SentBody: "other", ClientKey: "key-1", CreatedByName: "Ada"})
	if err != nil || created || again.ID != first.ID {
		t.Fatalf("same key: %v %v %v", again.ID, created, err)
	}
	if _, _, err := data.CreateFix(ctx, d.workspaceID, store.NewFix{ConversationID: conversation, RepoID: d.repoID, Connector: autofix.CursorAPI,
		SentTitle: "other", SentBody: "other", ClientKey: "key-2", CreatedByName: "Ada"}); !errors.Is(err, autofix.ErrFixInProgress) {
		t.Fatalf("second fix in progress: %v", err)
	}

	// The issue was closed by a pull request whose own webhook never came: merged, not cancelled.
	fix, result := observe(t, pool, d.workspaceID, first.ID, autofix.Observation{State: autofix.Sent, IssueNumber: 12, DeliveryID: "d1"})
	if result != autofix.Move || fix.State != autofix.Sent || fix.IssueURL == nil || *fix.IssueURL != "https://github.com/acme/shop-web/issues/12" || fix.RowVersion != 2 {
		t.Fatalf("sent: %v %+v", result, fix)
	}
	fix, result = observe(t, pool, d.workspaceID, first.ID, autofix.Observation{State: autofix.Merged, PRURL: "https://github.com/acme/shop-web/pull/13", PRNumber: 13,
		MergeSHA: "0123456789abcdef0123456789abcdef01234567", Kind: "issue_closed_by_pr"})
	if result != autofix.Move || fix.State != autofix.Merged || fix.MergedAt == nil || fix.PRLinkedBy == nil || *fix.PRLinkedBy != "auto" {
		t.Fatalf("closed by a merged pull request: %v %+v", result, fix)
	}
	if _, err := data.ActOnFix(ctx, d.workspaceID, first.ID, store.FixAction{Action: autofix.ActionDeployed, RowVersion: 1}, autofix.Actor{Kind: autofix.ActorPerson, ID: d.agentID, Name: "Ada"}); !errors.Is(err, autofix.ErrFixChanged) {
		t.Fatalf("a stale row_version: %v", err)
	}
	fix, err = data.ActOnFix(ctx, d.workspaceID, first.ID, store.FixAction{Action: autofix.ActionDeployed}, autofix.Actor{Kind: autofix.ActorPerson, ID: d.agentID, Name: "Ada"})
	if err != nil || fix.State != autofix.Deployed || fix.DeployedByName == nil || fix.Group != autofix.GroupDone {
		t.Fatalf("released: %v %+v", err, fix)
	}
	fix, err = data.ActOnFix(ctx, d.workspaceID, first.ID, store.FixAction{Action: autofix.ActionUndeployed}, autofix.Actor{Kind: autofix.ActorPerson, ID: d.agentID, Name: "Ada"})
	if err != nil || fix.State != autofix.Merged || fix.DeployedAt != nil || fix.Group != autofix.GroupNeedsPerson {
		t.Fatalf("release taken back: %v %+v", err, fix)
	}

	// Cancelled by the person in Colliedesk; the issue then closes: only noted, the reason stays.
	second := d.conversation(t, api)
	cancelled := d.send(t, pool, second, "key-3")
	observe(t, pool, d.workspaceID, cancelled.ID, autofix.Observation{State: autofix.Sent, IssueNumber: 20})
	if _, err := data.ActOnFix(ctx, d.workspaceID, cancelled.ID, store.FixAction{Action: autofix.ActionCancel}, autofix.Actor{Kind: autofix.ActorPerson, ID: d.agentID, Name: "Ada"}); err != nil {
		t.Fatal(err)
	}
	fix, result = observe(t, pool, d.workspaceID, cancelled.ID, autofix.Observation{State: autofix.Cancelled, Reason: "issue_closed", Kind: "issue_closed"})
	if result != autofix.Note || fix.State != autofix.Cancelled || *fix.OutcomeReason != "cancelled_by_person" {
		t.Fatalf("issue closed after the cancel: %v %+v", result, fix)
	}

	// Closed on GitHub, a retry started, then the issue reopens: only noted, the retry keeps the conversation.
	third := d.conversation(t, api)
	closed := d.send(t, pool, third, "key-4")
	observe(t, pool, d.workspaceID, closed.ID, autofix.Observation{State: autofix.Sent, IssueNumber: 30})
	observe(t, pool, d.workspaceID, closed.ID, autofix.Observation{State: autofix.Cancelled, Reason: "issue_closed"})
	retry, _, err := data.CreateFix(ctx, d.workspaceID, store.NewFix{ConversationID: third, RepoID: d.repoID, Connector: autofix.ClaudeIssue, SentTitle: "again",
		SentBody: "again", PreviousAttemptID: closed.ID, CreatedByName: "Ada"})
	if err != nil || retry.AttemptNo != 2 || retry.PreviousAttemptID == nil || *retry.PreviousAttemptID != closed.ID {
		t.Fatalf("retry: %v %+v", err, retry)
	}
	fix, result = observe(t, pool, d.workspaceID, closed.ID, autofix.Observation{State: autofix.Sent, Kind: "issue_reopened"})
	if result != autofix.Note || fix.State != autofix.Cancelled {
		t.Fatalf("reopened with a newer fix: %v %+v", result, fix)
	}
	// With no newer fix in progress, a reopened issue brings the fix back.
	if _, err := data.ActOnFix(ctx, d.workspaceID, retry.ID, store.FixAction{Action: autofix.ActionCancel}, autofix.Actor{Kind: autofix.ActorPerson, ID: d.agentID, Name: "Ada"}); err != nil {
		t.Fatal(err)
	}
	if fix, result = observe(t, pool, d.workspaceID, closed.ID, autofix.Observation{State: autofix.Sent, Kind: "issue_reopened"}); result != autofix.Move || fix.State != autofix.Sent || fix.OutcomeReason != nil {
		t.Fatalf("reopened alone: %v %+v", result, fix)
	}
	if _, _, err := data.CreateFix(ctx, d.workspaceID, store.NewFix{ConversationID: third, RepoID: d.repoID, Connector: autofix.ClaudeIssue, SentTitle: "x",
		SentBody: "x", PreviousAttemptID: closed.ID, CreatedByName: "Ada"}); !errors.Is(err, autofix.ErrMove) && !errors.Is(err, autofix.ErrFixInProgress) {
		t.Fatalf("a retry of a fix in progress: %v", err)
	}

	// Linking a pull request by hand: only one of the fix's repository; unlinking goes back to sent.
	if _, err := data.ActOnFix(ctx, d.workspaceID, closed.ID, store.FixAction{Action: autofix.ActionLinkPR, PRURL: "https://github.com/evil/shop-web/pull/4"},
		autofix.Actor{Kind: autofix.ActorPerson, ID: d.agentID, Name: "Ada"}); !errors.Is(err, autofix.ErrBadPRURL) {
		t.Fatalf("a pull request of another repository: %v", err)
	}
	fix, err = data.ActOnFix(ctx, d.workspaceID, closed.ID, store.FixAction{Action: autofix.ActionLinkPR, PRURL: "https://github.com/ACME/shop-web/pull/31"},
		autofix.Actor{Kind: autofix.ActorPerson, ID: d.agentID, Name: "Ada"})
	if err != nil || fix.State != autofix.PROpen || *fix.PRURL != "https://github.com/acme/shop-web/pull/31" || *fix.PRLinkedBy != "person" {
		t.Fatalf("linked: %v %+v", err, fix)
	}
	if fix, err = data.ActOnFix(ctx, d.workspaceID, closed.ID, store.FixAction{Action: autofix.ActionUnlinkPR}, autofix.Actor{Kind: autofix.ActorPerson, ID: d.agentID, Name: "Ada"}); err != nil || fix.State != autofix.Sent || fix.PRURL != nil {
		t.Fatalf("unlinked: %v %+v", err, fix)
	}

	var detail autofix.FixDetail
	decode(t, request(t, api, http.MethodGet, "/api/v1/autofix/fixes/"+closed.ID, nil, d.token, nil), &detail)
	var kinds []string
	for _, event := range detail.Events {
		kinds = append(kinds, event.Kind)
	}
	if got := strings.Join(kinds, ","); got != "created,state_changed,state_changed,issue_reopened,issue_reopened,link_pr,unlink_pr" {
		t.Errorf("timeline = %s", got)
	}
	if _, err := pool.Exec(ctx, `UPDATE fix_attempt_events SET kind = 'rewritten' WHERE fix_attempt_id = $1`, closed.ID); err == nil {
		t.Error("an event was rewritten: the timeline is append-only")
	}
}

func TestFixesListByGroupWithStaleSendsNeedingAPerson(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	d := newAutofixDesk(t, api, pool, createSession(t, api, service, "Fix Groups", "admin@fix-groups.example.com"))
	running := d.send(t, pool, d.conversation(t, api), "g1")
	stale := d.send(t, pool, d.conversation(t, api), "g2")
	observe(t, pool, d.workspaceID, stale.ID, autofix.Observation{State: autofix.Sent, IssueNumber: 1})
	if _, err := pool.Exec(context.Background(), `UPDATE fix_attempts SET state_changed_at = now() - interval '31 minutes' WHERE id = $1`, stale.ID); err != nil {
		t.Fatal(err)
	}
	failed := d.send(t, pool, d.conversation(t, api), "g3")
	observe(t, pool, d.workspaceID, failed.ID, autofix.Observation{State: autofix.Failed, Reason: "no_repo_access"})

	var status autofix.Status
	decode(t, request(t, api, http.MethodGet, "/api/v1/autofix/status", nil, d.token, nil), &status)
	if status != (autofix.Status{ConnectedRepos: 1, NeedsPerson: 2, Running: 1}) {
		t.Errorf("status = %+v", status)
	}
	type page struct {
		Items      []autofix.Fix `json:"items"`
		NextCursor *string       `json:"next_cursor"`
	}
	var needs page
	decode(t, request(t, api, http.MethodGet, "/api/v1/autofix/fixes?group=needs_person", nil, d.token, nil), &needs)
	if len(needs.Items) != 2 || needs.Items[0].ID != failed.ID || needs.Items[1].ID != stale.ID || !needs.Items[1].Stale {
		t.Errorf("needs_person = %+v", needs.Items)
	}
	var first, second page
	decode(t, request(t, api, http.MethodGet, "/api/v1/autofix/fixes?limit=2", nil, d.token, nil), &first)
	if len(first.Items) != 2 || first.NextCursor == nil {
		t.Fatalf("first page = %+v", first)
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/autofix/fixes?limit=2&cursor="+*first.NextCursor, nil, d.token, nil), &second)
	if len(second.Items) != 1 || second.Items[0].ID != running.ID || second.NextCursor != nil {
		t.Errorf("second page = %+v", second)
	}
	for _, query := range []string{"group=stuck", "limit=0", "limit=101", "cursor=nope"} {
		if response := request(t, api, http.MethodGet, "/api/v1/autofix/fixes?"+query, nil, d.token, nil); response.Code != http.StatusBadRequest {
			t.Errorf("%s: status=%d", query, response.Code)
		}
	}
}

func TestConfigSaysWhetherAutofixIsAvailable(t *testing.T) {
	type config struct {
		Autofix struct {
			Available       bool     `json:"available"`
			MissingSettings []string `json:"missing_settings"`
		} `json:"autofix"`
	}
	api, _ := testAPI(t)
	var off config
	decode(t, request(t, api, http.MethodGet, "/api/v1/config", nil, "", nil), &off)
	if off.Autofix.Available || len(off.Autofix.MissingSettings) != 6 || off.Autofix.MissingSettings[0] != github.EnvAppID {
		t.Errorf("without the App: %+v", off.Autofix)
	}
	on, _, _, _ := testAPIWithConfig(t, func(c *httpapi.Config) {
		c.GitHub = github.ConfigFromEnv(func(name string) string { return "set-" + name })
	})
	var configured config
	decode(t, request(t, on, http.MethodGet, "/api/v1/config", nil, "", nil), &configured)
	if !configured.Autofix.Available || len(configured.Autofix.MissingSettings) != 0 {
		t.Errorf("with the App: %+v", configured.Autofix)
	}
}

// seedContractFix gives the contract cases a merged fix with its timeline.
func seedContractFix(t *testing.T, api http.Handler, pool *pgxpool.Pool, token, conversationID string) string {
	t.Helper()
	d := newAutofixDesk(t, api, pool, token)
	fix := d.send(t, pool, conversationID, "contract")
	observe(t, pool, d.workspaceID, fix.ID, autofix.Observation{State: autofix.Sent, IssueNumber: 5, VendorURL: "https://github.com/acme/shop-web/issues/5"})
	observe(t, pool, d.workspaceID, fix.ID, autofix.Observation{State: autofix.Merged, PRURL: "https://github.com/acme/shop-web/pull/6", PRNumber: 6,
		Branch: "claude/issue-5", MergeSHA: "0123456789abcdef0123456789abcdef01234567", MergedBy: "octocat"})
	return fix.ID
}

// The export carries every fix with exactly what it sent and its timeline, the repositories and the
// coding agents' consent; never a key.
func TestExportCarriesFixesAndTheirTimelinesButNoKey(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	d := newAutofixDesk(t, api, pool, createSession(t, api, service, "Fix Export", "admin@fix-export.example.com"))
	fix := d.send(t, pool, d.conversation(t, api), "export")
	observe(t, pool, d.workspaceID, fix.ID, autofix.Observation{State: autofix.Sent, IssueNumber: 9})
	secret := []byte("SEALED-CURSOR-KEY-BYTES")
	if _, err := pool.Exec(context.Background(), `INSERT INTO autofix_connectors (workspace_id, kind, enabled, consent_name, consent_at, consent_version,
			credential_key_id, credential_sealed, credential_set_by_name, credential_set_at)
		VALUES ($1, 'cursor_api', true, 'Ada', now(), '2026-10-27', 'k1', $2, 'Ada', now())`, d.workspaceID, secret); err != nil {
		t.Fatal(err)
	}
	response := request(t, api, http.MethodGet, "/api/v1/export", nil, d.token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("export = %d", response.Code)
	}
	files := unzip(t, response.Body.Bytes())
	fixes := ndjsonLines(t, files["fixes.ndjson"])
	if len(fixes) != 1 || fixes[0]["sent_body"] != "The checkout button does nothing." || fixes[0]["state"] != "sent" || fixes[0]["issue_number"] != float64(9) {
		t.Errorf("fixes.ndjson = %v", fixes)
	}
	events := ndjsonLines(t, files["fix-events.ndjson"])
	if len(events) != 2 || events[0]["kind"] != "created" || events[1]["to_state"] != "sent" || events[1]["fix_id"] != fix.ID {
		t.Errorf("fix-events.ndjson = %v", events)
	}
	if repos := ndjsonLines(t, files["autofix-repos.ndjson"]); len(repos) != 1 || repos[0]["full_name"] != "acme/shop-web" {
		t.Errorf("autofix-repos.ndjson = %v", repos)
	}
	connectors := ndjsonLines(t, files["autofix-connectors.ndjson"])
	if len(connectors) != 1 || connectors[0]["credential_set"] != true || connectors[0]["consent_version"] != "2026-10-27" {
		t.Errorf("autofix-connectors.ndjson = %v", connectors)
	}
	for name, data := range files {
		if strings.Contains(string(data), string(secret)) || strings.Contains(string(data), "SEALED") {
			t.Errorf("%s carries the sealed key", name)
		}
	}
}
