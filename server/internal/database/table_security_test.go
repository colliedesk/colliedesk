package database

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// narrowedAppRoleTables are the workspace tables on which the app role may do less than SELECT, INSERT,
// UPDATE and DELETE. History and audit rows are only added; a role or a counter is never deleted, only
// marked deleted or reset. Every other workspace table allows all four.
var narrowedAppRoleTables = map[string][]string{
	"agent_requests":                  {"SELECT", "INSERT", "UPDATE"},
	"contact_merges":                  {"SELECT", "INSERT", "UPDATE"},
	"conversation_number_formats":     {"SELECT", "INSERT", "UPDATE"},
	"conversation_number_series":      {"SELECT", "INSERT", "UPDATE"},
	"fix_attempt_events":              {"SELECT", "INSERT"},
	"kb_article_translation_versions": {"SELECT", "INSERT"},
	"kb_article_versions":             {"SELECT", "INSERT"},
	"kb_article_vote_resets":          {"SELECT", "INSERT"},
	"kb_template_versions":            {"SELECT", "INSERT"},
	"response_targets":                {"SELECT", "INSERT", "UPDATE"},
	"roles":                           {"SELECT", "INSERT", "UPDATE"},
	"saved_reply_versions":            {"SELECT", "INSERT"},
	"workspace_events":                {"SELECT", "INSERT"},
}

func TestAppRoleHasExactlyThePrivilegesEachWorkspaceTableAllows(t *testing.T) {
	pool := testPool(t)
	if err := Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(context.Background(), `
		SELECT c.relname, array_agg(p ORDER BY o)
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace,
			unnest(ARRAY['SELECT', 'INSERT', 'UPDATE', 'DELETE', 'TRUNCATE', 'REFERENCES', 'TRIGGER']) WITH ORDINALITY AS u(p, o)
		WHERE n.nspname = current_schema() AND c.relkind IN ('r', 'p') AND has_table_privilege($1, c.oid, p)
		GROUP BY c.relname ORDER BY c.relname`, policyRole(t, pool))
	if err != nil {
		t.Fatal(err)
	}
	got, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (struct {
		table      string
		privileges []string
	}, error) {
		var r struct {
			table      string
			privileges []string
		}
		return r, row.Scan(&r.table, &r.privileges)
	})
	if err != nil {
		t.Fatal(err)
	}
	all := []string{"SELECT", "INSERT", "UPDATE", "DELETE"}
	for _, r := range got {
		want, narrowed := narrowedAppRoleTables[r.table]
		if !narrowed {
			want = all
		}
		if !slices.Equal(r.privileges, want) {
			t.Errorf("app role on %s: %v, want %v", r.table, r.privileges, want)
		}
	}
}

// probeWorkspaceTable creates a table with rows in workspaces a and b, written by the owner.
func probeWorkspaceTable(t *testing.T, db *DB, ids twoWorkspaces) {
	t.Helper()
	ctx := context.Background()
	err := db.Privileged(ctx, PrivilegeMigrate, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `CREATE TABLE probe_notes (workspace_id uuid NOT NULL REFERENCES workspaces(id), body text NOT NULL);
			SELECT secure_workspace_table('probe_notes')`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO probe_notes VALUES ($1, 'a'), ($2, 'b')`, ids.a, ids.b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSecureWorkspaceTableGivesANewTableTheWorkspaceShape(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	db := New(pool)
	ids := seedTwoWorkspaces(t, pool)
	probeWorkspaceTable(t, db, ids)
	if _, gaps := securityGaps(t, pool); len(gaps) != 0 {
		t.Fatalf("gaps = %v", gaps)
	}
	var bodies []string
	err := db.InWorkspace(ctx, ids.a, func(tx pgx.Tx) error {
		// The privileged flag means nothing to the app role: its policy belongs to the owner.
		if _, err := tx.Exec(ctx, `SELECT set_config('app.privileged', 'on', true)`); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT body FROM probe_notes ORDER BY body`)
		if err != nil {
			return err
		}
		bodies, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(bodies, []string{"a"}) {
		t.Fatalf("workspace a reads %v, want [a]", bodies)
	}
}

func TestSecureWorkspaceTableRefusesATableWithoutWorkspaceID(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	_, err := pool.Exec(ctx, `CREATE TABLE probe_loose (conversation_id uuid); SELECT secure_workspace_table('probe_loose')`)
	if err == nil || !strings.Contains(err.Error(), "workspace_id") {
		t.Fatalf("err = %v, want a refusal naming workspace_id", err)
	}
}

func TestSecureServerTableKeepsTheAppRoleOut(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	db := New(pool)
	ids := seedTwoWorkspaces(t, pool)
	if _, err := pool.Exec(ctx, `CREATE TABLE probe_server (id bigserial PRIMARY KEY, body text);
		SELECT secure_server_table('probe_server')`); err != nil {
		t.Fatal(err)
	}
	var tableAccess, sequenceAccess bool
	if err := pool.QueryRow(ctx, `SELECT has_table_privilege($1, 'probe_server', 'SELECT, INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER'),
		has_sequence_privilege($1, 'probe_server_id_seq', 'USAGE, SELECT, UPDATE')`, policyRole(t, pool)).Scan(&tableAccess, &sequenceAccess); err != nil {
		t.Fatal(err)
	}
	if tableAccess || sequenceAccess {
		t.Fatalf("app role reaches the table=%v its sequence=%v", tableAccess, sequenceAccess)
	}
	err := db.InWorkspace(ctx, ids.a, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `SELECT 1 FROM probe_server`)
		return err
	})
	if pgErr := new(pgconn.PgError); !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("workspace read of a server table: %v, want permission denied", err)
	}
	err = db.Privileged(ctx, PrivilegeMigrate, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO probe_server(body) VALUES ('server')`)
		return err
	})
	if err != nil {
		t.Fatalf("privileged write: %v", err)
	}
}

func TestLimitAppRoleLeavesOnlyTheNamedPrivileges(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	db := New(pool)
	ids := seedTwoWorkspaces(t, pool)
	probeWorkspaceTable(t, db, ids)
	if _, err := pool.Exec(ctx, `SELECT limit_app_role('probe_notes', 'SELECT', 'INSERT')`); err != nil {
		t.Fatal(err)
	}
	err := db.InWorkspace(ctx, ids.a, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM probe_notes`)
		return err
	})
	if pgErr := new(pgconn.PgError); !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("delete on an add-only table: %v, want permission denied", err)
	}
	for _, privilege := range []string{"TRUNCATE", "ALL", "SELECT; DROP TABLE workspaces"} {
		if _, err := pool.Exec(ctx, `SELECT limit_app_role('probe_notes', $1)`, privilege); err == nil {
			t.Errorf("limit_app_role accepted %q", privilege)
		}
	}
}

func TestSecurityGuardFlagsAPolicyThatOnlyMentionsTheWorkspace(t *testing.T) {
	for name, statement := range map[string]string{
		"widened":   `ALTER POLICY workspace_isolation ON probe_notes USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid) OR true)`,
		"extra":     `CREATE POLICY everyone ON probe_notes FOR SELECT USING (true)`,
		"to public": `ALTER POLICY privileged_access ON probe_notes TO PUBLIC`,
	} {
		t.Run(name, func(t *testing.T) {
			pool := testPool(t)
			if err := Migrate(context.Background(), pool); err != nil {
				t.Fatal(err)
			}
			probeWorkspaceTable(t, New(pool), seedTwoWorkspaces(t, pool))
			if _, err := pool.Exec(context.Background(), statement); err != nil {
				t.Fatal(err)
			}
			_, gaps := securityGaps(t, pool)
			if len(gaps) != 1 || !strings.HasPrefix(gaps[0], "probe_notes ") {
				t.Fatalf("gaps = %v, want only probe_notes", gaps)
			}
		})
	}
}
