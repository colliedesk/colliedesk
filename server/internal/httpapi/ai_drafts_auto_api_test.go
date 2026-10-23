package httpapi_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/ai"
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

	// The customer writes again: the ready one is replaced, a new one waits.
	d.customerWrites(t, d.workspaceA, conversation, "Still jammed.", nil)
	if outcome := queryString(t, d.pool, `SELECT outcome FROM ai_drafts WHERE id=$1`, got.ID); outcome != "superseded" {
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
	if _, err := d.service.SetAutomaticLimits(context.Background(), d.workspaceA, ai.Actor{}, 300, 30, &draftsPerDay); err != nil {
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
	if _, err := d.service.SetAutomaticLimits(context.Background(), d.workspaceA, ai.Actor{}, 300, 30, &draftsPerDay); err != nil {
		t.Fatal(err)
	}
	if _, err := d.pool.Exec(context.Background(), `INSERT INTO ai_tasks(workspace_id,kind,conversation_id,automatic,state,started_at,finished_at)
		SELECT $1,'draft',$2,true,'done',now()-g*interval '1 hour',now() FROM generate_series(1,5) g`, d.workspaceA, conversation); err != nil {
		t.Fatal(err)
	}
	d.customerWrites(t, d.workspaceA, conversation, "Hello again.", nil)
	d.due(t, conversation)
	d.run(t)
	if code := queryString(t, d.pool, `SELECT error_code FROM ai_tasks WHERE conversation_id=$1 AND kind='draft' AND automatic ORDER BY created_at DESC LIMIT 1`, conversation); code != "conversation_cap" {
		t.Fatalf("sixth automatic draft = %s", code)
	}
	// A person still gets one.
	d.fake.Script(draftAnswer("Hi Lin.", nil, ""))
	draft := d.ask(t, d.adminA, conversation, "")
	d.run(t)
	if got := d.get(t, d.adminA, conversation, draft.ID); got.Status != ai.DraftReady {
		t.Fatalf("a person's draft past the automatic limits = %+v", got)
	}
}
