package database

import (
	"context"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"
)

// TestRowSecurityMigrationGivesExistingCollectionsTheirTopCollection upgrades a database that already
// has nested collections: each one gets the top-level collection of its tree, and a reader of the
// help center that tree is on sees all of it.
func TestRowSecurityMigrationGivesExistingCollectionsTheirTopCollection(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := rollbackThrough(ctx, pool, "20261122081247-help-center-row-security.up.sql"); err != nil {
		t.Fatal(err)
	}
	ids := seedTwoWorkspaces(t, pool)
	db := New(pool)
	var top, middle, leaf, primary string
	if err := db.InWorkspace(ctx, ids.a, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO help_centers(workspace_id,title) VALUES($1,'Alpha') RETURNING id::text`, ids.a).
			Scan(&primary); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `WITH t AS (INSERT INTO kb_collections(workspace_id,name,slug) VALUES($1,'Top','top') RETURNING id),
			m AS (INSERT INTO kb_collections(workspace_id,parent_id,name,slug) SELECT $1,id,'Middle','middle' FROM t RETURNING id),
			l AS (INSERT INTO kb_collections(workspace_id,parent_id,name,slug) SELECT $1,id,'Leaf','leaf' FROM m RETURNING id)
			SELECT t.id::text,m.id::text,l.id::text FROM t,m,l`, ids.a).Scan(&top, &middle, &leaf)
	}); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	collections := []string{top, middle, leaf}
	rows, err := pool.Query(ctx, `SELECT top_collection_id::text FROM kb_collections WHERE id = ANY($1::uuid[]) ORDER BY name`,
		collections)
	if err != nil {
		t.Fatal(err)
	}
	tops, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil || !slices.Equal(tops, []string{top, top, top}) {
		t.Fatalf("top collections of Leaf, Middle, Top = %v (%v), want %s for each", tops, err, top)
	}
	var seen int
	if err := db.InHelpCenter(ctx, ids.a, primary, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM kb_collections WHERE id = ANY($1::uuid[])`, collections).Scan(&seen)
	}); err != nil || seen != 3 {
		t.Fatalf("reader of the primary help center sees %d of the 3 upgraded collections (%v)", seen, err)
	}
}
