package mcpserver_test

import "testing"

// An article with unpublished changes has one updated_at: what search, the trash and get show works
// for the next call, so an agent never needs to tell the article's from the draft's.
func TestAnAgentSendsBackTheUpdatedAtAnyReadOfADraftedArticleShows(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("KB Draft Token Tools", "admin@kb-draft-token-tools.example.com")
	session := e.connect(w.send, "")
	collectionID := at(t, e.ok(session, "colliedesk_kb_collection_create", map[string]any{"name": "Billing"}), "collection", "id").(string)
	created := e.ok(session, "colliedesk_kb_article_create", map[string]any{"collection_id": collectionID, "title": "Refunds", "body": "Within 14 days.", "status": "published"})
	articleID := at(t, created, "article", "id").(string)
	e.ok(session, "colliedesk_kb_article_update", map[string]any{"article_id": articleID, "updated_at": at(t, created, "article", "updated_at"), "body": "Within 30 days."})

	found := e.ok(session, "colliedesk_kb_articles_search", map[string]any{"query": "Refunds"})["articles"].([]any)
	if len(found) != 1 {
		t.Fatalf("search = %v", found)
	}
	e.ok(session, "colliedesk_kb_article_delete", map[string]any{"article_id": articleID, "updated_at": at(t, found[0], "updated_at")})
	trash := e.ok(session, "colliedesk_kb_articles_trash", nil)["articles"].([]any)
	e.ok(session, "colliedesk_kb_article_undelete", map[string]any{"article_id": articleID, "updated_at": at(t, trash[0], "updated_at")})

	got := e.ok(session, "colliedesk_kb_article_get", map[string]any{"article_id": articleID})
	saved := e.ok(session, "colliedesk_kb_article_update", map[string]any{"article_id": articleID, "updated_at": at(t, got, "article", "updated_at"), "body": "Within 45 days."})
	if at(t, saved, "article", "team_text", "body") != "Within 14 days." || at(t, saved, "article", "draft", "team_text", "body") != "Within 45 days." {
		t.Fatalf("saved = %v", saved)
	}
}
