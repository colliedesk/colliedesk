package store_test

import (
	"context"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/colliedesk/colliedesk/server/internal/database"
)

// testWorkspace makes a workspace named slug and returns its id.
func testWorkspace(t *testing.T, pool *pgxpool.Pool, slug string) string {
	t.Helper()
	var id string
	err := database.New(pool).Privileged(context.Background(), database.PrivilegeCreateWorkspace, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `INSERT INTO workspaces(slug,name) VALUES($1,$1) RETURNING id::text`, slug).Scan(&id)
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// testChannel adds a channel of kind to workspaceID and returns its id.
func testChannel(t *testing.T, pool *pgxpool.Pool, workspaceID, kind, name string) string {
	t.Helper()
	var id string
	err := database.New(pool).InWorkspace(context.Background(), workspaceID, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `INSERT INTO channels(workspace_id,type,name) VALUES($1,$2,$3) RETURNING id::text`, workspaceID, kind, name).Scan(&id)
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// atOnce runs do n times, each in its own goroutine, all released together, and waits for them.
func atOnce(n int, do func(i int)) {
	var start, done sync.WaitGroup
	start.Add(1)
	for i := range n {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			do(i)
		}()
	}
	start.Done()
	done.Wait()
}
