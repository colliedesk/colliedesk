package mcpserver_test

import (
	"context"
	"testing"
)

// An AI agent reads what readers searched for on the help center: the words that found nothing are
// the articles to write. The words are reader text, under untrusted_customer_content, and another
// workspace's searches never show.
func TestAnAgentReadsTheHelpCenterSearchesThatFoundNothing(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	if _, err := e.pool.Exec(context.Background(), `INSERT INTO kb_searches (workspace_id, query, query_key, results, created_at) VALUES
		($1, 'Export invoices', 'export invoices', 0, now() - interval '2 hours'),
		($1, 'export invoices', 'export invoices', 0, now() - interval '1 hour'),
		($1, 'reset password', 'reset password', 3, now() - interval '3 hours'),
		($1, 'old search', 'old search', 0, now() - interval '8 days'),
		($2, 'other workspace words', 'other workspace words', 0, now())`, f.w.id, f.other.id); err != nil {
		t.Fatal(err)
	}

	read := e.connect(f.w.read, "")
	report := e.ok(read, "colliedesk_kb_search_report", nil)
	e.requireMatchesOutputSchema(read, "colliedesk_kb_search_report", report)
	noResults := report["no_results"].([]any)
	nothingOpened := report["nothing_opened"].([]any)
	if report["searches"] != float64(3) || len(report["top"].([]any)) != 2 || len(noResults) != 1 || len(nothingOpened) != 1 {
		t.Fatalf("report = %v, want 3 searches: 2 top words, 1 without results, 1 with nothing opened", report)
	}
	if at(t, noResults[0], "untrusted_customer_content", "query") != "export invoices" || at(t, noResults[0], "searches") != float64(2) || at(t, noResults[0], "results") != float64(0) {
		t.Fatalf("no results = %v, want export invoices searched twice", noResults[0])
	}
	if at(t, nothingOpened[0], "untrusted_customer_content", "query") != "reset password" || at(t, nothingOpened[0], "results") != float64(3) {
		t.Fatalf("nothing opened = %v, want reset password with 3 results", nothingOpened[0])
	}

	other := e.ok(e.connect(f.other.read, ""), "colliedesk_kb_search_report", nil)
	if other["searches"] != float64(1) || at(t, other["no_results"].([]any)[0], "untrusted_customer_content", "query") != "other workspace words" {
		t.Fatalf("other workspace's report = %v, want only its own search", other)
	}
}
