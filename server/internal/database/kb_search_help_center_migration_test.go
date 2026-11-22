package database

import (
	"context"
	"testing"
)

const kbSearchHelpCenterMigration = "20261122075809-kb-search-help-center.up.sql"

// Searches kept before they named a help center belong to their workspace's primary: alpha's to the
// primary it has, beta's to one made for it, since beta had no help center row yet. Rolling back keeps
// every search.
func TestKBSearchHelpCenterMigrationPutsEarlierSearchesOnThePrimary(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := rollbackThrough(ctx, pool, kbSearchHelpCenterMigration); err != nil {
		t.Fatalf("roll back: %v", err)
	}
	ids := seedTwoWorkspaces(t, pool)
	alphaPrimary := query(t, pool, `INSERT INTO help_centers (workspace_id) VALUES ($1) RETURNING id::text`, ids.a)
	if _, err := pool.Exec(ctx, `INSERT INTO kb_searches (workspace_id, query, query_key, results) VALUES
		($1, 'Reset password', 'reset password', 2), ($1, 'invoice', 'invoice', 0), ($2, 'login', 'login', 1)`,
		ids.a, ids.b); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	betaPrimary := query(t, pool, `SELECT COALESCE(primary_help_center_id($1)::text, '')`, ids.b)
	if betaPrimary == "" {
		t.Fatal("beta has no primary help center after the migration")
	}
	searches := `SELECT string_agg(query||'@'||COALESCE(help_center_id::text, ''), ',' ORDER BY id) FROM kb_searches WHERE workspace_id=$1`
	if got, want := query(t, pool, searches, ids.a), "Reset password@"+alphaPrimary+",invoice@"+alphaPrimary; got != want {
		t.Fatalf("alpha's searches = %q, want %q", got, want)
	}
	if got, want := query(t, pool, searches, ids.b), "login@"+betaPrimary; got != want {
		t.Fatalf("beta's searches = %q, want %q", got, want)
	}

	if err := rollbackThrough(ctx, pool, kbSearchHelpCenterMigration); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if got := query(t, pool, `SELECT string_agg(query, ',' ORDER BY id) FROM kb_searches`); got != "Reset password,invoice,login" {
		t.Fatalf("searches after rollback = %q, want all three kept", got)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate again: %v", err)
	}
}
