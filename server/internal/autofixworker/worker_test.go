package autofixworker_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/autofix"
	"github.com/colliedesk/colliedesk/server/internal/autofix/githubsync"
	"github.com/colliedesk/colliedesk/server/internal/autofixworker"
	"github.com/colliedesk/colliedesk/server/internal/cursor"
	"github.com/colliedesk/colliedesk/server/internal/cursor/cursortest"
	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	keyA = "crsr_canary-key-A-DO-NOT-PRINT-7c1e"
	keyB = "crsr_canary-key-B-DO-NOT-PRINT-2d9f"
)

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *clock) Add(d time.Duration) { c.mu.Lock(); c.now = c.now.Add(d); c.mu.Unlock() }

// repos stands in for the GitHub connection: every repository is private unless the test says otherwise.
type repos struct {
	mu         sync.Mutex
	visibility map[string]string
	err        error
	calls      int
	// during runs while the repository is read, before the fix leaves (a person cancelling meanwhile).
	during func()
}

func (r *repos) CheckRepo(_ context.Context, _, repoID string) (autofix.RepoCheck, error) {
	r.mu.Lock()
	r.calls++
	during, err := r.during, r.err
	visibility := r.visibility[repoID]
	r.mu.Unlock()
	if during != nil {
		during()
	}
	if err != nil {
		return autofix.RepoCheck{}, err
	}
	if visibility == "" {
		visibility = "private"
	}
	return autofix.RepoCheck{Visibility: visibility, FullName: "acme/shop-web", DefaultBranch: "main"}, nil
}

type desk struct {
	id, agentID, repoID string
}

type env struct {
	pool   *pgxpool.Pool
	store  *store.Store
	clock  *clock
	cursor *cursortest.Server
	repos  *repos
	worker *autofixworker.Worker
	a, b   desk
	n      int
}

func ring(t *testing.T) tokenseal.Keys {
	t.Helper()
	raw := make([]byte, tokenseal.KeySize)
	_, _ = rand.Read(raw)
	keys, err := tokenseal.ParseKeys("k1:" + base64.StdEncoding.EncodeToString(raw))
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	pool := databasetest.Pool(t)
	service := auth.New(pool, "https://desk.example.com")
	e := &env{pool: pool, store: store.New(pool).WithSealKeys(ring(t)), clock: &clock{now: time.Now().UTC()},
		cursor: cursortest.New(keyA, keyB), repos: &repos{visibility: map[string]string{}}}
	t.Cleanup(e.cursor.Close)
	var made []desk
	for index, name := range []string{"Fix Worker A", "Fix Worker B"} {
		ws, _, err := service.CreateWorkspace(ctx, name, fmt.Sprintf("admin@worker-%d.example.com", index))
		if err != nil {
			t.Fatal(err)
		}
		d := desk{id: ws.ID}
		if err := pool.QueryRow(ctx, `SELECT id::text FROM agents WHERE workspace_id = $1`, ws.ID).Scan(&d.agentID); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO autofix_repos (workspace_id, github_repo_id, full_name, visibility, default_branch, connected_by_name)
			VALUES ($1, $2, 'acme/shop-web', 'private', 'main', 'Ada') RETURNING id::text`, ws.ID, 81000+index).Scan(&d.repoID); err != nil {
			t.Fatal(err)
		}
		made = append(made, d)
	}
	e.a, e.b = made[0], made[1]
	for _, item := range []struct {
		d   desk
		key string
	}{{e.a, keyA}, {e.b, keyB}} {
		ada := autofix.Actor{Kind: autofix.ActorPerson, ID: item.d.agentID, Name: "Ada"}
		if _, err := e.store.SetCursorKey(ctx, item.d.id, item.key, ada); err != nil {
			t.Fatal(err)
		}
		if _, err := e.store.EnableFixConnector(ctx, item.d.id, autofix.CursorAPI, autofix.ConsentVersion, ada); err != nil {
			t.Fatal(err)
		}
	}
	e.worker = &autofixworker.Worker{Cursor: &autofixworker.Cursor{Store: e.store, Base: e.cursor.URL(), Now: e.clock.Now}, Repos: e.repos}
	return e
}

// send stores a Cursor fix and asks the worker to send it now, as the send route does.
func (e *env) send(t *testing.T, d desk) autofix.Fix {
	t.Helper()
	ctx := context.Background()
	e.n++
	contact, err := e.store.CreateContact(ctx, d.id, "Pat", fmt.Sprintf("pat-%d@example.com", e.n))
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := e.store.CreateConversation(ctx, d.id, contact.ID, "Checkout broken", "")
	if err != nil {
		t.Fatal(err)
	}
	fix, _, err := e.store.CreateFix(ctx, d.id, store.NewFix{ConversationID: conversation.ID, RepoID: d.repoID, Connector: autofix.CursorAPI,
		SentTitle: "Checkout broken", SentBody: "The checkout button does nothing.", CreatedByAgentID: d.agentID, CreatedByName: "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.store.ScheduleFixCheck(ctx, d.id, fix.ID, e.clock.Now()); err != nil {
		t.Fatal(err)
	}
	return fix
}

func (e *env) tick(t *testing.T) int {
	t.Helper()
	n, err := e.worker.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *env) work(t *testing.T, d desk, id string) store.FixWork {
	t.Helper()
	work, err := e.store.GetFixWork(context.Background(), d.id, id)
	if err != nil {
		t.Fatal(err)
	}
	return work
}

func (e *env) nextCheck(t *testing.T, id string) *time.Time {
	t.Helper()
	var next *time.Time
	if err := e.pool.QueryRow(context.Background(), `SELECT next_check_at FROM fix_attempts WHERE id = $1`, id).Scan(&next); err != nil {
		t.Fatal(err)
	}
	return next
}

func reasonOf(work store.FixWork) string {
	if work.OutcomeReason == nil {
		return ""
	}
	return *work.OutcomeReason
}

func TestACursorFixIsSentFollowedToItsPullRequestAndThenLeftToGitHub(t *testing.T) {
	e := newEnv(t)
	e.cursor.Script(cursortest.Step{Status: "RUNNING"},
		cursortest.Step{Status: "FINISHED", Result: "Fixed the checkout button.", Branch: "cursor/fix-checkout", PRURL: "https://github.com/acme/shop-web/pull/42"})
	fix := e.send(t, e.a)
	if e.tick(t) != 1 {
		t.Fatal("the due fix was not leased")
	}
	sent := e.work(t, e.a, fix.ID)
	if sent.State != autofix.Sent || sent.VendorRef != "bc-"+fix.ID || sent.VendorRunID != "run-"+fix.ID || sent.VendorURL == nil ||
		*sent.VendorURL != "https://cursor.com/agents/bc-"+fix.ID || sent.DispatchStartedAt == nil {
		t.Fatalf("after the create: %+v", sent)
	}
	creates := e.cursor.Creates()
	if len(creates) != 1 || creates[0].Key != keyA {
		t.Fatalf("creates: %+v", creates)
	}
	body := creates[0].Body
	repos, _ := body["repos"].([]any)
	repo, _ := repos[0].(map[string]any)
	prompt, _ := body["prompt"].(map[string]any)
	if body["agentId"] != "bc-"+fix.ID || body["autoCreatePR"] != true || repo["url"] != "https://github.com/acme/shop-web" || repo["startingRef"] != "main" ||
		!strings.Contains(prompt["text"].(string), "The checkout button does nothing.") {
		t.Fatalf("create body: %+v", body)
	}
	if _, withEnv := body["envVars"]; withEnv {
		t.Fatal("envVars cannot go with a client agent id")
	}
	if e.tick(t) != 0 {
		t.Fatal("a fix was read again before its next check")
	}
	e.clock.Add(16 * time.Second)
	e.tick(t)
	if work := e.work(t, e.a, fix.ID); work.State != autofix.Working {
		t.Fatalf("a running run: %s", work.State)
	}
	e.clock.Add(16 * time.Second)
	e.tick(t)
	done := e.work(t, e.a, fix.ID)
	if done.State != autofix.PROpen || done.PRURL == nil || *done.PRURL != "https://github.com/acme/shop-web/pull/42" || done.PRNumber == nil || *done.PRNumber != 42 ||
		done.Branch == nil || *done.Branch != "cursor/fix-checkout" || done.PRLinkedBy == nil || *done.PRLinkedBy != "auto" {
		t.Fatalf("a finished run with its pull request: %+v", done)
	}
	if next := e.nextCheck(t, fix.ID); next != nil {
		t.Fatalf("Cursor is done, GitHub follows the merge; next check %v", next)
	}
	detail, err := e.store.GetFix(context.Background(), e.a.id, fix.ID)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, event := range detail.Events {
		kinds = append(kinds, event.Kind+":"+string(event.ActorKind))
	}
	if strings.Join(kinds, ",") != "created:person,dispatched:cursor,state_changed:cursor,state_changed:cursor" {
		t.Fatalf("timeline: %v", kinds)
	}
}

func TestASecondCreateOfTheSameFixReadsTheAgentBackAndSendsNothingTwice(t *testing.T) {
	e := newEnv(t)
	fix := e.send(t, e.a)
	// A process created the agent and died before it stored the answer.
	if _, err := (cursor.Client{Base: e.cursor.URL(), Key: keyA}).CreateAgent(context.Background(),
		cursor.NewAgent{ID: "bc-" + fix.ID, Name: "x", Prompt: "x", RepoURL: "https://github.com/acme/shop-web"}); err != nil {
		t.Fatal(err)
	}
	e.tick(t)
	work := e.work(t, e.a, fix.ID)
	if work.State != autofix.Sent || work.VendorRunID != "run-"+fix.ID || e.cursor.Agents() != 1 {
		t.Fatalf("after a 409: %+v, %d agents", work, e.cursor.Agents())
	}
}

func TestCursorRefusalsEndTheFixWithTheirReason(t *testing.T) {
	for _, item := range []struct {
		name    string
		failure cursortest.Failure
		reason  string
	}{
		{"unknown key", cursortest.Failure{Status: 401, Message: "Invalid API key"}, "vendor_auth"},
		{"repository not reached", cursortest.Failure{Status: 400, Code: "repository_access", Message: "Cursor cannot reach the repository"}, "vendor_repo_access"},
		{"forbidden", cursortest.Failure{Status: 403, Message: "No access"}, "vendor_repo_access"},
		{"invalid", cursortest.Failure{Status: 422, Code: "validation_error", Message: "prompt too long"}, "vendor_rejected"},
	} {
		t.Run(item.name, func(t *testing.T) {
			e := newEnv(t)
			fix := e.send(t, e.a)
			e.cursor.Fail("create", item.failure)
			e.tick(t)
			work := e.work(t, e.a, fix.ID)
			if work.State != autofix.Failed || reasonOf(work) != item.reason || e.nextCheck(t, fix.ID) != nil {
				t.Fatalf("%s: %s %s", item.name, work.State, reasonOf(work))
			}
			detail, _ := e.store.GetFix(context.Background(), e.a.id, fix.ID)
			last := detail.Events[len(detail.Events)-1]
			if summary, _ := last.Data["summary"].(string); item.failure.Message != "" && !strings.Contains(summary, item.failure.Message) {
				t.Errorf("the timeline does not say what Cursor said: %+v", last.Data)
			}
			if item.reason == "vendor_auth" {
				connector, _ := e.store.GetFixConnector(context.Background(), e.a.id, autofix.CursorAPI)
				if connector.LastCheck == nil || connector.LastCheck.Result != autofix.CheckKeyInvalid {
					t.Errorf("the settings do not show the key refused: %+v", connector.LastCheck)
				}
			}
		})
	}
}

func TestCursorDownIsTriedFiveTimesThenTheFixFails(t *testing.T) {
	e := newEnv(t)
	fix := e.send(t, e.a)
	for range 5 {
		e.cursor.Fail("create", cursortest.Failure{Status: 503, Message: "busy"})
	}
	for try := 1; try <= 5; try++ {
		e.tick(t)
		work := e.work(t, e.a, fix.ID)
		if try < 5 && (work.State != autofix.Sending || work.CheckFailures != try) {
			t.Fatalf("try %d: %s with %d failures", try, work.State, work.CheckFailures)
		}
		e.clock.Add(11 * time.Minute)
	}
	work := e.work(t, e.a, fix.ID)
	if work.State != autofix.Failed || reasonOf(work) != "provider_unavailable" || e.cursor.Calls("create") != 5 {
		t.Fatalf("after five: %s %s, %d creates", work.State, reasonOf(work), e.cursor.Calls("create"))
	}
}

func TestTooManyRequestsWaitsWhatCursorAsksWithoutCountingAFailure(t *testing.T) {
	e := newEnv(t)
	fix := e.send(t, e.a)
	e.cursor.Fail("create", cursortest.Failure{Status: 429, Message: "Rate limit exceeded", RetryAfter: 90})
	e.tick(t)
	work := e.work(t, e.a, fix.ID)
	next := e.nextCheck(t, fix.ID)
	if work.State != autofix.Sending || work.CheckFailures != 0 || next == nil || next.Sub(e.clock.Now()) < 89*time.Second {
		t.Fatalf("after a 429: %s, %d failures, next %v", work.State, work.CheckFailures, next)
	}
	e.clock.Add(91 * time.Second)
	e.tick(t)
	if work := e.work(t, e.a, fix.ID); work.State != autofix.Sent {
		t.Fatalf("after the wait: %s", work.State)
	}
}

func TestNothingLeavesForARepositoryThatIsNotPrivateOrNotReachedOrWithoutAKey(t *testing.T) {
	e := newEnv(t)
	public := e.send(t, e.a)
	e.repos.visibility[e.a.repoID] = "public"
	e.tick(t)
	if work := e.work(t, e.a, public.ID); work.State != autofix.Failed || reasonOf(work) != "repo_public" {
		t.Fatalf("public: %s %s", work.State, reasonOf(work))
	}
	e.repos.visibility[e.a.repoID] = "private"
	e.repos.err = autofix.ErrRepoUnreachable
	unreachable := e.send(t, e.a)
	e.tick(t)
	if work := e.work(t, e.a, unreachable.ID); work.State != autofix.Failed || reasonOf(work) != "no_repo_access" {
		t.Fatalf("unreachable: %s %s", work.State, reasonOf(work))
	}
	e.repos.err = autofix.ErrRepoNotFound
	gone := e.send(t, e.a)
	e.tick(t)
	if work := e.work(t, e.a, gone.ID); work.State != autofix.Cancelled || reasonOf(work) != "repo_disconnected" {
		t.Fatalf("disconnected meanwhile: %s %s", work.State, reasonOf(work))
	}
	e.repos.err = nil
	if _, err := e.store.RemoveCursorKey(context.Background(), e.a.id); err != nil {
		t.Fatal(err)
	}
	keyless := e.send(t, e.a)
	e.tick(t)
	if work := e.work(t, e.a, keyless.ID); work.State != autofix.Failed || reasonOf(work) != "vendor_auth" {
		t.Fatalf("no key: %s %s", work.State, reasonOf(work))
	}
	if len(e.cursor.Creates()) != 0 {
		t.Fatalf("something left: %+v", e.cursor.Creates())
	}
}

func TestAFixCancelledWhileItIsSentStopsTheRunItGot(t *testing.T) {
	e := newEnv(t)
	fix := e.send(t, e.a)
	person := autofix.Actor{Kind: autofix.ActorPerson, ID: e.a.agentID, Name: "Ada"}
	e.repos.during = func() {
		if _, err := e.store.ActOnFix(context.Background(), e.a.id, fix.ID, store.FixAction{Action: autofix.ActionCancel}, person); err != nil {
			t.Error(err)
		}
	}
	e.tick(t)
	work := e.work(t, e.a, fix.ID)
	if work.State != autofix.Cancelled || reasonOf(work) != "cancelled_by_person" {
		t.Fatalf("a person's cancel was undone: %s %s", work.State, reasonOf(work))
	}
	if cancels := e.cursor.Cancels(); len(cancels) != 1 || cancels[0] != "run-"+fix.ID {
		t.Fatalf("the run created meanwhile was not stopped: %v", cancels)
	}
	if work.VendorRef != "bc-"+fix.ID || work.VendorRunID != "run-"+fix.ID {
		t.Fatalf("what was created is not findable: %q %q", work.VendorRef, work.VendorRunID)
	}
}

func TestWaitingForTheBudgetOrRetryAfterKeepsTheFailuresInARow(t *testing.T) {
	e := newEnv(t)
	fix := e.send(t, e.a)
	e.cursor.Fail("create", cursortest.Failure{Status: 503}, cursortest.Failure{Status: 429, RetryAfter: 30}, cursortest.Failure{Status: 503})
	for range 3 {
		e.tick(t)
		e.clock.Add(11 * time.Minute)
	}
	if work := e.work(t, e.a, fix.ID); work.CheckFailures != 2 {
		t.Fatalf("a 429 between two 5xx restarted the count: %d", work.CheckFailures)
	}
}

func TestAPullRequestOfTheRepositoryRenamedMeanwhileIsTheFixs(t *testing.T) {
	e := newEnv(t)
	e.cursor.Script(cursortest.Step{Status: "FINISHED", PRURL: "https://github.com/acme/storefront/pull/7"})
	fix := e.send(t, e.a)
	e.tick(t)
	if _, err := e.pool.Exec(context.Background(), `UPDATE autofix_repos SET full_name = 'acme/storefront' WHERE id = $1`, e.a.repoID); err != nil {
		t.Fatal(err)
	}
	e.clock.Add(16 * time.Second)
	e.tick(t)
	if work := e.work(t, e.a, fix.ID); work.State != autofix.PROpen || work.PRURL == nil || *work.PRURL != "https://github.com/acme/storefront/pull/7" {
		t.Fatalf("renamed: %s %v", work.State, work.PRURL)
	}
}

func TestCancellingASentFixCancelsItsRunAndARunAlreadyEndedIsFine(t *testing.T) {
	e := newEnv(t)
	fix := e.send(t, e.a)
	e.tick(t)
	person := autofix.Actor{Kind: autofix.ActorPerson, ID: e.a.agentID, Name: "Ada"}
	if _, err := e.store.ActOnFix(context.Background(), e.a.id, fix.ID, store.FixAction{Action: autofix.ActionCancel}, person); err != nil {
		t.Fatal(err)
	}
	if err := e.worker.Cursor.Cancel(context.Background(), e.a.id, fix.ID); err != nil {
		t.Fatal(err)
	}
	if cancels := e.cursor.Cancels(); len(cancels) != 1 || cancels[0] != "run-"+fix.ID {
		t.Fatalf("cancels: %v", cancels)
	}
	if err := e.worker.Cursor.Cancel(context.Background(), e.a.id, fix.ID); err != nil {
		t.Fatalf("a run already ended: %v", err)
	}
	e.clock.Add(time.Minute)
	if e.tick(t) != 0 {
		t.Fatal("a cancelled fix is still read")
	}
	if work := e.work(t, e.a, fix.ID); work.State != autofix.Cancelled {
		t.Fatalf("the run's end changed a person's cancel: %s", work.State)
	}
}

func TestRunEndsAreReadForWhatTheyAre(t *testing.T) {
	for _, item := range []struct {
		step   cursortest.Step
		state  autofix.State
		reason string
	}{
		{cursortest.Step{Status: "ERROR", Result: "Could not build."}, autofix.Failed, "agent_error"},
		{cursortest.Step{Status: "EXPIRED"}, autofix.Failed, "timed_out"},
		{cursortest.Step{Status: "CANCELLED"}, autofix.Cancelled, "cancelled_by_person"},
		{cursortest.Step{Status: "FINISHED", Result: "Nothing to change."}, autofix.Failed, "agent_error"},
		// A pull request of another repository is not this fix's.
		{cursortest.Step{Status: "FINISHED", PRURL: "https://github.com/other/repo/pull/1"}, autofix.Failed, "agent_error"},
	} {
		t.Run(item.step.Status, func(t *testing.T) {
			e := newEnv(t)
			e.cursor.Script(item.step)
			fix := e.send(t, e.a)
			e.tick(t)
			e.clock.Add(16 * time.Second)
			e.tick(t)
			work := e.work(t, e.a, fix.ID)
			if work.State != item.state || reasonOf(work) != item.reason || work.PRURL != nil || e.nextCheck(t, fix.ID) != nil {
				t.Fatalf("%+v: %s %s pr %v", item.step, work.State, reasonOf(work), work.PRURL)
			}
		})
	}
}

func TestEachWorkspaceSendsWithItsOwnKeyAndAKeyCopiedAcrossDoesNotOpen(t *testing.T) {
	e := newEnv(t)
	e.send(t, e.a)
	e.send(t, e.b)
	e.tick(t)
	keys := map[string]bool{}
	for _, created := range e.cursor.Creates() {
		keys[created.Key] = true
	}
	if !keys[keyA] || !keys[keyB] || len(keys) != 2 {
		t.Fatalf("keys used: %v", keys)
	}
	ctx := context.Background()
	if _, err := e.pool.Exec(ctx, `UPDATE autofix_connectors b SET credential_key_id = a.credential_key_id, credential_sealed = a.credential_sealed
		FROM autofix_connectors a WHERE a.workspace_id = $1 AND a.kind = 'cursor_api' AND b.workspace_id = $2 AND b.kind = 'cursor_api'`, e.a.id, e.b.id); err != nil {
		t.Fatal(err)
	}
	if key, err := e.store.OpenCursorKey(ctx, e.b.id); err == nil || errors.Is(err, autofix.ErrCursorKeyMissing) || key != "" {
		t.Fatalf("A's sealed key opened in B: %q %v", key, err)
	}
	// Nothing in the database holds a key in clear.
	var found bool
	if err := e.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM autofix_connectors c WHERE position($1 in c::text) > 0 OR position($2 in c::text) > 0)
		OR EXISTS (SELECT 1 FROM fix_attempt_events v WHERE position($1 in v::text) > 0 OR position($2 in v::text) > 0)`, keyA, keyB).Scan(&found); err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("a Cursor key is stored in clear")
	}
}

func TestTheWorkerKeepsToItsCallBudgetPerWorkspace(t *testing.T) {
	e := newEnv(t)
	var fixes []autofix.Fix
	for range 12 {
		fixes = append(fixes, e.send(t, e.a))
	}
	other := e.send(t, e.b)
	e.tick(t)
	if calls := e.cursor.Calls("create"); calls != 11 {
		t.Fatalf("creates in the first minute: %d, want 10 of A and 1 of B", calls)
	}
	if work := e.work(t, e.b, other.ID); work.State != autofix.Sent {
		t.Fatalf("A's backlog held back B: %s", work.State)
	}
	waiting := 0
	for _, fix := range fixes {
		if work := e.work(t, e.a, fix.ID); work.State == autofix.Sending && work.CheckFailures == 0 {
			waiting++
		}
	}
	if waiting != 2 {
		t.Fatalf("waiting for the budget: %d", waiting)
	}
	// Each later minute A spends at most 10 calls, reads of its sent runs included, and the two
	// waiting fixes still leave.
	for minute := 1; minute <= 5 && e.cursor.Calls("create") < 13; minute++ {
		before := e.cursor.Calls("create") + e.cursor.Calls("run")
		e.clock.Add(61 * time.Second)
		e.tick(t)
		if spent := e.cursor.Calls("create") + e.cursor.Calls("run") - before; spent > 11 {
			t.Fatalf("minute %d: %d calls", minute, spent)
		}
	}
	if calls := e.cursor.Calls("create"); calls != 13 {
		t.Fatalf("the waiting fixes never left: %d creates", calls)
	}
}

func TestNothingLeavesOnceCursorIsTurnedOffOrItsConsentIsOld(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	off := e.send(t, e.a)
	if _, err := e.store.DisableFixConnector(ctx, e.a.id, autofix.CursorAPI); err != nil {
		t.Fatal(err)
	}
	e.tick(t)
	if work := e.work(t, e.a, off.ID); work.State != autofix.Cancelled || reasonOf(work) != "connector_disabled" || e.nextCheck(t, off.ID) != nil {
		t.Fatalf("turned off: %s %s", work.State, reasonOf(work))
	}
	old := e.send(t, e.b)
	if _, err := e.pool.Exec(ctx, `UPDATE autofix_connectors SET consent_version = '2020-01-01' WHERE workspace_id = $1`, e.b.id); err != nil {
		t.Fatal(err)
	}
	e.tick(t)
	if work := e.work(t, e.b, old.ID); work.State != autofix.Cancelled || reasonOf(work) != "connector_disabled" {
		t.Fatalf("an old consent: %s %s", work.State, reasonOf(work))
	}
	if len(e.cursor.Creates()) != 0 {
		t.Fatalf("sent anyway: %d", len(e.cursor.Creates()))
	}
}

func TestAFixWaitsWhileItsInstallationIsSuspendedAndLeavesOnceUnsuspended(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.pool.Exec(ctx, `INSERT INTO autofix_github_installations (workspace_id, installation_id, account_login, account_type, repository_selection, status, connected_by_name)
		VALUES ($1, 88001, 'acme', 'Organization', 'selected', 'suspended', 'Ada')`, e.a.id); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, `UPDATE autofix_repos SET installation_id = 88001, paused_reason = 'installation_suspended', paused_at = now() WHERE id = $1`, e.a.repoID); err != nil {
		t.Fatal(err)
	}
	fix := e.send(t, e.a)
	e.repos.err = autofix.ErrRepoUnreachable
	e.tick(t)
	if work := e.work(t, e.a, fix.ID); work.State != autofix.Sending || work.CheckFailures != 0 || len(e.cursor.Creates()) != 0 {
		t.Fatalf("suspended: %s %s, %d failures, %d creates", work.State, reasonOf(work), work.CheckFailures, len(e.cursor.Creates()))
	}
	// Unsuspended: the repository is reached again and the fix leaves.
	e.repos.err = nil
	e.clock.Add(6 * time.Minute)
	e.tick(t)
	if work := e.work(t, e.a, fix.ID); work.State != autofix.Sent {
		t.Fatalf("unsuspended: %s %s", work.State, reasonOf(work))
	}
}

// A repository disconnected in Colliedesk (by a person, or by GitHub's webhook or the reconcile pass)
// cancels its fixes; a Cursor run they started is stopped too, and one that cannot be stopped says so.
func TestARepositoryDisconnectedStopsTheCursorRunsOfItsFixes(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	byPerson := e.send(t, e.a)
	e.tick(t)
	ids, err := e.store.DisconnectAutofixRepo(ctx, e.a.id, e.a.repoID)
	if err != nil {
		t.Fatal(err)
	}
	githubsync.EndFixes(ctx, e.store, e.a.id, ids, autofix.Cancelled, "repo_disconnected", autofix.Actor{Kind: autofix.ActorPerson, ID: e.a.agentID, Name: "Ada"})
	e.clock.Add(time.Minute)
	e.tick(t)
	if cancels := e.cursor.Cancels(); len(cancels) != 1 || cancels[0] != "run-"+byPerson.ID {
		t.Fatalf("the run of a fix whose repository was disconnected goes on: cancels %v", cancels)
	}
	if work := e.work(t, e.a, byPerson.ID); work.State != autofix.Cancelled || reasonOf(work) != "repo_disconnected" {
		t.Fatalf("the cancel changed: %s %s", work.State, reasonOf(work))
	}
	if next := e.nextCheck(t, byPerson.ID); next != nil {
		t.Fatalf("still checked after the run was stopped: %v", next)
	}

	// Removed from the App on GitHub, with the key removed meanwhile: the run cannot be stopped, and the fix says so.
	byGitHub := e.send(t, e.b)
	e.tick(t)
	if _, err := e.store.RemoveCursorKey(ctx, e.b.id); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.store.ApplyFixObservation(ctx, e.b.id, byGitHub.ID, autofix.Observation{State: autofix.Cancelled, Reason: "repo_disconnected", Kind: "repo_disconnected"},
		autofix.Actor{Kind: autofix.ActorGitHub, Name: "octocat"}); err != nil {
		t.Fatal(err)
	}
	e.clock.Add(time.Minute)
	e.tick(t)
	detail, err := e.store.GetFix(ctx, e.b.id, byGitHub.ID)
	if err != nil {
		t.Fatal(err)
	}
	noted := false
	for _, event := range detail.Events {
		noted = noted || event.Kind == "cursor_cancel_failed"
	}
	if !noted {
		t.Fatal("a run left going is not on the fix's timeline")
	}
	if next := e.nextCheck(t, byGitHub.ID); next != nil {
		t.Fatalf("a run that cannot be stopped is checked forever: %v", next)
	}
	if e.clock.Add(time.Hour); e.tick(t) != 0 {
		t.Fatal("ended fixes are still leased")
	}
}
