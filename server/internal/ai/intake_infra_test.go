package ai_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
)

// intakeOn sets intake on a provider with two models: sonnet first, haiku as its fallback.
func (q *queueEnv) intakeOn(t *testing.T, fallback *string) ai.Provider {
	t.Helper()
	provider := q.add(t, q.a, "Intake key")
	if _, err := q.service.SetSlot(context.Background(), q.a.id, q.actor(q.a), ai.SlotIntake, ai.SlotInput{Source: ai.SourceProvider,
		ProviderID: provider.ID, Model: "claude-sonnet-5-5", FallbackModel: fallback, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	return provider
}

func pointer(value string) *string { return &value }

// handleIntake runs intake tasks with one request that asks for JSON.
func (q *queueEnv) handleIntake() {
	q.worker.Handle(ai.SlotIntake, ai.HandlerFunc(func(ctx context.Context, run *ai.Run) error {
		_, err := run.Metered(ctx, aiprovider.Request{System: "intake", Messages: []aiprovider.Message{{Role: aiprovider.RoleUser, Text: "hello"}}, MaxOutput: 100,
			Schema: map[string]any{"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}}}})
		return err
	}))
}

// models lists the models of the requests that are not a provider check.
func models(requests []aiprovider.Request) []string {
	var list []string
	for _, request := range requests {
		if request.System == "intake" {
			list = append(list, request.Model)
		}
	}
	return list
}

// Intake's request goes once more to its fallback model when its model is busy or answers what cannot
// be read; an error another model would get as well (a refused key) does not try it, and each call
// has its usage row.
func TestIntakeTriesItsFallbackModelWhenItsModelFails(t *testing.T) {
	q := newQueueEnv(t, nil)
	q.handleIntake()
	q.intakeOn(t, pointer("claude-haiku-5-5"))
	q.fake.Script(aiprovider.FakeReply{Err: &aiprovider.Error{Code: aiprovider.ErrorOverloaded, Status: 529}})
	busy := q.enqueue(t, q.a, ai.NewTask{Kind: ai.SlotIntake, Actor: q.actor(q.a)})
	q.tick(t)
	if state, _ := q.state(t, busy); state != ai.TaskDone || len(models(q.fake.Requests())) != 2 ||
		models(q.fake.Requests())[0] != "claude-sonnet-5-5" || models(q.fake.Requests())[1] != "claude-haiku-5-5" {
		t.Fatalf("busy model: task %s, requests %v", state, models(q.fake.Requests()))
	}
	if n := q.count(t, `SELECT count(*) FROM ai_usage WHERE task_id=$1 AND feature='intake' AND
			((model='claude-sonnet-5-5' AND status='error') OR (model='claude-haiku-5-5' AND status='ok'))`, busy); n != 2 {
		t.Fatalf("usage rows = %d, want one per model", n)
	}
	q.fake.Script(aiprovider.FakeReply{Response: aiprovider.Response{Text: "not json", Stop: aiprovider.StopDone, Model: "claude-sonnet-5-5"}})
	unreadable := q.enqueue(t, q.a, ai.NewTask{Kind: ai.SlotIntake, Actor: q.actor(q.a)})
	q.tick(t)
	if state, _ := q.state(t, unreadable); state != ai.TaskDone || len(models(q.fake.Requests())) != 4 || models(q.fake.Requests())[3] != "claude-haiku-5-5" {
		t.Fatalf("unreadable answer: task %s, requests %v", state, models(q.fake.Requests()))
	}
	q.fake.Script(aiprovider.FakeReply{Err: &aiprovider.Error{Code: aiprovider.ErrorAuth, Status: 401}})
	refused := q.enqueue(t, q.a, ai.NewTask{Kind: ai.SlotIntake, Actor: q.actor(q.a)})
	q.tick(t)
	if state, code := q.state(t, refused); state != ai.TaskFailed || code != "auth" || len(models(q.fake.Requests())) != 5 {
		t.Fatalf("refused key: task %s %s, requests %v", state, code, models(q.fake.Requests()))
	}
}

// Without a fallback model nothing is tried twice.
func TestIntakeWithoutAFallbackModelCallsOnce(t *testing.T) {
	q := newQueueEnv(t, nil)
	q.handleIntake()
	q.intakeOn(t, nil)
	q.fake.Script(aiprovider.FakeReply{Err: &aiprovider.Error{Code: aiprovider.ErrorOverloaded, Status: 529}})
	id := q.enqueue(t, q.a, ai.NewTask{Kind: ai.SlotIntake, Actor: q.actor(q.a)})
	q.tick(t)
	if state, code := q.state(t, id); state != ai.TaskQueued || code != "overloaded" || len(models(q.fake.Requests())) != 1 {
		t.Fatalf("task %s %s, requests %v", state, code, models(q.fake.Requests()))
	}
}

// A fallback model is intake's, another model of the same provider; the provider cannot drop it while
// intake names it, and it goes with its provider.
func TestAFallbackModelIsAnotherModelOfIntakesProvider(t *testing.T) {
	e := newEnv(t, nil)
	provider := e.add(t, e.a, "Team key")
	set := func(slot, model string, fallback *string) (ai.Slot, error) {
		return e.service.SetSlot(context.Background(), e.a.id, e.actor(e.a), slot, ai.SlotInput{Source: ai.SourceProvider, ProviderID: provider.ID,
			Model: model, FallbackModel: fallback, Enabled: true})
	}
	var invalid *ai.InvalidError
	for name, try := range map[string]func() (ai.Slot, error){
		"triage":         func() (ai.Slot, error) { return set(ai.SlotTriage, "claude-sonnet-5-5", pointer("claude-haiku-5-5")) },
		"its own model":  func() (ai.Slot, error) { return set(ai.SlotIntake, "claude-sonnet-5-5", pointer("claude-sonnet-5-5")) },
		"another vendor": func() (ai.Slot, error) { return set(ai.SlotIntake, "claude-sonnet-5-5", pointer("gpt-5")) },
	} {
		if _, err := try(); !errors.As(err, &invalid) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	slot, err := set(ai.SlotIntake, "claude-sonnet-5-5", pointer("claude-haiku-5-5"))
	if err != nil || slot.FallbackModel == nil || *slot.FallbackModel != "claude-haiku-5-5" {
		t.Fatalf("slot = %+v, %v", slot, err)
	}
	// Left out, it stays; the provider cannot drop it.
	if slot, err = set(ai.SlotIntake, "claude-sonnet-5-5", nil); err != nil || slot.FallbackModel == nil {
		t.Fatalf("kept: %+v, %v", slot, err)
	}
	var inUse *ai.InUseError
	if _, err := e.service.UpdateProvider(context.Background(), e.a.id, e.actor(e.a), provider.ID,
		ai.ProviderChange{Models: &[]string{"claude-sonnet-5-5"}}); !errors.As(err, &inUse) || inUse.Model != "claude-haiku-5-5" {
		t.Fatalf("drop the fallback model: %v", err)
	}
	if slot, err = set(ai.SlotIntake, "claude-sonnet-5-5", pointer("")); err != nil || slot.FallbackModel != nil {
		t.Fatalf("removed: %+v, %v", slot, err)
	}
	if _, err := set(ai.SlotIntake, "claude-sonnet-5-5", pointer("claude-haiku-5-5")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.service.RemoveProvider(context.Background(), e.a.id, e.actor(e.a), provider.ID); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, `SELECT count(*) FROM ai_slots WHERE workspace_id=$1 AND fallback_model IS NOT NULL`, e.a.id); n != 0 {
		t.Fatalf("fallback models left after the provider went: %d", n)
	}
}

// Automatic intake decisions stop at intake_per_day, inside the day's automatic limit.
func TestAutomaticIntakeStopsAtItsDailyLimit(t *testing.T) {
	q := newQueueEnv(t, nil)
	q.handleIntake()
	q.intakeOn(t, nil)
	one := 1
	limits, err := q.service.SetAutomaticLimits(context.Background(), q.a.id, q.actor(q.a), 300, 30, nil, &one)
	if err != nil || limits.IntakePerDay != 1 || limits.DraftPerDay != ai.DefaultAutomaticDraftsPerDay {
		t.Fatalf("limits = %+v, %v", limits, err)
	}
	first := q.enqueue(t, q.a, ai.NewTask{Kind: ai.SlotIntake, Automatic: true})
	q.tick(t)
	second := q.enqueue(t, q.a, ai.NewTask{Kind: ai.SlotIntake, Automatic: true})
	q.tick(t)
	if state, _ := q.state(t, first); state != ai.TaskDone {
		t.Fatalf("first = %s", state)
	}
	if state, code := q.state(t, second); state != ai.TaskCancelled || code != ai.CodeIntakeLimit {
		t.Fatalf("second = %s %s", state, code)
	}
	settings, _ := q.service.Settings(context.Background(), q.a.id)
	if settings.Automatic.IntakeToday != 1 || settings.Automatic.IntakePausedUntil == nil ||
		!settings.Automatic.IntakePausedUntil.Equal(time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)) || settings.Automatic.DraftsPausedUntil != nil {
		t.Fatalf("automatic = %+v", settings.Automatic)
	}
	var detail string
	if err := q.pool.QueryRow(context.Background(), `SELECT detail::text FROM ai_settings_events WHERE workspace_id=$1 AND kind='automatic_limits_changed'`, q.a.id).Scan(&detail); err != nil {
		t.Fatal(err)
	}
	if want := `"intake_per_day": 1`; !strings.Contains(detail, want) {
		t.Fatalf("history = %s", detail)
	}
}

// What the provider says a call cost is kept as it said it, in its own unit, and summed per month.
func TestTheProvidersOwnCostIsKeptInItsUnit(t *testing.T) {
	q := newQueueEnv(t, nil)
	q.handleIntake()
	q.intakeOn(t, nil)
	cost := 0.004215
	usage := aiprovider.Usage{Reported: true, InputTokens: 10, OutputTokens: 5}
	q.fake.Script(aiprovider.FakeReply{Response: aiprovider.Response{Text: `{"ok":true}`, JSON: `{"ok":true}`, Stop: aiprovider.StopDone,
		Model: "claude-sonnet-5-5", Usage: usage, ProviderCost: &cost}})
	q.enqueue(t, q.a, ai.NewTask{Kind: ai.SlotIntake, Actor: q.actor(q.a)})
	q.enqueue(t, q.a, ai.NewTask{Kind: ai.SlotIntake, Actor: q.actor(q.a)})
	q.tick(t)
	if n := q.count(t, `SELECT count(*) FROM ai_usage WHERE workspace_id=$1 AND provider_cost=0.004215 AND cost_micro_usd IS NULL`, q.a.id); n != 1 {
		t.Fatalf("rows with the provider's cost = %d", n)
	}
	month, _ := q.service.ParseMonth("")
	report, err := q.service.Usage(context.Background(), q.a.id, month)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range report.Rows {
		if row.Feature == ai.FeatureIntake && (row.ProviderCost == nil || *row.ProviderCost != cost || row.Calls != 2) {
			t.Fatalf("intake row = %+v", row)
		}
	}
}

// OpenRouter's routing is stored with the provider and reaches every call, its check included; another
// server refuses it; tightening it runs at once and loosening it needs a person.
func TestOpenRouterRoutingReachesItsCallsAndLooseningItNeedsAPerson(t *testing.T) {
	e := newEnv(t, nil)
	if _, err := e.service.AddProvider(context.Background(), e.a.id, e.actor(e.a), ai.ProviderInput{Kind: "anthropic", Label: "Claude", APIKey: testKey,
		Models: []string{"claude-sonnet-5-5"}, Routing: &ai.ProviderRouting{DataCollection: "deny"}}); err == nil {
		t.Fatal("an Anthropic provider took OpenRouter's routing")
	}
	provider, err := e.service.AddProvider(context.Background(), e.a.id, e.actor(e.a), ai.ProviderInput{Kind: "openai_compatible", Label: "OpenRouter",
		BaseURL: "https://openrouter.ai/api/v1", APIKey: testKey, Models: []string{"openai/gpt-5"}, Routing: &ai.ProviderRouting{DataCollection: "deny", ZDR: true}})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Routing == nil || *provider.Routing != (ai.ProviderRouting{DataCollection: "deny", ZDR: true}) {
		t.Fatalf("routing = %+v", provider.Routing)
	}
	e.mu.Lock()
	checked := e.specs[len(e.specs)-1].Routing
	e.mu.Unlock()
	if checked != (aiprovider.Routing{DataCollection: "deny", ZDR: true}) {
		t.Fatalf("the check went with %+v", checked)
	}
	loose := ai.ProviderRouting{DataCollection: "allow", ZDR: true}
	if _, err := e.service.UpdateProvider(context.Background(), e.a.id, e.actor(e.a), provider.ID, ai.ProviderChange{Routing: &loose}); !errors.Is(err, ai.ErrLoosenNeedsApproval) {
		t.Fatalf("loosen without a person: %v", err)
	}
	if got, _ := e.service.Provider(context.Background(), e.a.id, provider.ID); got.Routing.DataCollection != "deny" {
		t.Fatalf("a refused loosening changed the routing: %+v", got.Routing)
	}
	if updated, err := e.service.UpdateProvider(context.Background(), e.a.id, e.actor(e.a), provider.ID, ai.ProviderChange{Routing: &loose, MayLoosen: true}); err != nil || updated.Routing.DataCollection != "allow" {
		t.Fatalf("a person loosens: %+v, %v", updated.Routing, err)
	}
	tight := ai.ProviderRouting{DataCollection: "deny", ZDR: true}
	if _, err := e.service.UpdateProvider(context.Background(), e.a.id, e.actor(e.a), provider.ID, ai.ProviderChange{Routing: &tight}); err != nil {
		t.Fatalf("tighten: %v", err)
	}
	if n := e.count(t, `SELECT count(*) FROM ai_settings_events WHERE workspace_id=$1 AND kind='provider_changed' AND detail ? 'routing'`, e.a.id); n != 2 {
		t.Fatalf("routing changes in the history = %d", n)
	}
	if n := e.count(t, `SELECT count(*) FROM ai_providers WHERE workspace_id=$1 AND routing='{"data_collection":"deny","zdr":true}'::jsonb`, e.a.id); n != 1 {
		t.Fatalf("stored routing rows = %d", n)
	}
	var invalid *ai.InvalidError
	if _, err := e.service.UpdateProvider(context.Background(), e.a.id, e.actor(e.a), provider.ID, ai.ProviderChange{Routing: &ai.ProviderRouting{DataCollection: "sometimes"}}); !errors.As(err, &invalid) {
		t.Fatalf("an unknown data policy: %v", err)
	}
}

// A change that does not name the routing keeps the routing it finds when it saves, not the one it
// read before its check: an admin who tightened it meanwhile is not undone, and no token's rename is
// filed as a loosening.
func TestAChangeWithoutRoutingKeepsTheRoutingItFindsWhenItSaves(t *testing.T) {
	e := newEnv(t, nil)
	provider, err := e.service.AddProvider(context.Background(), e.a.id, e.actor(e.a), ai.ProviderInput{Kind: "openai_compatible", Label: "OpenRouter",
		BaseURL: "https://openrouter.ai/api/v1", APIKey: testKey, Models: []string{"openai/gpt-5", "openai/gpt-5-mini"}})
	if err != nil {
		t.Fatal(err)
	}
	checking, release := make(chan struct{}), make(chan struct{})
	e.fake.Answer = func(request aiprovider.Request) (aiprovider.Response, error) {
		if request.Model == "openai/gpt-5-mini" {
			close(checking)
			<-release
		}
		return aiprovider.Response{Text: `{"ok":true}`, JSON: `{"ok":true}`, Stop: aiprovider.StopDone, Model: request.Model}, nil
	}
	done := make(chan error, 1)
	go func() {
		// A token reorders the models; the new first model is checked before it saves.
		_, err := e.service.UpdateProvider(context.Background(), e.a.id, ai.Actor{}, provider.ID,
			ai.ProviderChange{Models: &[]string{"openai/gpt-5-mini", "openai/gpt-5"}})
		done <- err
	}()
	<-checking
	if _, err := e.service.UpdateProvider(context.Background(), e.a.id, e.actor(e.a), provider.ID,
		ai.ProviderChange{Routing: &ai.ProviderRouting{DataCollection: "deny", ZDR: true}, MayLoosen: true}); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("the reorder: %v", err)
	}
	got, _ := e.service.Provider(context.Background(), e.a.id, provider.ID)
	if got.Routing == nil || *got.Routing != (ai.ProviderRouting{DataCollection: "deny", ZDR: true}) || got.Models[0] != "openai/gpt-5-mini" {
		t.Fatalf("provider = %+v routing %+v", got, got.Routing)
	}
}
