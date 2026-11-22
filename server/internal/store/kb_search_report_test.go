package store_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

func terms(list []domain.KBSearchTerm) []string {
	out := make([]string, 0, len(list))
	for _, term := range list {
		out = append(out, term.Query)
	}
	return out
}

// A search that found nothing is in the report as soon as it is made, and the report never shows the
// email address or the long number a reader typed.
func TestANoResultSearchIsInTheReportAtOnceWithoutTheEmailOrTheNumber(t *testing.T) {
	pool := databasetest.Pool(t)
	s := store.New(pool)
	ctx := context.Background()
	workspace := testWorkspace(t, pool, "kb-search-report-now")
	if _, err := s.LogKBSearch(ctx, workspace, "", "Invoice for anna@example.com 4400123456", 0); err != nil {
		t.Fatal(err)
	}
	report, err := s.KBSearchReport(ctx, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if report.Searches != 1 || len(report.NoResults) != 1 || report.NoResults[0].Query != "Invoice for [email removed] [number removed]" {
		t.Fatalf("report = %+v, want the no-result search listed at once", report)
	}
	body, _ := json.Marshal(report)
	if strings.Contains(string(body), "anna@example.com") || strings.Contains(string(body), "4400123456") {
		t.Fatalf("report carries the raw words: %s", body)
	}
}

// The report groups searches by their folded words over the last seven days of one workspace: the most
// searched words first, the words that found nothing, and the searches whose results nobody opened.
func TestTheSearchReportListsTopNoResultAndNothingOpenedSearchesOfTheWeek(t *testing.T) {
	pool := databasetest.Pool(t)
	s := store.New(pool)
	ctx := context.Background()
	workspace, other := testWorkspace(t, pool, "kb-search-report"), testWorkspace(t, pool, "kb-search-report-other")
	article := searchArticle(t, s, workspace, "export")
	log := func(workspace, query string, results int) int64 {
		id, err := s.LogKBSearch(ctx, workspace, "", query, results)
		if err != nil || id == 0 {
			t.Fatalf("log %q = %d, %v", query, id, err)
		}
		return id
	}
	old := log(workspace, "last month", 0)
	if _, err := pool.Exec(ctx, `UPDATE kb_searches SET created_at=now() - interval '8 days' WHERE id=$1`, old); err != nil {
		t.Fatal(err)
	}
	if err := s.OpenKBSearch(ctx, workspace, log(workspace, "how to export", 3), article); err != nil {
		t.Fatal(err)
	}
	log(workspace, "invoice pdf", 0)
	log(workspace, "How to Éxport", 3)
	log(workspace, "change plan", 2)
	log(other, "someone else", 0)

	report, err := s.KBSearchReport(ctx, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if report.Searches != 4 {
		t.Fatalf("searches = %d, want the 4 of this week in this workspace", report.Searches)
	}
	if got := strings.Join(terms(report.Top), "|"); got != "How to Éxport|change plan|invoice pdf" {
		t.Fatalf("top = %s", got)
	}
	if top := report.Top[0]; top.Searches != 2 || top.Opened != 1 || top.Results != 3 {
		t.Fatalf("top row = %+v, want 2 searches, 1 opened, 3 results", top)
	}
	if got := strings.Join(terms(report.NoResults), "|"); got != "invoice pdf" {
		t.Fatalf("no results = %s", got)
	}
	if got := strings.Join(terms(report.NothingOpened), "|"); got != "change plan|How to Éxport" {
		t.Fatalf("nothing opened = %s", got)
	}
	if report.To.Sub(report.From).Hours() != 24*domain.KBSearchReportDays {
		t.Fatalf("window %s to %s, want seven days", report.From, report.To)
	}
}
