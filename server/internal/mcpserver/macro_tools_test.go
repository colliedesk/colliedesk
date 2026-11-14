package mcpserver_test

import (
	"testing"
)

func TestMacroApplyFillsTheReplyAndSetsTheConversation(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Macro Tools", "admin@macro-tools.example.com")
	session := e.connect(w.adminTier, "")
	contact := e.created("/api/v1/contacts", map[string]any{"name": "Mai Linh", "email": "mai@macro-tools.example.com"}, w.admin)
	conversation := e.created("/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Refund please"}, w.admin)
	tag := e.created("/api/v1/tags", map[string]any{"name": "billing"}, w.admin)

	created := e.ok(session, "colliedesk_macro_save", map[string]any{
		"title": "Refund", "scope": "shared", "body": "Hi {{customer.name}}",
		"actions": map[string]any{"status": "pending", "add_tag_ids": []string{tag}},
	})
	id, _ := at(t, created, "macro", "id").(string)
	if id == "" || at(t, created, "macro", "title") != "Refund" {
		t.Fatalf("create = %v", created)
	}
	listed := e.ok(session, "colliedesk_macros_list", nil)
	macros := listed["macros"].([]any)
	if len(macros) != 1 || at(t, macros[0], "title") != "Refund" {
		t.Fatalf("list = %v", listed)
	}
	applied := e.ok(session, "colliedesk_macro_apply", map[string]any{"conversation_id": conversation, "macro_id": id})
	if at(t, applied, "body") != "Hi Mai Linh" || at(t, applied, "title") != "Refund" || at(t, applied, "conversation", "subject") != "Refund please" ||
		at(t, applied, "conversation", "status") != "pending" || at(t, applied, "conversation", "tags", 0, "name") != "billing" {
		t.Fatalf("apply = %v", applied)
	}
	undone := e.ok(session, "colliedesk_macro_undo", map[string]any{"conversation_id": conversation, "application_id": at(t, applied, "application_id")})
	if at(t, undone, "conversation", "status") != "open" || len(at(t, undone, "restored").([]any)) == 0 {
		t.Fatalf("undo = %v", undone)
	}
}
