package database

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// rlsMigration turns on row-level security; upgrade tests stop just before it.
const rlsMigration = "20261014135950-workspace-row-level-security.up.sql"

type twoWorkspaces struct{ a, b string }

// seedTwoWorkspaces writes a contact, a conversation, a message and a tag in workspaces alpha and
// beta through the migrating (owner) connection, the way existing installs already hold data.
func seedTwoWorkspaces(t *testing.T, pool *pgxpool.Pool) twoWorkspaces {
	t.Helper()
	var ids twoWorkspaces
	for slug, target := range map[string]*string{"alpha": &ids.a, "beta": &ids.b} {
		err := pool.QueryRow(context.Background(), `WITH w AS (INSERT INTO workspaces(slug,name) VALUES($1,$1) RETURNING id),
			c AS (INSERT INTO contacts(workspace_id,name,email) SELECT id,$1,$1||'@example.com' FROM w RETURNING id,workspace_id),
			v AS (INSERT INTO conversations(workspace_id,contact_id,subject) SELECT workspace_id,id,$1||' ticket' FROM c RETURNING id,workspace_id),
			m AS (INSERT INTO messages(workspace_id,conversation_id,kind,body) SELECT workspace_id,id,'note',$1||' note' FROM v RETURNING 1),
			g AS (INSERT INTO tags(workspace_id,name) SELECT id,$1||'-tag' FROM w RETURNING 1)
			SELECT id::text FROM w`, slug).Scan(target)
		if err != nil {
			t.Fatalf("seed %s: %v", slug, err)
		}
	}
	return ids
}

func subjects(t *testing.T, db *DB, workspaceID string) []string {
	t.Helper()
	var got []string
	err := db.InWorkspace(context.Background(), workspaceID, func(tx pgx.Tx) error {
		// Deliberately unfiltered: row-level security alone must scope it.
		rows, err := tx.Query(context.Background(), `SELECT subject FROM conversations ORDER BY subject`)
		if err != nil {
			return err
		}
		got, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if err != nil {
		t.Fatalf("unfiltered query in workspace %s: %v", workspaceID, err)
	}
	return got
}

func TestUnfilteredQueryInWorkspaceSeesOnlyThatWorkspace(t *testing.T) {
	pool := testPool(t)
	if err := Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	ids := seedTwoWorkspaces(t, pool)
	db := New(pool)
	if got := subjects(t, db, ids.a); !slices.Equal(got, []string{"alpha ticket"}) {
		t.Fatalf("workspace alpha sees %v", got)
	}
	if got := subjects(t, db, ids.b); !slices.Equal(got, []string{"beta ticket"}) {
		t.Fatalf("workspace beta sees %v", got)
	}
	var workspaces []string
	err := db.InWorkspace(context.Background(), ids.a, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), `SELECT slug FROM workspaces ORDER BY slug`)
		if err != nil {
			return err
		}
		workspaces, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if err != nil || !slices.Equal(workspaces, []string{"alpha"}) {
		t.Fatalf("workspace alpha lists workspaces %v (%v)", workspaces, err)
	}
}

func TestQueryWithoutWorkspaceContextSeesNoRows(t *testing.T) {
	pool := testPool(t)
	if err := Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	ids := seedTwoWorkspaces(t, pool)
	ctx := context.Background()
	// One connection, so the bare transaction below reuses the connection the scoped one used.
	single := singleConnection(t, pool)
	if got := subjects(t, New(single), ids.a); len(got) != 1 {
		t.Fatalf("scoped query = %v", got)
	}
	role := pgx.Identifier{policyRole(t, pool)}.Sanitize()
	for _, table := range []string{"workspaces", "contacts", "conversations", "messages", "tags"} {
		tx, err := single.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var count int
		_, err = tx.Exec(ctx, `SET LOCAL ROLE `+role)
		if err == nil {
			err = tx.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&count)
		}
		_ = tx.Rollback(ctx)
		if err != nil {
			t.Fatalf("%s without workspace context: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s without workspace context returned %d rows", table, count)
		}
	}
}

func TestWorkspaceCannotWriteRowsOfAnotherWorkspace(t *testing.T) {
	pool := testPool(t)
	if err := Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	ids := seedTwoWorkspaces(t, pool)
	db := New(pool)
	ctx := context.Background()
	err := db.InWorkspace(ctx, ids.a, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO tags(workspace_id,name) VALUES($1,'smuggled')`, ids.b)
		return err
	})
	if err == nil {
		t.Fatal("workspace alpha inserted a tag into workspace beta")
	}
	var changed int64
	err = db.InWorkspace(ctx, ids.a, func(tx pgx.Tx) error {
		result, err := tx.Exec(ctx, `UPDATE conversations SET subject='overwritten'`)
		changed = result.RowsAffected()
		return err
	})
	if err != nil || changed != 1 {
		t.Fatalf("unfiltered update in alpha changed %d rows (%v), want 1", changed, err)
	}
	if got := subjects(t, db, ids.b); !slices.Equal(got, []string{"beta ticket"}) {
		t.Fatalf("beta after alpha's unfiltered update: %v", got)
	}
}

func TestInWorkspaceRejectsMissingOrMalformedWorkspace(t *testing.T) {
	pool := testPool(t)
	if err := Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	called := false
	for _, id := range []string{"", "alpha", "00000000-0000-0000-0000-00000000000g"} {
		err := New(pool).InWorkspace(context.Background(), id, func(pgx.Tx) error { called = true; return nil })
		if !errors.Is(err, ErrNoWorkspace) {
			t.Fatalf("workspace %q: err=%v, want ErrNoWorkspace", id, err)
		}
	}
	if called {
		t.Fatal("callback ran without a workspace")
	}
}

func TestPrivilegedPathSeesEveryWorkspace(t *testing.T) {
	pool := testPool(t)
	if err := Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	seedTwoWorkspaces(t, pool)
	var count int
	err := New(pool).Privileged(context.Background(), PrivilegeWorkspaceBySlug, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM conversations`).Scan(&count)
	})
	if err != nil || count != 2 {
		t.Fatalf("privileged count = %d (%v), want 2", count, err)
	}
}

// securityGaps lists every table of the current schema that needs row-level security and lacks
// ENABLE, FORCE or the workspace_isolation policy for the app role.
func securityGaps(t *testing.T, pool *pgxpool.Pool) (checked, gaps []string) {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT c.relname, c.relrowsecurity, c.relforcerowsecurity,
			EXISTS (SELECT 1 FROM pg_policies p WHERE p.schemaname = n.nspname AND p.tablename = c.relname
				AND p.policyname = 'workspace_isolation' AND p.cmd = 'ALL' AND p.roles = ARRAY[$1]::name[]
				AND p.qual LIKE '%app.workspace_id%' AND p.with_check LIKE '%app.workspace_id%')
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema() AND c.relkind IN ('r', 'p')
			AND (c.relname = 'workspaces'
				OR EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.oid AND a.attname = 'workspace_id' AND NOT a.attisdropped)
				OR has_table_privilege($1, c.oid, 'SELECT, INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER'))
		ORDER BY c.relname`, policyRole(t, pool))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var enabled, forced, policy bool
		if err := rows.Scan(&name, &enabled, &forced, &policy); err != nil {
			t.Fatal(err)
		}
		checked = append(checked, name)
		if !enabled || !forced || !policy {
			gaps = append(gaps, fmt.Sprintf("%s (enabled=%v forced=%v workspace_isolation policy=%v)", name, enabled, forced, policy))
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return checked, gaps
}

// TestEveryWorkspaceTableEnforcesRowLevelSecurity is the gate for future tables: a table with a
// workspace_id column, or any table the app role can reach, must enable and force row-level
// security and carry the workspace policy.
func TestEveryWorkspaceTableEnforcesRowLevelSecurity(t *testing.T) {
	pool := testPool(t)
	if err := Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	checked, gaps := securityGaps(t, pool)
	for _, gap := range gaps {
		t.Errorf("table %s; add ENABLE, FORCE and the policy in its migration, or revoke the app role "+
			"(docs/internals/workspace-isolation.md)", gap)
	}
	if len(checked) < 14 {
		t.Fatalf("checked only %v", checked)
	}
}

func TestSecurityGuardFlagsAnyTableTheAppRoleCanReach(t *testing.T) {
	pool := testPool(t)
	if err := Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	// Keyed only by a conversation, with no workspace_id: default privileges still grant it to the app role.
	if _, err := pool.Exec(context.Background(), `CREATE TABLE guard_probe (conversation_id uuid)`); err != nil {
		t.Fatal(err)
	}
	_, gaps := securityGaps(t, pool)
	if len(gaps) != 1 || !strings.HasPrefix(gaps[0], "guard_probe ") {
		t.Fatalf("gaps = %v, want only guard_probe", gaps)
	}
}

func TestAppRoleCannotBypassRowLevelSecurity(t *testing.T) {
	pool := testPool(t)
	if err := Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	if err := New(pool).Verify(context.Background()); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	var super, bypass, login bool
	role := policyRole(t, pool)
	if !strings.HasPrefix(role, "colliedesk_app_") {
		t.Fatalf("app role %q is not per database", role)
	}
	if err := pool.QueryRow(context.Background(), `SELECT rolsuper,rolbypassrls,rolcanlogin FROM pg_roles WHERE rolname=$1`, role).
		Scan(&super, &bypass, &login); err != nil {
		t.Fatal(err)
	}
	if super || bypass || login {
		t.Fatalf("%s superuser=%v bypassrls=%v login=%v", role, super, bypass, login)
	}
	var ledger bool
	if err := pool.QueryRow(context.Background(), `SELECT has_table_privilege($1, 'schema_migrations', 'SELECT')`, role).Scan(&ledger); err != nil || ledger {
		t.Fatalf("%s can read the migration ledger: %v %v", role, ledger, err)
	}
}

func TestRowLevelSecurityMigrationKeepsAndIsolatesExistingData(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	for _, name := range upMigrations(t) {
		if name < rlsMigration {
			if err := applyMigration(ctx, pool, name); err != nil {
				t.Fatal(err)
			}
		}
	}
	ids := seedTwoWorkspaces(t, pool)
	before := rowCounts(t, pool)

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if after := rowCounts(t, pool); after != before {
		t.Fatalf("row counts changed by the upgrade: %s -> %s", before, after)
	}
	db := New(pool)
	if got := subjects(t, db, ids.a); !slices.Equal(got, []string{"alpha ticket"}) {
		t.Fatalf("alpha after upgrade sees %v", got)
	}
	if got := subjects(t, db, ids.b); !slices.Equal(got, []string{"beta ticket"}) {
		t.Fatalf("beta after upgrade sees %v", got)
	}
	if err := rollbackThrough(ctx, pool, rlsMigration); err != nil {
		t.Fatalf("down: %v", err)
	}
	if after := rowCounts(t, pool); after != before {
		t.Fatalf("rolling back changed data: %s -> %s", before, after)
	}
	var secured int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
		WHERE n.nspname=current_schema() AND (c.relrowsecurity OR c.relforcerowsecurity)`).Scan(&secured); err != nil || secured != 0 {
		t.Fatalf("tables still secured after down: %d %v", secured, err)
	}
}

// ownedSchemaPool connects as a fresh non-superuser role that owns its own schema, the shape of a
// managed PostgreSQL. With grantApp the role may switch to the app role, as an operator would set it
// up. The role and schema are dropped when the test ends.
func ownedSchemaPool(t *testing.T, admin *pgxpool.Pool, grantApp bool) (*pgxpool.Pool, string) {
	t.Helper()
	ctx := context.Background()
	if err := Migrate(ctx, admin); err != nil { // creates this database's app role if missing
		t.Fatal(err)
	}
	owner := fmt.Sprintf("colliedesk_test_owner_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, `CREATE ROLE `+owner+` NOLOGIN NOSUPERUSER NOBYPASSRLS`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), `DROP ROLE `+owner); err != nil {
			t.Errorf("drop role %s: %v", owner, err)
		}
	})
	if grantApp {
		if _, err := admin.Exec(ctx, `GRANT `+pgx.Identifier{policyRole(t, admin)}.Sanitize()+` TO `+owner); err != nil {
			t.Fatal(err)
		}
	}
	ownedSchema := strings.Replace(owner, "_owner_", "_schema_", 1) // short: identifiers stop at 63 bytes
	if _, err := admin.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %s AUTHORIZATION %s`, ownedSchema, owner)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), `DROP SCHEMA `+ownedSchema+` CASCADE`); err != nil {
			t.Errorf("drop schema %s: %v", ownedSchema, err)
		}
	})
	config := admin.Config().Copy()
	config.ConnConfig.RuntimeParams["search_path"] = ownedSchema
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, `SET ROLE `+owner)
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool, owner
}

// TestNonSuperuserOwnerIsAlsoFailClosed runs migrations and the privileged path as a table owner that
// is not a superuser: forced row-level security applies to it too.
func TestNonSuperuserOwnerIsAlsoFailClosed(t *testing.T) {
	ctx := context.Background()
	pool, _ := ownedSchemaPool(t, testPool(t), true)
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate as non-superuser owner: %v", err)
	}
	db := New(pool)
	var a string
	err := db.Privileged(ctx, PrivilegeCreateWorkspace, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `WITH w AS (INSERT INTO workspaces(slug,name) VALUES('alpha','Alpha') RETURNING id),
			c AS (INSERT INTO contacts(workspace_id,name,email) SELECT id,'A','a@example.com' FROM w RETURNING id,workspace_id),
			v AS (INSERT INTO conversations(workspace_id,contact_id,subject) SELECT workspace_id,id,'alpha ticket' FROM c RETURNING 1)
			SELECT id::text FROM w`).Scan(&a)
	})
	if err != nil {
		t.Fatalf("privileged insert as owner: %v", err)
	}
	var bare int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM conversations`).Scan(&bare); err != nil || bare != 0 {
		t.Fatalf("owner query outside both paths saw %d rows (%v), want 0", bare, err)
	}
	if got := subjects(t, db, a); !slices.Equal(got, []string{"alpha ticket"}) {
		t.Fatalf("scoped query as owner-connected app role: %v", got)
	}
}

func TestMigrateExplainsHowToGrantTheAppRole(t *testing.T) {
	admin := testPool(t)
	pool, owner := ownedSchemaPool(t, admin, false)
	err := Migrate(context.Background(), pool)
	want := "GRANT " + policyRole(t, admin) + " TO " + owner
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("migrate without the app role: %v; want the instruction %q", err, want)
	}
}

func singleConnection(t *testing.T, pool *pgxpool.Pool) *pgxpool.Pool {
	t.Helper()
	config := pool.Config().Copy()
	config.MaxConns = 1
	single, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(single.Close)
	return single
}

// rollbackThrough rolls back the latest migrations until name has been rolled back.
func rollbackThrough(ctx context.Context, pool *pgxpool.Pool, name string) error {
	for {
		var latest string
		if err := pool.QueryRow(ctx, `SELECT name FROM schema_migrations ORDER BY applied_at DESC,name DESC LIMIT 1`).Scan(&latest); err != nil {
			return fmt.Errorf("find latest migration before %s: %w", name, err)
		}
		if err := rollbackLatest(ctx, pool); err != nil {
			return err
		}
		if latest == name || strings.Compare(latest, name) < 0 {
			return nil
		}
	}
}

// policyRole reads the app role that the migration bound to workspace_isolation.
func policyRole(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var role string
	if err := pool.QueryRow(context.Background(), `SELECT roles[1]::text FROM pg_policies
		WHERE schemaname=current_schema() AND tablename='workspaces' AND policyname='workspace_isolation'`).Scan(&role); err != nil {
		t.Fatal(err)
	}
	return role
}

// otherDatabase creates a second database on the same cluster, the shape of a second install.
func otherDatabase(t *testing.T, pool *pgxpool.Pool) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	name := fmt.Sprintf("colliedesk_test_other_%d", time.Now().UnixNano())
	if _, err := pool.Exec(ctx, `CREATE DATABASE `+name); err != nil {
		t.Fatal(err)
	}
	config := pool.Config().Copy()
	config.ConnConfig.Database = name
	delete(config.ConnConfig.RuntimeParams, "search_path")
	other, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		other.Close()
		if _, err := pool.Exec(context.Background(), `DROP DATABASE `+name+` WITH (FORCE)`); err != nil {
			t.Errorf("drop %s: %v", name, err)
		}
		// The migration created this database's own app role; nothing else uses it.
		if _, err := pool.Exec(context.Background(), `DROP ROLE IF EXISTS colliedesk_app_`+name); err != nil {
			t.Errorf("drop app role of %s: %v", name, err)
		}
	})
	return other
}

func TestAppRoleOfOneDatabaseCannotReachAnother(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	other := otherDatabase(t, pool)
	if err := Migrate(ctx, other); err != nil {
		t.Fatal(err)
	}
	ids := seedTwoWorkspaces(t, other)
	mine, theirs := policyRole(t, pool), policyRole(t, other)
	tx, err := other.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var count int
	_, err = tx.Exec(ctx, `SELECT set_config('role',$1,true),set_config('app.workspace_id',$2,true)`, mine, ids.a)
	if err == nil {
		err = tx.QueryRow(ctx, `SELECT count(*) FROM workspaces`).Scan(&count)
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("app role %s of this database read %d workspace(s) of another database (err=%v); app role there is %s", mine, count, err, theirs)
	}
}

func TestPanickingCallbackReleasesConnectionAndLocks(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	ids := seedTwoWorkspaces(t, pool)
	single := singleConnection(t, pool)
	db := New(single)
	lockAndPanic := func(tx pgx.Tx) error {
		var id string
		if err := tx.QueryRow(ctx, `SELECT id::text FROM conversations WHERE workspace_id=$1 FOR UPDATE`, ids.a).Scan(&id); err != nil {
			t.Fatal(err)
		}
		panic("callback failed")
	}
	for name, run := range map[string]func(){
		"InWorkspace": func() { _ = db.InWorkspace(ctx, ids.a, lockAndPanic) },
		"Privileged":  func() { _ = db.Privileged(ctx, PrivilegeWorkspaceBySlug, lockAndPanic) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("%s swallowed the panic", name)
				}
			}()
			run()
		}()
		if acquired := single.Stat().AcquiredConns(); acquired != 0 {
			t.Fatalf("%s: %d connection(s) still checked out after the panic", name, acquired)
		}
		bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := New(pool).InWorkspace(bounded, ids.a, func(tx pgx.Tx) error {
			_, err := tx.Exec(bounded, `SELECT id FROM conversations WHERE workspace_id=$1 FOR UPDATE NOWAIT`, ids.a)
			return err
		})
		cancel()
		if err != nil {
			t.Fatalf("%s: row lock survived the panic: %v", name, err)
		}
	}
}
