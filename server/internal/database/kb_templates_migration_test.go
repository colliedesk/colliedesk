package database

import (
	"context"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"
)

// The upgrade gives every workspace that already exists the three starter templates, also when the
// schema owner is not a superuser: row-level security then hides the workspaces from a plain query.
func TestStarterTemplatesReachExistingWorkspacesWhenANonSuperuserOwnerUpgrades(t *testing.T) {
	ctx := context.Background()
	pool, _ := ownedSchemaPool(t, testPool(t), true)
	migrateBefore(t, pool, "20261120141816-kb-article-templates.up.sql")
	db := New(pool)
	var workspace string
	if err := db.Privileged(ctx, PrivilegeCreateWorkspace, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `INSERT INTO workspaces(slug,name) VALUES('before-templates','Before templates') RETURNING id::text`).Scan(&workspace)
	}); err != nil {
		t.Fatalf("seed a workspace from before templates: %v", err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade as non-superuser owner: %v", err)
	}
	var names []string
	if err := db.Privileged(ctx, PrivilegeMigrate, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT name FROM kb_templates WHERE workspace_id=$1 ORDER BY name`, workspace)
		if err != nil {
			return err
		}
		names, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"FAQ", "How-to", "Troubleshooting"}; !slices.Equal(names, want) {
		t.Fatalf("starter templates of an existing workspace = %v, want %v", names, want)
	}
}
