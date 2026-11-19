package mcpserver_test

import (
	"strings"
	"testing"
)

// Agents often name the id of the one thing a tool acts on just id. A tool that takes exactly one
// required *_id argument and no id argument reads id as that argument.
func TestIDNamesTheOneRequiredIDArgument(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	session := e.connect(f.w.adminTier, "")

	got := e.ok(session, "colliedesk_conversation_get", map[string]any{"id": f.c2})
	if id := at(t, got, "conversation", "id"); id != f.c2 {
		t.Errorf("conversation_get id=%s read conversation %v", f.c2, id)
	}
	same := e.ok(session, "colliedesk_conversation_get", map[string]any{"id": f.c2, "conversation_id": f.c2})
	if id := at(t, same, "conversation", "id"); id != f.c2 {
		t.Errorf("conversation_get with id and conversation_id both %s read conversation %v", f.c2, id)
	}

	e.ok(session, "colliedesk_conversation_update", map[string]any{"id": f.c3, "status": "pending"})
	if status := at(t, e.ok(session, "colliedesk_conversation_get", map[string]any{"conversation_id": f.c3}), "conversation", "status"); status != "pending" {
		t.Errorf("conversation_update id=%s: status %v, want pending", f.c3, status)
	}

	differ := e.fails(session, "colliedesk_conversation_get", map[string]any{"id": f.c2, "conversation_id": f.c3})
	if !strings.HasPrefix(differ, "Invalid arguments") || !strings.Contains(differ, "id") || !strings.Contains(differ, "conversation_id") {
		t.Errorf("id and conversation_id differ: message %q, want an invalid-arguments error naming both", differ)
	}

	// id is only another name: another workspace's conversation stays out of reach.
	other := e.fails(session, "colliedesk_conversation_get", map[string]any{"id": f.otherConvID})
	if strings.Contains(other, f.otherSubject) {
		t.Errorf("conversation_get id of another workspace's conversation leaked it: %q", other)
	}
}
