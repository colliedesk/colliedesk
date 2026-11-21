package main

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCommandsStopOnAPostgresBelowTheFloorBeforeTouchingIt(t *testing.T) {
	databaseURL := databasetest.EmptySchema(t)
	floor := postgresFloor
	postgresFloor = 99
	t.Cleanup(func() { postgresFloor = floor })
	for _, command := range [][]string{{"serve"}, {"migrate"}, {"create-workspace", "--name", "Old", "--admin-email", "admin@old.example.com"}} {
		err := run(context.Background(), command, io.Discard, databaseURL, "https://desk.example.com")
		if err == nil || !strings.Contains(err.Error(), "PostgreSQL 99 or newer is required") || !strings.Contains(err.Error(), "sh upgrade-postgres.sh") {
			t.Fatalf("collie %s on an old PostgreSQL: %v", command[0], err)
		}
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var ledger bool
	if err := pool.QueryRow(context.Background(), `SELECT to_regclass('schema_migrations') IS NOT NULL`).Scan(&ledger); err != nil {
		t.Fatal(err)
	}
	if ledger {
		t.Fatal("a refused command still ran migrations")
	}
}
