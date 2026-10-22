package mcpserver_test

import (
	"net/http"
	"strings"
	"testing"
)

// viewersOf announces the admin person's own tab and returns who else the REST API lists.
func viewersOf(t *testing.T, e *env, w workspace, conversationID string, writing bool) []any {
	t.Helper()
	status, answer := e.rest(http.MethodPut, "/api/v1/conversations/"+conversationID+"/presence", map[string]any{"client_id": "admin-tab-1", "writing": writing}, w.admin)
	if status != http.StatusOK {
		t.Fatalf("PUT presence status=%d body=%v", status, answer)
	}
	return answer["viewers"].([]any)
}

func TestConversationPresenceToolShowsTheAgentToPeopleAndPeopleToTheAgent(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	session := e.connect(f.w.read, "")

	out := e.ok(session, "colliedesk_conversation_presence", map[string]any{"conversation_id": f.c2})
	if at(t, out, "action") != "announce" || len(at(t, out, "viewers").([]any)) != 0 || at(t, out, "listed_for_seconds") != float64(120) {
		t.Fatalf("first announce = %v", out)
	}
	viewers := viewersOf(t, e, f.w, f.c2, true)
	if len(viewers) != 1 || at(t, viewers, 0, "name") != "Ada Admin" || at(t, viewers, 0, "agent_token", "label") != "test read" {
		t.Fatalf("people see %v, want Ada Admin through the read token", viewers)
	}

	// The person the token acts as is writing a reply: the agent is told, and told what to do instead.
	out = e.ok(session, "colliedesk_conversation_presence", map[string]any{"conversation_id": f.c2})
	listed := at(t, out, "viewers").([]any)
	if len(listed) != 1 || at(t, listed, 0, "name") != "Ada Admin" || at(t, listed, 0, "writing") != true || at(t, listed, 0, "agent_token") != nil {
		t.Fatalf("agent sees %v", listed)
	}
	if hint, _ := out["hint"].(string); !strings.Contains(hint, "Ada Admin") || !strings.Contains(hint, "colliedesk_reply_draft") {
		t.Errorf("hint %q must name who is writing and suggest a draft", out["hint"])
	}

	if out := e.ok(session, "colliedesk_conversation_presence", map[string]any{"conversation_id": f.c2, "action": "leave"}); at(t, out, "action") != "leave" {
		t.Fatalf("leave = %v", out)
	}
	if viewers := viewersOf(t, e, f.w, f.c2, false); len(viewers) != 0 {
		t.Fatalf("after leave people see %v", viewers)
	}
}

func TestConversationPresenceToolStaysInItsWorkspace(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	message := e.fails(e.connect(f.w.read, ""), "colliedesk_conversation_presence", map[string]any{"conversation_id": f.otherConvID})
	if !strings.Contains(message, "No conversation") {
		t.Fatalf("another workspace's conversation: %q", message)
	}
	if viewers := viewersOf(t, e, f.other, f.otherConvID, false); len(viewers) != 0 {
		t.Fatalf("the other workspace lists %v", viewers)
	}
}
