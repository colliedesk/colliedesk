package mcpserver_test

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
)

func TestInboxSearchTakesEveryInboxFilterAndASavedView(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	// c1: pending, billing, assigned to the admin. c3 gets a customer message, so it awaits the team.
	e.incoming(f.w, f.c3, "Still broken")
	e.rest(http.MethodPatch, "/api/v1/conversations/"+f.c2, map[string]any{"priority": "urgent"}, f.w.admin)
	session := e.connect(f.w.read, "")
	for _, check := range []struct {
		args map[string]any
		want []string
	}{
		{map[string]any{"status": []any{"open", "pending"}, "sort": "created_asc"}, []string{f.c1, f.c2, f.c3}},
		{map[string]any{"status": "pending"}, []string{f.c1}},
		{map[string]any{"priority": []any{"urgent"}}, []string{f.c2}},
		{map[string]any{"assignee": []any{"unassigned", f.adminID}, "sort": "created_asc"}, []string{f.c1, f.c2, f.c3}},
		{map[string]any{"tag": []any{"BILLING"}, "tag_match": "all"}, []string{f.c1}},
		{map[string]any{"awaiting": "team"}, []string{f.c3}},
		{map[string]any{"channel": "agent", "source": "native", "sort": "priority"}, []string{f.c2, f.c3, f.c1}},
		{map[string]any{"q": "alan@cust"}, []string{f.c2}},
		{map[string]any{"has_pending_draft": true}, nil},
	} {
		if got := conversationIDs(t, e.ok(session, "colliedesk_inbox_search", check.args)); !slices.Equal(got, check.want) {
			t.Errorf("inbox_search %v = %v, want %v", check.args, got, check.want)
		}
	}
	row := at(t, e.ok(session, "colliedesk_inbox_search", map[string]any{"q": "Login"}), "conversations", 0)
	if at(t, row, "channel") != "agent" {
		t.Errorf("row channel = %v, want agent", at(t, row, "channel"))
	}

	// A saved view's filters apply, and an argument next to it replaces that one filter.
	draft := e.connect(f.w.draft, "")
	view := at(t, e.ok(draft, "colliedesk_saved_view_create", map[string]any{"name": "Urgent", "scope": "shared",
		"filters": map[string]any{"priority": []any{"urgent", "high"}, "status": []any{"open"}}}), "saved_view", "id").(string)
	if got := conversationIDs(t, e.ok(session, "colliedesk_inbox_search", map[string]any{"saved_view_id": view})); !slices.Equal(got, []string{f.c2}) {
		t.Errorf("search with the view = %v, want [%s]", got, f.c2)
	}
	if got := conversationIDs(t, e.ok(session, "colliedesk_inbox_search", map[string]any{"saved_view_id": view, "priority": "normal"})); !slices.Equal(got, []string{f.c3}) {
		t.Errorf("search with the view and priority normal = %v, want [%s]", got, f.c3)
	}
	// A cursor of one filter set does not page through another.
	first := e.ok(session, "colliedesk_inbox_search", map[string]any{"limit": 1, "priority": "normal"})
	cursor := first["next_cursor"].(string)
	if message := e.fails(session, "colliedesk_inbox_search", map[string]any{"limit": 1, "priority": "urgent", "cursor": cursor}); !strings.Contains(message, "different search") {
		t.Errorf("cursor reused with another priority: %q", message)
	}
	if message := e.fails(session, "colliedesk_inbox_search", map[string]any{"limit": 1, "priority": "normal", "sort": "created_asc", "cursor": cursor}); !strings.Contains(message, "different search") {
		t.Errorf("cursor reused with another sort: %q", message)
	}

	// Nothing of the other workspace is reachable: not its conversations, not its views.
	otherView := e.created("/api/v1/saved-views", map[string]any{"name": "Theirs", "scope": "shared", "filters": map[string]any{}}, f.other.admin)
	if message := e.fails(session, "colliedesk_inbox_search", map[string]any{"saved_view_id": otherView}); !strings.Contains(message, "No saved view") {
		t.Errorf("another workspace's view: %q", message)
	}
	for _, args := range []map[string]any{{"q": "Bravo"}, {"q": "secret.example.com"}, {"channel": "agent"}} {
		for _, id := range conversationIDs(t, e.ok(session, "colliedesk_inbox_search", args)) {
			if id == f.otherConvID {
				t.Fatalf("inbox_search %v lists the other workspace's conversation", args)
			}
		}
	}
}

func TestSavedViewToolsKeepPersonalViewsPrivateAndDeletesRestorable(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("View Tools", "admin@view-tools.example.com")
	other := e.newWorkspace("Other Views", "admin@other-views.example.com")
	draft := e.connect(w.draft, "")

	mine := e.ok(draft, "colliedesk_saved_view_create", map[string]any{"name": "Mine", "scope": "personal", "filters": map[string]any{"awaiting": "team"}})
	id := at(t, mine, "saved_view", "id").(string)
	if at(t, mine, "saved_view", "mine") != true || at(t, mine, "saved_view", "filters", "awaiting") != "team" {
		t.Fatalf("created view = %v", mine)
	}
	// A change on an older version is refused; the version just read goes through.
	if message := e.fails(draft, "colliedesk_saved_view_update", map[string]any{"saved_view_id": id, "updated_at": "2026-01-01T00:00:00Z", "name": "Waiting on us"}); !strings.Contains(message, "read it again") {
		t.Fatalf("update on an older version: %q", message)
	}
	renamed := e.ok(draft, "colliedesk_saved_view_update", map[string]any{"saved_view_id": id, "updated_at": at(t, mine, "saved_view", "updated_at"), "name": "Waiting on us", "scope": "shared"})
	if at(t, renamed, "saved_view", "scope") != "shared" || at(t, renamed, "saved_view", "mine") != false {
		t.Fatalf("shared view = %v", renamed)
	}
	deleted := e.ok(draft, "colliedesk_saved_view_delete", map[string]any{"saved_view_id": id})
	if at(t, deleted, "saved_view", "purges_at") == nil {
		t.Fatalf("deleted view = %v", deleted)
	}
	if views := e.ok(draft, "colliedesk_saved_views_list", nil)["saved_views"].([]any); len(views) != 0 {
		t.Fatalf("list after delete = %v", views)
	}
	if views := e.ok(draft, "colliedesk_saved_views_list", map[string]any{"trash": true})["saved_views"].([]any); len(views) != 1 {
		t.Fatalf("trash = %v", views)
	}
	e.ok(draft, "colliedesk_saved_view_restore", map[string]any{"saved_view_id": id})
	if views := e.ok(e.connect(w.read, ""), "colliedesk_saved_views_list", nil)["saved_views"].([]any); len(views) != 1 {
		t.Fatalf("list after restore = %v", views)
	}
	// A read token's write waits for a person.
	requestedForTier(t, e, w.read, "colliedesk_saved_view_create", map[string]any{"name": "Proposed", "scope": "shared", "filters": map[string]any{}})
	requestedForTier(t, e, w.read, "colliedesk_saved_view_delete", map[string]any{"saved_view_id": id})

	// The admin's personal view is the admin's: a token acting as the admin sees it, the other workspace never does.
	personal := e.created("/api/v1/saved-views", map[string]any{"name": "Admin only", "scope": "personal", "filters": map[string]any{}}, w.admin)
	theirs := e.created("/api/v1/saved-views", map[string]any{"name": "Theirs", "scope": "shared", "filters": map[string]any{}}, other.admin)
	otherSession := e.connect(other.draft, "")
	for _, view := range e.ok(otherSession, "colliedesk_saved_views_list", nil)["saved_views"].([]any) {
		if at(t, view, "id") != theirs {
			t.Fatalf("the other workspace lists %v", view)
		}
	}
	for tool, args := range map[string]map[string]any{
		"colliedesk_saved_view_update":  {"saved_view_id": personal, "updated_at": "2026-01-01T00:00:00Z", "name": "Stolen"},
		"colliedesk_saved_view_delete":  {"saved_view_id": personal},
		"colliedesk_saved_view_restore": {"saved_view_id": personal},
	} {
		if message := e.fails(otherSession, tool, args); !strings.Contains(message, "No saved view") {
			t.Errorf("%s on another workspace's view: %q", tool, message)
		}
	}
	var name string
	if err := e.pool.QueryRow(context.Background(), `SELECT name FROM saved_views WHERE id=$1`, personal).Scan(&name); err != nil || name != "Admin only" {
		t.Fatalf("the view changed: %q %v", name, err)
	}
}
