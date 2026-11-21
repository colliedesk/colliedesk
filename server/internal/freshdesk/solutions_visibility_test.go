package freshdesk_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
)

// kbCollectionVisibilities lists each collection as "name=visibility", sorted by name.
func (e *importEnv) kbCollectionVisibilities(t *testing.T) string {
	t.Helper()
	var got string
	if err := e.pool.QueryRow(context.Background(), `SELECT string_agg(name||'='||visibility, ' | ' ORDER BY name) FROM kb_collections WHERE workspace_id=$1`,
		e.workspaceID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	return got
}

// kbShownTo lists, sorted, the Freshdesk ids of the articles a view of readers shows.
func (e *importEnv) kbShownTo(t *testing.T, view string) string {
	t.Helper()
	var got string
	if err := e.pool.QueryRow(context.Background(), `SELECT COALESCE(string_agg(a.source_meta->>'freshdesk_id', ',' ORDER BY a.source_meta->>'freshdesk_id'),'')
		FROM `+view+` v JOIN kb_articles a ON a.workspace_id=v.workspace_id AND a.id=v.id WHERE v.workspace_id=$1`, e.workspaceID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	return got
}

func setFolderVisibility(fixture map[string]any, category string, index int, visibility float64) {
	fixture["category_folders"].(map[string]any)[category].([]any)[index].(map[string]any)["visibility"] = visibility
}

// A Freshdesk folder's visibility becomes its collection's: all users everyone, agents agents, bots help
// chat, and any limit to some customers agents, reported per folder. Published articles stay published
// and are shown only where their folders were; a folder flattened beside its parents keeps the most
// restrictive of them.
func TestSolutionsFolderVisibilityBecomesCollectionVisibility(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	env.editSolutions(func(fixture map[string]any) {
		setFolderVisibility(fixture, "1", 2, 5) // Advanced (111), parent of the flattened Deep (1111): bots
		setFolderVisibility(fixture, "2", 0, 4) // Invoices (21): selected companies
		fixture["subfolders"].(map[string]any)["11"].([]any)[0].(map[string]any)["visibility"] = 5.0
	})
	report := env.mustRun(t)
	if report.Status != freshdesk.StatusOK {
		t.Fatalf("status=%s failures=%+v verification=%+v", report.Status, report.Failures, report.Verification.KnowledgeBase)
	}
	if got := env.kbCollectionVisibilities(t); got != "Advanced=help_chat | Advanced / Deep=help_chat | Basics=everyone | Billing=everyone | "+
		"Getting started=everyone | Internal=agents | Invoices=agents" {
		t.Fatalf("collections = %s", got)
	}
	if got := warningIDs(report, "kb_folder_limited_to_some_customers"); got != "21" {
		t.Fatalf("limited folders = %q, warnings=%+v", got, report.Warnings)
	}
	if markdown := report.Markdown(); !strings.Contains(markdown, "limited to some customers in Freshdesk; kept for agents only until that limit exists here") {
		t.Fatalf("report lacks the limited folder:\n%s", markdown)
	}
	articles := env.kbArticles(t, env.workspaceID)
	for _, id := range []string{"101", "103", "104", "105", "106"} {
		if articles[id].Status != "published" {
			t.Fatalf("article %s = %s, want published", id, articles[id].Status)
		}
	}
	if got := fmt.Sprint(report.KnowledgeBase.Drafts); got != "map[draft_in_freshdesk:[102]]" {
		t.Fatalf("drafts = %s", got)
	}
	if got := env.kbShownTo(t, "kb_public_articles"); got != "101" {
		t.Fatalf("help center articles = %s, want only 101", got)
	}
	if got := env.kbShownTo(t, "kb_help_chat_articles"); got != "101,104,105" {
		t.Fatalf("help chat articles = %s", got)
	}
}

// A folder restricted in Freshdesk after the import restricts its collection on the next run, with a
// warning; a folder opened up in Freshdesk never opens its collection by itself.
func TestSolutionsAFolderNoLongerPublicRestrictsItsCollection(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	env.mustRun(t)
	env.editSolutions(func(fixture map[string]any) { setFolderVisibility(fixture, "1", 0, 3) }) // Basics (11): agents
	report := env.mustRun(t)
	if report.Status != freshdesk.StatusOK || warningIDs(report, "kb_collection_restricted") != "11" {
		t.Fatalf("status=%s warnings=%+v", report.Status, report.Warnings)
	}
	if got := env.kbShownTo(t, "kb_public_articles"); got != "106" {
		t.Fatalf("help center articles = %s, want only 106", got)
	}
	if after := env.kbArticles(t, env.workspaceID); after["101"].Status != "published" || len(report.KnowledgeBase.Unpublished) != 0 {
		t.Fatalf("101=%s unpublished=%v", after["101"].Status, report.KnowledgeBase.Unpublished)
	}
	env.editSolutions(nil)
	env.mustRun(t)
	if got := env.kbShownTo(t, "kb_public_articles"); got != "106" {
		t.Fatalf("an import opened Basics again: help center articles = %s", got)
	}
}
