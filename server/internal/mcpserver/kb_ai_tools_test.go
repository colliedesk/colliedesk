package mcpserver_test

import "testing"

func TestSendTokenKeepsAnArticleAwayFromAIAndLetsItBack(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("KB AI Desk", "admin@kb-ai.example.com")
	session := e.connect(w.send, "")
	collection := at(t, e.ok(session, "colliedesk_kb_collection_create", map[string]any{"name": "Billing"}), "collection", "id").(string)
	created := e.ok(session, "colliedesk_kb_article_create", map[string]any{"collection_id": collection, "title": "Refund rules", "body": "Within five days.", "status": "published"})
	articleID := at(t, created, "article", "id").(string)
	if at(t, created, "article", "ai_exclusion") != nil {
		t.Fatalf("a new article is excluded from AI: %v", created)
	}
	excluded := e.ok(session, "colliedesk_kb_article_ai_exclude", map[string]any{"article_id": articleID})
	if at(t, excluded, "article", "ai_exclusion", "at") == nil || at(t, excluded, "article", "updated_at") != at(t, created, "article", "updated_at") ||
		at(t, excluded, "article", "last_change", "kind") != "ai_exclude" {
		t.Fatalf("exclude = %v", excluded)
	}
	again := e.ok(session, "colliedesk_kb_article_ai_exclude", map[string]any{"article_id": articleID})
	if at(t, again, "article", "last_change", "version") != at(t, excluded, "article", "last_change", "version") {
		t.Errorf("excluding twice made another version: %v", again)
	}
	included := e.ok(session, "colliedesk_kb_article_ai_include", map[string]any{"article_id": articleID})
	if at(t, included, "article", "ai_exclusion") != nil || at(t, included, "article", "last_change", "kind") != "ai_include" {
		t.Fatalf("include = %v", included)
	}
	e.fails(session, "colliedesk_kb_article_ai_exclude", map[string]any{"article_id": "00000000-0000-4000-8000-000000000000"})
}
