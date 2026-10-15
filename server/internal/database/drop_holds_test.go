package database

import (
	"context"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

const dropHoldsMigration = "20261015082744-drop-conversation-holds.up.sql"

func columnExists(t *testing.T, pool *pgxpool.Pool, table, column string) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name=$1 AND column_name=$2)`, table, column).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	return exists
}

// Dropping the hold column keeps every conversation, held ones included, and the down migration
// brings the column and its index back.
func TestDropConversationHoldsKeepsEveryConversation(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	entries, err := migrations.Files.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".up.sql") && entry.Name() < dropHoldsMigration {
			if err := applyMigration(ctx, pool, entry.Name()); err != nil {
				t.Fatal(err)
			}
		}
	}
	var workspace, contact string
	if err := pool.QueryRow(ctx, `INSERT INTO workspaces(slug,name,trial_ends_at) VALUES('holds','Holds','2026-12-01T00:00:00Z') RETURNING id::text`).Scan(&workspace); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Customer','c@customer.example') RETURNING id::text`, workspace).Scan(&contact); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO conversations(workspace_id,contact_id,subject,status,held_at) VALUES($1,$2,'Held ticket','open','2026-10-02T00:00:00Z'),($1,$2,'Plain ticket','open',NULL)`, workspace, contact); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	var kept int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM conversations WHERE workspace_id=$1`, workspace).Scan(&kept); err != nil || kept != 2 {
		t.Fatalf("conversations after the drop: %d %v, want 2", kept, err)
	}
	if columnExists(t, pool, "conversations", "held_at") {
		t.Fatal("held_at is still there after the drop")
	}
	if err := rollbackLatest(ctx, pool); err != nil {
		t.Fatalf("down: %v", err)
	}
	if !columnExists(t, pool, "conversations", "held_at") {
		t.Fatal("down did not restore held_at")
	}
	var indexes int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_indexes WHERE schemaname=current_schema() AND indexname='conversations_held_idx'`).Scan(&indexes); err != nil || indexes != 1 {
		t.Fatalf("down restored %d held indexes (%v), want 1", indexes, err)
	}
}
