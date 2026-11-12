package database

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/colliedesk/colliedesk/server/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Migrate applies every unapplied up migration in lexical order.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		name text PRIMARY KEY,
		applied_at timestamptz NOT NULL DEFAULT now()
	)`); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}
	names, err := upMigrationNames()
	if err != nil {
		return err
	}
	for _, name := range names {
		if err := applyMigration(ctx, pool, name); err != nil {
			return err
		}
	}
	return nil
}

// PendingMigrations names the up migrations this binary carries that the database has not applied, in
// the order Migrate would apply them. A database that never ran Migrate has them all pending.
func (d *DB) PendingMigrations(ctx context.Context) ([]string, error) {
	names, err := upMigrationNames()
	if err != nil {
		return nil, err
	}
	var ledger bool
	if err := d.pool.QueryRow(ctx, `SELECT to_regclass('schema_migrations') IS NOT NULL`).Scan(&ledger); err != nil {
		return nil, fmt.Errorf("find the migration ledger: %w", err)
	}
	if !ledger {
		return names, nil
	}
	var pending []string
	err = d.pool.QueryRow(ctx, `SELECT coalesce(array_agg(name ORDER BY name), '{}') FROM unnest($1::text[]) AS name
		WHERE name NOT IN (SELECT name FROM schema_migrations)`, names).Scan(&pending)
	if err != nil {
		return nil, fmt.Errorf("read the migration ledger: %w", err)
	}
	return pending, nil
}

func upMigrationNames() ([]string, error) {
	entries, err := fs.ReadDir(migrations.Files, ".")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	var names []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".up.sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

func applyMigration(ctx context.Context, pool *pgxpool.Pool, name string) error {
	var applied bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE name=$1)`, name).Scan(&applied); err != nil {
		return fmt.Errorf("check migration %s: %w", name, err)
	}
	if applied {
		return nil
	}
	contents, err := migrations.Files.ReadFile(name)
	if err != nil {
		return fmt.Errorf("read migration %s: %w", name, err)
	}
	// Privileged so data changes in a migration see every workspace when the owner is not a superuser.
	return New(pool).Privileged(ctx, PrivilegeMigrate, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, string(contents)); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations(name) VALUES($1)`, name); err != nil {
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		return nil
	})
}
