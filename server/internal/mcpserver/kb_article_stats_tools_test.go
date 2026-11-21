package mcpserver_test

import (
	"context"
	"testing"
)

// An AI agent reads how much an article was read and used: the article carries its last 30 days, the
// stats tool gives them per day with the replies that used it, and results_get the help center block.
func TestAnAgentReadsAnArticlesViewsAndTheRepliesThatUsedIt(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	ctx := context.Background()
	send := e.connect(f.w.send, "")
	collection := at(t, e.ok(send, "colliedesk_kb_collection_create", map[string]any{"name": "Printing"}), "collection", "id").(string)
	article := at(t, e.ok(send, "colliedesk_kb_article_create", map[string]any{"collection_id": collection, "title": "Clear a paper jam", "body": "Open tray two.", "status": "published"}), "article", "id").(string)
	// Today, in the workspace's own timezone, readers opened it 4 times and one reply linked it.
	if _, err := e.pool.Exec(ctx, `INSERT INTO kb_article_views(workspace_id,kb_article_id,locale,day,views)
		SELECT a.workspace_id,a.id,'en',(now() AT TIME ZONE w.timezone)::date,4 FROM kb_articles a JOIN workspaces w ON w.id=a.workspace_id WHERE a.id=$1`, article); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, `WITH m AS (INSERT INTO messages(workspace_id,conversation_id,kind,body)
		SELECT workspace_id,id,'outgoing','See the guide' FROM conversations WHERE id=$1 RETURNING workspace_id,conversation_id,id)
		INSERT INTO kb_article_uses(workspace_id,kb_article_id,conversation_id,message_id,kind) SELECT workspace_id,$2,conversation_id,id,'link' FROM m`, f.c1, article); err != nil {
		t.Fatal(err)
	}

	read := e.connect(f.w.read, "")
	got := e.ok(read, "colliedesk_kb_article_get", map[string]any{"article_id": article})
	if at(t, got, "article", "views_30d") != float64(4) || at(t, got, "article", "uses_30d") != float64(1) {
		t.Fatalf("article = %v, want views_30d 4 and uses_30d 1", got["article"])
	}

	stats := e.ok(read, "colliedesk_kb_article_stats", map[string]any{"article_id": article})
	e.requireMatchesOutputSchema(read, "colliedesk_kb_article_stats", stats)
	days := stats["days"].([]any)
	replies := stats["replies"].([]any)
	if stats["views"] != float64(4) || stats["uses"] != float64(1) || len(days) != 30 || at(t, days[29], "views") != float64(4) || len(replies) != 1 ||
		at(t, replies[0], "conversation_id") != f.c1 || at(t, replies[0], "untrusted_customer_content", "subject") != "Printer jam" {
		t.Fatalf("stats = %v, want 4 views and the reply on %s (Printer jam) over 30 days", stats, f.c1)
	}
	if message := e.fails(read, "colliedesk_kb_article_stats", map[string]any{"article_id": article, "from": "2026-11-02", "to": "2026-11-01"}); message == "" {
		t.Error("a range that ends before it starts was accepted")
	}
	e.fails(e.connect(f.other.read, ""), "colliedesk_kb_article_stats", map[string]any{"article_id": article})

	results := e.ok(read, "colliedesk_results_get", nil)
	e.requireMatchesOutputSchema(read, "colliedesk_results_get", results)
	if at(t, results, "results", "help_center", "windows", "30d", "views") != float64(4) {
		t.Fatalf("results help_center = %v, want 4 views in 30 days", at(t, results, "results", "help_center"))
	}
}
