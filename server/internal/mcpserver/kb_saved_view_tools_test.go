package mcpserver_test

import (
	"fmt"
	"strings"
	"testing"
)

func TestKnowledgeBaseSavedViewsThroughMCP(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("KB Views", "admin@kb-views.example.com")
	other := e.newWorkspace("Other KB Views", "admin@other-kb-views.example.com")
	draft := e.connect(w.draft, "")

	created := e.ok(draft, "colliedesk_saved_view_create", map[string]any{"name": "Billing drafts", "area": "kb", "scope": "shared",
		"filters": map[string]any{"status": "draft", "tag": []any{" Billing "}}})
	id := at(t, created, "saved_view", "id").(string)
	if at(t, created, "saved_view", "area") != "kb" || fmt.Sprint(at(t, created, "saved_view", "filters")) != "map[status:draft tag:[billing]]" {
		t.Fatalf("created view = %v", created)
	}
	e.ok(draft, "colliedesk_saved_view_create", map[string]any{"name": "Billing drafts", "scope": "shared", "filters": map[string]any{}})

	// Each area lists its own views; the inbox is the default.
	for area, want := range map[string]string{"": "inbox", "inbox": "inbox", "kb": "kb"} {
		args := map[string]any{}
		if area != "" {
			args["area"] = area
		}
		views := e.ok(draft, "colliedesk_saved_views_list", args)["saved_views"].([]any)
		if len(views) != 1 || at(t, views[0], "area") != want {
			t.Errorf("saved_views_list %v = %v, want one %s view", args, views, want)
		}
	}

	// A knowledge base view never opens in the inbox, and keeps to article filters.
	if message := e.fails(draft, "colliedesk_inbox_search", map[string]any{"saved_view_id": id}); !strings.Contains(message, "colliedesk_kb_articles_search") {
		t.Errorf("inbox search with a knowledge base view: %q", message)
	}
	if message := e.fails(draft, "colliedesk_saved_view_update", map[string]any{"saved_view_id": id, "updated_at": at(t, created, "saved_view", "updated_at"),
		"filters": map[string]any{"awaiting": "team"}}); !strings.Contains(message, "knowledge base") {
		t.Errorf("inbox filters on a knowledge base view: %q", message)
	}

	e.ok(draft, "colliedesk_saved_view_delete", map[string]any{"saved_view_id": id})
	if views := e.ok(draft, "colliedesk_saved_views_list", map[string]any{"area": "kb", "trash": true})["saved_views"].([]any); len(views) != 1 || at(t, views[0], "id") != id {
		t.Fatalf("knowledge base trash = %v", views)
	}
	if views := e.ok(draft, "colliedesk_saved_views_list", map[string]any{"trash": true})["saved_views"].([]any); len(views) != 0 {
		t.Fatalf("inbox trash = %v", views)
	}

	// The other workspace sees none of it.
	if views := e.ok(e.connect(other.draft, ""), "colliedesk_saved_views_list", map[string]any{"area": "kb", "trash": true})["saved_views"].([]any); len(views) != 0 {
		t.Fatalf("the other workspace lists %v", views)
	}
	if message := e.fails(e.connect(other.draft, ""), "colliedesk_saved_view_restore", map[string]any{"saved_view_id": id}); !strings.Contains(message, "No saved view") {
		t.Errorf("restore from another workspace: %q", message)
	}
}

// A knowledge base view's filters, with q passed as query and tag as tags, find in
// colliedesk_kb_articles_search the articles the view shows a person.
func TestAKnowledgeBaseViewsFiltersSearchTheArticlesItShows(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("KB View Search", "admin@kb-view-search.example.com")
	send := e.connect(w.send, "")
	collection := at(t, e.ok(send, "colliedesk_kb_collection_create", map[string]any{"name": "Billing"}), "collection", "id").(string)
	for _, article := range []map[string]any{
		{"title": "Refund rules", "body": "Refunds within five days.", "status": "published", "tags": []any{"billing", "refunds"}},
		{"title": "Refund form", "body": "Fill in the refund form.", "status": "published", "tags": []any{"forms"}},
		{"title": "Invoices", "body": "Monthly refund of credits.", "status": "draft", "tags": []any{"billing"}},
	} {
		article["collection_id"] = collection
		e.ok(send, "colliedesk_kb_article_create", article)
	}
	e.ok(send, "colliedesk_saved_view_create", map[string]any{"name": "Published billing refunds", "area": "kb", "scope": "personal",
		"filters": map[string]any{"q": "refund", "status": "published", "tag": []any{"billing"}}})

	views := e.ok(send, "colliedesk_saved_views_list", map[string]any{"area": "kb"})["saved_views"].([]any)
	filters := at(t, views[0], "filters").(map[string]any)
	filters["query"], filters["tags"] = filters["q"], filters["tag"]
	delete(filters, "q")
	delete(filters, "tag")
	found := e.ok(send, "colliedesk_kb_articles_search", filters)
	var titles []string
	for _, row := range found["articles"].([]any) {
		titles = append(titles, at(t, row, "team_text", "title").(string))
	}
	if fmt.Sprint(titles) != "[Refund rules]" {
		t.Fatalf("articles of the view = %v, want [Refund rules]", titles)
	}
}
