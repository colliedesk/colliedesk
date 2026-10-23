package ai_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5"
)

// clock is a settable time for the service and worker.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *clock) Add(d time.Duration) { c.mu.Lock(); c.now = c.now.Add(d); c.mu.Unlock() }

type queueEnv struct {
	*env
	clock  *clock
	worker *ai.Worker
	runs   []ai.Task
	mu     sync.Mutex
}

var testServer = &ai.ServerProvider{Kind: aiprovider.KindAnthropic, Label: "Colliedesk AI", APIKey: "server-key",
	Models: map[string][]string{ai.SlotTriage: {"claude-haiku-5-5"}, ai.SlotDraft: {"claude-sonnet-5-5"}},
	Prices: map[string]ai.Price{"claude-haiku-5-5": {Input: 0.1, Output: 0.5}, "claude-sonnet-5-5": {Input: 2, Output: 10}}}

// newQueueEnv has a draft handler that sends one request and records the task it ran.
func newQueueEnv(t *testing.T, mutate func(*ai.Config)) *queueEnv {
	t.Helper()
	q := &queueEnv{clock: &clock{now: time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)}}
	q.env = newEnv(t, func(config *ai.Config) {
		config.Server = testServer
		if mutate != nil {
			mutate(config)
		}
	})
	q.service.SetClock(q.clock.Now)
	q.worker = ai.NewWorker(q.service, database.New(q.pool), ai.WorkerOptions{})
	handler := ai.HandlerFunc(func(ctx context.Context, run *ai.Run) error {
		q.mu.Lock()
		q.runs = append(q.runs, run.Task)
		q.mu.Unlock()
		_, err := run.Metered(ctx, aiprovider.Request{System: "test", Messages: []aiprovider.Message{{Role: aiprovider.RoleUser, Text: "hello"}}, MaxOutput: 100})
		return err
	})
	q.worker.Handle(ai.SlotDraft, handler)
	q.worker.Handle(ai.SlotTriage, handler)
	return q
}

func (q *queueEnv) turnOn(t *testing.T, w workspace, slot string) {
	t.Helper()
	if _, err := q.service.SetSlot(context.Background(), w.id, q.actor(w), slot, ai.SlotInput{Source: ai.SourceServer, Enabled: true}); err != nil {
		t.Fatal(err)
	}
}

func (q *queueEnv) enqueue(t *testing.T, w workspace, task ai.NewTask) string {
	t.Helper()
	var id string
	err := database.New(q.pool).InWorkspace(context.Background(), w.id, func(tx pgx.Tx) error {
		var err error
		id, err = q.service.Enqueue(context.Background(), tx, w.id, task)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (q *queueEnv) tick(t *testing.T) {
	t.Helper()
	if err := q.worker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	q.worker.Wait()
}

func (q *queueEnv) state(t *testing.T, id string) (string, string) {
	t.Helper()
	var state string
	var code *string
	if err := q.pool.QueryRow(context.Background(), `SELECT state, error_code FROM ai_tasks WHERE id=$1`, id).Scan(&state, &code); err != nil {
		t.Fatal(err)
	}
	if code == nil {
		return state, ""
	}
	return state, *code
}

func TestATaskRunsOnItsFeatureAndItsCallIsMetered(t *testing.T) {
	q := newQueueEnv(t, nil)
	q.turnOn(t, q.a, ai.SlotDraft)
	id := q.enqueue(t, q.a, ai.NewTask{Kind: ai.SlotDraft, Actor: q.actor(q.a)})
	q.tick(t)
	if state, _ := q.state(t, id); state != ai.TaskDone {
		t.Fatalf("task state = %s", state)
	}
	if got := q.fake.Requests(); len(got) != 1 || got[0].Model != "claude-sonnet-5-5" || got[0].Effort != aiprovider.EffortLow {
		t.Fatalf("requests = %+v", got)
	}
	// The fake reports 10 input and 5 output tokens: 10×2 + 5×10 micro-dollars on Sonnet's prices.
	if n := q.count(t, `SELECT count(*) FROM ai_usage WHERE task_id=$1 AND feature='draft' AND billed_to='server' AND NOT automatic AND cost_micro_usd=70 AND actor_agent_id=$2`, id, q.a.agentID); n != 1 {
		t.Fatalf("usage rows of the task = %d", n)
	}
}

func TestAFeatureTurnedOffCancelsItsTasksWithoutACall(t *testing.T) {
	q := newQueueEnv(t, nil)
	id := q.enqueue(t, q.a, ai.NewTask{Kind: ai.SlotDraft, Actor: q.actor(q.a)})
	q.tick(t)
	if state, code := q.state(t, id); state != ai.TaskCancelled || code != ai.CodeFeatureOff || len(q.fake.Requests()) != 0 {
		t.Fatalf("task = %s %s, %d calls", state, code, len(q.fake.Requests()))
	}
}

func TestARetryableFailureWaitsAndThenFails(t *testing.T) {
	q := newQueueEnv(t, nil)
	q.turnOn(t, q.a, ai.SlotDraft)
	busy := aiprovider.FakeReply{Err: &aiprovider.Error{Code: aiprovider.ErrorOverloaded, Status: 529, RetryAfter: 40 * time.Second}}
	q.fake.Script(busy, busy, busy)
	id := q.enqueue(t, q.a, ai.NewTask{Kind: ai.SlotDraft, Actor: q.actor(q.a)})
	q.tick(t)
	if state, code := q.state(t, id); state != ai.TaskQueued || code != "overloaded" {
		t.Fatalf("after one overload: %s %s", state, code)
	}
	q.tick(t)
	if len(q.fake.Requests()) != 1 {
		t.Fatal("the task ran again before its retry-after")
	}
	for range 2 {
		q.clock.Add(10 * time.Minute)
		q.tick(t)
	}
	if state, code := q.state(t, id); state != ai.TaskFailed || code != "overloaded" || len(q.fake.Requests()) != 3 {
		t.Fatalf("after three attempts: %s %s, %d calls", state, code, len(q.fake.Requests()))
	}
	if n := q.count(t, `SELECT count(*) FROM ai_usage WHERE task_id=$1 AND status='error'`, id); n != 3 {
		t.Fatalf("usage rows = %d, want one per attempt", n)
	}
}

func TestAnErrorThatWillNotPassFailsAtOnce(t *testing.T) {
	q := newQueueEnv(t, nil)
	q.turnOn(t, q.a, ai.SlotDraft)
	q.fake.Script(aiprovider.FakeReply{Err: &aiprovider.Error{Code: aiprovider.ErrorAuth, Status: 401}})
	id := q.enqueue(t, q.a, ai.NewTask{Kind: ai.SlotDraft})
	q.tick(t)
	if state, code := q.state(t, id); state != ai.TaskFailed || code != "auth" {
		t.Fatalf("task = %s %s", state, code)
	}
}

func TestAutomaticTasksStopAtTheDailyLimitAndPeopleDoNot(t *testing.T) {
	q := newQueueEnv(t, nil)
	q.turnOn(t, q.a, ai.SlotTriage)
	q.turnOn(t, q.a, ai.SlotDraft)
	if _, err := q.service.SetAutomaticLimits(context.Background(), q.a.id, q.actor(q.a), 2, 30); err != nil {
		t.Fatal(err)
	}
	var automatic []string
	for range 3 {
		automatic = append(automatic, q.enqueue(t, q.a, ai.NewTask{Kind: ai.SlotTriage, Automatic: true}))
	}
	person := q.enqueue(t, q.a, ai.NewTask{Kind: ai.SlotDraft, Actor: q.actor(q.a)})
	// Two run at a time in a workspace: three ticks reach all four.
	for range 3 {
		q.tick(t)
	}
	done, limited := 0, 0
	for _, id := range automatic {
		switch state, code := q.state(t, id); {
		case state == ai.TaskDone:
			done++
		case state == ai.TaskCancelled && code == ai.CodeAutomaticLimit:
			limited++
		}
	}
	if done != 2 || limited != 1 {
		t.Fatalf("automatic tasks: %d done, %d limited", done, limited)
	}
	if state, _ := q.state(t, person); state != ai.TaskDone {
		t.Fatalf("a person's draft = %s", state)
	}
	settings, _ := q.service.Settings(context.Background(), q.a.id)
	if settings.Automatic.PausedUntil == nil || !settings.Automatic.PausedUntil.Equal(time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)) || settings.Automatic.UsedToday != 2 {
		t.Fatalf("automatic = %+v", settings.Automatic)
	}
	// Another workspace keeps its own limit.
	q.turnOn(t, q.b, ai.SlotTriage)
	other := q.enqueue(t, q.b, ai.NewTask{Kind: ai.SlotTriage, Automatic: true})
	q.tick(t)
	if state, _ := q.state(t, other); state != ai.TaskDone {
		t.Fatalf("workspace B's automatic task = %s", state)
	}
}

func TestTheServerProviderStopsAtTheMonthlyAllowance(t *testing.T) {
	server := *testServer
	// 1,100 micro-dollars; each call holds about 1,006 while it runs and costs 70: two fit, one after
	// the other, and the third no longer does.
	server.Allowance = &ai.Allowance{PerWorkspace: 0.0011}
	q := newQueueEnv(t, func(config *ai.Config) { config.Server = &server })
	q.turnOn(t, q.a, ai.SlotDraft)
	first := q.enqueue(t, q.a, ai.NewTask{Kind: ai.SlotDraft})
	q.tick(t)
	second := q.enqueue(t, q.a, ai.NewTask{Kind: ai.SlotDraft})
	q.tick(t)
	third := q.enqueue(t, q.a, ai.NewTask{Kind: ai.SlotDraft})
	q.tick(t)
	for _, id := range []string{first, second} {
		if state, _ := q.state(t, id); state != ai.TaskDone {
			t.Fatalf("task %s = %s", id, state)
		}
	}
	if state, code := q.state(t, third); state != ai.TaskFailed || code != ai.CodeAllowanceReached {
		t.Fatalf("third = %s %s, want refused once what is left cannot pay for a call", state, code)
	}
	if n := q.count(t, `SELECT count(*) FROM ai_usage WHERE workspace_id=$1`, q.a.id); n > 2 {
		t.Fatalf("%d calls went out; a refused task sends nothing", n)
	}
	settings, _ := q.service.Settings(context.Background(), q.a.id)
	if settings.Allowance == nil || settings.Allowance.LimitMicroUSD != 1100 || settings.Allowance.UsedMicroUSD != 140 ||
		!settings.Allowance.ResetsAt.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("allowance = %+v", settings.Allowance)
	}
	// A new month starts again.
	q.clock.Add(15 * 24 * time.Hour)
	next := q.enqueue(t, q.a, ai.NewTask{Kind: ai.SlotDraft})
	q.tick(t)
	if state, _ := q.state(t, next); state != ai.TaskDone {
		t.Fatalf("in November: %s", state)
	}
}

func TestALockedWorkspaceSpendsNothing(t *testing.T) {
	q := newQueueEnv(t, func(config *ai.Config) { config.PlanLimits = true })
	q.turnOn(t, q.a, ai.SlotDraft)
	id := q.enqueue(t, q.a, ai.NewTask{Kind: ai.SlotDraft})
	if _, err := q.pool.Exec(context.Background(), `UPDATE workspaces SET trial_ends_at=$2 WHERE id=$1`, q.a.id, q.clock.Now().Add(-60*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	q.tick(t)
	if state, code := q.state(t, id); state != ai.TaskCancelled || code != ai.CodeWorkspaceLocked || len(q.fake.Requests()) != 0 {
		t.Fatalf("task in a locked workspace = %s %s, %d calls", state, code, len(q.fake.Requests()))
	}
}

func TestACancelledTaskKeepsItsCancellation(t *testing.T) {
	q := newQueueEnv(t, nil)
	q.turnOn(t, q.a, ai.SlotDraft)
	var id string
	q.fake.Answer = func(aiprovider.Request) (aiprovider.Response, error) {
		err := database.New(q.pool).InWorkspace(context.Background(), q.a.id, func(tx pgx.Tx) error {
			_, err := q.service.CancelTask(context.Background(), tx, q.a.id, id)
			return err
		})
		return aiprovider.Response{Text: "late", Stop: aiprovider.StopDone, Usage: aiprovider.Usage{Reported: true}}, err
	}
	id = q.enqueue(t, q.a, ai.NewTask{Kind: ai.SlotDraft})
	q.tick(t)
	if state, code := q.state(t, id); state != ai.TaskCancelled || code != ai.CodeCancelledByPerson {
		t.Fatalf("a task cancelled during its call = %s %s", state, code)
	}
	if n := q.count(t, `SELECT count(*) FROM ai_usage WHERE task_id=$1 AND status='ok'`, id); n != 1 {
		t.Fatal("the call that went out left no usage row")
	}
}

func TestUsageRowsADeadProcessLeftStartedAreClosed(t *testing.T) {
	q := newQueueEnv(t, nil)
	if _, err := q.pool.Exec(context.Background(), `INSERT INTO ai_usage(workspace_id,feature,billed_to,provider_kind,provider_label,model,status,started_at)
		VALUES($1,'draft','server','anthropic','Colliedesk AI','claude-sonnet-5-5','started',$2),($1,'draft','server','anthropic','Colliedesk AI','claude-sonnet-5-5','started',$3)`,
		q.a.id, q.clock.Now().Add(-time.Hour), q.clock.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	q.tick(t)
	if n := q.count(t, `SELECT count(*) FROM ai_usage WHERE status='error' AND error_code='unknown' AND finished_at IS NOT NULL`); n != 1 {
		t.Fatalf("closed rows = %d, want only the hour-old one", n)
	}
}

func TestATaskWithoutAHandlerFails(t *testing.T) {
	q := newQueueEnv(t, nil)
	q.worker = ai.NewWorker(q.service, database.New(q.pool), ai.WorkerOptions{})
	q.turnOn(t, q.a, ai.SlotDraft)
	id := q.enqueue(t, q.a, ai.NewTask{Kind: ai.SlotDraft})
	q.tick(t)
	if state, code := q.state(t, id); state != ai.TaskFailed || code != ai.CodeNoHandler {
		t.Fatalf("task = %s %s", state, code)
	}
	if _, err := q.service.Enqueue(context.Background(), nil, q.a.id, ai.NewTask{Kind: "summary"}); err == nil {
		t.Fatal("an unknown kind was queued")
	}
}

func TestAStoppingProcessPutsItsTaskBackWithoutCountingIt(t *testing.T) {
	q := newQueueEnv(t, nil)
	q.turnOn(t, q.a, ai.SlotDraft)
	ctx, stop := context.WithCancel(context.Background())
	q.worker.Handle(ai.SlotDraft, ai.HandlerFunc(func(callCtx context.Context, run *ai.Run) error {
		stop()
		_, err := run.Metered(callCtx, aiprovider.Request{Messages: []aiprovider.Message{{Role: aiprovider.RoleUser, Text: "hi"}}, MaxOutput: 10})
		return err
	}))
	id := q.enqueue(t, q.a, ai.NewTask{Kind: ai.SlotDraft})
	if err := q.worker.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	q.worker.Wait()
	if state, _ := q.state(t, id); state != ai.TaskQueued {
		t.Fatalf("a task interrupted by a shutdown = %s, want queued", state)
	}
	if n := q.count(t, `SELECT attempts FROM ai_tasks WHERE id=$1`, id); n != 0 {
		t.Fatalf("attempts = %d, the interrupted one must not count", n)
	}
	if n := q.count(t, `SELECT count(*) FROM ai_usage WHERE status='started'`); n != 0 {
		t.Fatal("a usage row was left started")
	}
}

func TestABurstPastTheMinuteLimitWaitsForTheNextMinute(t *testing.T) {
	q := newQueueEnv(t, nil)
	q.turnOn(t, q.a, ai.SlotTriage)
	if _, err := q.service.SetAutomaticLimits(context.Background(), q.a.id, q.actor(q.a), 300, 1); err != nil {
		t.Fatal(err)
	}
	first := q.enqueue(t, q.a, ai.NewTask{Kind: ai.SlotTriage, Automatic: true})
	q.tick(t)
	second := q.enqueue(t, q.a, ai.NewTask{Kind: ai.SlotTriage, Automatic: true})
	q.tick(t)
	if state, _ := q.state(t, first); state != ai.TaskDone {
		t.Fatalf("first = %s", state)
	}
	if state, code := q.state(t, second); state != ai.TaskQueued || code != "" {
		t.Fatalf("second, past the minute limit = %s %s, want queued", state, code)
	}
	q.clock.Add(time.Minute)
	q.tick(t)
	if state, _ := q.state(t, second); state != ai.TaskDone {
		t.Fatalf("second, a minute later = %s", state)
	}
}

func TestOneWorkspacesBacklogDoesNotHoldBackAnother(t *testing.T) {
	q := newQueueEnv(t, nil)
	q.turnOn(t, q.a, ai.SlotTriage)
	q.turnOn(t, q.b, ai.SlotTriage)
	for range 20 {
		q.enqueue(t, q.a, ai.NewTask{Kind: ai.SlotTriage, Automatic: true})
	}
	q.clock.Add(time.Second)
	other := q.enqueue(t, q.b, ai.NewTask{Kind: ai.SlotTriage, Automatic: true})
	q.tick(t)
	if state, _ := q.state(t, other); state != ai.TaskDone {
		t.Fatalf("B's task behind A's backlog = %s", state)
	}
	if n := q.count(t, `SELECT count(*) FROM ai_tasks WHERE workspace_id=$1 AND state='done'`, q.a.id); n != 2 {
		t.Fatalf("A ran %d at once, want 2", n)
	}
}

func TestARecordedResultNeedsTheTaskStillHeld(t *testing.T) {
	q := newQueueEnv(t, nil)
	q.turnOn(t, q.a, ai.SlotDraft)
	var id string
	var recordErr error
	q.worker.Handle(ai.SlotDraft, ai.HandlerFunc(func(ctx context.Context, run *ai.Run) error {
		err := database.New(q.pool).InWorkspace(context.Background(), q.a.id, func(tx pgx.Tx) error {
			_, err := q.service.CancelTask(context.Background(), tx, q.a.id, id)
			return err
		})
		if err != nil {
			return err
		}
		recordErr = run.Record(ctx, func(pgx.Tx) error { return nil })
		return recordErr
	}))
	id = q.enqueue(t, q.a, ai.NewTask{Kind: ai.SlotDraft})
	q.tick(t)
	if !errors.Is(recordErr, ai.ErrClaimLost) {
		t.Fatalf("record after a cancel: %v", recordErr)
	}
	if state, code := q.state(t, id); state != ai.TaskCancelled || code != ai.CodeCancelledByPerson {
		t.Fatalf("task = %s %s", state, code)
	}
	recorded := q.enqueue(t, q.a, ai.NewTask{Kind: ai.SlotDraft})
	q.worker.Handle(ai.SlotDraft, ai.HandlerFunc(func(ctx context.Context, run *ai.Run) error {
		return run.Record(ctx, func(tx pgx.Tx) error { return nil })
	}))
	q.tick(t)
	if state, _ := q.state(t, recorded); state != ai.TaskDone {
		t.Fatalf("a recorded task = %s", state)
	}
}

func TestTheMinuteLimitLooksAcrossMidnight(t *testing.T) {
	q := newQueueEnv(t, nil)
	q.clock.now = time.Date(2026, 10, 22, 23, 59, 40, 0, time.UTC)
	q.turnOn(t, q.a, ai.SlotTriage)
	if _, err := q.service.SetAutomaticLimits(context.Background(), q.a.id, q.actor(q.a), 300, 1); err != nil {
		t.Fatal(err)
	}
	before := q.enqueue(t, q.a, ai.NewTask{Kind: ai.SlotTriage, Automatic: true})
	q.tick(t)
	q.clock.Add(40 * time.Second) // 00:00:20, the next day
	after := q.enqueue(t, q.a, ai.NewTask{Kind: ai.SlotTriage, Automatic: true})
	q.tick(t)
	if state, _ := q.state(t, before); state != ai.TaskDone {
		t.Fatalf("before midnight = %s", state)
	}
	if state, _ := q.state(t, after); state != ai.TaskQueued {
		t.Fatalf("20 seconds after midnight, within the minute = %s, want queued", state)
	}
}

func TestCallsRunningAtOnceNeverPassTheAllowance(t *testing.T) {
	server := *testServer
	// 1,500 micro-dollars. One test call reserves about 1,006 (100 tokens out at 10, a few in at 2) and
	// costs 70: two that start together must not both fit.
	server.Allowance = &ai.Allowance{PerWorkspace: 0.0015}
	q := newQueueEnv(t, func(config *ai.Config) { config.Server = &server })
	q.turnOn(t, q.a, ai.SlotDraft)
	// Each call waits (up to a second) for the other to be in flight, so both run at once when both
	// are let through.
	var inFlight sync.WaitGroup
	inFlight.Add(2)
	q.fake.Answer = func(request aiprovider.Request) (aiprovider.Response, error) {
		inFlight.Done()
		waited := make(chan struct{})
		go func() { inFlight.Wait(); close(waited) }()
		select {
		case <-waited:
		case <-time.After(time.Second):
		}
		usage := aiprovider.Usage{Reported: true, InputTokens: 10, OutputTokens: 5}
		return aiprovider.Response{Text: "ok", Stop: aiprovider.StopDone, Model: request.Model, Usage: usage, Hops: []aiprovider.Hop{{Model: request.Model, Usage: usage}}}, nil
	}
	first := q.enqueue(t, q.a, ai.NewTask{Kind: ai.SlotDraft})
	second := q.enqueue(t, q.a, ai.NewTask{Kind: ai.SlotDraft})
	q.tick(t)
	// One call went out; the other waited for its hold (queued again), never a second call at once.
	done, waiting := 0, 0
	for _, id := range []string{first, second} {
		switch state, _ := q.state(t, id); state {
		case ai.TaskDone:
			done++
		case ai.TaskQueued:
			waiting++
		}
	}
	if done != 1 || waiting != 1 {
		t.Fatalf("done %d, waiting %d: two calls started together both fit an allowance that holds one", done, waiting)
	}
	if n := q.count(t, `SELECT count(*) FROM ai_usage WHERE workspace_id=$1`, q.a.id); n != 1 {
		t.Fatalf("%d calls went out", n)
	}
	if n := q.count(t, `SELECT count(*) FROM ai_usage WHERE workspace_id=$1 AND status='started'`, q.a.id); n != 0 {
		t.Fatalf("%d calls still hold a reservation", n)
	}
}

func TestATaskWaitingOnlyForAnotherCallsHoldRunsOnceItIsFree(t *testing.T) {
	server := *testServer
	server.Allowance = &ai.Allowance{PerWorkspace: 0.0015} // holds one call (about 1,006) at a time
	q := newQueueEnv(t, func(config *ai.Config) { config.Server = &server })
	q.turnOn(t, q.a, ai.SlotDraft)
	var inFlight sync.WaitGroup
	inFlight.Add(2)
	q.fake.Answer = func(request aiprovider.Request) (aiprovider.Response, error) {
		inFlight.Done()
		waited := make(chan struct{})
		go func() { inFlight.Wait(); close(waited) }()
		select {
		case <-waited:
		case <-time.After(300 * time.Millisecond):
		}
		usage := aiprovider.Usage{Reported: true, InputTokens: 10, OutputTokens: 5}
		return aiprovider.Response{Text: "ok", Stop: aiprovider.StopDone, Model: request.Model, Usage: usage, Hops: []aiprovider.Hop{{Model: request.Model, Usage: usage}}}, nil
	}
	first := q.enqueue(t, q.a, ai.NewTask{Kind: ai.SlotDraft})
	second := q.enqueue(t, q.a, ai.NewTask{Kind: ai.SlotDraft})
	q.tick(t)
	// One ran; the other only waited on its hold: it is queued again, not failed for an allowance
	// that still has room.
	states := map[string]int{}
	for _, id := range []string{first, second} {
		state, _ := q.state(t, id)
		states[state]++
	}
	if states[ai.TaskDone] != 1 || states[ai.TaskQueued] != 1 {
		t.Fatalf("states after the first tick = %v, want one done and one queued", states)
	}
	q.clock.Add(10 * time.Second)
	q.tick(t)
	for _, id := range []string{first, second} {
		if state, code := q.state(t, id); state != ai.TaskDone {
			t.Fatalf("task %s = %s %s once the other call finished", id, state, code)
		}
	}
	// Past what is left with nothing running, a task fails at once and says so.
	third := q.enqueue(t, q.a, ai.NewTask{Kind: ai.SlotDraft})
	if _, err := q.pool.Exec(context.Background(), `UPDATE ai_usage SET cost_micro_usd=1000 WHERE workspace_id=$1`, q.a.id); err != nil {
		t.Fatal(err)
	}
	q.tick(t)
	if state, code := q.state(t, third); state != ai.TaskFailed || code != ai.CodeAllowanceReached {
		t.Fatalf("third = %s %s", state, code)
	}
}

func TestUndecidedDraftsExpireAndOldDraftTextIsErased(t *testing.T) {
	q := newQueueEnv(t, nil)
	ctx := context.Background()
	var conversation string
	if err := q.pool.QueryRow(ctx, `WITH c AS (INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Ada','ada@example.com') RETURNING id)
		INSERT INTO conversations(workspace_id,contact_id,subject) SELECT $1,id,'Hello' FROM c RETURNING id::text`, q.a.id).Scan(&conversation); err != nil {
		t.Fatal(err)
	}
	now := q.clock.Now()
	draft := func(status string, created time.Time, ready, decided *time.Time, outcome *string) string {
		var id string
		if err := q.pool.QueryRow(ctx, `INSERT INTO ai_drafts(workspace_id,conversation_id,status,prompt,text,created_at,ready_at,decided_at,outcome)
			VALUES($1,$2,$3,'say hi','Hello Ada',$4,$5,$6,$7) RETURNING id::text`, q.a.id, conversation, status, created, ready, decided, outcome).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	at := func(d time.Duration) *time.Time { v := now.Add(-d); return &v }
	discarded := "discarded"
	day := 24 * time.Hour
	stale := draft("ready", now.Add(-8*day), at(8*day), nil, nil)
	fresh := draft("ready", now.Add(-day), at(day), nil, nil)
	old := draft("ready", now.Add(-100*day), at(100*day), at(91*day), &discarded)
	recent := draft("ready", now.Add(-100*day), at(100*day), at(30*day), &discarded)
	cancelled := draft("cancelled", now.Add(-91*day), nil, nil, nil)
	q.tick(t)
	state := func(id string) string {
		var outcome, text *string
		if err := q.pool.QueryRow(ctx, `SELECT outcome, text FROM ai_drafts WHERE id=$1`, id).Scan(&outcome, &text); err != nil {
			t.Fatal(err)
		}
		result := "-"
		if outcome != nil {
			result = *outcome
		}
		if text == nil {
			return result + " erased"
		}
		return result + " kept"
	}
	for id, want := range map[string]string{stale: "expired kept", fresh: "- kept", old: "discarded erased", recent: "discarded kept", cancelled: "- erased"} {
		if got := state(id); got != want {
			t.Fatalf("draft %s = %q, want %q", id, got, want)
		}
	}
	if n := q.count(t, `SELECT count(*) FROM ai_drafts WHERE text_cleared_at IS NOT NULL AND prompt=''`); n != 2 {
		t.Fatalf("erased prompts = %d", n)
	}
}

func TestUndecidedSuggestionsExpireAndOldReasonsAreErased(t *testing.T) {
	q := newQueueEnv(t, nil)
	ctx := context.Background()
	var conversation string
	if err := q.pool.QueryRow(ctx, `WITH c AS (INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Ada','ada@example.com') RETURNING id)
		INSERT INTO conversations(workspace_id,contact_id,subject) SELECT $1,id,'Hello' FROM c RETURNING id::text`, q.a.id).Scan(&conversation); err != nil {
		t.Fatal(err)
	}
	now := q.clock.Now()
	day := 24 * time.Hour
	suggestion := func(ready time.Time, applied string, decided *time.Time, outcome *string) string {
		var id string
		if err := q.pool.QueryRow(ctx, `INSERT INTO ai_triage_suggestions(workspace_id,conversation_id,status,priority,reason,created_at,ready_at,applied,decided_at,outcome)
			VALUES($1,$2,'ready','high','Printer down',$3,$3,$4,$5,$6) RETURNING id::text`, q.a.id, conversation, ready, applied, decided, outcome).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	dismissed := "dismissed"
	old := now.Add(-91 * day)
	untouched := suggestion(now.Add(-8*day), `{}`, nil, nil)
	part := suggestion(now.Add(-8*day), `{"tag_ids":[],"priority":"high","assignee_agent_id":null}`, nil, nil)
	fresh := suggestion(now.Add(-day), `{}`, nil, nil)
	ended := suggestion(now.Add(-100*day), `{}`, &old, &dismissed)
	q.tick(t)
	state := func(id string) string {
		var outcome, reason *string
		if err := q.pool.QueryRow(ctx, `SELECT outcome, reason FROM ai_triage_suggestions WHERE id=$1`, id).Scan(&outcome, &reason); err != nil {
			t.Fatal(err)
		}
		result := "-"
		if outcome != nil {
			result = *outcome
		}
		if reason == nil {
			return result + " erased"
		}
		return result + " kept"
	}
	for id, want := range map[string]string{untouched: "expired kept", part: "partly_accepted kept", fresh: "- kept", ended: "dismissed erased"} {
		if got := state(id); got != want {
			t.Fatalf("suggestion %s = %q, want %q", id, got, want)
		}
	}
}

func TestTheAllowanceSaysAboutHowManyDraftsAreLeft(t *testing.T) {
	server := *testServer
	server.Allowance = &ai.Allowance{PerWorkspace: 1} // 1,000,000 micro-dollars
	q := newQueueEnv(t, func(config *ai.Config) { config.Server = &server })
	settings, err := q.service.Settings(context.Background(), q.a.id)
	if err != nil {
		t.Fatal(err)
	}
	// Before the first draft, a typical one on Sonnet's prices: 6,000×2 + 500×10 = 17,000.
	if settings.Allowance == nil || settings.Allowance.DraftsLeft == nil || *settings.Allowance.DraftsLeft != 58 {
		t.Fatalf("allowance before any draft = %+v", settings.Allowance)
	}
	q.turnOn(t, q.a, ai.SlotDraft)
	q.enqueue(t, q.a, ai.NewTask{Kind: ai.SlotDraft})
	q.tick(t)
	// Then the workspace's own: one draft of 70 micro-dollars.
	settings, _ = q.service.Settings(context.Background(), q.a.id)
	if *settings.Allowance.DraftsLeft != (1_000_000-70)/70 {
		t.Fatalf("drafts left after one draft = %d", *settings.Allowance.DraftsLeft)
	}
}

func TestADraftThatCouldNeverFitWhatIsLeftIsRefusedAtOnce(t *testing.T) {
	server := *testServer
	// 30,000 micro-dollars: a typical draft on Sonnet holds 6,000×2 + 4,000×10 = 52,000.
	server.Allowance = &ai.Allowance{PerWorkspace: 0.03}
	q := newQueueEnv(t, func(config *ai.Config) { config.Server = &server })
	q.turnOn(t, q.a, ai.SlotDraft)
	var conversation string
	if err := q.pool.QueryRow(context.Background(), `WITH c AS (INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Ada','ada@example.com') RETURNING id)
		INSERT INTO conversations(workspace_id,contact_id,subject) SELECT $1,id,'Hello' FROM c RETURNING id::text`, q.a.id).Scan(&conversation); err != nil {
		t.Fatal(err)
	}
	if _, err := q.service.CreateDraft(context.Background(), q.a.id, q.actor(q.a), conversation, ""); !errors.Is(err, ai.ErrAllowanceReached) {
		t.Fatalf("CreateDraft = %v, want the allowance refusal before anything is queued", err)
	}
	if n := q.count(t, `SELECT count(*) FROM ai_tasks WHERE workspace_id=$1`, q.a.id); n != 0 {
		t.Fatalf("%d tasks queued", n)
	}
}
