package mcpserver_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// An AI agent works the knowledge gaps: it lists what readers asked for and did not find, most askers
// first, reads one gap's signals, links it to the article that answers it and dismisses another. What
// readers wrote sits under untrusted_customer_content, who asked never shows, and another workspace's
// gaps stay out of reach.
func TestAnAgentListsTheKnowledgeGapsAndLinksOneToAnArticle(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	ctx := context.Background()
	send := e.connect(f.w.send, "")
	collection := at(t, e.ok(send, "colliedesk_kb_collection_create", map[string]any{"name": "Billing"}), "collection", "id").(string)
	article := at(t, e.ok(send, "colliedesk_kb_article_create", map[string]any{"collection_id": collection, "title": "Download your invoices", "body": "Open Billing.", "status": "published"}), "article", "id").(string)

	var invoices, shipping, otherGap string
	seed := `WITH g AS (INSERT INTO kb_gaps(workspace_id, title, query_key, askers, first_seen, last_seen)
		VALUES ($1, $2, lower($2), $3, now() - interval '3 hours', now() - $4::interval) RETURNING workspace_id, id)
		SELECT id::text FROM g`
	for _, gap := range []struct {
		into                  *string
		workspace, title, ago string
		askers                int
	}{{&invoices, f.w.id, "Where are my invoices", "1 hour", 2}, {&shipping, f.w.id, "shipping to canada", "2 hours", 1}, {&otherGap, f.other.id, "other workspace words", "1 hour", 1}} {
		if err := e.pool.QueryRow(ctx, seed, gap.workspace, gap.title, gap.askers, gap.ago).Scan(gap.into); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.pool.Exec(ctx, `INSERT INTO kb_gap_signals(workspace_id, gap_id, kind, source_id, text, asker_key, created_at) VALUES
		($1, $2, 'search_no_result', '1', 'where are my invoices', sha256('reader one'::bytea), now() - interval '3 hours'),
		($1, $2, 'vote_no', 'v1', 'Where are my invoices', sha256('reader two'::bytea), now() - interval '1 hour'),
		($1, $3, 'search_no_result', '2', 'shipping to canada', sha256('reader three'::bytea), now() - interval '2 hours'),
		($4, $5, 'search_no_result', '3', 'other workspace words', sha256('reader four'::bytea), now() - interval '1 hour')`,
		f.w.id, invoices, shipping, f.other.id, otherGap); err != nil {
		t.Fatal(err)
	}

	read := e.connect(f.w.read, "")
	open := e.ok(read, "colliedesk_kb_gaps_list", nil)
	e.requireMatchesOutputSchema(read, "colliedesk_kb_gaps_list", open)
	items := open["items"].([]any)
	if len(items) != 2 || at(t, items[0], "id") != invoices || at(t, items[1], "id") != shipping {
		t.Fatalf("open gaps = %v, want the invoices gap (2 askers) before the shipping gap (1)", open)
	}
	if at(t, items[0], "untrusted_customer_content", "title") != "Where are my invoices" || at(t, items[0], "recent_askers") != float64(2) ||
		at(t, items[0], "askers") != float64(2) || at(t, items[0], "status") != "open" {
		t.Fatalf("invoices gap = %v, want its title under untrusted_customer_content and 2 askers", items[0])
	}
	if kinds := at(t, items[0], "kinds").([]any); len(kinds) != 2 || kinds[0] != "search_no_result" || kinds[1] != "vote_no" {
		t.Fatalf("invoices kinds = %v, want search_no_result and vote_no", kinds)
	}
	votes := e.ok(read, "colliedesk_kb_gaps_list", map[string]any{"kind": "vote_no"})["items"].([]any)
	if len(votes) != 1 || at(t, votes[0], "id") != invoices {
		t.Fatalf("vote_no gaps = %v, want only the invoices gap", votes)
	}

	detail := e.ok(read, "colliedesk_kb_gap_get", map[string]any{"gap_id": invoices})
	e.requireMatchesOutputSchema(read, "colliedesk_kb_gap_get", detail)
	signals := detail["signals"].([]any)
	if detail["signal_count"] != float64(2) || len(signals) != 2 || at(t, signals[0], "kind") != "vote_no" ||
		at(t, signals[0], "untrusted_customer_content", "text") != "Where are my invoices" || at(t, signals[1], "kind") != "search_no_result" {
		t.Fatalf("invoices detail = %v, want 2 signals, the vote first", detail)
	}
	if raw, _ := json.Marshal(detail); strings.Contains(string(raw), "asker_key") {
		t.Fatalf("detail %s tells the askers apart", raw)
	}

	e.refused(read, "colliedesk_kb_gap_update", map[string]any{"gap_id": invoices, "status": "dismissed"})
	fixed := e.ok(send, "colliedesk_kb_gap_update", map[string]any{"gap_id": invoices, "fixed_by_article_id": article})
	e.requireMatchesOutputSchema(send, "colliedesk_kb_gap_update", fixed)
	if fixed["status"] != "fixed" || at(t, fixed, "fixed_by_article", "id") != article || at(t, fixed, "fixed_by_article", "title") != "Download your invoices" ||
		fixed["fixed_automatically"] != false || at(t, fixed, "status_changed_by", "name") != "Ada Admin" {
		t.Fatalf("fixed gap = %v, want fixed by Download your invoices, changed by Ada Admin", fixed)
	}
	dismissed := e.ok(send, "colliedesk_kb_gap_update", map[string]any{"gap_id": shipping, "status": "dismissed"})
	if dismissed["status"] != "dismissed" || dismissed["fixed_by_article"] != nil {
		t.Fatalf("dismissed gap = %v, want dismissed with no article", dismissed)
	}
	if left := e.ok(read, "colliedesk_kb_gaps_list", nil)["items"].([]any); len(left) != 0 {
		t.Fatalf("open gaps after the changes = %v, want none", left)
	}
	if done := e.ok(read, "colliedesk_kb_gaps_list", map[string]any{"status": "fixed"})["items"].([]any); len(done) != 1 || at(t, done[0], "id") != invoices {
		t.Fatalf("fixed gaps = %v, want the invoices gap", done)
	}

	if message := e.fails(read, "colliedesk_kb_gap_get", map[string]any{"gap_id": otherGap}); !strings.Contains(message, "colliedesk_kb_gaps_list") {
		t.Fatalf("another workspace's gap: %q, want a not-found that names colliedesk_kb_gaps_list", message)
	}
	e.fails(send, "colliedesk_kb_gap_update", map[string]any{"gap_id": otherGap, "status": "dismissed"})
	other := e.ok(e.connect(f.other.read, ""), "colliedesk_kb_gaps_list", nil)["items"].([]any)
	if len(other) != 1 || at(t, other[0], "id") != otherGap {
		t.Fatalf("other workspace's gaps = %v, want only its own", other)
	}
}
