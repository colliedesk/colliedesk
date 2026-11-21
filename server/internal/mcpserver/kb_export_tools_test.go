package mcpserver_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// An AI agent hands a person the article list as a spreadsheet: the CSV the list page downloads, with
// its filters, a title that starts like a formula kept as text, and the link to the whole file when
// the agent asked for fewer rows. The tag filter's values come from colliedesk_kb_tags_list, and
// colliedesk_help_center_update shows the tags to readers.
func TestKBArticlesExportAndTagsThroughMCP(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("KB Export Desk", "admin@kb-export.example.com")
	session := e.connect(w.adminTier, "")
	guides := at(t, e.ok(session, "colliedesk_kb_collection_create", map[string]any{"name": "Guides"}), "collection", "id").(string)
	for _, article := range []map[string]any{
		{"title": "Card refund", "tags": []any{"refund"}},
		{"title": "Invoice copy", "tags": []any{"billing"}},
		{"title": "=SUM(A1)", "tags": []any{"Billing"}},
	} {
		article["collection_id"], article["body"] = guides, "Text."
		e.ok(session, "colliedesk_kb_article_create", article)
	}
	other := e.newWorkspace("KB Export Elsewhere", "admin@kb-export-elsewhere.example.com")
	elsewhere := e.connect(other.adminTier, "")
	shelf := at(t, e.ok(elsewhere, "colliedesk_kb_collection_create", map[string]any{"name": "Shelf"}), "collection", "id").(string)
	e.ok(elsewhere, "colliedesk_kb_article_create", map[string]any{"collection_id": shelf, "title": "Theirs", "body": "x", "tags": []any{"billing"}})

	tags := e.ok(session, "colliedesk_kb_tags_list", map[string]any{})
	e.requireMatchesOutputSchema(session, "colliedesk_kb_tags_list", tags)
	if got := fmt.Sprint(tags["tags"]); got != "[map[article_count:2 tag:billing] map[article_count:1 tag:refund]]" {
		t.Fatalf("tags = %s, want billing 2 and refund 1", got)
	}

	shown := e.ok(session, "colliedesk_help_center_update", map[string]any{"enabled": true, "title": "", "locale": "en", "show_tags": true})
	e.requireMatchesOutputSchema(session, "colliedesk_help_center_update", shown)
	if got := at(t, shown, "help_center", "show_tags"); got != true {
		t.Fatalf("help center show_tags = %v, want true", got)
	}
	read := e.ok(session, "colliedesk_help_center_get", map[string]any{})
	e.requireMatchesOutputSchema(session, "colliedesk_help_center_get", read)
	if got := at(t, read, "help_center", "show_tags"); got != true {
		t.Fatalf("help center get show_tags = %v, want true", got)
	}

	exported := e.ok(session, "colliedesk_kb_articles_export", map[string]any{"tags": []any{"billing"}})
	e.requireMatchesOutputSchema(session, "colliedesk_kb_articles_export", exported)
	lines := strings.Split(strings.TrimSuffix(exported["csv"].(string), "\n"), "\n")
	if lines[0] != "title,address,collection,status,author,tags,languages,created_at,updated_at,helpful_votes,not_helpful_votes" {
		t.Fatalf("header = %q", lines[0])
	}
	titles := []string{}
	for _, line := range lines[1:] {
		titles = append(titles, strings.SplitN(line, ",", 2)[0])
	}
	slices.Sort(titles)
	if got := fmt.Sprintf("%v %v %v %v", titles, exported["rows"], exported["truncated"], exported["download_path"]); got != "['=SUM(A1) Invoice copy] 2 false /api/v1/kb/articles/export.csv?tag=billing" {
		t.Fatalf("export = %s, want ['=SUM(A1) Invoice copy] 2 false /api/v1/kb/articles/export.csv?tag=billing", got)
	}

	first := e.ok(session, "colliedesk_kb_articles_export", map[string]any{"tags": []any{"billing"}, "limit": 1})
	if got := fmt.Sprint(strings.Count(first["csv"].(string), "\n"), first["rows"], first["truncated"]); got != "2 1 true" {
		t.Fatalf("limit 1: lines, rows, truncated = %s, want 2 1 true", got)
	}
	if refused := e.fails(session, "colliedesk_kb_articles_export", map[string]any{"created_after": "yesterday"}); !strings.Contains(refused, "created_after") {
		t.Fatalf("a bad date = %q, want it to name created_after", refused)
	}

	// A file that broke off after it started is an error, never a short csv with truncated false.
	if _, err := e.pool.Exec(context.Background(), `DROP TABLE kb_article_vote_resets CASCADE`); err != nil {
		t.Fatal(err)
	}
	if broken := e.fails(session, "colliedesk_kb_articles_export", map[string]any{}); !strings.Contains(broken, "before its end") {
		t.Fatalf("a broken file = %q, want it to say the file stopped before its end", broken)
	}
}
