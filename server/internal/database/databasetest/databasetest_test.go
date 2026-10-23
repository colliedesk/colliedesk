package databasetest_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// A clone holds every migration, keeps row-level security on (as the app role a transaction sees only
// its own workspace) and shares no rows with another clone.
func TestCloneIsMigratedIsolatedAndEnforcesRowLevelSecurity(t *testing.T) {
	ctx := context.Background()
	first, second := databasetest.Pool(t), databasetest.Pool(t)
	var pending int
	if err := first.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&pending); err != nil || pending == 0 {
		t.Fatalf("schema_migrations rows = %d, err %v; want every migration applied", pending, err)
	}
	if err := database.Migrate(ctx, first); err != nil {
		t.Fatalf("Migrate on a clone: %v", err)
	}

	alpha, beta := seedWorkspace(t, first, "alpha"), seedWorkspace(t, first, "beta")
	db := database.New(first)
	var slugs []string
	if err := db.InWorkspace(ctx, alpha, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT slug FROM workspaces ORDER BY slug`)
		if err != nil {
			return err
		}
		slugs, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(slugs, ",") != "alpha" {
		t.Fatalf("alpha's transaction sees workspaces %v, want only alpha (beta is %s)", slugs, beta)
	}
	var rolsuper, bypass bool
	if err := db.InWorkspace(ctx, alpha, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT rolsuper, rolbypassrls FROM pg_roles WHERE rolname=current_user`).Scan(&rolsuper, &bypass)
	}); err != nil || rolsuper || bypass {
		t.Fatalf("workspace transaction role superuser=%v bypassrls=%v err=%v", rolsuper, bypass, err)
	}

	var count int
	if err := second.QueryRow(ctx, `SELECT count(*) FROM workspaces`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("second clone has %d workspaces, err %v; want none", count, err)
	}
}

// Packages run in parallel processes; tests in one process may also clone at once.
func TestConcurrentClonesSucceed(t *testing.T) {
	ctx := context.Background()
	base := databasetest.BaseURL(t)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			db, err := databasetest.Create(ctx, base)
			if err != nil {
				errs <- err
				return
			}
			errs <- db.Drop(ctx)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

// EmptySchema is for tests that need a database that was never migrated.
func TestEmptySchemaHoldsNoMigrations(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databasetest.EmptySchema(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var ledger bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass(current_schema() || '.schema_migrations') IS NOT NULL`).Scan(&ledger); err != nil || ledger {
		t.Fatalf("empty schema has a migration ledger=%v, err %v", ledger, err)
	}
}

func seedWorkspace(t *testing.T, pool *pgxpool.Pool, slug string) string {
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
