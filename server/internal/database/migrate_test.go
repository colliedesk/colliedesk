package database

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMigrateCreatesTenantScopedTables(t *testing.T) {
	pool := testPool(t)
	if err := Migrate(context.Background(), pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	rows, err := pool.Query(context.Background(), `
		SELECT table_name FROM information_schema.columns
		WHERE table_schema = current_schema() AND column_name = 'workspace_id'
		ORDER BY table_name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		got = append(got, name)
	}
	want := []string{"agents", "attachments", "contacts", "conversation_tags", "conversations", "import_items", "messages", "pairing_links", "sessions", "tags", "time_entries"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("workspace-scoped tables = %v, want %v", got, want)
	}
}

func TestMigrateCreatesNoPasswordColumns(t *testing.T) {
	pool := testPool(t)
	if err := Migrate(context.Background(), pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	var count int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM information_schema.columns
		WHERE table_schema = current_schema() AND column_name ILIKE '%password%'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("password columns = %d, want 0", count)
	}
}

func TestMigrateRejectsCrossWorkspaceRelationships(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	var workspaceA, workspaceB, contactB string
	if err := pool.QueryRow(ctx, `INSERT INTO workspaces(slug,name) VALUES('alpha','Alpha') RETURNING id::text`).Scan(&workspaceA); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO workspaces(slug,name) VALUES('beta','Beta') RETURNING id::text`).Scan(&workspaceB); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Example','person@example.com') RETURNING id::text`, workspaceB).Scan(&contactB); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO conversations(workspace_id,contact_id,subject) VALUES($1,$2,'Cross tenant')`, workspaceA, contactB); err == nil {
		t.Fatal("cross-workspace contact relationship was accepted")
	}
}

func TestMigrationAppliesUpDownAndUpAgain(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("first up: %v", err)
	}
	if !tableExists(t, pool, "workspaces") {
		t.Fatal("workspaces table missing after first up")
	}
	var applied int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	for range applied {
		if err := rollbackLatest(ctx, pool); err != nil {
			t.Fatalf("down: %v", err)
		}
	}
	if tableExists(t, pool, "workspaces") {
		t.Fatal("workspaces table remains after down")
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("second up: %v", err)
	}
	if !tableExists(t, pool, "workspaces") {
		t.Fatal("workspaces table missing after second up")
	}
}

func tableExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(context.Background(), `SELECT to_regclass(current_schema() || '.' || $1) IS NOT NULL`, name).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	return exists
}

func rollbackLatest(ctx context.Context, pool *pgxpool.Pool) error {
	var upName string
	if err := pool.QueryRow(ctx, `SELECT name FROM schema_migrations ORDER BY applied_at DESC,name DESC LIMIT 1`).Scan(&upName); err != nil {
		return err
	}
	downName := strings.TrimSuffix(upName, ".up.sql") + ".down.sql"
	contents, err := migrations.Files.ReadFile(downName)
	if err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, string(contents)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM schema_migrations WHERE name=$1`, upName); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is unset; skipping real PostgreSQL test")
	}
	schema := "test_" + regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(t.Name(), "_") + fmt.Sprint(time.Now().UnixNano())
	admin, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(context.Background(), "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
	})
	return pool
}
