package mcpserver_test

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
)

// An AI agent chooses the article list columns of the person its token acts as, reads them back with
// the workspace settings, and sets them back to the default; another workspace keeps its own.
func TestAnAgentChoosesItsPersonsKnowledgeBaseListColumns(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("KB Columns Desk", "admin@kb-columns.example.com")
	other := e.newWorkspace("Other Columns Desk", "admin@other-columns.example.com")
	draft := e.connect(w.draft, "")
	columns := func(answer map[string]any) string {
		return fmt.Sprint(answer["columns"], " ", answer["default"])
	}

	settings := e.ok(draft, "colliedesk_workspace_settings_get", nil)
	e.requireMatchesOutputSchema(draft, "colliedesk_workspace_settings_get", settings)
	if got := columns(settings["your_kb_list_columns"].(map[string]any)); got != "[status author tags updated] true" {
		t.Fatalf("columns before choosing = %s", got)
	}
	chosen := e.ok(draft, "colliedesk_my_kb_list_columns_set", map[string]any{"columns": []any{"votes", "status", "languages"}})
	e.requireMatchesOutputSchema(draft, "colliedesk_my_kb_list_columns_set", chosen)
	if got := columns(chosen); got != "[votes status languages] false" {
		t.Fatalf("set columns = %s", got)
	}
	if got := columns(e.ok(draft, "colliedesk_workspace_settings_get", nil)["your_kb_list_columns"].(map[string]any)); got != "[votes status languages] false" {
		t.Fatalf("columns read back = %s", got)
	}
	theirs := e.ok(e.connect(other.draft, ""), "colliedesk_workspace_settings_get", nil)
	if got := columns(theirs["your_kb_list_columns"].(map[string]any)); got != "[status author tags updated] true" {
		t.Fatalf("another workspace's columns = %s", got)
	}
	if message := e.fails(draft, "colliedesk_my_kb_list_columns_set", map[string]any{"columns": []any{"status", "status"}}); !strings.Contains(message, "uniqueItems") {
		t.Errorf("a column named twice: %q", message)
	}
	refusedWithoutPermission(t, e, w.read, "colliedesk_my_kb_list_columns_set", map[string]any{"columns": nil})
	if got := columns(e.ok(draft, "colliedesk_my_kb_list_columns_set", map[string]any{"columns": nil})); got != "[status author tags updated] true" {
		t.Fatalf("columns after null = %s", got)
	}
}

// The article search rows carry each article's votes, so an agent sees the Votes column a person sees.
func TestArticleSearchRowsCarryTheirVotes(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("KB Row Votes Desk", "admin@kb-row-votes.example.com")
	send := e.connect(w.send, "")
	e.ok(e.connect(w.adminTier, ""), "colliedesk_help_center_update", map[string]any{"enabled": true, "title": "Help", "locale": "en"})
	collection := at(t, e.ok(send, "colliedesk_kb_collection_create", map[string]any{"name": "Billing"}), "collection", "id").(string)
	created := e.ok(send, "colliedesk_kb_article_create", map[string]any{"collection_id": collection, "title": "Refund rules", "body": "Within five days.", "status": "published"})
	e.ok(send, "colliedesk_kb_article_create", map[string]any{"collection_id": collection, "title": "Invoices", "body": "Monthly.", "status": "published"})
	address := at(t, created, "article", "url").(string)
	e.vote(address, url.Values{"helpful": {"yes"}}, "192.0.2.81")
	e.vote(address, url.Values{"helpful": {"no"}}, "192.0.2.82")
	e.vote(address, url.Values{"helpful": {"yes"}}, "192.0.2.83")

	read := e.connect(w.read, "")
	found := e.ok(read, "colliedesk_kb_articles_search", nil)
	e.requireMatchesOutputSchema(read, "colliedesk_kb_articles_search", found)
	got := map[string]string{}
	for _, row := range found["articles"].([]any) {
		got[at(t, row, "team_text", "title").(string)] = fmt.Sprint(at(t, row, "votes", "helpful"), "/", at(t, row, "votes", "not_helpful"))
	}
	if fmt.Sprint(got) != "map[Invoices:0/0 Refund rules:2/1]" {
		t.Fatalf("votes on the search rows = %v", got)
	}
}
