package mcpserver_test

import (
	"context"
	"testing"
)

// An AI agent credits an article to a teammate like a person on the article page: the article and its
// search row name the author, and the history names the author before and after.
func TestKBArticleAuthorThroughMCP(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("KB Author Desk", "admin@kb-author.example.com")
	var lan string
	if err := e.pool.QueryRow(context.Background(), `INSERT INTO agents(workspace_id,email,display_name,role) VALUES($1,'lan@kb-author.example.com','Lan Pham','agent') RETURNING id::text`, w.id).Scan(&lan); err != nil {
		t.Fatal(err)
	}
	session := e.connect(w.send, "")
	collectionID := at(t, e.ok(session, "colliedesk_kb_collection_create", map[string]any{"name": "Guides"}), "collection", "id").(string)
	created := e.ok(session, "colliedesk_kb_article_create", map[string]any{"collection_id": collectionID, "title": "Refunds", "body": "Within 14 days."})
	if got := at(t, created, "article", "author", "name"); got != "admin@kb-author.example.com" {
		t.Fatalf("created author = %v, want the admin who made the token", got)
	}

	updated := e.ok(session, "colliedesk_kb_article_update", map[string]any{"article_id": at(t, created, "article", "id"),
		"updated_at": at(t, created, "article", "updated_at"), "author_agent_id": lan})
	e.requireMatchesOutputSchema(session, "colliedesk_kb_article_update", updated)
	if got := at(t, updated, "article", "author", "name"); got != "Lan Pham" {
		t.Fatalf("updated author = %v, want Lan Pham", got)
	}
	found := e.ok(session, "colliedesk_kb_articles_search", map[string]any{})
	e.requireMatchesOutputSchema(session, "colliedesk_kb_articles_search", found)
	if got := found["articles"].([]any)[0].(map[string]any)["author"].(map[string]any)["name"]; got != "Lan Pham" {
		t.Fatalf("search row author = %v, want Lan Pham", got)
	}

	history := e.ok(session, "colliedesk_kb_article_history", map[string]any{"article_id": at(t, created, "article", "id")})
	e.requireMatchesOutputSchema(session, "colliedesk_kb_article_history", history)
	newest := history["versions"].([]any)[0].(map[string]any)
	change, _ := newest["author_change"].(map[string]any)
	if newest["kind"] != "author_change" || change == nil ||
		change["from"].(map[string]any)["name"] != "admin@kb-author.example.com" || change["to"].(map[string]any)["name"] != "Lan Pham" {
		t.Fatalf("newest version = %+v, want author_change from the admin to Lan Pham", newest)
	}
}
