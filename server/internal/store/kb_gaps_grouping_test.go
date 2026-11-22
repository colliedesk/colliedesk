package store_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// gapRow is one gap of a workspace as stored, with how many signals it holds.
type gapRow struct {
	title, status   string
	askers, signals int
	fixedBy         string
	automatic       bool
}

func groupGaps(t *testing.T, s *store.Store) {
	t.Helper()
	if _, err := s.BuildKBGaps(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// storedGaps lists the gaps of a workspace, the oldest first.
func storedGaps(t *testing.T, pool *pgxpool.Pool, workspace string) []gapRow {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT g.title, g.status, g.askers,
			(SELECT count(*) FROM kb_gap_signals s WHERE s.workspace_id=g.workspace_id AND s.gap_id=g.id),
			COALESCE(g.fixed_by_article_id::text, ''), g.fixed_automatically
		FROM kb_gaps g WHERE g.workspace_id=$1 ORDER BY g.created_at, g.id`, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var gaps []gapRow
	for rows.Next() {
		var g gapRow
		if err := rows.Scan(&g.title, &g.status, &g.askers, &g.signals, &g.fixedBy, &g.automatic); err != nil {
			t.Fatal(err)
		}
		gaps = append(gaps, g)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return gaps
}

func searchFindingNothing(t *testing.T, s *store.Store, workspace string, queries ...string) {
	t.Helper()
	for _, query := range queries {
		if id, err := s.LogKBSearch(context.Background(), workspace, "", query, 0); err != nil || id == 0 {
			t.Fatalf("log search %q = %d, %v", query, id, err)
		}
	}
}

func wantGaps(t *testing.T, got []gapRow, want ...gapRow) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("gaps = %+v, want %+v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("gaps = %+v, want %+v", got, want)
		}
	}
}

// Two searches whose words fold the same are one gap; each search is its own asker, and the gap is
// titled by its most common wording.
func TestSearchesWithTheSameFoldedWordsJoinOneGap(t *testing.T) {
	pool := databasetest.Pool(t)
	s := store.New(pool)
	workspace := testWorkspace(t, pool, "kb-gaps-same")
	searchFindingNothing(t, s, workspace, "Refund Policy", "refund policy", "refund  policy")
	groupGaps(t, s)

	wantGaps(t, storedGaps(t, pool, workspace), gapRow{title: "refund policy", status: "open", askers: 3, signals: 3})
}

// A wording that shares most of a gap's words joins it, in either order and with a plural; one that
// shares only "how to" opens a gap of its own. The threshold is KBGapMatchThreshold.
func TestARelatedWordingAboveTheMatchThresholdJoinsAGapAndAnUnrelatedOneOpensANewGap(t *testing.T) {
	pool := databasetest.Pool(t)
	s := store.New(pool)
	workspace := testWorkspace(t, pool, "kb-gaps-related")
	searchFindingNothing(t, s, workspace, "export invoices")
	groupGaps(t, s)
	searchFindingNothing(t, s, workspace, "invoice export", "how to export invoices", "how to change my password")
	groupGaps(t, s)

	wantGaps(t, storedGaps(t, pool, workspace),
		gapRow{title: "how to export invoices", status: "open", askers: 3, signals: 3},
		gapRow{title: "how to change my password", status: "open", askers: 1, signals: 1})
	if store.KBGapMatchThreshold != 0.6 {
		t.Fatalf("KBGapMatchThreshold = %v, want 0.6", store.KBGapMatchThreshold)
	}
}

// Readers on another workspace never join this workspace's gaps.
func TestAGapHoldsOnlyItsOwnWorkspacesSignals(t *testing.T) {
	pool := databasetest.Pool(t)
	s := store.New(pool)
	workspace, other := testWorkspace(t, pool, "kb-gaps-own"), testWorkspace(t, pool, "kb-gaps-other")
	searchFindingNothing(t, s, workspace, "refund policy")
	searchFindingNothing(t, s, other, "refund policy")
	groupGaps(t, s)

	wantGaps(t, storedGaps(t, pool, workspace), gapRow{title: "refund policy", status: "open", askers: 1, signals: 1})
	wantGaps(t, storedGaps(t, pool, other), gapRow{title: "refund policy", status: "open", askers: 1, signals: 1})
}
