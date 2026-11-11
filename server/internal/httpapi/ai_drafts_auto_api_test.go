package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
)

// autoDraftsOn lets workspace A's drafts write themselves when a customer writes.
func (d draftDesk) autoDraftsOn(t *testing.T) {
	t.Helper()
	var settings ai.Settings
	decode(t, request(t, d.api, http.MethodGet, "/api/v1/ai", nil, d.adminA, nil), &settings)
	response := request(t, d.api, http.MethodPut, "/api/v1/ai/slots/draft", map[string]any{"source": "provider", "provider_id": settings.Providers[0].ID, "enabled": true, "automatic": true}, d.adminA, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("turn automatic drafts on: %d %s", response.Code, response.Body.String())
	}
}

// due makes the conversation's waiting automatic draft due now, past its minute of waiting for more.
func (d draftDesk) due(t *testing.T, conversation string) {
	t.Helper()
	if _, err := d.pool.Exec(context.Background(), `UPDATE ai_tasks SET next_run_at=now()-interval '1 second' WHERE conversation_id=$1 AND kind='draft' AND automatic AND state='queued'`, conversation); err != nil {
		t.Fatal(err)
	}
}

func (d draftDesk) automaticTasks(t *testing.T, conversation string) string {
	t.Helper()
	return queryString(t, d.pool, `SELECT count(*)::text||':'||COALESCE(string_agg(state||'/'||COALESCE(error_code,'-'),',' ORDER BY created_at),'')
		FROM ai_tasks WHERE conversation_id=$1 AND kind='draft' AND automatic`, conversation)
}

func (d draftDesk) automaticDraft(t *testing.T, token, conversation string) *ai.Draft {
	t.Helper()
	var list struct {
		Items []ai.Draft `json:"items"`
	}
	decode(t, request(t, d.api, http.MethodGet, "/api/v1/conversations/"+conversation+"/ai-drafts", nil, token, nil), &list)
	for _, draft := range list.Items {
		if draft.Automatic {
			return &draft
		}
	}
	return nil
}

func TestADraftWritesItselfWhenACustomerWritesOnlyWhenAnAdminLetsIt(t *testing.T) {
	d := newDraftDesk(t)
	contact := create(t, d.api, d.adminA, "/api/v1/contacts", map[string]any{"name": "Lin", "email": "lin@customer-a.example"})
	before := create(t, d.api, d.adminA, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Before"})
	d.customerWrites(t, d.workspaceA, before, "Hello?", nil)
	if got := d.automaticTasks(t, before); got != "0:" {
		t.Fatalf("automatic drafts while off = %s", got)
	}

	d.autoDraftsOn(t)
	conversation := create(t, d.api, d.adminA, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Printer jam"})
	// Three messages in a row get one draft, which answers the newest.
	for _, body := range []string{"My printer jams.", "On tray two.", "Forgot: it is the office one."} {
		d.customerWrites(t, d.workspaceA, conversation, body, nil)
	}
	if got := d.automaticTasks(t, conversation); got != "1:queued/-" {
		t.Fatalf("automatic drafts after three messages = %s", got)
	}
	wait := queryString(t, d.pool, `SELECT (extract(epoch FROM next_run_at-now())>30)::text FROM ai_tasks WHERE conversation_id=$1 AND kind='draft' AND automatic`, conversation)
	if wait != "true" {
		t.Fatal("an automatic draft does not wait for more messages")
	}
	newest := queryString(t, d.pool, `SELECT id::text FROM messages WHERE conversation_id=$1 ORDER BY created_at DESC,id DESC LIMIT 1`, conversation)
	if answers := queryString(t, d.pool, `SELECT answers_message_id::text FROM ai_drafts WHERE conversation_id=$1 AND automatic`, conversation); answers != newest {
		t.Fatalf("the draft answers %s, want the newest %s", answers, newest)
	}
	// Not for an import, nor for mail that failed its sender check, nor in the other workspace.
	d.customerWrites(t, d.workspaceA, before, "Imported", `{"system":"freshdesk","external_id":"9","kind":"reply"}`)
	if _, err := d.pool.Exec(context.Background(), `INSERT INTO messages(workspace_id,conversation_id,kind,body,email_meta) VALUES($1,$2,'incoming','Spoofed','{"authentication":"fail"}')`, d.workspaceA, before); err != nil {
		t.Fatal(err)
	}
	if got := d.automaticTasks(t, before); got != "0:" {
		t.Fatalf("automatic drafts for an import or failed mail = %s", got)
	}
	d.customerWrites(t, d.workspaceB, d.convoB, "Hello from B", nil)
	if got := d.automaticTasks(t, d.convoB); got != "0:" {
		t.Fatalf("workspace B got an automatic draft: %s", got)
	}

	// Written: the whole team sees it, it says whether a reply is needed, and nothing was sent.
	d.fake.Script(draftAnswer("Hi Lin, open tray two of the office printer.", []string{"kb1"}, ""))
	d.due(t, conversation)
	d.run(t)
	agent := joinAsAgent(t, d.api, d.adminA, "sam@draft-a.example.com")
	got := d.automaticDraft(t, agent, conversation)
	if got == nil || got.Status != ai.DraftReady || got.ReplyNeeded == nil || !*got.ReplyNeeded || got.RequestedByAgentID != nil || got.AnswersMessageID == nil {
		t.Fatalf("automatic draft = %+v", got)
	}
	if usage := queryString(t, d.pool, `SELECT count(*)::text||' '||bool_and(automatic)::text FROM ai_usage WHERE workspace_id=$1 AND feature='draft'`, d.workspaceA); usage != "1 true" {
		t.Fatalf("automatic draft usage = %s", usage)
	}
	if sent := queryString(t, d.pool, `SELECT count(*)::text FROM messages WHERE conversation_id=$1 AND kind='outgoing'`, conversation); sent != "0" {
		t.Fatalf("%s replies were sent", sent)
	}

	// The customer writes again: a new one waits; the ready one stays usable until the new one is written.
	d.customerWrites(t, d.workspaceA, conversation, "Still jammed.", nil)
	if outcome := queryString(t, d.pool, `SELECT COALESCE(outcome,'open') FROM ai_drafts WHERE id=$1`, got.ID); outcome != "open" {
		t.Fatalf("old automatic draft outcome = %s", outcome)
	}
	// A person asks for a draft: the waiting automatic one stops, no call spent.
	d.fake.Script(draftAnswer("Hi Lin, I am on it.", nil, ""))
	d.ask(t, d.adminA, conversation, "")
	if got := d.automaticTasks(t, conversation); got != "2:done/-,cancelled/cancelled" {
		t.Fatalf("automatic drafts after a person asked = %s", got)
	}
	// A reply from the team stops a waiting one.
	d.customerWrites(t, d.workspaceA, conversation, "Any news?", nil)
	kbCall(t, d.api, http.MethodPost, "/api/v1/conversations/"+conversation+"/messages", map[string]any{"kind": "outgoing", "body": "Working on it."}, d.adminA, http.StatusCreated, nil)
	if got := d.automaticTasks(t, conversation); got != "3:done/-,cancelled/cancelled,cancelled/answered" {
		t.Fatalf("automatic drafts after a reply = %s", got)
	}
}

func TestAutomaticDraftsStopAtTheirLimitsAndLeaveTriageAndPeople(t *testing.T) {
	d := newDraftDesk(t)
	d.triageOn(t)
	d.autoDraftsOn(t)
	draftsPerDay := 0
	if _, err := d.service.SetAutomaticLimits(context.Background(), d.workspaceA, ai.Actor{}, 300, 30, &draftsPerDay, nil); err != nil {
		t.Fatal(err)
	}
	contact := create(t, d.api, d.adminA, "/api/v1/contacts", map[string]any{"name": "Lin", "email": "lin@customer-a.example"})
	conversation := create(t, d.api, d.adminA, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Printer down"})
	d.fake.Script(triageAnswer(nil, "high", ""))
	d.customerWrites(t, d.workspaceA, conversation, "The printer is down.", nil)
	d.due(t, conversation)
	d.run(t)
	if got := d.automaticTasks(t, conversation); got != "1:cancelled/draft_limit" {
		t.Fatalf("past the drafts of the day = %s", got)
	}
	if triage := d.triage(t, d.adminA, conversation); triage.Status != ai.DraftReady {
		t.Fatalf("triage past the drafts of the day = %+v", triage)
	}
	var settings ai.Settings
	decode(t, request(t, d.api, http.MethodGet, "/api/v1/ai", nil, d.adminA, nil), &settings)
	if settings.Automatic.DraftPerDay != 0 || settings.Automatic.DraftsPausedUntil == nil {
		t.Fatalf("automatic limits = %+v", settings.Automatic)
	}
	// Five automatic drafts on one conversation in a day: the sixth is not written.
	draftsPerDay = 100
	if _, err := d.service.SetAutomaticLimits(context.Background(), d.workspaceA, ai.Actor{}, 300, 30, &draftsPerDay, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := d.pool.Exec(context.Background(), `INSERT INTO ai_tasks(workspace_id,kind,conversation_id,automatic,state,started_at,finished_at)
		SELECT $1,'draft',$2,true,'done',now()-g*interval '1 hour',now() FROM generate_series(1,5) g`, d.workspaceA, conversation); err != nil {
		t.Fatal(err)
	}
	// The fifth was written and nobody used it yet.
	if _, err := d.pool.Exec(context.Background(), `INSERT INTO ai_drafts(workspace_id,conversation_id,status,automatic,text,ready_at,reply_needed)
		VALUES ($1,$2,'ready',true,'Hi Lin, the fifth.',now(),true)`, d.workspaceA, conversation); err != nil {
		t.Fatal(err)
	}
	d.customerWrites(t, d.workspaceA, conversation, "Hello again.", nil)
	d.due(t, conversation)
	d.run(t)
	if code := queryString(t, d.pool, `SELECT error_code FROM ai_tasks WHERE conversation_id=$1 AND kind='draft' AND automatic ORDER BY created_at DESC LIMIT 1`, conversation); code != "conversation_cap" {
		t.Fatalf("sixth automatic draft = %s", code)
	}
	// The draft the team had stays usable: a new one replaces it only once written.
	if got := queryString(t, d.pool, `SELECT status||'/'||COALESCE(outcome,'-') FROM ai_drafts WHERE conversation_id=$1 AND text='Hi Lin, the fifth.'`, conversation); got != "ready/-" {
		t.Fatalf("the fifth draft after the sixth message = %s", got)
	}
	// A person still gets one.
	d.fake.Script(draftAnswer("Hi Lin.", nil, ""))
	draft := d.ask(t, d.adminA, conversation, "")
	d.run(t)
	if got := d.get(t, d.adminA, conversation, draft.ID); got.Status != ai.DraftReady {
		t.Fatalf("a person's draft past the automatic limits = %+v", got)
	}
}

// Automatic drafts do not count against the 30 drafts a minute people may ask for.
func TestAutomaticDraftsLeavePeoplesMinuteCap(t *testing.T) {
	d := newDraftDesk(t)
	d.autoDraftsOn(t)
	if _, err := d.pool.Exec(context.Background(), `INSERT INTO ai_drafts(workspace_id,conversation_id,status,automatic)
		SELECT $1,$2,'cancelled',true FROM generate_series(1,30)`, d.workspaceA, d.conversationA); err != nil {
		t.Fatal(err)
	}
	d.fake.Script(draftAnswer("Hi.", nil, ""))
	d.ask(t, d.adminA, d.conversationA, "")
}

// While an automatic draft is written: a teammate's reply sets it aside unseen; a newer customer message
// queues a newer draft, which replaces it once written; a try again with a newer one waiting ends it.
func TestAnAutomaticDraftOvertakenWhileWrittenIsNotShown(t *testing.T) {
	d := newDraftDesk(t)
	d.autoDraftsOn(t)
	ctx := context.Background()
	contact := create(t, d.api, d.adminA, "/api/v1/contacts", map[string]any{"name": "Lin", "email": "lin@customer-a.example"})

	replied := create(t, d.api, d.adminA, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Replied"})
	d.customerWrites(t, d.workspaceA, replied, "Printer jam?", nil)
	d.fake.Answer = func(aiprovider.Request) (aiprovider.Response, error) {
		if _, err := d.pool.Exec(ctx, `INSERT INTO messages(workspace_id,conversation_id,kind,body) VALUES($1,$2,'outgoing','Already on it.')`, d.workspaceA, replied); err != nil {
			t.Error(err)
		}
		return draftAnswer("Hi Lin.", nil, "").Response, nil
	}
	d.due(t, replied)
	d.run(t)
	if status := queryString(t, d.pool, `SELECT status FROM ai_drafts WHERE conversation_id=$1 AND automatic`, replied); status != "cancelled" {
		t.Fatalf("automatic draft overtaken by a reply = %s", status)
	}

	newer := create(t, d.api, d.adminA, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Newer"})
	d.customerWrites(t, d.workspaceA, newer, "Printer jam?", nil)
	d.fake.Answer = func(aiprovider.Request) (aiprovider.Response, error) {
		d.fake.Answer = nil
		d.customerWrites(t, d.workspaceA, newer, "And the scanner too.", nil)
		return draftAnswer("Hi Lin, about the printer.", nil, "").Response, nil
	}
	d.due(t, newer)
	d.run(t)
	d.fake.Script(draftAnswer("Hi Lin, about both.", nil, ""))
	d.due(t, newer)
	d.run(t)
	if got := queryString(t, d.pool, `SELECT string_agg(status||'/'||COALESCE(outcome,'-'),',' ORDER BY created_at) FROM ai_drafts WHERE conversation_id=$1 AND automatic`, newer); got != "ready/superseded,ready/-" {
		t.Fatalf("automatic drafts after a newer message = %s", got)
	}

	// A newer one queued while a draft was written, then stopped by the conversation's cap: the draft
	// written stays usable.
	capped := create(t, d.api, d.adminA, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Capped"})
	d.customerWrites(t, d.workspaceA, capped, "Printer jam?", nil)
	d.fake.Answer = func(aiprovider.Request) (aiprovider.Response, error) {
		d.fake.Answer = nil
		d.customerWrites(t, d.workspaceA, capped, "Hello?", nil)
		if _, err := d.pool.Exec(ctx, `INSERT INTO ai_tasks(workspace_id,kind,conversation_id,automatic,state,started_at,finished_at)
			SELECT $1,'draft',$2,true,'done',now()-g*interval '1 hour',now() FROM generate_series(1,5) g`, d.workspaceA, capped); err != nil {
			t.Error(err)
		}
		return draftAnswer("Hi Lin, about the printer.", nil, "").Response, nil
	}
	d.due(t, capped)
	d.run(t)
	d.due(t, capped)
	d.run(t)
	if got := queryString(t, d.pool, `SELECT string_agg(status||'/'||COALESCE(outcome,'-'),',' ORDER BY created_at) FROM ai_drafts WHERE conversation_id=$1 AND automatic`, capped); got != "ready/-,pending/-" || !strings.Contains(d.automaticTasks(t, capped), "cancelled/conversation_cap") {
		t.Fatalf("automatic drafts after a newer one hit the cap = %s, tasks %s", got, d.automaticTasks(t, capped))
	}

	// A slow call finishes after the newer draft was written: the older draft stays out of sight.
	slow := create(t, d.api, d.adminA, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Slow"})
	d.customerWrites(t, d.workspaceA, slow, "Printer jam?", nil)
	d.fake.Answer = func(aiprovider.Request) (aiprovider.Response, error) {
		d.fake.Answer = nil
		d.customerWrites(t, d.workspaceA, slow, "And the scanner too.", nil)
		d.fake.Script(draftAnswer("Hi Lin, about both.", nil, ""))
		d.due(t, slow)
		if err := d.worker.Tick(context.Background()); err != nil {
			t.Error(err)
		}
		for range 100 {
			if queryString(t, d.pool, `SELECT count(*)::text FROM ai_drafts WHERE conversation_id=$1 AND status='ready'`, slow) == "1" {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		return draftAnswer("Hi Lin, about the printer.", nil, "").Response, nil
	}
	d.due(t, slow)
	d.run(t)
	if got := queryString(t, d.pool, `SELECT string_agg(status||'/'||COALESCE(outcome,'-')||'/'||COALESCE(text,''),',' ORDER BY created_at) FROM ai_drafts WHERE conversation_id=$1 AND automatic`, slow); got != "cancelled/-/,ready/-/Hi Lin, about both." {
		t.Fatalf("automatic drafts after a slow call = %s", got)
	}

	retried := create(t, d.api, d.adminA, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Retried"})
	d.customerWrites(t, d.workspaceA, retried, "Printer jam?", nil)
	d.fake.Answer = func(aiprovider.Request) (aiprovider.Response, error) {
		d.fake.Answer = nil
		d.customerWrites(t, d.workspaceA, retried, "Hello?", nil)
		return aiprovider.Response{}, &aiprovider.Error{Code: aiprovider.ErrorOverloaded}
	}
	d.due(t, retried)
	d.run(t)
	if got := d.automaticTasks(t, retried); got != "2:cancelled/superseded,queued/-" {
		t.Fatalf("automatic drafts after a try again with a newer one waiting = %s", got)
	}

	// A try again after a teammate replied is not written.
	again := create(t, d.api, d.adminA, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Again"})
	d.customerWrites(t, d.workspaceA, again, "Printer jam?", nil)
	d.fake.Script(aiprovider.FakeReply{Err: &aiprovider.Error{Code: aiprovider.ErrorOverloaded}})
	d.due(t, again)
	d.run(t)
	if _, err := d.pool.Exec(ctx, `INSERT INTO messages(workspace_id,conversation_id,kind,body) VALUES($1,$2,'outgoing','Done.')`, d.workspaceA, again); err != nil {
		t.Fatal(err)
	}
	if _, err := d.pool.Exec(ctx, `UPDATE ai_tasks SET next_run_at=now()-interval '1 second' WHERE conversation_id=$1 AND state='queued'`, again); err != nil {
		t.Fatal(err)
	}
	calls := len(d.draftRequests())
	d.run(t)
	if got := d.automaticTasks(t, again); got != "1:cancelled/answered" || len(d.draftRequests()) != calls {
		t.Fatalf("try again after a reply = %s, %d calls", got, len(d.draftRequests())-calls)
	}
}

// The channel's own received notice answers nobody: the automatic draft the customer's message asked for
// still waits, and is written.
func TestTheReceivedNoticeLeavesTheAutomaticDraftWaiting(t *testing.T) {
	d := newDraftDesk(t)
	d.autoDraftsOn(t)
	contact := create(t, d.api, d.adminA, "/api/v1/contacts", map[string]any{"name": "Lin", "email": "lin@customer-a.example"})
	conversation := create(t, d.api, d.adminA, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Printer jam"})
	d.customerWrites(t, d.workspaceA, conversation, "My printer jams.", nil)
	if _, err := d.pool.Exec(context.Background(), `INSERT INTO messages(workspace_id,conversation_id,kind,body,notice_kind) VALUES($1,$2,'outgoing','We got your message.','received')`,
		d.workspaceA, conversation); err != nil {
		t.Fatal(err)
	}
	if got := d.automaticTasks(t, conversation); got != "1:queued/-" {
		t.Fatalf("automatic drafts after the notice = %s", got)
	}
	d.fake.Script(draftAnswer("Hi Lin, open tray two.", []string{"kb1"}, ""))
	d.due(t, conversation)
	d.run(t)
	if got := d.automaticDraft(t, d.adminA, conversation); got == nil || got.Status != ai.DraftReady {
		t.Fatalf("automatic draft after the notice = %+v, tasks %s", got, d.automaticTasks(t, conversation))
	}
}
