package database

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const scopedTokenMigration = "20261017020421-scoped-agent-tokens.up.sql"

func TestScopedTokenMigrationKeepsSessionsAndRevokesTokensOnRollback(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	for _, name := range upMigrations(t) {
		if name < scopedTokenMigration {
			if err := applyMigration(ctx, pool, name); err != nil {
				t.Fatal(err)
			}
		}
	}
	var workspace, agent string
	if err := pool.QueryRow(ctx, `WITH w AS (INSERT INTO workspaces(slug,name) VALUES('legacy','Legacy') RETURNING id),
		a AS (INSERT INTO agents(workspace_id,email,role) SELECT id,'admin@legacy.example','admin' FROM w RETURNING id,workspace_id),
		s AS (INSERT INTO sessions(workspace_id,agent_id,token_hash,role_ceiling,label,expires_at)
			SELECT workspace_id,id,'\x01','admin','Laptop',now()+interval '1 day' FROM a)
		SELECT workspace_id::text,id::text FROM a`).Scan(&workspace, &agent); err != nil {
		t.Fatalf("seed a session on the previous schema: %v", err)
	}

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	var label, ceiling string
	var scope, revoked *string
	if err := pool.QueryRow(ctx, `SELECT label,role_ceiling,scope,revoked_at::text FROM sessions WHERE token_hash='\x01'`).Scan(&label, &ceiling, &scope, &revoked); err != nil {
		t.Fatal(err)
	}
	if label != "Laptop" || ceiling != "admin" || scope != nil || revoked != nil {
		t.Fatalf("upgrade changed the existing session: %q %q scope=%v revoked=%v", label, ceiling, scope, revoked)
	}

	insert := func(hash, ceiling, scope string) error {
		_, err := pool.Exec(ctx, `INSERT INTO sessions(workspace_id,agent_id,token_hash,role_ceiling,scope,label,expires_at)
			VALUES($1,$2,$3::bytea,$4,$5,'Claude Code',now()+interval '1 day')`, workspace, agent, hash, ceiling, scope)
		return err
	}
	if err := insert(`\x02`, "agent", "draft"); err != nil {
		t.Fatalf("a draft token with the agent ceiling: %v", err)
	}
	for name, args := range map[string][2]string{"unknown tier": {"agent", "owner"}, "admin ceiling on a token": {"admin", "read"}} {
		var violation *pgconn.PgError
		if err := insert(`\x03`, args[0], args[1]); !errors.As(err, &violation) || violation.Code != "23514" {
			t.Fatalf("%s: err=%v, want a check violation", name, err)
		}
	}

	if err := rollbackThrough(ctx, pool, scopedTokenMigration); err != nil {
		t.Fatalf("down: %v", err)
	}
	var hasScope bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='sessions' AND column_name='scope')`).Scan(&hasScope); err != nil || hasScope {
		t.Fatalf("scope column survived the rollback (err=%v)", err)
	}
	assertRevoked(t, pool, `\x01`, false)
	assertRevoked(t, pool, `\x02`, true)

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("up again: %v", err)
	}
}

// A token left active after the column is gone would be an ordinary agent session.
func assertRevoked(t *testing.T, pool *pgxpool.Pool, hash string, want bool) {
	t.Helper()
	var revoked bool
	if err := pool.QueryRow(context.Background(), `SELECT revoked_at IS NOT NULL FROM sessions WHERE token_hash=$1::bytea`, hash).Scan(&revoked); err != nil {
		t.Fatal(err)
	}
	if revoked != want {
		t.Fatalf("session %s revoked=%v after the rollback, want %v", hash, revoked, want)
	}
}

// The owner of a managed database is not a superuser; forced row-level security hides the session
// rows from it unless the rollback marks itself privileged.
func TestScopedTokenRollbackRevokesTokensWhenTheOwnerIsNotASuperuser(t *testing.T) {
	ctx := context.Background()
	pool, _ := ownedSchemaPool(t, testPool(t), true)
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	db := New(pool)
	err := db.Privileged(ctx, PrivilegeCreateWorkspace, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `WITH w AS (INSERT INTO workspaces(slug,name) VALUES('owned','Owned') RETURNING id),
			a AS (INSERT INTO agents(workspace_id,email,role) SELECT id,'admin@owned.example','admin' FROM w RETURNING id,workspace_id)
			INSERT INTO sessions(workspace_id,agent_id,token_hash,role_ceiling,scope,label,expires_at)
			SELECT workspace_id,id,'\x02','agent','send','Claude Code',now()+interval '1 day' FROM a`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rollbackThrough(ctx, pool, scopedTokenMigration); err != nil {
		t.Fatalf("down: %v", err)
	}
	var revoked bool
	err = db.Privileged(ctx, PrivilegeCreateWorkspace, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT revoked_at IS NOT NULL FROM sessions WHERE token_hash='\x02'`).Scan(&revoked)
	})
	if err != nil || !revoked {
		t.Fatalf("token still active after the rollback (revoked=%v, err=%v)", revoked, err)
	}
}
