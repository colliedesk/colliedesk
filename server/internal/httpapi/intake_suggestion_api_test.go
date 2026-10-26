package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// aiSuggestion writes what AI suggested of a card and did not do, as the AI decider records it, seconds
// after now; it returns the decision's id.
func (d levelDesk) aiSuggestion(t *testing.T, workspace, card, action, target, runnerUp, replyKind string, seconds int) string {
	t.Helper()
	return queryString(t, d.pool, `INSERT INTO intake_decisions(workspace_id,gathering_id,decider,basis,action,target_conversation_id,runner_up_conversation_id,
			runner_up_confidence,command,subject,reply_kind,confidence,reason,applied,not_applied_reason,items_seen,model,prompt_version,created_at)
		VALUES($1,$2,'ai','model',$3,NULLIF($4,'')::uuid,NULLIF($5,'')::uuid,CASE WHEN $5='' THEN NULL ELSE 0.31 END,
			CASE WHEN $3='create' THEN 'open' END,CASE WHEN $3='create' THEN 'AISUBJECT export' END,$6,0.87,'AIREASON quotes the printer',false,'gate_closed',
			'{}','test-model','intake-2026-10-26.1',now()+make_interval(secs=>$7)) RETURNING id::text`,
		workspace, card, action, target, runnerUp, replyKind, seconds)
}

type suggestionView struct {
	DecisionID             string   `json:"decision_id"`
	Action                 string   `json:"action"`
	TargetConversationID   *string  `json:"target_conversation_id"`
	TargetSubject          *string  `json:"target_subject"`
	TargetStatus           *string  `json:"target_status"`
	RunnerUpConversationID *string  `json:"runner_up_conversation_id"`
	RunnerUpSubject        *string  `json:"runner_up_subject"`
	RunnerUpConfidence     *float64 `json:"runner_up_confidence"`
	Confidence             *float64 `json:"confidence"`
	Subject                *string  `json:"subject"`
	Reason                 *string  `json:"reason"`
	ReplyKind              *string  `json:"reply_kind"`
	NotAppliedReason       string   `json:"not_applied_reason"`
}

type cardWithSuggestion struct {
	ID         string          `json:"id"`
	State      string          `json:"state"`
	Suggestion *suggestionView `json:"suggestion"`
	Decisions  []struct {
		ID                 string  `json:"id"`
		Decider            string  `json:"decider"`
		ReplyKind          *string `json:"reply_kind"`
		FollowsDecisionID  *string `json:"follows_decision_id"`
		CorrectsDecisionID *string `json:"corrects_decision_id"`
	} `json:"decisions"`
}

func (d levelDesk) card(t *testing.T, token, card string) cardWithSuggestion {
	t.Helper()
	response := request(t, d.api, http.MethodGet, "/api/v1/intake/gatherings/"+card, nil, token, nil)
	var view cardWithSuggestion
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &view) != nil {
		t.Fatalf("card: %d %s", response.Code, response.Body.String())
	}
	return view
}

// A card shows what AI suggested and did not do, in the list and in its detail, with the conversations it
// names; once a person decides, it shows none.
func TestACardShowsWhatAISuggested(t *testing.T) {
	d := newLevelDesk(t)
	card := d.heldCard(t, d.workspaceA, d.chatA, "u1", "the printer prints blank labels again")
	resolved := d.groupConversation(t, d.workspaceA, d.chatA, "Blank labels", "resolved", 2)
	open := d.groupConversation(t, d.workspaceA, d.chatA, "Till is slow", "open", 1)
	if got := d.card(t, d.adminA, card); got.Suggestion != nil {
		t.Fatalf("a card AI never read suggests %+v", got.Suggestion)
	}
	decision := d.aiSuggestion(t, d.workspaceA, card, "attach", resolved, open, "substantive", 1)

	got := d.card(t, d.adminA, card).Suggestion
	if got == nil || got.DecisionID != decision || got.Action != "attach" || *got.TargetConversationID != resolved || *got.TargetSubject != "Blank labels" ||
		*got.TargetStatus != "resolved" || *got.RunnerUpConversationID != open || *got.RunnerUpSubject != "Till is slow" || *got.RunnerUpConfidence != 0.31 ||
		*got.Confidence != 0.87 || *got.Reason != "AIREASON quotes the printer" || got.NotAppliedReason != "gate_closed" {
		t.Fatalf("suggestion = %+v", got)
	}
	list := request(t, d.api, http.MethodGet, "/api/v1/intake/gatherings?list=review", nil, d.adminA, nil)
	var page struct {
		Items []cardWithSuggestion `json:"items"`
	}
	if list.Code != http.StatusOK || json.Unmarshal(list.Body.Bytes(), &page) != nil || len(page.Items) != 1 || page.Items[0].Suggestion == nil ||
		page.Items[0].Suggestion.DecisionID != decision {
		t.Fatalf("list: %d %s", list.Code, list.Body.String())
	}

	if response := request(t, d.api, http.MethodPost, "/api/v1/intake/gatherings/"+card+"/decide", map[string]any{"action": "ignore"}, d.adminA, nil); response.Code != http.StatusOK ||
		strings.Contains(response.Body.String(), `"suggestion":{`) {
		t.Fatalf("decided: %d %s", response.Code, response.Body.String())
	}
}

// A person who decides as AI suggested follows it, and takes its reply kind; one who decides otherwise
// corrects it. Either way the suggestion they saw is named, the labels of intake's eval.
func TestADecisionSaysWhetherItFollowedOrCorrectedAI(t *testing.T) {
	d := newLevelDesk(t)
	follow := d.heldCard(t, d.workspaceA, d.chatA, "u1", "thanks, it prints now")
	resolved := d.groupConversation(t, d.workspaceA, d.chatA, "Blank labels", "resolved", 2)
	suggested := d.aiSuggestion(t, d.workspaceA, follow, "attach", resolved, "", "acknowledgement", -1)
	response := request(t, d.api, http.MethodPost, "/api/v1/intake/gatherings/"+follow+"/decide",
		map[string]any{"action": "attach", "conversation_id": resolved, "follows_decision_id": suggested}, d.adminA, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("follow: %d %s", response.Code, response.Body.String())
	}
	person := d.card(t, d.adminA, follow).Decisions[0]
	if person.Decider != "agent" || person.FollowsDecisionID == nil || *person.FollowsDecisionID != suggested || person.CorrectsDecisionID != nil ||
		person.ReplyKind == nil || *person.ReplyKind != "acknowledgement" {
		t.Fatalf("followed: %+v", person)
	}

	other := d.heldCard(t, d.workspaceA, d.chatA, "u2", "a new printer for the back room")
	wrong := d.aiSuggestion(t, d.workspaceA, other, "attach", resolved, "", "substantive", -1)
	if response := request(t, d.api, http.MethodPost, "/api/v1/intake/gatherings/"+other+"/decide",
		map[string]any{"action": "create", "subject": "Back room printer"}, d.adminA, nil); response.Code != http.StatusOK {
		t.Fatalf("decide otherwise: %d %s", response.Code, response.Body.String())
	}
	person = d.card(t, d.adminA, other).Decisions[0]
	if person.CorrectsDecisionID == nil || *person.CorrectsDecisionID != wrong || person.FollowsDecisionID != nil {
		t.Fatalf("corrected: %+v", person)
	}
}

// follows_decision_id names an AI suggestion of the card being decided: another card's, a person's
// decision, another workspace's or no decision at all is refused and the card stays as it was.
func TestFollowsDecisionMustBeAnAISuggestionOfTheCard(t *testing.T) {
	d := newLevelDesk(t)
	card := d.heldCard(t, d.workspaceA, d.chatA, "u1", "the stock export fails")
	other := d.heldCard(t, d.workspaceA, d.chatA, "u2", "the stock import fails")
	othersSuggestion := d.aiSuggestion(t, d.workspaceA, other, "create", "", "", "substantive", 1)
	cardB := d.heldCard(t, d.workspaceB, d.chatB, "u1", "the stock export fails")
	suggestionB := d.aiSuggestion(t, d.workspaceB, cardB, "create", "", "", "substantive", 1)
	if response := request(t, d.api, http.MethodPost, "/api/v1/intake/gatherings/"+other+"/decide", map[string]any{"action": "ignore"}, d.adminA, nil); response.Code != http.StatusOK {
		t.Fatalf("ignore other: %d %s", response.Code, response.Body.String())
	}
	persons := d.card(t, d.adminA, other).Decisions[0].ID

	for name, id := range map[string]string{"another card's": othersSuggestion, "a person's": persons, "another workspace's": suggestionB,
		"none": "00000000-0000-4000-8000-000000000000", "not an id": "C1"} {
		response := request(t, d.api, http.MethodPost, "/api/v1/intake/gatherings/"+card+"/decide",
			map[string]any{"action": "create", "subject": "Export", "follows_decision_id": id}, d.adminA, nil)
		if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "stock") {
			t.Errorf("%s: %d %s", name, response.Code, response.Body.String())
		}
	}
	if got := queryString(t, d.pool, `SELECT state FROM intake_gatherings WHERE id=$1`, card); got != "review" {
		t.Fatalf("card = %s", got)
	}
}

// Joining a card to a conversation that was merged into another joins the one it was merged into, never
// reopening the one merged away; a conversation that is not there is not found.
func TestAttachingToAMergedConversationJoinsTheOneItWasMergedInto(t *testing.T) {
	d := newLevelDesk(t)
	card := d.heldCard(t, d.workspaceA, d.chatA, "u1", "the printer prints blank labels again")
	kept := d.groupConversation(t, d.workspaceA, d.chatA, "Blank labels", "open", 1)
	merged := d.groupConversation(t, d.workspaceA, d.chatA, "Labels blank", "open", 1)
	if _, err := d.pool.Exec(t.Context(), `UPDATE conversations SET status='closed',merged_into_id=$2 WHERE id=$1`, merged, kept); err != nil {
		t.Fatal(err)
	}
	if response := request(t, d.api, http.MethodPost, "/api/v1/intake/gatherings/"+card+"/decide",
		map[string]any{"action": "attach", "conversation_id": "00000000-0000-4000-8000-000000000000"}, d.adminA, nil); response.Code != http.StatusNotFound {
		t.Fatalf("a conversation that is not there: %d %s", response.Code, response.Body.String())
	}
	response := request(t, d.api, http.MethodPost, "/api/v1/intake/gatherings/"+card+"/decide", map[string]any{"action": "attach", "conversation_id": merged}, d.adminA, nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"conversation_id":"`+kept+`"`) {
		t.Fatalf("attach to merged: %d %s", response.Code, response.Body.String())
	}
	if got := queryString(t, d.pool, `SELECT status FROM conversations WHERE id=$1`, merged); got != "closed" {
		t.Fatalf("the conversation merged away is %s", got)
	}
}

// A token that may decide cards but not read messages gets the suggestion without what was written.
func TestASuggestionWithoutMessagesReadHasNoText(t *testing.T) {
	d := newLevelDesk(t)
	card := d.heldCard(t, d.workspaceA, d.chatA, "u1", "SECRET stock export fails")
	if response := request(t, d.api, http.MethodPost, "/api/v1/intake/gatherings/"+card+"/decide", map[string]any{"action": "ignore"}, d.adminA, nil); response.Code != http.StatusOK {
		t.Fatalf("ignore: %d", response.Code)
	}
	target := d.groupConversation(t, d.workspaceA, d.chatA, "CANDSUBJECT printer", "open", 1)
	d.aiSuggestion(t, d.workspaceA, card, "attach", target, target, "substantive", 5)
	token := issueCustomToken(t, d.api, d.adminA, "decider", "conversations:update").Token
	response := request(t, d.api, http.MethodPost, "/api/v1/intake/gatherings/"+card+"/requeue", nil, token, nil)
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, `"suggestion":{`) || strings.Contains(body, "CANDSUBJECT") || strings.Contains(body, "AIREASON") {
		t.Fatalf("requeue: %d %s", response.Code, body)
	}
}
