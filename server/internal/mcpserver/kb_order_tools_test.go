package mcpserver_test

import (
	"strings"
	"testing"
)

func kbOrderTitles(t *testing.T, found []any) []string {
	t.Helper()
	var titles []string
	for _, item := range found {
		titles = append(titles, at(t, item, "team_text", "title").(string))
	}
	return titles
}

func TestKnowledgeBaseOrderToolSetsTheOrderAndStaysInItsWorkspace(t *testing.T) {
	e := newEnv(t)
	alpha := e.newWorkspace("KB Order Tools", "admin@kb-order-tools.example.com")
	beta := e.newWorkspace("KB Order Beta", "admin@kb-order-beta.example.com")
	collectionID := e.created("/api/v1/kb/collections", map[string]any{"name": "Guides"}, alpha.admin)
	first := e.created("/api/v1/kb/articles", map[string]any{"collection_id": collectionID, "title": "First steps", "body_md": "1", "status": "published"}, alpha.admin)
	second := e.created("/api/v1/kb/articles", map[string]any{"collection_id": collectionID, "title": "Second steps", "body_md": "2", "status": "published"}, alpha.admin)
	send := e.connect(alpha.send, "")

	ordered := e.ok(send, "colliedesk_kb_order_set", map[string]any{"parent_id": collectionID, "collection_ids": []string{}, "article_ids": []string{second, first}})
	if at(t, ordered, "parent_id") != collectionID || at(t, ordered, "article_ids", 0) != second {
		t.Fatalf("order = %v", ordered)
	}
	listed := e.ok(send, "colliedesk_kb_articles_search", map[string]any{"collection_id": collectionID})["articles"].([]any)
	if got := kbOrderTitles(t, listed); strings.Join(got, ",") != "Second steps,First steps" || at(t, listed[0], "position") != float64(1) {
		t.Fatalf("collection list after ordering = %v", listed)
	}
	picker := e.ok(send, "colliedesk_kb_articles_search", map[string]any{"status": "published", "sort": "manual"})["articles"].([]any)
	if got := kbOrderTitles(t, picker); strings.Join(got, ",") != "Second steps,First steps" {
		t.Fatalf("published in the help center's order = %v", got)
	}
	if message := e.fails(send, "colliedesk_kb_order_set", map[string]any{"parent_id": collectionID, "article_ids": []string{first}}); !strings.Contains(message, "Nothing changed") {
		t.Errorf("stale order: %q", message)
	}

	read := e.connect(alpha.read, "")
	if proposed := e.ok(read, "colliedesk_kb_order_set", map[string]any{"parent_id": collectionID, "article_ids": []string{first, second}}); proposed["agent_request"] == nil {
		t.Fatalf("read token order = %v, want an agent request", proposed)
	}

	other := e.connect(beta.send, "")
	if message := e.fails(other, "colliedesk_kb_order_set", map[string]any{"parent_id": collectionID, "article_ids": []string{first, second}}); !strings.Contains(message, "No knowledge base collection") {
		t.Errorf("beta orders alpha's collection: %q", message)
	}
	if message := e.fails(other, "colliedesk_kb_order_set", map[string]any{"collection_ids": []string{collectionID}}); !strings.Contains(message, "Nothing changed") {
		t.Errorf("beta puts alpha's collection at its top: %q", message)
	}
	listed = e.ok(send, "colliedesk_kb_articles_search", map[string]any{"collection_id": collectionID})["articles"].([]any)
	if got := kbOrderTitles(t, listed); strings.Join(got, ",") != "Second steps,First steps" {
		t.Fatalf("alpha's order after beta's calls = %v", got)
	}
}
