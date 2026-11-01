package database

import (
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestImportNoticeUpgradePreservesJobsAndDoesNotMailHistoricalImports(t *testing.T) {
	pool := testPool(t)
	ctx := t.Context()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	const migration = "20261101161632-import-completion-notice.up.sql"
	if err := rollbackThrough(ctx, pool, migration); err != nil {
		t.Fatal(err)
	}
	var mine, theirs string
	if err := pool.QueryRow(ctx, `INSERT INTO workspaces(slug,name) VALUES('notice-mine','Mine') RETURNING id::text`).Scan(&mine); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO workspaces(slug,name) VALUES('notice-theirs','Theirs') RETURNING id::text`).Scan(&theirs); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO import_jobs(workspace_id,kind,source,domain,state,phase,status,report)
		VALUES($1,'import','freshdesk','mine.freshdesk.com','finished','full','OK','{"tickets":{"imported":3}}'),
		($2,'import','freshdesk','theirs.freshdesk.com','finished','full','FAILED','{"missing":["ticket 42"]}')`, mine, theirs); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO workspace_onboarding(workspace_id,goal,move_steps) VALUES($1,'leave_helpdesk','{"preview":"done"}')`, mine); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	for _, ws := range []struct{ id, report string }{{mine, `{"tickets": {"imported": 3}}`}, {theirs, `{"missing": ["ticket 42"]}`}} {
		err := New(pool).InWorkspace(ctx, ws.id, func(tx pgx.Tx) error {
			var count int
			var report string
			if err := tx.QueryRow(ctx, `SELECT count(*),min(report::text) FROM import_jobs WHERE notice_state IS NULL`).Scan(&count, &report); err != nil {
				return err
			}
			if count != 1 || report != ws.report {
				t.Fatalf("workspace %s: jobs=%d report=%s", ws.id, count, report)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	var preview string
	if err := pool.QueryRow(ctx, `SELECT move_steps->>'preview' FROM workspace_onboarding WHERE workspace_id=$1`, mine).Scan(&preview); err != nil || preview != "done" {
		t.Fatalf("stored preview=%s err=%v", preview, err)
	}
	if err := rollbackThrough(ctx, pool, migration); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM import_jobs`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("rollback jobs=%d err=%v", count, err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
}
