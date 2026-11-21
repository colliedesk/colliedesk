package mcpserver_test

import (
	"context"
	"fmt"
	"slices"
	"testing"
)

// An AI agent narrows the article list with the filters a person has on the list page: sub-collections,
// author, tags, dates and missing translations, in one call; each row says which translations it has.
func TestKBArticlesSearchTakesTheListFiltersThroughMCP(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("KB List Filter Desk", "admin@kb-list-filter.example.com")
	var lan string
	if err := e.pool.QueryRow(context.Background(), `INSERT INTO agents(workspace_id,email,display_name,role) VALUES($1,'lan@kb-list-filter.example.com','Lan Pham','agent') RETURNING id::text`, w.id).Scan(&lan); err != nil {
		t.Fatal(err)
	}
	session := e.connect(w.adminTier, "")
	e.ok(session, "colliedesk_help_center_languages_set", map[string]any{"languages": []any{map[string]any{"locale": "fr", "visible": true}}})
	billing := at(t, e.ok(session, "colliedesk_kb_collection_create", map[string]any{"name": "Billing"}), "collection", "id").(string)
	refunds := at(t, e.ok(session, "colliedesk_kb_collection_create", map[string]any{"name": "Refunds", "parent_id": billing}), "collection", "id").(string)
	card := at(t, e.ok(session, "colliedesk_kb_article_create", map[string]any{"collection_id": refunds, "title": "Card refund", "body": "Five days.", "tags": []any{"refund", "card"}}), "article").(map[string]any)
	e.ok(session, "colliedesk_kb_article_create", map[string]any{"collection_id": billing, "title": "Invoice copy", "body": "From settings.", "tags": []any{"card"}})
	e.ok(session, "colliedesk_kb_translation_create", map[string]any{"article_id": card["id"], "locale": "fr", "title": "Remboursement", "body": "Cinq jours."})
	e.ok(session, "colliedesk_kb_article_update", map[string]any{"article_id": card["id"], "updated_at": card["updated_at"], "author_agent_id": lan})

	titles := func(arguments map[string]any) []string {
		t.Helper()
		found := e.ok(session, "colliedesk_kb_articles_search", arguments)
		e.requireMatchesOutputSchema(session, "colliedesk_kb_articles_search", found)
		names := []string{}
		for _, row := range found["articles"].([]any) {
			names = append(names, row.(map[string]any)["team_text"].(map[string]any)["title"].(string))
		}
		slices.Sort(names)
		return names
	}
	for _, c := range []struct {
		arguments map[string]any
		want      string
	}{
		{map[string]any{"collection_id": billing}, "[Invoice copy]"},
		{map[string]any{"collection_id": billing, "include_subcollections": true}, "[Card refund Invoice copy]"},
		{map[string]any{"author": []any{lan}}, "[Card refund]"},
		{map[string]any{"tags": []any{"card"}}, "[Card refund Invoice copy]"},
		{map[string]any{"tags": []any{"card", "refund"}, "tag_match": "all"}, "[Card refund]"},
		{map[string]any{"translation": "missing", "locale": "fr"}, "[Invoice copy]"},
		{map[string]any{"created_after": "2000-01-01", "updated_before": "2000-01-01T00:00:00Z"}, "[]"},
		{map[string]any{"created_after": "2000-01-01"}, "[Card refund Invoice copy]"},
	} {
		if got := fmt.Sprint(titles(c.arguments)); got != c.want {
			t.Errorf("%v = %s, want %s", c.arguments, got, c.want)
		}
	}

	found := e.ok(session, "colliedesk_kb_articles_search", map[string]any{"tags": []any{"refund"}})
	if got := fmt.Sprint(found["articles"].([]any)[0].(map[string]any)["languages"]); got != "[map[locale:fr outdated:false]]" {
		t.Fatalf("languages = %s, want [map[locale:fr outdated:false]]", got)
	}
	if refused := e.fails(session, "colliedesk_kb_articles_search", map[string]any{"include_subcollections": true}); refused == "" {
		t.Fatal("include_subcollections without collection_id was not refused")
	}
}
