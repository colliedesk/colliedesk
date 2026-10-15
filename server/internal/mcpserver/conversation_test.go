package mcpserver_test

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// restConversation reads a conversation straight from the REST API as the workspace admin.
func restConversation(t *testing.T, e *env, w workspace, id string) map[string]any {
	t.Helper()
	status, body := e.rest(http.MethodGet, "/api/v1/conversations/"+id, nil, w.admin)
	if status != http.StatusOK {
		t.Fatalf("GET conversation %s status=%d body=%v", id, status, body)
	}
	return body
}

func tagNames(t *testing.T, conversation map[string]any) []string {
	t.Helper()
	names := []string{}
	for _, tag := range conversation["tags"].([]any) {
		names = append(names, tag.(map[string]any)["name"].(string))
	}
	slices.Sort(names)
	return names
}

func TestConversationUpdateNeedsTheDraftTier(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	e.created("/api/v1/tags", map[string]any{"name": "urgent-bug"}, f.w.admin)
	args := map[string]any{"conversation_id": f.c3, "status": "pending", "priority": "high", "assignee": "me", "add_tags": []string{"billing", "urgent-bug"}}

	message := e.fails(e.connect(f.w.read, ""), "colliedesk_conversation_update", args)
	for _, want := range []string{"`read`", "`draft`", "admin"} {
		if !strings.Contains(message, want) {
			t.Errorf("read token message %q lacks %q", message, want)
		}
	}
	if after := restConversation(t, e, f.w, f.c3); after["status"] != "open" || after["priority"] != "normal" || after["assignee_id"] != nil || len(tagNames(t, after)) != 0 {
		t.Fatalf("a refused update changed the conversation: %v", after)
	}

	draft := e.connect(f.w.draft, "")
	out := e.ok(draft, "colliedesk_conversation_update", args)
	if at(t, out, "conversation", "status") != "pending" || at(t, out, "conversation", "assignee", "email") != "admin@inbox.example.com" {
		t.Errorf("result does not show the new state: %v", out)
	}
	after := restConversation(t, e, f.w, f.c3)
	if after["status"] != "pending" || after["priority"] != "high" || after["assignee_id"] != f.adminID || !slices.Equal(tagNames(t, after), []string{"billing", "urgent-bug"}) {
		t.Fatalf("REST shows %v, want pending/high/assigned/tagged", after)
	}

	e.ok(draft, "colliedesk_conversation_update", args)
	if again := restConversation(t, e, f.w, f.c3); again["status"] != "pending" || !slices.Equal(tagNames(t, again), []string{"billing", "urgent-bug"}) {
		t.Errorf("repeating the call changed the state: %v", again)
	}

	e.ok(draft, "colliedesk_conversation_update", map[string]any{"conversation_id": f.c3, "remove_tags": []string{"urgent-bug"}, "assignee": "unassigned"})
	after = restConversation(t, e, f.w, f.c3)
	if !slices.Equal(tagNames(t, after), []string{"billing"}) || after["assignee_id"] != nil || after["status"] != "pending" {
		t.Errorf("remove tag + unassign: %v", after)
	}
	e.ok(draft, "colliedesk_conversation_update", map[string]any{"conversation_id": f.c3, "assignee": "admin@inbox.example.com", "status": "open"})
	if after = restConversation(t, e, f.w, f.c3); after["assignee_id"] != f.adminID || after["status"] != "open" {
		t.Errorf("assign by email and reopen: %v", after)
	}
}

func TestConversationUpdateRefusesBadInputBeforeChangingAnything(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	draft := e.connect(f.w.draft, "")
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"nothing to change", map[string]any{"conversation_id": f.c3}, "Nothing to change"},
		{"unknown tag", map[string]any{"conversation_id": f.c3, "status": "closed", "add_tags": []string{"nonesuch"}}, "No tag named"},
		{"unknown agent", map[string]any{"conversation_id": f.c3, "status": "closed", "assignee": "nobody@inbox.example.com"}, "No team member"},
		{"bad status", map[string]any{"conversation_id": f.c3, "status": "done"}, "status"},
		{"not a uuid", map[string]any{"conversation_id": "7", "status": "closed"}, "conversation_id"},
		{"unknown field", map[string]any{"conversation_id": f.c3, "status": "closed", "reply": "hello"}, "reply"},
	}
	for _, c := range cases {
		if message := e.fails(draft, "colliedesk_conversation_update", c.args); !strings.Contains(message, c.want) {
			t.Errorf("%s: message %q lacks %q", c.name, message, c.want)
		}
	}
	if after := restConversation(t, e, f.w, f.c3); after["status"] != "open" {
		t.Errorf("a refused call changed the status: %v", after)
	}
}

func TestNoteAddNeedsTheDraftTierAndStaysPrivate(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	args := map[string]any{"conversation_id": f.c2, "body": "Checked the invoice: refund is due."}
	if message := e.fails(e.connect(f.w.read, ""), "colliedesk_note_add", args); !strings.Contains(message, "`draft`") {
		t.Errorf("read token message %q does not name the draft tier", message)
	}
	out := e.ok(e.connect(f.w.draft, ""), "colliedesk_note_add", args)
	if at(t, out, "message", "kind") != "note" || at(t, out, "conversation_id") != f.c2 {
		t.Errorf("note result: %v", out)
	}
	status, listed := e.rest(http.MethodGet, "/api/v1/conversations/"+f.c2+"/messages", nil, f.w.admin)
	items, _ := listed["items"].([]any)
	if status != http.StatusOK || len(items) != 1 || at(t, items[0], "kind") != "note" || at(t, items[0], "body") != args["body"] {
		t.Errorf("REST messages after the note: %d %v", status, listed)
	}
}

func TestToolsNeverReachAConversationOfAnotherWorkspace(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	e.created("/api/v1/tags", map[string]any{"name": "bravo-tag"}, f.other.admin)
	e.incoming(f.other, f.otherConvID, "bravo secret body")
	secrets := []string{f.otherSubject, f.otherContactMail, "Bravo Customer", "bravo-tag", "bravo secret body", "admin@other.example.com"}
	draft := e.connect(f.w.draft, "")

	calls := []struct {
		tool string
		args map[string]any
	}{
		{"colliedesk_conversation_get", map[string]any{"conversation_id": f.otherConvID, "detail": "full"}},
		{"colliedesk_conversation_update", map[string]any{"conversation_id": f.otherConvID, "status": "closed"}},
		{"colliedesk_note_add", map[string]any{"conversation_id": f.otherConvID, "body": "not yours"}},
		{"colliedesk_conversation_update", map[string]any{"conversation_id": f.c3, "add_tags": []string{"bravo-tag"}}},
		{"colliedesk_conversation_update", map[string]any{"conversation_id": f.c3, "assignee": "admin@other.example.com"}},
	}
	for index, c := range calls {
		result := e.call(draft, c.tool, c.args)
		if !result.IsError {
			t.Errorf("call %d %s succeeded across workspaces: %s", index, c.tool, text(result))
			continue
		}
		blob := text(result)
		for _, secret := range secrets {
			// A message may repeat the words the caller sent; it must never add anything of the other workspace.
			if strings.Contains(blob, secret) && !strings.Contains(fmt.Sprint(c.args), secret) {
				t.Errorf("call %d %s leaked %q: %s", index, c.tool, secret, blob)
			}
		}
		if index < 3 && !strings.Contains(blob, "No conversation "+f.otherConvID+" in this workspace") {
			t.Errorf("call %d %s message=%q, want a plain not found", index, c.tool, blob)
		}
		if index == 3 && !strings.Contains(blob, "Existing tags: billing.") || index == 4 && !strings.Contains(blob, "Ada Admin <admin@inbox.example.com>.") {
			t.Errorf("call %d %s message=%q must list only this workspace's own tags or team", index, c.tool, blob)
		}
	}
	other := restConversation(t, e, f.other, f.otherConvID)
	if other["status"] != "open" || len(tagNames(t, other)) != 0 {
		t.Errorf("workspace B conversation was changed: %v", other)
	}
	if status, listed := e.rest(http.MethodGet, "/api/v1/conversations/"+f.otherConvID+"/messages", nil, f.other.admin); status != http.StatusOK || len(listed["items"].([]any)) != 1 {
		t.Errorf("workspace B gained a message: %v", listed)
	}
}
