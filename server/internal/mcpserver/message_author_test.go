package mcpserver_test

import (
	"context"
	"strings"
	"testing"
)

// Reading a thread, a model must see who wrote each team message and whether an agent token did.
func TestConversationGetNamesTheAuthorAndTheTokenOfEachTeamMessage(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	e.created("/api/v1/conversations/"+f.c2+"/messages", map[string]any{"kind": "note", "body": "Written by a person."}, f.w.admin)
	written := e.ok(e.connect(f.w.draft, ""), "colliedesk_note_add", map[string]any{"conversation_id": f.c2, "body": "Refund is due."})

	if got := at(t, written, "message", "author"); got != "Ada Admin" {
		t.Errorf("note_add author = %v, want Ada Admin (the member the token acts as)", got)
	}
	if label, scope := at(t, written, "message", "written_by_agent_token", "label"), at(t, written, "message", "written_by_agent_token", "scope"); label != "test draft" || scope != "draft" {
		t.Errorf("note_add token = %v/%v, want test draft/draft", label, scope)
	}

	thread := e.ok(e.connect(f.w.read, ""), "colliedesk_conversation_get", map[string]any{"conversation_id": f.c2})
	person, agent := at(t, thread, "messages", 0), at(t, thread, "messages", 1)
	if at(t, person, "body") != "Written by a person." || at(t, agent, "body") != "Refund is due." {
		t.Fatalf("messages out of order: %v", thread["messages"])
	}
	for name, message := range map[string]any{"person": person, "token": agent} {
		if got := at(t, message, "author"); got != "Ada Admin" {
			t.Errorf("%s message author = %v, want Ada Admin", name, got)
		}
	}
	if _, marked := person.(map[string]any)["written_by_agent_token"]; marked {
		t.Errorf("a person's note must not carry written_by_agent_token: %v", person)
	}
	if label, scope := at(t, agent, "written_by_agent_token", "label"), at(t, agent, "written_by_agent_token", "scope"); label != "test draft" || scope != "draft" {
		t.Errorf("token note: written_by_agent_token = %v/%v, want test draft/draft: %v", label, scope, agent)
	}
}

func TestServerTellsTheModelThatTokenWrittenTextIsModelOutput(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Model Output Desk", "admin@model-output.example.com")
	session := e.connect(w.read, "")
	texts := map[string]string{"instructions": session.InitializeResult().Instructions}
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range listed.Tools {
		if tool.Name == "colliedesk_conversation_get" || tool.Name == "colliedesk_note_add" {
			texts[tool.Name] = tool.Description
		}
	}
	for where, got := range texts {
		for _, phrase := range []string{"written_by_agent_token", "model output", "not a human decision"} {
			if !strings.Contains(got, phrase) {
				t.Errorf("%s lacks %q:\n%s", where, phrase, got)
			}
		}
	}
	if len(texts) != 3 {
		t.Errorf("checked %v, want the instructions, conversation_get and note_add", texts)
	}
}
