package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
)

// autoTriageOn lets workspace A's triage suggestions apply the parts by themselves.
func (d draftDesk) autoTriageOn(t *testing.T, parts []string) ai.Slot {
	t.Helper()
	var settings ai.Settings
	decode(t, request(t, d.api, http.MethodGet, "/api/v1/ai", nil, d.adminA, nil), &settings)
	body := map[string]any{"source": "provider", "provider_id": settings.Providers[0].ID, "model": "claude-sonnet-5-5", "enabled": true, "automatic": true}
	if parts != nil {
		body["auto_parts"] = parts
	}
	var slot ai.Slot
	response := request(t, d.api, http.MethodPut, "/api/v1/ai/slots/triage", body, d.adminA, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("turn automatic apply on: %d %s", response.Code, response.Body.String())
	}
	decode(t, response, &slot)
	return slot
}

// customerTicket is a conversation a customer started by email, with its first message.
func (d draftDesk) customerTicket(t *testing.T, subject, priority string) string {
	t.Helper()
	contact := create(t, d.api, d.adminA, "/api/v1/contacts", map[string]any{"name": "Lin", "email": "lin-" + subject + "@customer-a.example"})
	conversation := create(t, d.api, d.adminA, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": subject})
	if _, err := d.pool.Exec(context.Background(), `UPDATE conversations SET channel='email',priority=$2 WHERE id=$1`, conversation, priority); err != nil {
		t.Fatal(err)
	}
	return conversation
}

func (d draftDesk) conversationState(t *testing.T, conversation string) (priority string, assignee string, tags []string) {
	t.Helper()
	var conv struct {
		Priority   string  `json:"priority"`
		AssigneeID *string `json:"assignee_id"`
		Tags       []struct {
			ID string `json:"id"`
		} `json:"tags"`
	}
	decode(t, request(t, d.api, http.MethodGet, "/api/v1/conversations/"+conversation, nil, d.adminA, nil), &conv)
	for _, tag := range conv.Tags {
		tags = append(tags, tag.ID)
	}
	if conv.AssigneeID != nil {
		assignee = *conv.AssigneeID
	}
	return conv.Priority, assignee, tags
}

func TestAutomaticTriageIsOffUntilAnAdminTurnsItOnAndGoesOffWithTheFeature(t *testing.T) {
	d := newDraftDesk(t)
	d.triageOn(t)
	var settings ai.Settings
	decode(t, request(t, d.api, http.MethodGet, "/api/v1/ai", nil, d.adminA, nil), &settings)
	for _, slot := range settings.Slots {
		if slot.Automatic || len(slot.AutoParts) != 0 {
			t.Fatalf("slot %s starts automatic: %+v", slot.Slot, slot)
		}
	}
	agent := joinAsAgent(t, d.api, d.adminA, "sam@draft-a.example.com")
	wantResponse(t, request(t, d.api, http.MethodPut, "/api/v1/ai/slots/triage", map[string]any{"source": "provider", "provider_id": settings.Providers[0].ID, "enabled": true, "automatic": true}, agent, nil),
		http.StatusForbidden, "forbidden")
	wantResponse(t, request(t, d.api, http.MethodPut, "/api/v1/ai/slots/draft", map[string]any{"source": "provider", "provider_id": settings.Providers[0].ID, "enabled": true, "auto_parts": []string{"tags"}}, d.adminA, nil),
		http.StatusBadRequest, "invalid_request")

	slot := d.autoTriageOn(t, nil)
	if !slot.Automatic || !slices.Equal(slot.AutoParts, []string{"tags", "priority"}) {
		t.Fatalf("turned on with no parts named = %+v", slot)
	}
	// A request that does not name it keeps it; turning the feature off turns it off, and on again does not bring it back.
	kbCall(t, d.api, http.MethodPut, "/api/v1/ai/slots/triage", map[string]any{"source": "provider", "provider_id": settings.Providers[0].ID, "model": "claude-sonnet-5-5", "effort": "medium", "enabled": true}, d.adminA, http.StatusOK, &slot)
	if !slot.Automatic {
		t.Fatalf("a change of effort turned automatic apply off: %+v", slot)
	}
	kbCall(t, d.api, http.MethodPut, "/api/v1/ai/slots/triage", map[string]any{"source": "provider", "provider_id": settings.Providers[0].ID, "enabled": false}, d.adminA, http.StatusOK, &slot)
	kbCall(t, d.api, http.MethodPut, "/api/v1/ai/slots/triage", map[string]any{"source": "provider", "provider_id": settings.Providers[0].ID, "enabled": true}, d.adminA, http.StatusOK, &slot)
	if slot.Automatic {
		t.Fatalf("turning triage on again brought automatic apply back: %+v", slot)
	}
	decode(t, request(t, d.api, http.MethodGet, "/api/v1/ai", nil, d.adminA, nil), &settings)
	var on, off bool
	for _, event := range settings.Events {
		if event.Kind != "slot_changed" || event.Slot == nil || *event.Slot != "triage" {
			continue
		}
		var detail struct {
			From, To map[string]any
		}
		if err := json.Unmarshal(event.Detail, &detail); err != nil {
			t.Fatal(err)
		}
		if detail.To["automatic"] == true && detail.From["automatic"] == false {
			on = true
		}
		if detail.From["automatic"] == true && detail.To["automatic"] == false {
			off = true
		}
	}
	if !on || !off {
		t.Fatalf("history does not say automatic apply was turned on and off: %+v", settings.Events)
	}
	// The other workspace never was.
	if got := queryString(t, d.pool, `SELECT count(*)::text FROM ai_slots WHERE workspace_id=$1 AND automatic`, d.workspaceB); got != "0" {
		t.Fatalf("workspace B automatic slots = %s", got)
	}
}

func TestAutomaticTriageFillsOnlyWhatNobodySetAndLeavesTheRestToAccept(t *testing.T) {
	d := newDraftDesk(t)
	d.triageOn(t)
	d.autoTriageOn(t, nil)
	hardware := create(t, d.api, d.adminA, "/api/v1/tags", map[string]any{"name": "hardware"})
	existing := create(t, d.api, d.adminA, "/api/v1/tags", map[string]any{"name": "existing"})
	sam := joinAsAgent(t, d.api, d.adminA, "sam@draft-a.example.com")
	var me struct {
		Agent struct {
			ID string `json:"id"`
		} `json:"agent"`
	}
	decode(t, request(t, d.api, http.MethodGet, "/api/v1/auth/me", nil, sam, nil), &me)
	kbCall(t, d.api, http.MethodPatch, "/api/v1/agents/"+me.Agent.ID, map[string]any{"display_name": "Sam Rivera"}, d.adminA, http.StatusOK, nil)

	// A customer's ticket: tags and priority apply, the assignee (not chosen to apply) waits to be accepted.
	conversation := d.customerTicket(t, "printer", "normal")
	kbCall(t, d.api, http.MethodPut, "/api/v1/conversations/"+conversation+"/tags/"+existing, nil, d.adminA, http.StatusOK, nil)
	d.fake.Script(triageAnswer([]string{"hardware"}, "urgent", "Sam Rivera"))
	d.customerWrites(t, d.workspaceA, conversation, "The office printer is down.", nil)
	d.run(t)
	priority, assignee, tags := d.conversationState(t, conversation)
	if priority != "urgent" || assignee != "" || !slices.Contains(tags, hardware) || !slices.Contains(tags, existing) {
		t.Fatalf("after automatic apply: priority %s, assignee %q, tags %v", priority, assignee, tags)
	}
	got := d.triage(t, d.adminA, conversation)
	if got.AutoAppliedAt == nil || got.Outcome != nil || got.AssigneeAgentID == nil || got.AutoApplied.AssigneeAgentID != nil || got.AutoApplied.Priority == nil {
		t.Fatalf("suggestion after automatic apply = %+v", got)
	}
	if by := queryString(t, d.pool, `SELECT COALESCE(assigned_by_agent_id::text,'none') FROM conversations WHERE id=$1`, conversation); by != "none" {
		t.Fatalf("assigned_by = %s", by)
	}
	kbCall(t, d.api, http.MethodPost, "/api/v1/conversations/"+conversation+"/ai-triage/"+got.ID+"/accept", nil, d.adminA, http.StatusOK, &got)
	if got.Outcome == nil || *got.Outcome != "accepted" {
		t.Fatalf("accepting the rest = %+v", got)
	}

	// A priority a person set, and an assignee already there, are left alone and said so.
	d.autoTriageOn(t, []string{"tags", "priority", "assignee"})
	high := d.customerTicket(t, "outage", "high")
	kbCall(t, d.api, http.MethodPatch, "/api/v1/conversations/"+high, map[string]any{"assignee_id": me.Agent.ID}, d.adminA, http.StatusOK, nil)
	d.fake.Script(triageAnswer([]string{"hardware"}, "urgent", "Sam Rivera"))
	d.customerWrites(t, d.workspaceA, high, "Everything is down.", nil)
	d.run(t)
	priority, _, _ = d.conversationState(t, high)
	got = d.triage(t, d.adminA, high)
	if priority != "high" || got.Skipped["priority"].Reason != "set_by_person" || got.Priority != nil || got.Outcome == nil || *got.Outcome != "auto_applied" {
		t.Fatalf("a person's priority: conversation %s, suggestion %+v", priority, got)
	}
	agentStarted := create(t, d.api, d.adminA, "/api/v1/conversations", map[string]any{"contact_id": d.contactA, "subject": "Agent started"})
	d.fake.Script(triageAnswer(nil, "urgent", ""))
	d.customerWrites(t, d.workspaceA, agentStarted, "Thanks for reaching out first.", nil)
	d.run(t)
	if got := d.triage(t, d.adminA, agentStarted); got.Skipped["priority"].Reason != "agent_conversation" {
		t.Fatalf("a teammate's conversation = %+v", got)
	}

	// One asked for by hand never applies by itself.
	d.fake.Script(triageAnswer(nil, "low", ""))
	kbCall(t, d.api, http.MethodPost, "/api/v1/conversations/"+conversation+"/ai-triage", nil, d.adminA, http.StatusAccepted, nil)
	d.run(t)
	priority, _, _ = d.conversationState(t, conversation)
	var both struct {
		Suggestion  *ai.Triage `json:"suggestion"`
		AutoApplied *ai.Triage `json:"auto_applied"`
	}
	decode(t, request(t, d.api, http.MethodGet, "/api/v1/conversations/"+conversation+"/ai-triage", nil, d.adminA, nil), &both)
	if priority != "urgent" || both.Suggestion.AutoAppliedAt != nil || both.AutoApplied == nil || both.AutoApplied.ID == both.Suggestion.ID {
		t.Fatalf("asked by hand: priority %s, %+v", priority, both)
	}
}

func TestUndoTakesBackWhatAIAppliedAndKeepsWhatAPersonChanged(t *testing.T) {
	d := newDraftDesk(t)
	d.triageOn(t)
	d.autoTriageOn(t, nil)
	hardware := create(t, d.api, d.adminA, "/api/v1/tags", map[string]any{"name": "hardware"})
	office := create(t, d.api, d.adminA, "/api/v1/tags", map[string]any{"name": "office"})
	conversation := d.customerTicket(t, "printer", "normal")
	d.fake.Script(triageAnswer([]string{"hardware", "office"}, "high", ""))
	d.customerWrites(t, d.workspaceA, conversation, "The office printer is down.", nil)
	d.run(t)
	got := d.triage(t, d.adminA, conversation)
	path := "/api/v1/conversations/" + conversation + "/ai-triage/" + got.ID
	// Another workspace cannot undo it.
	wantResponse(t, request(t, d.api, http.MethodPost, path+"/undo", nil, d.adminB, nil), http.StatusNotFound, "not_found")
	wantResponse(t, request(t, d.api, http.MethodPost, "/api/v1/conversations/"+d.convoB+"/ai-triage/"+got.ID+"/undo", nil, d.adminB, nil), http.StatusNotFound, "not_found")

	// A person changes the priority and removes one tag; undo takes back the other tag only.
	kbCall(t, d.api, http.MethodPatch, "/api/v1/conversations/"+conversation, map[string]any{"priority": "urgent"}, d.adminA, http.StatusOK, nil)
	kbCall(t, d.api, http.MethodDelete, "/api/v1/conversations/"+conversation+"/tags/"+office, nil, d.adminA, http.StatusOK, nil)
	kbCall(t, d.api, http.MethodPost, path+"/undo", map[string]any{}, d.adminA, http.StatusOK, &got)
	priority, _, tags := d.conversationState(t, conversation)
	if priority != "urgent" || slices.Contains(tags, hardware) || got.Outcome == nil || *got.Outcome != "undone" || got.Kept.Priority == nil || *got.Kept.Priority != "urgent" {
		t.Fatalf("after undo: priority %s, tags %v, suggestion %+v", priority, tags, got)
	}
	if !slices.Contains(got.UndoneParts.TagIDs, hardware) || slices.Contains(got.UndoneParts.TagIDs, office) || got.UndoneAt == nil {
		t.Fatalf("undone parts = %+v", got.UndoneParts)
	}
	// Undo again changes nothing.
	var again ai.Triage
	kbCall(t, d.api, http.MethodPost, path+"/undo", nil, d.adminA, http.StatusOK, &again)
	if again.UndoneAt == nil || !again.UndoneAt.Equal(*got.UndoneAt) {
		t.Fatalf("undo again = %+v", again)
	}
	// Apply again applies what the undo took back, filling only what is still empty: the tag, not the
	// priority a person set, nor the tag a person removed. It is AI's again, with its own Undo.
	kbCall(t, d.api, http.MethodPost, path+"/accept", nil, d.adminA, http.StatusOK, &again)
	priority, _, tags = d.conversationState(t, conversation)
	if priority != "urgent" || !slices.Contains(tags, hardware) || slices.Contains(tags, office) {
		t.Fatalf("after apply again: priority %s, tags %v", priority, tags)
	}
	// The tag a person removed stays a part anyone can accept again.
	if again.Outcome != nil || again.AutoAppliedAt == nil || !slices.Equal(again.AutoApplied.TagIDs, []string{hardware}) ||
		again.Skipped["priority"].Reason != "set_by_person" || again.UndoneAt == nil {
		t.Fatalf("applied again = %+v", again)
	}
	kbCall(t, d.api, http.MethodPost, path+"/undo", nil, d.adminA, http.StatusOK, &again)
	if _, _, tags = d.conversationState(t, conversation); slices.Contains(tags, hardware) || again.Outcome == nil || *again.Outcome != "undone" {
		t.Fatalf("undo after apply again: %v %+v", tags, again)
	}
	// A second round undoes only what the second application did: nothing of the first comes back.
	if !slices.Equal(again.UndoneParts.TagIDs, []string{hardware}) || again.UndoneParts.Priority != nil {
		t.Fatalf("second undo's parts = %+v", again.UndoneParts)
	}
	kbCall(t, d.api, http.MethodPost, path+"/accept", nil, d.adminA, http.StatusOK, &again)
	if _, _, tags = d.conversationState(t, conversation); !slices.Contains(tags, hardware) || slices.Contains(tags, office) {
		t.Fatalf("second apply again: %v", tags)
	}

	// Undo of one part, then the rest.
	second := d.customerTicket(t, "scanner", "normal")
	d.fake.Script(triageAnswer([]string{"hardware"}, "high", ""))
	d.customerWrites(t, d.workspaceA, second, "The scanner is down.", nil)
	d.run(t)
	got = d.triage(t, d.adminA, second)
	kbCall(t, d.api, http.MethodPost, "/api/v1/conversations/"+second+"/ai-triage/"+got.ID+"/undo", map[string]any{"parts": []string{"priority"}}, d.adminA, http.StatusOK, &got)
	priority, _, tags = d.conversationState(t, second)
	if priority != "normal" || !slices.Contains(tags, hardware) || got.Outcome == nil || *got.Outcome != "auto_applied" {
		t.Fatalf("after undoing the priority: %s %v %+v", priority, tags, got)
	}
	kbCall(t, d.api, http.MethodPost, "/api/v1/conversations/"+second+"/ai-triage/"+got.ID+"/undo", nil, d.adminA, http.StatusOK, &got)
	if _, _, tags = d.conversationState(t, second); slices.Contains(tags, hardware) || *got.Outcome != "undone" {
		t.Fatalf("after undoing the rest: %v %+v", tags, got)
	}
	// Nothing AI applied: nothing to undo.
	d.fake.Script(triageAnswer(nil, "low", ""))
	kbCall(t, d.api, http.MethodPost, "/api/v1/conversations/"+second+"/ai-triage", nil, d.adminA, http.StatusAccepted, nil)
	d.run(t)
	manual := d.triage(t, d.adminA, second)
	wantResponse(t, request(t, d.api, http.MethodPost, "/api/v1/conversations/"+second+"/ai-triage/"+manual.ID+"/undo", nil, d.adminA, nil), http.StatusConflict, "ai_triage_not_auto_applied")
}

// A teammate AI assigns finds it in the inbox activity marked as AI's, so the notification says so.
func TestATeammateAIAssignsSeesItWasAI(t *testing.T) {
	d := newDraftDesk(t)
	d.triageOn(t)
	d.autoTriageOn(t, []string{"assignee"})
	sam := joinAsAgent(t, d.api, d.adminA, "sam@draft-a.example.com")
	var me struct {
		Agent struct {
			ID string `json:"id"`
		} `json:"agent"`
	}
	decode(t, request(t, d.api, http.MethodGet, "/api/v1/auth/me", nil, sam, nil), &me)
	kbCall(t, d.api, http.MethodPatch, "/api/v1/agents/"+me.Agent.ID, map[string]any{"display_name": "Sam Rivera"}, d.adminA, http.StatusOK, nil)
	var activity struct {
		Now          string `json:"now"`
		AssignedToMe []struct {
			ID   string `json:"id"`
			ByAI bool   `json:"by_ai"`
		} `json:"assigned_to_me"`
	}
	decode(t, request(t, d.api, http.MethodGet, "/api/v1/inbox/activity", nil, sam, nil), &activity)
	since := activity.Now
	conversation := d.customerTicket(t, "printer", "normal")
	d.fake.Script(triageAnswer(nil, "normal", "Sam Rivera"))
	d.customerWrites(t, d.workspaceA, conversation, "The office printer is down.", nil)
	d.run(t)
	decode(t, request(t, d.api, http.MethodGet, "/api/v1/inbox/activity?since="+url.QueryEscape(since), nil, sam, nil), &activity)
	if len(activity.AssignedToMe) != 1 || activity.AssignedToMe[0].ID != conversation || !activity.AssignedToMe[0].ByAI {
		t.Fatalf("assigned to Sam = %+v", activity.AssignedToMe)
	}
	// A person who assigns Sam afterwards is a person, not AI.
	since = activity.Now
	var admin struct {
		Agent struct {
			ID string `json:"id"`
		} `json:"agent"`
	}
	decode(t, request(t, d.api, http.MethodGet, "/api/v1/auth/me", nil, d.adminA, nil), &admin)
	kbCall(t, d.api, http.MethodPatch, "/api/v1/conversations/"+conversation, map[string]any{"assignee_id": admin.Agent.ID}, d.adminA, http.StatusOK, nil)
	kbCall(t, d.api, http.MethodPatch, "/api/v1/conversations/"+conversation, map[string]any{"assignee_id": me.Agent.ID}, d.adminA, http.StatusOK, nil)
	decode(t, request(t, d.api, http.MethodGet, "/api/v1/inbox/activity?since="+url.QueryEscape(since), nil, sam, nil), &activity)
	if len(activity.AssignedToMe) != 1 || activity.AssignedToMe[0].ByAI {
		t.Fatalf("assigned to Sam by a person = %+v", activity.AssignedToMe)
	}
}

// A tag deleted while the model answers is not applied, and the suggestion still comes.
func TestATagDeletedWhileTheModelAnswersIsLeftOut(t *testing.T) {
	d := newDraftDesk(t)
	d.triageOn(t)
	d.autoTriageOn(t, nil)
	hardware := create(t, d.api, d.adminA, "/api/v1/tags", map[string]any{"name": "hardware"})
	create(t, d.api, d.adminA, "/api/v1/tags", map[string]any{"name": "office"})
	d.fake.Answer = func(aiprovider.Request) (aiprovider.Response, error) {
		if _, err := d.pool.Exec(context.Background(), `DELETE FROM tags WHERE id=$1`, hardware); err != nil {
			t.Error(err)
		}
		return triageAnswer([]string{"hardware", "office"}, "high", "").Response, nil
	}
	conversation := d.customerTicket(t, "printer", "normal")
	d.customerWrites(t, d.workspaceA, conversation, "The printer is down.", nil)
	d.run(t)
	got := d.triage(t, d.adminA, conversation)
	if got.Status != ai.DraftReady || len(got.AutoApplied.TagIDs) != 1 || slices.Contains(got.AutoApplied.TagIDs, hardware) {
		t.Fatalf("suggestion after its tag was deleted = %+v", got)
	}
}
