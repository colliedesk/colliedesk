package database

import (
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestMoveProgressUpgradePreservesOnboardingAndBackfillsOnlyItsDomain(t *testing.T) {
	pool := testPool(t)
	ctx := t.Context()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	const migration = "20261101094007-workspace-move-progress.up.sql"
	if err := rollbackThrough(ctx, pool, migration); err != nil {
		t.Fatal(err)
	}
	var mine, theirs string
	if err := pool.QueryRow(ctx, `INSERT INTO workspaces(slug,name) VALUES('moving','Moving') RETURNING id::text`).Scan(&mine); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO workspaces(slug,name) VALUES('other','Other') RETURNING id::text`).Scan(&theirs); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO workspace_onboarding(workspace_id,goal,role,source) VALUES($1,'leave_helpdesk','support_lead','email'),($2,'leave_helpdesk','founder','github')`, mine, theirs); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO help_center_domains(workspace_id,hostname,token,claim,status,verified_at,address_checked_at)
  VALUES($1,'help.moving.example',repeat('a',32),repeat('a',24),'active','2026-10-31T12:00:00Z','2026-10-31T12:01:00Z'),
  ($2,'help.other.example',repeat('b',32),repeat('b',24),'pending',NULL,NULL)`, mine, theirs); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var state, answers string
	if err := pool.QueryRow(ctx, `SELECT move_steps::text,goal||','||role||','||source FROM workspace_onboarding WHERE workspace_id=$1`, mine).Scan(&state, &answers); err != nil {
		t.Fatal(err)
	}
	if state != `{"cname": "done", "custom_domain": "done"}` || answers != "leave_helpdesk,support_lead,email" {
		t.Fatalf("upgrade state=%s answers=%s", state, answers)
	}
	if err := New(pool).InWorkspace(ctx, theirs, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT string_agg(move_steps::text,',') FROM workspace_onboarding`).Scan(&state)
	}); err != nil {
		t.Fatal(err)
	}
	if state != "{}" {
		t.Fatalf("other workspace inherited progress: %s", state)
	}
	if err := rollbackThrough(ctx, pool, migration); err != nil {
		t.Fatal(err)
	}
	var hosts string
	if err := pool.QueryRow(ctx, `SELECT string_agg(hostname,',' ORDER BY hostname) FROM help_center_domains`).Scan(&hosts); err != nil {
		t.Fatal(err)
	}
	if hosts != "help.moving.example,help.other.example" {
		t.Fatalf("rollback changed domains: %s", hosts)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
}
