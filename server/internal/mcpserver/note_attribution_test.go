package mcpserver_test

import (
	"net/http"
	"testing"
)

// A person reading the timeline must be able to tell that the model, not a colleague, wrote the note.
func TestNoteAddedThroughMCPNamesTheTokenInTheTimeline(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	e.ok(e.connect(f.w.draft, ""), "colliedesk_note_add", map[string]any{"conversation_id": f.c2, "body": "Refund is due."})

	status, listed := e.rest(http.MethodGet, "/api/v1/conversations/"+f.c2+"/messages", nil, f.w.admin)
	items, _ := listed["items"].([]any)
	if status != http.StatusOK || len(items) != 1 {
		t.Fatalf("REST messages: %d %v", status, listed)
	}
	if label := at(t, items[0], "agent_token", "label"); label != "test draft" {
		t.Errorf("agent_token.label = %v, want the label of the draft token (test draft); message %v", label, items[0])
	}
	if scope := at(t, items[0], "agent_token", "scope"); scope != "draft" {
		t.Errorf("agent_token.scope = %v, want draft", scope)
	}
}
