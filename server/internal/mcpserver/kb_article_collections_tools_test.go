package mcpserver_test

import (
	"slices"
	"strings"
	"testing"
)

func anyStrings(t *testing.T, value any) []string {
	t.Helper()
	list, _ := value.([]any)
	out := make([]string, 0, len(list))
	for _, item := range list {
		out = append(out, item.(string))
	}
	return out
}

// An AI agent lists one article in several collections with the create and update calls it already
// makes; every answer says where the article is listed, its home first.
func TestAnAgentListsAnArticleInSeveralCollections(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("KB Listed Desk", "admin@kb-listed.example.com")
	session := e.connect(w.send, "")
	billing := at(t, e.ok(session, "colliedesk_kb_collection_create", map[string]any{"name": "Billing"}), "collection", "id").(string)
	bookings := at(t, e.ok(session, "colliedesk_kb_collection_create", map[string]any{"name": "Bookings"}), "collection", "id").(string)

	created := e.ok(session, "colliedesk_kb_article_create", map[string]any{"collection_ids": []string{billing, bookings}, "title": "Get a VAT invoice", "body": "Ask us."})
	e.requireMatchesOutputSchema(session, "colliedesk_kb_article_create", created)
	if at(t, created, "article", "collection_id") != billing || !slices.Equal(anyStrings(t, at(t, created, "article", "collection_ids")), []string{billing, bookings}) {
		t.Fatalf("create = %v", created)
	}
	found := e.ok(session, "colliedesk_kb_articles_search", map[string]any{"query": "invoice"})
	e.requireMatchesOutputSchema(session, "colliedesk_kb_articles_search", found)
	if rows := found["articles"].([]any); len(rows) != 1 || !slices.Equal(anyStrings(t, at(t, rows[0], "collection_ids")), []string{billing, bookings}) {
		t.Fatalf("search = %v", found)
	}
	updated := e.ok(session, "colliedesk_kb_article_update", map[string]any{"article_id": at(t, created, "article", "id"),
		"updated_at": at(t, created, "article", "updated_at"), "collection_ids": []string{billing}})
	if !slices.Equal(anyStrings(t, at(t, updated, "article", "collection_ids")), []string{billing}) || at(t, updated, "article", "url") != at(t, created, "article", "url") {
		t.Fatalf("update = %v", updated)
	}
	// A collection that is not one of this workspace's says where to find the right id.
	message := e.fails(session, "colliedesk_kb_article_update", map[string]any{"article_id": at(t, created, "article", "id"),
		"updated_at": at(t, updated, "article", "updated_at"), "collection_ids": []string{billing, "00000000-0000-4000-8000-000000000000"}})
	if !strings.Contains(message, "00000000-0000-4000-8000-000000000000") || !strings.Contains(message, "collections") {
		t.Errorf("unknown collection: %q", message)
	}
}
