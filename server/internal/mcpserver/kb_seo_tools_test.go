package mcpserver_test

import "testing"

// An AI agent sets an article's search title and meta description like a person in the editor: on
// create and on update, read back under team_text, and cleared with "".
func TestKBArticleSearchFieldsThroughMCP(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("KB SEO Desk", "admin@kb-seo.example.com")
	session := e.connect(w.send, "")
	collectionID := at(t, e.ok(session, "colliedesk_kb_collection_create", map[string]any{"name": "Guides"}), "collection", "id").(string)

	created := e.ok(session, "colliedesk_kb_article_create", map[string]any{"collection_id": collectionID, "title": "Reset your password", "body": "Open Settings.",
		"status": "published", "seo_title": "Reset an Acme password", "seo_description": "Choose a new password in two steps."})
	e.requireMatchesOutputSchema(session, "colliedesk_kb_article_create", created)
	if at(t, created, "article", "team_text", "seo_title") != "Reset an Acme password" || at(t, created, "article", "team_text", "seo_description") != "Choose a new password in two steps." {
		t.Fatalf("create = %v", created)
	}
	updated := e.ok(session, "colliedesk_kb_article_update", map[string]any{"article_id": at(t, created, "article", "id"), "updated_at": at(t, created, "article", "updated_at"),
		"seo_title": ""})
	if at(t, updated, "article", "team_text", "seo_title") != "" || at(t, updated, "article", "team_text", "seo_description") != "Choose a new password in two steps." {
		t.Fatalf("update = %v", updated)
	}
}
