package database

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// upgradeFixtureDir holds the data tests/upgrade-test.sh loads into the previous release before it
// upgrades: one SQL file per migration, run right after that migration.
const upgradeFixtureDir = "../../../tests/upgrade/fixture"

// The upgrade test only proves what its fixture holds, so the fixture must grow with the schema: a
// new table or column fails here until a fixture file gives it a value. The file runs after the
// migration it is named after, so it is written once, against that schema, and never changes.
func TestUpgradeFixtureLoadsAfterEachMigrationAndFillsEveryColumn(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	entries, err := os.ReadDir(upgradeFixtureDir)
	if err != nil {
		t.Fatal(err)
	}
	migrationNames := map[string]bool{}
	for _, name := range upMigrations(t) {
		migrationNames[strings.TrimSuffix(name, ".up.sql")] = true
	}
	fixtures := map[string]string{}
	for _, entry := range entries {
		stem, ok := strings.CutSuffix(entry.Name(), ".sql")
		if !ok || !migrationNames[stem] {
			t.Fatalf("%s/%s is not named after a migration (<migration>.sql)", upgradeFixtureDir, entry.Name())
		}
		fixtures[stem] = filepath.Join(upgradeFixtureDir, entry.Name())
	}

	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	for _, name := range upMigrations(t) {
		if err := applyMigration(ctx, pool, name); err != nil {
			t.Fatal(err)
		}
		path, ok := fixtures[strings.TrimSuffix(name, ".up.sql")]
		if !ok {
			continue
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, string(contents))
			return err
		}); err != nil {
			t.Fatalf("%s after its migration: %v", path, err)
		}
	}

	rows, err := pool.Query(ctx, `SELECT c.relname, a.attname FROM pg_class c
		JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
		WHERE c.relnamespace = current_schema()::regnamespace AND c.relkind IN ('r', 'p') AND c.relname <> 'schema_migrations'
		ORDER BY 1, 2`)
	if err != nil {
		t.Fatal(err)
	}
	columns, err := pgx.CollectRows(rows, pgx.RowToStructByPos[struct{ Table, Column string }])
	if err != nil {
		t.Fatal(err)
	}
	var empty []string
	for _, column := range columns {
		var filled bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM `+pgx.Identifier{column.Table}.Sanitize()+
			` WHERE `+pgx.Identifier{column.Column}.Sanitize()+` IS NOT NULL)`).Scan(&filled); err != nil {
			t.Fatal(err)
		}
		if !filled {
			empty = append(empty, column.Table+"."+column.Column)
		}
	}
	if len(empty) > 0 {
		t.Fatalf("no fixture row has a value in %s: add one in %s/<the migration that made it>.sql (docs/internals/upgrades.md)",
			strings.Join(empty, ", "), upgradeFixtureDir)
	}
}
