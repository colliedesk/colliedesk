package store_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

func searchRow(t *testing.T, pool *pgxpool.Pool, id int64) (query, key string, results int, opened string) {
	t.Helper()
	err := pool.QueryRow(context.Background(), `SELECT query, query_key, results, COALESCE(opened_article_id::text, '')
		FROM kb_searches WHERE id=$1`, id).Scan(&query, &key, &results, &opened)
	if err != nil {
		t.Fatal(err)
	}
	return query, key, results, opened
}

// A reader's search is kept masked: an email address and a run of six or more digits never reach the
// table, neither in the words nor in the key they are grouped by.
func TestASearchIsStoredWithoutTheEmailAddressOrTheLongNumberInIt(t *testing.T) {
	pool := databasetest.Pool(t)
	s := store.New(pool)
	workspace := testWorkspace(t, pool, "kb-search-mask")
	id, err := s.LogKBSearch(context.Background(), workspace, "Refund for Anna.Lee@example.com order 12345678", 0)
	if err != nil || id == 0 {
		t.Fatalf("log search = %d, %v", id, err)
	}
	query, key, results, opened := searchRow(t, pool, id)
	if query != "Refund for [email removed] order [number removed]" || key != "refund for [email removed] order [number removed]" {
		t.Fatalf("stored query=%q key=%q, want both masked", query, key)
	}
	if results != 0 || opened != "" {
		t.Fatalf("results=%d opened=%q, want 0 and nothing", results, opened)
	}
}

// One workspace keeps at most KBSearchesPerMinute searches a minute; the next ones are not stored, and
// another workspace still logs its own.
func TestAWorkspaceLogsAtMostItsSearchesPerMinute(t *testing.T) {
	pool := databasetest.Pool(t)
	s := store.New(pool)
	ctx := context.Background()
	busy, quiet := testWorkspace(t, pool, "kb-search-busy"), testWorkspace(t, pool, "kb-search-quiet")
	for index := range 120 {
		if id, err := s.LogKBSearch(ctx, busy, fmt.Sprintf("word %d", index), 1); err != nil || id == 0 {
			t.Fatalf("search %d = %d, %v; want it stored", index, id, err)
		}
	}
	if id, err := s.LogKBSearch(ctx, busy, "one more", 1); err != nil || id != 0 {
		t.Fatalf("search past the cap = %d, %v; want 0 and no error", id, err)
	}
	if id, err := s.LogKBSearch(ctx, quiet, "one more", 1); err != nil || id == 0 {
		t.Fatalf("another workspace's search = %d, %v; want it stored", id, err)
	}
	var stored int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM kb_searches WHERE workspace_id=$1`, busy).Scan(&stored); err != nil || stored != 120 {
		t.Fatalf("stored %d, %v; want 120", stored, err)
	}
}
