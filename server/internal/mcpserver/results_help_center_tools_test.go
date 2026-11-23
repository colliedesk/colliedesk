package mcpserver_test

import (
	"context"
	"strings"
	"testing"
)

// An AI agent reads the help center block of results for one help center by its id; another
// workspace's help center is not found and the answer says where the ids are.
func TestResultsToolReadsOneHelpCenter(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Results Help Centers", "admin@results-help-centers.example.com")
	other := e.newWorkspace("Other Results Help Centers", "admin@other-results-help-centers.example.com")
	admin := e.connect(w.adminTier, "")
	hardware, _ := at(t, e.ok(admin, "colliedesk_help_center_create", map[string]any{"name": "Hardware", "handle": "hardware", "locale": "en"}), "help_center", "id").(string)
	elsewhere, _ := at(t, e.ok(e.connect(other.adminTier, ""), "colliedesk_help_center_create", map[string]any{"name": "Elsewhere", "handle": "elsewhere", "locale": "en"}), "help_center", "id").(string)

	guides := at(t, e.ok(admin, "colliedesk_kb_collection_create", map[string]any{"name": "Guides"}), "collection", "id").(string)
	cables := at(t, e.ok(admin, "colliedesk_kb_collection_create", map[string]any{"name": "Cables", "help_center_ids": []string{hardware}}), "collection", "id").(string)
	jam := at(t, e.ok(admin, "colliedesk_kb_article_create", map[string]any{"collection_id": guides, "title": "Clear a paper jam", "body": "Open tray two.", "status": "published"}), "article", "id").(string)
	swap := at(t, e.ok(admin, "colliedesk_kb_article_create", map[string]any{"collection_id": cables, "title": "Swap a cable", "body": "Unplug it.", "status": "published"}), "article", "id").(string)
	// Today, in the workspace's own timezone: 4 views of the primary's article, 3 of Hardware's.
	if _, err := e.pool.Exec(context.Background(), `INSERT INTO kb_article_views(workspace_id,kb_article_id,locale,day,views)
		SELECT a.workspace_id,a.id,'en',(now() AT TIME ZONE ws.timezone)::date,CASE WHEN a.id=$1 THEN 4 ELSE 3 END
		FROM kb_articles a JOIN workspaces ws ON ws.id=a.workspace_id WHERE a.id IN ($1,$2)`, jam, swap); err != nil {
		t.Fatal(err)
	}

	whole := e.ok(admin, "colliedesk_results_get", nil)
	one := e.ok(admin, "colliedesk_results_get", map[string]any{"help_center_id": hardware})
	e.requireMatchesOutputSchema(admin, "colliedesk_results_get", one)
	if at(t, whole, "results", "help_center", "windows", "30d", "views") != float64(7) ||
		at(t, one, "results", "help_center", "windows", "30d", "views") != float64(3) ||
		at(t, one, "results", "help_center", "windows", "30d", "articles", 0, "title") != "Swap a cable" {
		t.Fatalf("whole = %v, Hardware = %v; want 7 views, then 3 views of Swap a cable", at(t, whole, "results", "help_center"), at(t, one, "results", "help_center"))
	}
	if message := e.fails(admin, "colliedesk_results_get", map[string]any{"help_center_id": elsewhere}); !strings.Contains(message, "colliedesk_help_centers_list") {
		t.Errorf("another workspace's help center: %q, want it not found and where the ids are", message)
	}
}
