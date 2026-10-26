package channeltest_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/intake"
	"github.com/colliedesk/colliedesk/server/internal/intake/prompt"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// aiDesk is a loopback desk whose intake AI answers from a script, with the AI worker of the process.
type aiDesk struct {
	loopDesk
	fake   *aiprovider.Fake
	worker *ai.Worker
}

func newAIDesk(t *testing.T, gate intake.Gate) aiDesk {
	t.Helper()
	d := aiDesk{loopDesk: newLoopDesk(t, intakeLoop()), fake: &aiprovider.Fake{}}
	service := ai.NewService(database.New(d.pool), ai.Config{
		Server: &ai.ServerProvider{Kind: aiprovider.KindAnthropic, Label: "Colliedesk AI", APIKey: "server-key", Models: map[string][]string{ai.SlotIntake: {"claude-haiku-5-5"}}},
		Build:  func(ai.Spec) (aiprovider.Provider, error) { return d.fake, nil }})
	service.SetClock(func() time.Time { return *d.clock })
	d.worker = ai.NewWorker(service, database.New(d.pool), ai.WorkerOptions{MaxRunning: 1, PerWorkspace: 1})
	d.worker.Handle(ai.SlotIntake, d.intake().WithGate(gate).AIHandler())
	return d
}

// runAI runs the AI tasks that are due.
func (d aiDesk) runAI(t *testing.T) {
	t.Helper()
	if err := d.worker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	d.worker.Wait()
}

// answers has the model answer each request with the next decision.
func (d aiDesk) answers(decisions ...map[string]any) {
	for _, decision := range decisions {
		full := map[string]any{"action": "create", "target": "", "runner_up": "", "runner_up_confidence": 0, "command": "", "subject": "",
			"reply_kind": "substantive", "ignore_reason": "", "confidence": 0.5, "reason": "because"}
		for key, value := range decision {
			full[key] = value
		}
		raw, _ := json.Marshal(full)
		d.fake.Script(aiprovider.FakeReply{Response: aiprovider.Response{Text: string(raw), JSON: string(raw), Stop: aiprovider.StopDone, Model: "claude-haiku-5-5",
			Usage: aiprovider.Usage{Reported: true, InputTokens: 100, OutputTokens: 20}}})
	}
}

// decisions lists AI's decisions oldest first as "action/target?/applied|reason/error".
func (d aiDesk) decisions(t *testing.T) string {
	t.Helper()
	return d.one(t, `SELECT COALESCE(string_agg(action||'/'||CASE WHEN applied THEN 'applied' ELSE not_applied_reason END||COALESCE('/'||error_code,''),',' ORDER BY created_at,id),'')
		FROM intake_decisions WHERE workspace_id=$1 AND decider='ai'`)
}

func (d aiDesk) tasks(t *testing.T) string {
	t.Helper()
	return d.one(t, `SELECT COALESCE(string_agg(state||COALESCE(':'||error_code,''),',' ORDER BY state||COALESCE(':'||error_code,'')),'') FROM ai_tasks WHERE workspace_id=$1 AND kind='intake'`)
}

// At auto with every gate closed, AI says what it would do and the rules decide at once, without waiting
// for the card's time: the customer is never kept waiting for AI that may not act.
func TestAtAutoWithTheGateClosedAISuggestsAndTheRulesApplyAtOnce(t *testing.T) {
	d := newAIDesk(t, nil)
	d.intakeAt(t, "auto")
	d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "a", Name: "Lan Nguyen", Group: "g1", Text: "the stock export fails since this morning"})
	d.flush(t)
	if got := d.tasks(t); got != "queued" {
		t.Fatalf("tasks = %s", got)
	}
	d.answers(map[string]any{"action": "create", "subject": "Stock export fails", "confidence": 0.97})
	d.runAI(t)
	if got := d.gatherings(t); got != "created/create/new_thread/rules:1" {
		t.Fatalf("gatherings = %s", got)
	}
	if got := d.one(t, `SELECT fallback::text FROM intake_gatherings WHERE workspace_id=$1`); got != "false" {
		t.Fatalf("fallback = %s", got)
	}
	if got := d.decisions(t); got != "create/gate_closed" {
		t.Fatalf("decisions = %s", got)
	}
	if got := d.one(t, `SELECT model||' '||prompt_version||' '||(manifest->'sources'->0->>'source') FROM intake_decisions WHERE workspace_id=$1`); got != "claude-haiku-5-5 "+prompt.Version+" incoming" {
		t.Fatalf("how it was made = %s", got)
	}
	// The prompt holds labels, not names.
	if requests := d.fake.Requests(); len(requests) != 1 || strings.Contains(requests[0].Messages[0].Text, "Lan Nguyen") ||
		!strings.Contains(requests[0].Messages[0].Text, "Customer A") {
		t.Fatalf("requests = %+v", requests)
	}
}

// Through an open gate, AI sure enough opens the conversation, and joins to it what another customer of
// the group says about the same problem.
func TestThroughAnOpenGateAISureEnoughDecidesTheCard(t *testing.T) {
	d := newAIDesk(t, intake.OpenGate)
	d.intakeAt(t, "auto")
	d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "a", Name: "Lan", Group: "g1", Text: "the stock export fails"})
	d.flush(t)
	d.answers(map[string]any{"action": "create", "subject": "Stock export fails", "confidence": 0.97})
	d.runAI(t)
	conversation := d.one(t, `SELECT COALESCE(conversation_id::text,'') FROM intake_gatherings WHERE workspace_id=$1`)
	if got := d.one(t, `SELECT subject FROM conversations WHERE workspace_id=$1 AND id=$2`, conversation); got != "Stock export fails" {
		t.Fatalf("subject = %s", got)
	}
	// Another customer of the group writes about the same problem: the rules would open a new request.
	d.send(t, channeltest.LoopbackEvent{ID: "b-1", From: "b", Name: "Minh", Group: "g1", Text: "same here, the export fails for me too"})
	d.flush(t)
	d.answers(map[string]any{"action": "attach", "target": "C1", "confidence": 0.95})
	d.runAI(t)
	if got := d.gatherings(t); got != "created/create/new_thread/ai:1,attached/create/new_thread/ai:1" {
		t.Fatalf("gatherings = %s", got)
	}
	if got := d.one(t, `SELECT count(*)::text FROM messages WHERE workspace_id=$1 AND conversation_id=$2 AND kind='incoming'`, conversation); got != "2" {
		t.Fatalf("messages of the conversation = %s", got)
	}
	if got := d.decisions(t); got != "create/applied,attach/applied" {
		t.Fatalf("decisions = %s", got)
	}
}

// AI names a conversation that stopped being one the card may join while it thought (merged into another
// customer's): nothing joins it, the rules decide the card, and AI's decision says the target changed.
func TestAnAIDecisionWhoseConversationWentAwayIsLeftToTheRules(t *testing.T) {
	d := newAIDesk(t, intake.OpenGate)
	d.intakeAt(t, "auto")
	d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "a", Name: "Lan", Group: "g1", Text: "the stock export fails"})
	d.flush(t)
	d.answers(map[string]any{"action": "create", "subject": "Stock export fails", "confidence": 0.97})
	d.runAI(t)
	conversation := d.one(t, `SELECT conversation_id::text FROM intake_gatherings WHERE workspace_id=$1`)
	elsewhere := d.one(t, `WITH c AS (INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Other','other@example.com') RETURNING id)
		INSERT INTO conversations(workspace_id,contact_id,subject) SELECT $1,c.id,'Invoice' FROM c RETURNING id::text`)
	d.send(t, channeltest.LoopbackEvent{ID: "b-1", From: "b", Name: "Minh", Group: "g1", Text: "same here, the export fails for me too"})
	d.flush(t)
	attach, _ := json.Marshal(map[string]any{"action": "attach", "target": "C1", "runner_up": "", "runner_up_confidence": 0, "command": "", "subject": "",
		"reply_kind": "substantive", "ignore_reason": "", "confidence": 0.95, "reason": "same problem"})
	d.fake.Answer = func(aiprovider.Request) (aiprovider.Response, error) {
		// While the model answers, a person merges the conversation it will name into another customer's.
		if _, err := d.pool.Exec(context.Background(), `UPDATE conversations SET status='closed',merged_into_id=$2 WHERE id=$1`, conversation, elsewhere); err != nil {
			return aiprovider.Response{}, err
		}
		return aiprovider.Response{Text: string(attach), JSON: string(attach), Stop: aiprovider.StopDone, Model: "claude-haiku-5-5",
			Usage: aiprovider.Usage{Reported: true, InputTokens: 100, OutputTokens: 20}}, nil
	}
	d.runAI(t)
	if got := d.decisions(t); got != "create/applied,attach/target_changed" {
		t.Fatalf("decisions = %s", got)
	}
	if got := d.gatherings(t); got != "created/create/new_thread/ai:1,created/create/new_thread/rules:1" {
		t.Fatalf("gatherings = %s", got)
	}
	if got := d.one(t, `SELECT count(*)::text FROM messages WHERE workspace_id=$1 AND conversation_id=ANY($2::uuid[]) AND kind='incoming'`,
		[]string{conversation, elsewhere}); got != "1" {
		t.Fatalf("incoming messages of the named conversations = %s", got)
	}
}

// Below its threshold AI only suggests, and the rules decide.
func TestBelowItsThresholdAISuggests(t *testing.T) {
	d := newAIDesk(t, intake.OpenGate)
	d.intakeAt(t, "auto")
	d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "a", Group: "g1", Text: "the stock export fails"})
	d.flush(t)
	d.answers(map[string]any{"action": "ignore", "ignore_reason": "chat", "confidence": 0.6})
	d.runAI(t)
	if got := d.decisions(t); got != "ignore/below_threshold" {
		t.Fatalf("decisions = %s", got)
	}
	if got := d.gatherings(t); got != "created/create/new_thread/rules:1" {
		t.Fatalf("gatherings = %s", got)
	}
}

// A person who decides while AI is reading the card wins; AI's decision says so and changes nothing.
func TestAPersonWhoDecidesWhileAIReadsWins(t *testing.T) {
	d := newAIDesk(t, intake.OpenGate)
	d.intakeAt(t, "auto")
	d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "a", Group: "g1", Text: "the stock export fails"})
	d.flush(t)
	card := d.one(t, `SELECT id::text FROM intake_gatherings WHERE workspace_id=$1`)
	answered := false
	d.fake.Answer = func(request aiprovider.Request) (aiprovider.Response, error) {
		if !answered {
			answered = true
			if _, err := d.intake().Decide(context.Background(), d.workspaceID, card, intake.Decision{Action: intake.Ignore}, store.DecidedByAgent,
				store.ChannelActor{AgentID: d.agentID}); err != nil {
				t.Error(err)
			}
		}
		raw := `{"action":"create","target":"","runner_up":"","runner_up_confidence":0,"command":"","subject":"Export","reply_kind":"substantive","ignore_reason":"","confidence":0.99,"reason":"x"}`
		return aiprovider.Response{Text: raw, JSON: raw, Stop: aiprovider.StopDone, Model: "claude-haiku-5-5"}, nil
	}
	d.runAI(t)
	if got := d.gatherings(t); got != "ignored/create/new_thread/agent:1" {
		t.Fatalf("gatherings = %s", got)
	}
	if got := d.decisions(t); got != "create/person_first" {
		t.Fatalf("decisions = %s", got)
	}
}

// One card of a lane is with AI at a time: the sender's next card waits for its turn, and gets it once the
// card before it is decided.
func TestOneCardOfALaneIsWithAIAtATime(t *testing.T) {
	d := newAIDesk(t, nil)
	d.intakeAt(t, "auto")
	d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "a", Group: "g1", Text: "the stock export fails"})
	d.flush(t)
	d.send(t, channeltest.LoopbackEvent{ID: "a-2", From: "a", Group: "g1", Text: "and the printer too"})
	d.flush(t)
	d.send(t, channeltest.LoopbackEvent{ID: "b-1", From: "b", Group: "g1", Text: "the scale is off"})
	d.flush(t)
	if got := d.gatherings(t); got != "deciding/create/new_thread/-:1,deciding/create/new_thread/-:1,deciding/create/new_thread/-:1" {
		t.Fatalf("gatherings = %s", got)
	}
	if got := d.tasks(t); got != "queued,queued" {
		t.Fatalf("tasks of two lanes = %s", got)
	}
	d.answers(map[string]any{"action": "create", "subject": "Export", "confidence": 0.9}, map[string]any{"action": "create", "subject": "Scale", "confidence": 0.9})
	d.runAI(t)
	// The first card decided, its sender's next card has its turn; the other lane still waits for the worker.
	if got := d.tasks(t); got != "done,queued,queued" {
		t.Fatalf("tasks after the first card = %s", got)
	}
	d.answers(map[string]any{"action": "create", "subject": "Printer", "confidence": 0.9})
	d.runAI(t)
	d.runAI(t)
	if got := d.tasks(t); got != "done,done,done" {
		t.Fatalf("tasks at the end = %s", got)
	}
}

// Cards that waited behind one a person decides get their turn with AI one after the other, even while
// more of them still wait behind.
func TestALaneBlockedBehindAPersonGoesBackToAI(t *testing.T) {
	d := newAIDesk(t, nil)
	d.intakeAt(t, "auto")
	d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "a", Group: "g1", Text: "the stock export fails"})
	d.flush(t)
	first := d.one(t, `SELECT id::text FROM intake_gatherings WHERE workspace_id=$1`)
	actor := store.ChannelActor{AgentID: d.agentID}
	if _, err := d.intake().Decide(context.Background(), d.workspaceID, first, intake.Decision{Action: intake.Review, ReviewReason: "ai_unsure"}, store.DecidedByAI, store.ChannelActor{}); err != nil {
		t.Fatal(err)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "a-2", From: "a", Group: "g1", Text: "and the printer"})
	d.flush(t)
	d.send(t, channeltest.LoopbackEvent{ID: "a-3", From: "a", Group: "g1", Text: "and the scale"})
	d.flush(t)
	if got := d.one(t, `SELECT string_agg(state||':'||COALESCE(review_reason,'-'),',' ORDER BY closed_at) FROM intake_gatherings WHERE workspace_id=$1`); got != "review:ai_unsure,review:lane_blocked,review:lane_blocked" {
		t.Fatalf("gatherings = %s", got)
	}
	if _, err := d.intake().Decide(context.Background(), d.workspaceID, first, intake.Decision{Action: intake.Ignore}, store.DecidedByAgent, actor); err != nil {
		t.Fatal(err)
	}
	if got := d.one(t, `SELECT string_agg(g.state||':'||COALESCE(t.state,'-'),',' ORDER BY g.closed_at) FROM intake_gatherings g
		LEFT JOIN ai_tasks t ON t.workspace_id=g.workspace_id AND t.gathering_id=g.id WHERE g.workspace_id=$1`); got != "ignored:queued,deciding:queued,review:-" {
		t.Fatalf("gatherings and tasks = %s", got)
	}
}

// A customer whose batch is held is the same person in the prompt as in the conversation they opened before.
func TestAHeldCustomerIsTheSamePersonAsInTheirConversation(t *testing.T) {
	d := newAIDesk(t, nil)
	d.intakeAt(t, "auto")
	d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "a", Name: "Lan", Group: "g1", Text: "the stock export fails"})
	d.flush(t)
	d.answers(map[string]any{"action": "create", "subject": "Export", "confidence": 0.9})
	d.runAI(t)
	// Without the thread's key the rules no longer know the next batch continues it: it is held for AI.
	if _, err := d.pool.Exec(context.Background(), `DELETE FROM conversation_thread_keys WHERE workspace_id=$1`, d.workspaceID); err != nil {
		t.Fatal(err)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "a-2", From: "a", Name: "Lan", Group: "g1", Text: "still failing"})
	d.flush(t)
	if got := d.tasks(t); got != "done,queued" {
		t.Fatalf("tasks = %s", got)
	}
	d.answers(map[string]any{"action": "attach", "target": "C1", "confidence": 0.9})
	d.runAI(t)
	requests := d.fake.Requests()
	prompt := requests[len(requests)-1].Messages[0].Text
	if !strings.Contains(prompt, "C1 · reported by Customer A") || strings.Contains(prompt, "Customer B") || !strings.Contains(prompt, ">> ") {
		t.Fatalf("prompt:\n%s", prompt)
	}
}

// An answer naming a conversation it was not offered is asked once more; twice is an error, and the
// rules decide.
func TestAnUnknownConversationTwiceIsAnErrorAndTheRulesDecide(t *testing.T) {
	d := newAIDesk(t, intake.OpenGate)
	d.intakeAt(t, "auto")
	d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "a", Group: "g1", Text: "the stock export fails"})
	d.flush(t)
	d.answers(map[string]any{"action": "attach", "target": "C9", "confidence": 0.99}, map[string]any{"action": "attach", "target": "C4", "confidence": 0.99})
	d.runAI(t)
	if got := d.decisions(t); got != "review/error/target_invalid" {
		t.Fatalf("decisions = %s", got)
	}
	if got := d.one(t, `SELECT state||'/'||decided_by||'/'||fallback::text FROM intake_gatherings WHERE workspace_id=$1`); got != "created/rules/true" {
		t.Fatalf("gathering = %s", got)
	}
	card := d.one(t, `SELECT id::text FROM intake_gatherings WHERE workspace_id=$1`)
	detail, err := d.store.IntakeGatheringByID(context.Background(), d.workspaceID, card, *d.clock)
	if err != nil || len(detail.Decisions) != 1 || detail.Decisions[0].ErrorCode == nil || *detail.Decisions[0].ErrorCode != "target_invalid" {
		t.Fatalf("decisions = %+v, %v", detail.Decisions, err)
	}
	if n := len(d.fake.Requests()); n != 2 {
		t.Fatalf("requests = %d", n)
	}
}

// What holds a credential is never decided by AI on its own, and the credential never reaches it.
func TestACredentialIsNeverAppliedByAI(t *testing.T) {
	d := newAIDesk(t, intake.OpenGate)
	d.intakeAt(t, "auto")
	d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "a", Group: "g1", Text: "lan.store12@example.com"},
		channeltest.LoopbackEvent{ID: "a-2", From: "a", Group: "g1", Text: "Shop@2026"})
	d.flush(t)
	d.answers(map[string]any{"action": "create", "subject": "Login", "confidence": 0.99})
	d.runAI(t)
	if got := d.decisions(t); got != "create/credential" {
		t.Fatalf("decisions = %s", got)
	}
	if requests := d.fake.Requests(); len(requests) != 1 || strings.Contains(requests[0].Messages[0].Text, "Shop@2026") {
		t.Fatalf("the credential reached the model: %+v", requests)
	}
}

// At suggest the rules decide as they always did and AI's suggestion shows on the card.
func TestAtSuggestTheCardShowsWhatAIWouldDo(t *testing.T) {
	d := newAIDesk(t, intake.OpenGate)
	d.intakeAt(t, "suggest")
	d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "a", Group: "g1", Text: "the stock export fails"})
	d.flush(t)
	if got := d.gatherings(t); got != "applied/create/new_thread/rules:1" {
		t.Fatalf("gatherings = %s", got)
	}
	d.answers(map[string]any{"action": "create", "subject": "Export", "confidence": 0.99})
	d.runAI(t)
	if got := d.decisions(t); got != "create/level_suggest" {
		t.Fatalf("decisions = %s", got)
	}
	card := d.one(t, `SELECT id::text FROM intake_gatherings WHERE workspace_id=$1`)
	detail, err := d.store.IntakeGatheringByID(context.Background(), d.workspaceID, card, *d.clock)
	if err != nil || detail.Suggestion == nil || detail.Suggestion.Action != "create" || detail.Suggestion.NotAppliedReason != "level_suggest" {
		t.Fatalf("suggestion = %+v, %v", detail.Suggestion, err)
	}
}

// A card AI will not read (the day's intake is used) is the rules' at once.
func TestACardAIWillNotReadIsTheRulesAtOnce(t *testing.T) {
	d := newAIDesk(t, intake.OpenGate)
	d.intakeAt(t, "auto")
	if _, err := d.pool.Exec(context.Background(), `INSERT INTO ai_automatic_limits(workspace_id,per_day,per_minute,intake_per_day) VALUES($1,300,30,0)
		ON CONFLICT (workspace_id) DO UPDATE SET intake_per_day=0`, d.workspaceID); err != nil {
		t.Fatal(err)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "a", Group: "g1", Text: "the stock export fails"})
	d.flush(t)
	d.runAI(t)
	if got := d.tasks(t); got != "cancelled:intake_limit" {
		t.Fatalf("tasks = %s", got)
	}
	if n, err := d.intake().Sweep(context.Background()); err != nil || n != 1 {
		t.Fatalf("sweep = %d, %v", n, err)
	}
	if got := d.gatherings(t); got != "created/create/new_thread/rules:1" {
		t.Fatalf("gatherings = %s", got)
	}
}

// After five failed calls in a row a workspace's intake stops calling the model for ten minutes: its
// cards go to the rules at once.
func TestIntakeStopsCallingAFailingModelForAWhile(t *testing.T) {
	d := newAIDesk(t, intake.OpenGate)
	d.intakeAt(t, "auto")
	for i := 0; i < 5; i++ {
		d.fake.Script(aiprovider.FakeReply{Err: &aiprovider.Error{Code: aiprovider.ErrorQuotaExhausted}})
	}
	for _, sender := range []string{"a", "b", "c", "e", "f", "g"} {
		d.send(t, channeltest.LoopbackEvent{ID: sender + "-1", From: sender, Group: "g1", Text: "the stock export fails for " + sender})
	}
	d.flush(t)
	for i := 0; i < 6; i++ {
		d.runAI(t)
	}
	if got := d.tasks(t); got != "failed:intake_paused,failed:quota_exhausted,failed:quota_exhausted,failed:quota_exhausted,failed:quota_exhausted,failed:quota_exhausted" {
		t.Fatalf("tasks = %s", got)
	}
	if n := len(d.fake.Requests()); n != 5 {
		t.Fatalf("calls = %d", n)
	}
	if got := d.one(t, `SELECT count(*)::text FROM intake_gatherings WHERE workspace_id=$1 AND state='deciding' AND decide_by<=$2`, *d.clock); got != "6" {
		t.Fatalf("cards handed to the rules = %s", got)
	}
	*d.clock = d.clock.Add(11 * time.Minute)
	d.send(t, channeltest.LoopbackEvent{ID: "h-1", From: "h", Group: "g1", Text: "the printer is off"})
	d.flush(t)
	d.answers(map[string]any{"action": "create", "subject": "Printer", "confidence": 0.9})
	d.runAI(t)
	if n := len(d.fake.Requests()); n != 6 {
		t.Fatalf("calls after the pause = %d", n)
	}
}

// A scope turned off after its card was queued sends nothing to the model; the card is the rules' at once.
func TestAScopeTurnedOffSendsNothingToTheModel(t *testing.T) {
	d := newAIDesk(t, intake.OpenGate)
	d.intakeAt(t, "auto")
	d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "a", Group: "g1", Text: "the stock export fails"})
	d.flush(t)
	d.intakeAt(t, "off")
	d.runAI(t)
	if n := len(d.fake.Requests()); n != 0 {
		t.Fatalf("requests to the model after intake went off = %d", n)
	}
	if got := d.tasks(t); got != "cancelled:automation_off" {
		t.Fatalf("tasks = %s", got)
	}
	if n, err := d.intake().Sweep(context.Background()); err != nil || n != 1 {
		t.Fatalf("sweep = %d, %v", n, err)
	}
}

// A customer's picture reaches the model only when the workspace agreed to send pictures and the model
// reads them; a card that is only pictures is then something AI can decide on, and one that talks about
// a password keeps its pictures.
func TestACardsPicturesReachAModelThatReadsThem(t *testing.T) {
	d := newAIDesk(t, intake.OpenGate)
	d.intakeAt(t, "auto")
	// A chat type serves pictures by link; the frame fetches them before the batch is held.
	server, _ := mediaServer(t)
	d.pipeline = guardedPipeline(d.loopDesk, server)
	picture := []channeltest.LoopbackLink{{URL: server.URL + "/ok.txt", Name: "screen.png", Type: "image/png"}}
	imagesOf := func() (int, string) {
		requests := d.fake.Requests()
		last := requests[len(requests)-1]
		return len(last.Messages[0].Images), d.one(t, `SELECT COALESCE(manifest->'sources'->4->>'skipped','')||'/'||(manifest->'sources'->4->>'items')
			FROM intake_decisions WHERE workspace_id=$1 ORDER BY created_at DESC LIMIT 1`)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "a", Group: "g1", Links: picture})
	d.flush(t)
	d.answers(map[string]any{"action": "create", "subject": "Screen", "confidence": 0.5})
	d.runAI(t)
	if n, manifest := imagesOf(); n != 0 || manifest != "consent/0" {
		t.Fatalf("without consent: %d pictures, manifest %s", n, manifest)
	}
	if _, err := d.pool.Exec(context.Background(), `INSERT INTO ai_source_consents(workspace_id,source_class) VALUES($1,'images')`, d.workspaceID); err != nil {
		t.Fatal(err)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "b-1", From: "b", Group: "g1", Links: picture})
	d.flush(t)
	d.answers(map[string]any{"action": "create", "subject": "Screen", "confidence": 0.5})
	d.runAI(t)
	if n, manifest := imagesOf(); n != 0 || manifest != "model_text_only/0" {
		t.Fatalf("a model that reads no pictures: %d pictures, manifest %s", n, manifest)
	}
	d.fake.ImageModels = []string{"claude-haiku-5-5"}
	d.send(t, channeltest.LoopbackEvent{ID: "c-1", From: "c", Group: "g1", Links: picture})
	d.flush(t)
	d.answers(map[string]any{"action": "create", "subject": "Screen", "confidence": 0.95})
	d.runAI(t)
	if n, manifest := imagesOf(); n != 1 || manifest != "/1" {
		t.Fatalf("with consent and a model that reads them: %d pictures, manifest %s", n, manifest)
	}
	if got := d.decisions(t); !strings.HasSuffix(got, ",create/applied") {
		t.Fatalf("a card of pictures only = %s, want it decided", got)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "e-1", From: "e", Group: "g1", Text: "my password does not work", Links: picture})
	d.flush(t)
	d.answers(map[string]any{"action": "create", "subject": "Login", "confidence": 0.95})
	d.runAI(t)
	if n, manifest := imagesOf(); n != 0 || manifest != "credential/0" {
		t.Fatalf("pictures with talk of a password: %d pictures, manifest %s", n, manifest)
	}
}

// A thank-you AI joins to a resolved conversation keeps it resolved and asks for no automatic draft; what
// adds something to it reopens it.
func TestAThankYouKeepsAResolvedConversationResolved(t *testing.T) {
	d := newAIDesk(t, intake.OpenGate)
	d.intakeAt(t, "auto")
	if _, err := d.pool.Exec(context.Background(), `INSERT INTO ai_slots(workspace_id,slot,uses_server,model,enabled,default_level,consented_by,consented_at,updated_by,updated_at)
		VALUES($1,'draft',true,'claude-haiku-5-5',true,'suggest',$2,now(),$2,now()) ON CONFLICT (workspace_id,slot) DO UPDATE SET default_level='suggest',enabled=true`,
		d.workspaceID, d.agentID); err != nil {
		t.Fatal(err)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "a-1", From: "a", Group: "g1", Text: "the stock export fails"})
	d.flush(t)
	d.answers(map[string]any{"action": "create", "subject": "Stock export fails", "confidence": 0.97})
	d.runAI(t)
	conversation := d.one(t, `SELECT conversation_id::text FROM intake_gatherings WHERE workspace_id=$1`)
	if _, err := d.pool.Exec(context.Background(), `UPDATE conversations SET status='resolved' WHERE id=$1`, conversation); err != nil {
		t.Fatal(err)
	}
	drafts := func() string {
		return d.one(t, `SELECT count(*)::text FROM ai_tasks WHERE workspace_id=$1 AND kind='draft' AND conversation_id=$2`, conversation)
	}
	before := drafts()
	d.send(t, channeltest.LoopbackEvent{ID: "b-1", From: "b", Group: "g1", Text: "thanks, it works now"})
	d.flush(t)
	d.answers(map[string]any{"action": "attach", "target": "C1", "reply_kind": "acknowledgement", "confidence": 0.96})
	d.runAI(t)
	if got := d.one(t, `SELECT status FROM conversations WHERE workspace_id=$1 AND id=$2`, conversation); got != "resolved" {
		t.Fatalf("after a thank-you the conversation is %s", got)
	}
	if got := d.one(t, `SELECT COALESCE(string_agg(COALESCE(intake_reply_kind,'-'),',' ORDER BY created_at),'') FROM messages
		WHERE workspace_id=$1 AND conversation_id=$2 AND kind='incoming'`, conversation); got != "-,acknowledgement" {
		t.Fatalf("reply kinds = %s", got)
	}
	if got := drafts(); got != before {
		t.Fatalf("automatic drafts %s → %s after a thank-you", before, got)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "c-1", From: "c", Group: "g1", Text: "it fails again for me"})
	d.flush(t)
	d.answers(map[string]any{"action": "attach", "target": "C1", "confidence": 0.96})
	d.runAI(t)
	if got := d.one(t, `SELECT status FROM conversations WHERE workspace_id=$1 AND id=$2`, conversation); got != "open" {
		t.Fatalf("after more to do the conversation is %s", got)
	}
	if got := drafts(); got == before {
		t.Fatalf("no automatic draft for what reopened it (%s)", got)
	}
	// A thank-you to a conversation that is open kept nothing resolved: its message does not say it did.
	d.send(t, channeltest.LoopbackEvent{ID: "f-1", From: "f", Group: "g1", Text: "thanks for looking"})
	d.flush(t)
	d.answers(map[string]any{"action": "attach", "target": "C1", "reply_kind": "acknowledgement", "confidence": 0.96})
	d.runAI(t)
	if got := d.one(t, `SELECT COALESCE(m.intake_reply_kind,'-') FROM messages m WHERE m.workspace_id=$1 AND m.conversation_id=$2 AND m.body='thanks for looking'`,
		conversation); got != "substantive" {
		t.Fatalf("a thank-you to an open conversation is marked %s", got)
	}
	// A thank-you joined to a conversation closed meanwhile (a person following AI late) opens it: a person sees it.
	if _, err := d.pool.Exec(context.Background(), `UPDATE conversations SET status='closed' WHERE id=$1`, conversation); err != nil {
		t.Fatal(err)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "e-1", From: "e", Group: "g1", Text: "thank you all"})
	d.flush(t)
	card := d.one(t, `SELECT id::text FROM intake_gatherings WHERE workspace_id=$1 AND state='deciding'`)
	if _, err := d.intake().Decide(context.Background(), d.workspaceID, card, intake.Decision{Action: intake.Attach, ConversationID: conversation,
		ReplyKind: "acknowledgement"}, store.DecidedByAgent, store.ChannelActor{AgentID: d.agentID}); err != nil {
		t.Fatal(err)
	}
	if got := d.one(t, `SELECT status FROM conversations WHERE workspace_id=$1 AND id=$2`, conversation); got != "open" {
		t.Fatalf("after a thank-you to a closed conversation it is %s", got)
	}
}
