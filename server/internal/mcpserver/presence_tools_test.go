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

func TestConversationGetSaysWhoElseHasItOpen(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	session := e.connect(f.w.read, "")
	out := e.ok(session, "colliedesk_conversation_get", map[string]any{"conversation_id": f.c2})
	if viewers := at(t, out, "viewers").([]any); len(viewers) != 0 {
		t.Fatalf("nobody has it open, viewers = %v", viewers)
	}
	viewersOf(t, e, f.w, f.c2, true)
	out = e.ok(session, "colliedesk_conversation_get", map[string]any{"conversation_id": f.c2})
	viewers := at(t, out, "viewers").([]any)
	if len(viewers) != 1 || at(t, viewers, 0, "name") != "Ada Admin" || at(t, viewers, 0, "writing") != true {
		t.Fatalf("viewers = %v, want Ada Admin writing", viewers)
	}
	if hint, _ := out["hint"].(string); !strings.Contains(hint, "Ada Admin") || !strings.Contains(hint, "colliedesk_reply_draft") {
		t.Errorf("hint %q must say who is writing", out["hint"])
	}
	// Another conversation is not affected.
	if viewers := at(t, e.ok(session, "colliedesk_conversation_get", map[string]any{"conversation_id": f.c1}), "viewers").([]any); len(viewers) != 0 {
		t.Fatalf("c1 viewers = %v", viewers)
	}
}

func TestConversationPresenceToolWaitsOutAnAnnounceTooSoon(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	session := e.connect(f.w.read, "")
	e.ok(session, "colliedesk_conversation_presence", map[string]any{"conversation_id": f.c3})
	// At once again, now writing: the server asks for 2 seconds, the tool waits and the change lands.
	e.ok(session, "colliedesk_conversation_presence", map[string]any{"conversation_id": f.c3, "writing": true})
	viewers := viewersOf(t, e, f.w, f.c3, false)
	if len(viewers) != 1 || at(t, viewers, 0, "writing") != true {
		t.Fatalf("people see %v, want the agent writing", viewers)
	}
}

// An AI agent's reply says which replies it had read (latest_reply_number from colliedesk_conversation_get):
// a reply sent since refuses it, with that reply in the error, and the agent reads again before sending.
func TestReplySendRefusesWhenSomeoneRepliedSinceTheAgentRead(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	session := e.connect(f.w.send, "")
	read := e.ok(session, "colliedesk_conversation_get", map[string]any{"conversation_id": f.c2})
	seen := at(t, read, "latest_reply_number").(float64)
	if seen != 0 {
		t.Fatalf("latest_reply_number of a conversation without replies = %v", seen)
	}
	// A person answers while the agent prepares its reply.
	if status, body := e.rest(http.MethodPost, "/api/v1/conversations/"+f.c2+"/messages", map[string]any{"kind": "outgoing", "body": "Ada already refunded you."}, f.w.admin); status != http.StatusCreated {
		t.Fatalf("person's reply status=%d body=%v", status, body)
	}
	message := e.fails(session, "colliedesk_reply_send", map[string]any{"conversation_id": f.c2, "body": "Refund on its way.", "seen_reply_number": seen})
	if !strings.Contains(message, "Ada already refunded you.") || !strings.Contains(message, "Ada Admin") || !strings.Contains(message, "colliedesk_conversation_get") || !strings.Contains(message, "Nothing was sent") {
		t.Fatalf("conflict message = %q", message)
	}
	if kinds := messageKinds(t, e, f.w, f.c2); len(kinds) != 1 {
		t.Fatalf("timeline = %v, want only the person's reply", kinds)
	}
	// Without the number the tool refuses before sending anything.
	if message := e.fails(session, "colliedesk_reply_send", map[string]any{"conversation_id": f.c2, "body": "x"}); !strings.Contains(message, "seen_reply_number") {
		t.Fatalf("missing seen_reply_number: %q", message)
	}
	// Read again: the number moved, and the reply goes; a person writing on it is a warning, not a refusal.
	viewersOf(t, e, f.w, f.c2, true)
	read = e.ok(session, "colliedesk_conversation_get", map[string]any{"conversation_id": f.c2})
	out := e.ok(session, "colliedesk_reply_send", map[string]any{"conversation_id": f.c2, "body": "Refund on its way.", "seen_reply_number": at(t, read, "latest_reply_number")})
	if at(t, out, "message", "kind") != "outgoing" {
		t.Fatalf("reply after reading again = %v", out)
	}
	if warning, _ := out["warning"].(string); !strings.Contains(warning, "Ada Admin") {
		t.Errorf("warning %q must say a person was writing a reply", out["warning"])
	}
}
