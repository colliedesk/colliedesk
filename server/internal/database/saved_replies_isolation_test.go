package database

import (
	"context"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"
)

func titlesIn(t *testing.T, db *DB, workspaceID string) []string {
	t.Helper()
	var got []string
	err := db.InWorkspace(context.Background(), workspaceID, func(tx pgx.Tx) error {
		// Deliberately unfiltered: row-level security alone must scope it.
		rows, err := tx.Query(context.Background(), `SELECT title FROM saved_replies ORDER BY title`)
		if err != nil {
			return err
		}
		got, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if err != nil {
		t.Fatalf("unfiltered saved replies query in %s: %v", workspaceID, err)
	}
	return got
}

func TestSavedRepliesAreScopedByRowLevelSecurity(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	ids := seedTwoWorkspaces(t, pool)
	for workspace, title := range map[string]string{ids.a: "Alpha greeting", ids.b: "Beta greeting"} {
		if _, err := pool.Exec(ctx, `INSERT INTO saved_replies(workspace_id,title,body) VALUES($1,$2,'Hello')`, workspace, title); err != nil {
			t.Fatal(err)
		}
	}
	db := New(pool)
	if got := titlesIn(t, db, ids.a); !slices.Equal(got, []string{"Alpha greeting"}) {
		t.Fatalf("alpha sees %v", got)
	}
	if got := titlesIn(t, db, ids.b); !slices.Equal(got, []string{"Beta greeting"}) {
		t.Fatalf("beta sees %v", got)
	}
	err := db.InWorkspace(ctx, ids.a, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO saved_replies(workspace_id,title,body) VALUES($1,'Smuggled','x')`, ids.b)
		return err
	})
	if err == nil {
		t.Fatal("workspace alpha inserted a saved reply into workspace beta")
	}
	single := singleConnection(t, pool)
	role := pgx.Identifier{policyRole(t, pool)}.Sanitize()
	tx, err := single.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var count int
	if _, err := tx.Exec(ctx, `SET LOCAL ROLE `+role); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM saved_replies`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("saved replies without workspace context = %d (%v), want 0", count, err)
	}
}

func TestSavedReplyTitleIsUniquePerWorkspaceIgnoringCase(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	ids := seedTwoWorkspaces(t, pool)
	insert := func(workspace, title string) error {
		_, err := pool.Exec(ctx, `INSERT INTO saved_replies(workspace_id,title,body) VALUES($1,$2,'Hello')`, workspace, title)
		return err
	}
	if err := insert(ids.a, "Refund"); err != nil {
		t.Fatal(err)
	}
	if err := insert(ids.a, "REFUND"); err == nil {
		t.Fatal("same title in another case was accepted in one workspace")
	}
	if err := insert(ids.b, "refund"); err != nil {
		t.Fatalf("same title in another workspace: %v", err)
	}
}
