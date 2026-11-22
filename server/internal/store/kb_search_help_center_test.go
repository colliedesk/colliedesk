package store_test

import (
	"context"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// A search keeps the help center that served it: a search on a second help center records that one,
// and a search that names none records the workspace's primary.
func TestASearchRecordsTheHelpCenterThatServedIt(t *testing.T) {
	pool := databasetest.Pool(t)
	s := store.New(pool)
	ctx := context.Background()
	workspace := testWorkspace(t, pool, "kb-search-help-center")
	var primary, hardware string
	if err := pool.QueryRow(ctx, `INSERT INTO help_centers (workspace_id) VALUES ($1) RETURNING id::text`,
		workspace).Scan(&primary); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO help_centers (workspace_id, handle, primary_help_center, enabled, title, locale)
		VALUES ($1, 'hardware', false, true, 'Hardware answers', 'en') RETURNING id::text`, workspace).Scan(&hardware); err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct{ served, recorded string }{{hardware, hardware}, {"", primary}} {
		id, err := s.LogKBSearch(ctx, workspace, want.served, "cable colours", 0)
		if err != nil || id == 0 {
			t.Fatalf("log search on %q = %d, %v", want.served, id, err)
		}
		var recorded string
		if err := pool.QueryRow(ctx, `SELECT help_center_id::text FROM kb_searches WHERE id=$1`, id).Scan(&recorded); err != nil {
			t.Fatal(err)
		}
		if recorded != want.recorded {
			t.Fatalf("search on %q recorded help center %s, want %s", want.served, recorded, want.recorded)
		}
	}
}

// A help center of another workspace cannot be recorded on a search: the row is refused.
func TestASearchCannotRecordAnotherWorkspacesHelpCenter(t *testing.T) {
	pool := databasetest.Pool(t)
	s := store.New(pool)
	ctx := context.Background()
	mine, theirs := testWorkspace(t, pool, "kb-search-hc-mine"), testWorkspace(t, pool, "kb-search-hc-theirs")
	var other string
	if err := pool.QueryRow(ctx, `INSERT INTO help_centers (workspace_id) VALUES ($1) RETURNING id::text`,
		theirs).Scan(&other); err != nil {
		t.Fatal(err)
	}
	if id, err := s.LogKBSearch(ctx, mine, other, "cable colours", 0); err == nil || !strings.Contains(err.Error(), "kb_searches_help_center_id_fkey") {
		t.Fatalf("search on another workspace's help center = %d, %v; want the help center refused", id, err)
	}
	var stored int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM kb_searches WHERE workspace_id=$1`, mine).Scan(&stored); err != nil || stored != 0 {
		t.Fatalf("stored %d, %v; want 0", stored, err)
	}
}
