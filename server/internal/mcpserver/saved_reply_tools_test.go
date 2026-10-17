package mcpserver_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestSendTokenManagesSavedRepliesWithHistoryTrashAndRestore(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Macro Desk", "admin@macro.example.com")
	session := e.connect(w.send, "")

	created := e.ok(session, "colliedesk_saved_reply_create", map[string]any{"title": "Refund", "body": "Hi {{customer.name}}, we refunded you."})
	id, _ := at(t, created, "saved_reply", "id").(string)
	if id == "" || at(t, created, "saved_reply", "team_text", "title") != "Refund" {
		t.Fatalf("create = %v", created)
	}
	listed := e.ok(session, "colliedesk_saved_replies_list", nil)
	replies := listed["saved_replies"].([]any)
	if len(replies) != 1 || at(t, replies[0], "team_text", "body") != "Hi {{customer.name}}, we refunded you." || at(t, replies[0], "last_change", "via_token") != "test send" {
		t.Fatalf("list = %v", listed)
	}
	seen := at(t, replies[0], "updated_at").(string)

	updated := e.ok(session, "colliedesk_saved_reply_update", map[string]any{"saved_reply_id": id, "updated_at": seen, "body": "Hi {{customer.name}}, your refund is on its way."})
	if at(t, updated, "saved_reply", "last_change", "version") != float64(2) {
		t.Fatalf("update = %v", updated)
	}
	if message := e.fails(session, "colliedesk_saved_reply_update", map[string]any{"saved_reply_id": id, "updated_at": seen, "body": "Overwrite"}); !strings.Contains(message, "changed by someone else") {
		t.Errorf("update with a stale updated_at: %q", message)
	}

	got := e.ok(session, "colliedesk_saved_reply_get", map[string]any{"title": "refund"})
	if at(t, got, "saved_reply", "id") != id || at(t, got, "in_trash") != false {
		t.Fatalf("get by title = %v", got)
	}

	deleted := e.ok(session, "colliedesk_saved_reply_delete", map[string]any{"saved_reply_id": id})
	if at(t, deleted, "in_trash") != true || !strings.Contains(deleted["next"].(string), "colliedesk_saved_reply_undelete") {
		t.Fatalf("delete = %v", deleted)
	}
	if replies := e.ok(session, "colliedesk_saved_replies_list", nil)["saved_replies"].([]any); len(replies) != 0 {
		t.Fatalf("a deleted reply is listed: %v", replies)
	}
	trash := e.ok(session, "colliedesk_saved_replies_trash", nil)["saved_replies"].([]any)
	if len(trash) != 1 || at(t, trash[0], "id") != id || at(t, trash[0], "purges_at") == nil {
		t.Fatalf("trash = %v", trash)
	}
	if got := e.ok(session, "colliedesk_saved_reply_get", map[string]any{"saved_reply_id": id}); at(t, got, "in_trash") != true {
		t.Errorf("get of a trashed reply = %v, want in_trash", got)
	}

	e.ok(session, "colliedesk_saved_reply_undelete", map[string]any{"saved_reply_id": id})
	current := e.ok(session, "colliedesk_saved_reply_get", map[string]any{"saved_reply_id": id})
	restored := e.ok(session, "colliedesk_saved_reply_version_restore", map[string]any{"saved_reply_id": id, "version": 1, "updated_at": at(t, current, "saved_reply", "updated_at")})
	if at(t, restored, "saved_reply", "team_text", "body") != "Hi {{customer.name}}, we refunded you." {
		t.Fatalf("restore version 1 = %v", restored)
	}

	history := e.ok(session, "colliedesk_saved_reply_history", map[string]any{"saved_reply_id": id})["versions"].([]any)
	var kinds []string
	for _, version := range history {
		kinds = append(kinds, at(t, version, "kind").(string))
	}
	if strings.Join(kinds, ",") != "restore,undelete,delete,edit,create" || at(t, history[0], "restored_from_version") != float64(1) || at(t, history[3], "team_text", "body") != "Hi {{customer.name}}, your refund is on its way." {
		t.Fatalf("history = %v", history)
	}
}

func TestLowerTiersProposeSavedReplyChangesThroughTheTools(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Proposal Tools Desk", "admin@proposal-tools.example.com")
	id := e.created("/api/v1/saved-replies", map[string]any{"title": "Closing", "body": "Thanks."}, w.admin)
	_, list := e.rest(http.MethodGet, "/api/v1/saved-replies", nil, w.admin)
	seen := at(t, list["items"].([]any)[0], "updated_at").(string)

	edit := requestedForTier(t, e, w.draft, "colliedesk_saved_reply_update", map[string]any{"saved_reply_id": id, "updated_at": seen, "body": "Thanks a lot."})
	if summary := at(t, edit, "agent_request", "summary").(string); !strings.Contains(summary, `"Closing"`) || !strings.Contains(summary, "Thanks a lot.") {
		t.Errorf("edit proposal summary = %q", summary)
	}
	requestedForTier(t, e, w.read, "colliedesk_saved_reply_create", map[string]any{"title": "Greeting", "body": "Hello."})
	requestedForTier(t, e, w.draft, "colliedesk_saved_reply_delete", map[string]any{"saved_reply_id": id})
	requestedForTier(t, e, w.read, "colliedesk_saved_reply_version_restore", map[string]any{"saved_reply_id": id, "version": 1, "updated_at": seen})

	_, after := e.rest(http.MethodGet, "/api/v1/saved-replies", nil, w.admin)
	if items := after["items"].([]any); len(items) != 1 || at(t, items[0], "body") != "Thanks." {
		t.Fatalf("proposals changed the saved replies before approval: %v", items)
	}
	// Reading needs only the read tier.
	read := e.connect(w.read, "")
	e.ok(read, "colliedesk_saved_replies_list", nil)
	e.ok(read, "colliedesk_saved_reply_history", map[string]any{"saved_reply_id": id})
	e.ok(read, "colliedesk_saved_replies_trash", nil)
}

func TestSavedReplyToolsStayInTheirWorkspace(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Own Macros", "admin@own-macros.example.com")
	other := e.newWorkspace("Other Macros", "admin@other-macros.example.com")
	theirs := e.created("/api/v1/saved-replies", map[string]any{"title": "Their secret", "body": "other-workspace text"}, other.admin)
	session := e.connect(w.send, "")

	if replies := e.ok(session, "colliedesk_saved_replies_list", nil)["saved_replies"].([]any); len(replies) != 0 {
		t.Fatalf("list shows another workspace's replies: %v", replies)
	}
	for tool, args := range map[string]map[string]any{
		"colliedesk_saved_reply_get":             {"saved_reply_id": theirs},
		"colliedesk_saved_reply_history":         {"saved_reply_id": theirs},
		"colliedesk_saved_reply_update":          {"saved_reply_id": theirs, "updated_at": "2026-10-17T09:00:00Z", "body": "x"},
		"colliedesk_saved_reply_delete":          {"saved_reply_id": theirs},
		"colliedesk_saved_reply_undelete":        {"saved_reply_id": theirs},
		"colliedesk_saved_reply_version_restore": {"saved_reply_id": theirs, "version": 1, "updated_at": "2026-10-17T09:00:00Z"},
	} {
		if message := e.fails(session, tool, args); !strings.Contains(message, "No saved reply") {
			t.Errorf("%s on another workspace's reply: %q", tool, message)
		}
	}
	if _, body := e.rest(http.MethodGet, "/api/v1/saved-replies/"+theirs+"/versions", nil, other.admin); len(body["items"].([]any)) != 1 {
		t.Fatalf("the other workspace's history changed: %v", body)
	}
}
