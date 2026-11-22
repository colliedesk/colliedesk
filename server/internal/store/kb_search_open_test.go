package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

func searchArticle(t *testing.T, s *store.Store, workspace, slug string) string {
	t.Helper()
	ctx := context.Background()
	collection, err := s.CreateKBCollection(ctx, workspace, store.Actor{}, store.KBCollectionInput{Name: "Billing " + slug, Slug: "billing-" + slug})
	if err != nil {
		t.Fatal(err)
	}
	article, err := s.CreateKBArticle(ctx, workspace, store.Actor{}, store.KBArticleInput{
		CollectionID: collection.ID, Title: "Article " + slug, Slug: slug, BodyMD: "Open Billing.", Status: "published",
	})
	if err != nil {
		t.Fatal(err)
	}
	return article.ID
}

// The first article a reader opens from a search is kept; a later open, or an article or search of
// another workspace, changes nothing.
func TestOnlyTheFirstArticleOpenedFromASearchOfTheSameWorkspaceIsKept(t *testing.T) {
	pool := databasetest.Pool(t)
	s := store.New(pool)
	ctx := context.Background()
	workspace, other := testWorkspace(t, pool, "kb-search-open"), testWorkspace(t, pool, "kb-search-open-other")
	first, second := searchArticle(t, s, workspace, "first"), searchArticle(t, s, workspace, "second")
	foreign := searchArticle(t, s, other, "foreign")
	id, err := s.LogKBSearch(ctx, workspace, "", "change plan", 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.OpenKBSearch(ctx, other, id, foreign); err != nil {
		t.Fatal(err)
	}
	if err := s.OpenKBSearch(ctx, workspace, id, foreign); err != nil {
		t.Fatal(err)
	}
	if _, _, _, opened := searchRow(t, pool, id); opened != "" {
		t.Fatalf("opened = %q after opens across workspaces, want nothing", opened)
	}
	for _, article := range []string{first, second} {
		if err := s.OpenKBSearch(ctx, workspace, id, article); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, _, opened := searchRow(t, pool, id); opened != first {
		t.Fatalf("opened = %q, want the first article %q", opened, first)
	}
}

// Purging the opened article keeps the search: it still counts, with nothing opened.
func TestPurgingTheOpenedArticleKeepsTheSearch(t *testing.T) {
	pool := databasetest.Pool(t)
	s := store.New(pool)
	ctx := context.Background()
	workspace := testWorkspace(t, pool, "kb-search-purge")
	article := searchArticle(t, s, workspace, "purged")
	id, err := s.LogKBSearch(ctx, workspace, "", "old article", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.OpenKBSearch(ctx, workspace, id, article); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM kb_articles WHERE id=$1`, article); err != nil {
		t.Fatal(err)
	}
	if query, _, results, opened := searchRow(t, pool, id); query != "old article" || results != 1 || opened != "" {
		t.Fatalf("search after purge = %q %d %q, want it kept with nothing opened", query, results, opened)
	}
}

// The retention delete removes searches older than the retention and keeps the newer ones.
func TestSearchesOlderThanTheRetentionAreDeleted(t *testing.T) {
	pool := databasetest.Pool(t)
	s := store.New(pool)
	ctx := context.Background()
	workspace := testWorkspace(t, pool, "kb-search-retention")
	old, err := s.LogKBSearch(ctx, workspace, "", "old words", 0)
	if err != nil {
		t.Fatal(err)
	}
	kept, err := s.LogKBSearch(ctx, workspace, "", "new words", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE kb_searches SET created_at=now() - interval '181 days' WHERE id=$1`, old); err != nil {
		t.Fatal(err)
	}
	if deleted, err := s.DeleteOldKBSearches(ctx, 180*24*time.Hour); err != nil || deleted < 1 {
		t.Fatalf("deleted %d, %v; want the old search deleted", deleted, err)
	}
	var left []int64
	rows, err := pool.Query(ctx, `SELECT id FROM kb_searches WHERE workspace_id=$1`, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		left = append(left, id)
	}
	if len(left) != 1 || left[0] != kept {
		t.Fatalf("left %v, want only %d", left, kept)
	}
}
