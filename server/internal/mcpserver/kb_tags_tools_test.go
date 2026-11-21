package mcpserver_test

import (
	"fmt"
	"testing"
)

// An AI agent tags an article like a person in the editor: the tags come back cleaned and sorted, in
// the search row and the article, and an update with only tags is a change.
func TestKBArticleTagsThroughMCP(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("KB Tags Desk", "admin@kb-tags.example.com")
	session := e.connect(w.send, "")
	collectionID := at(t, e.ok(session, "colliedesk_kb_collection_create", map[string]any{"name": "Guides"}), "collection", "id").(string)

	created := e.ok(session, "colliedesk_kb_article_create", map[string]any{"collection_id": collectionID, "title": "Refunds", "body": "Within 14 days.",
		"tags": []any{"  Billing ", "refunds"}})
	e.requireMatchesOutputSchema(session, "colliedesk_kb_article_create", created)
	if got := fmt.Sprint(at(t, created, "article", "tags")); got != "[billing refunds]" {
		t.Fatalf("created tags = %s, want [billing refunds]", got)
	}
	updated := e.ok(session, "colliedesk_kb_article_update", map[string]any{"article_id": at(t, created, "article", "id"),
		"updated_at": at(t, created, "article", "updated_at"), "tags": []any{"vat"}})
	if got := fmt.Sprint(at(t, updated, "article", "tags")); got != "[vat]" {
		t.Fatalf("updated tags = %s, want [vat]", got)
	}
	found := e.ok(session, "colliedesk_kb_articles_search", map[string]any{})
	e.requireMatchesOutputSchema(session, "colliedesk_kb_articles_search", found)
	rows := found["articles"].([]any)
	if got := fmt.Sprint(rows[0].(map[string]any)["tags"]); got != "[vat]" {
		t.Fatalf("search row tags = %s, want [vat]", got)
	}
}
