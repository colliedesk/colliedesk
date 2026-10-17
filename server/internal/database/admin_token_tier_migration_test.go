package database

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

const adminTokenTierMigration = "20261017132104-admin-token-tier.up.sql"

func TestAdminTokenTierMigrationKeepsTokensAndRevokesAdminTokensOnRollback(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	for _, name := range upMigrations(t) {
		if name < adminTokenTierMigration {
			if err := applyMigration(ctx, pool, name); err != nil {
				t.Fatal(err)
			}
		}
	}
	var workspace, agent string
	if err := pool.QueryRow(ctx, `WITH w AS (INSERT INTO workspaces(slug,name) VALUES('tiers','Tiers') RETURNING id),
		a AS (INSERT INTO agents(workspace_id,email,role) SELECT id,'admin@tiers.example','admin' FROM w RETURNING id,workspace_id),
		s AS (INSERT INTO sessions(workspace_id,agent_id,token_hash,role_ceiling,scope,label,expires_at)
			SELECT workspace_id,id,'\x01','agent','send','Claude Code',now()+interval '1 day' FROM a)
		SELECT workspace_id::text,id::text FROM a`).Scan(&workspace, &agent); err != nil {
		t.Fatalf("seed a send token on the previous schema: %v", err)
	}

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	var ceiling, scope string
	if err := pool.QueryRow(ctx, `SELECT role_ceiling,scope FROM sessions WHERE token_hash='\x01' AND revoked_at IS NULL`).Scan(&ceiling, &scope); err != nil {
		t.Fatalf("the send token did not survive the upgrade: %v", err)
	}
	if ceiling != "agent" || scope != "send" {
		t.Fatalf("upgrade changed the send token: ceiling=%q scope=%q", ceiling, scope)
	}

	insert := func(hash, ceiling, scope string) error {
		_, err := pool.Exec(ctx, `INSERT INTO sessions(workspace_id,agent_id,token_hash,role_ceiling,scope,label,expires_at)
			VALUES($1,$2,$3::bytea,$4,$5,'Ops agent',now()+interval '1 day')`, workspace, agent, hash, ceiling, scope)
		return err
	}
	if err := insert(`\x02`, "admin", "admin"); err != nil {
		t.Fatalf("an admin token with the admin ceiling: %v", err)
	}
	for name, args := range map[string][2]string{
		"admin token with the agent ceiling": {"agent", "admin"},
		"send token with the admin ceiling":  {"admin", "send"},
		"unknown tier":                       {"admin", "owner"},
	} {
		var violation *pgconn.PgError
		if err := insert(`\x03`, args[0], args[1]); !errors.As(err, &violation) || violation.Code != "23514" {
			t.Fatalf("%s: err=%v, want a check violation", name, err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO agent_requests(workspace_id,token_id,token_label,token_scope,kind,method,route,path,summary,expires_at)
		SELECT workspace_id,id,'Ops agent','admin','action','POST','/api/v1/auth/tokens','/api/v1/auth/tokens','Create an agent token',now()+interval '7 days'
		FROM sessions WHERE token_hash='\x02'`); err != nil {
		t.Fatalf("an admin token files a request: %v", err)
	}

	if err := rollbackThrough(ctx, pool, adminTokenTierMigration); err != nil {
		t.Fatalf("down: %v", err)
	}
	assertRevoked(t, pool, `\x01`, false)
	assertRevoked(t, pool, `\x02`, true)
	var requests int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_requests WHERE token_scope='send'`).Scan(&requests); err != nil || requests != 1 {
		t.Fatalf("the admin token's request after the rollback: count=%d err=%v, want it kept under send", requests, err)
	}

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("up again: %v", err)
	}
}
