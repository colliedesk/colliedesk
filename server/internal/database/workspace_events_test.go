package database

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestWorkspaceEventsRefuseUpdateDeleteAndTruncate(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO workspaces(id, slug, name) VALUES (md5('ws-acme')::uuid, 'acme', 'Acme');
		INSERT INTO workspace_events(workspace_id, kind, at, source) VALUES (md5('ws-acme')::uuid, 'signed_up', '2026-09-01T00:00:00Z', 'app')`); err != nil {
		t.Fatal(err)
	}
	db := New(pool)
	refused := map[string]string{
		"update":   `UPDATE workspace_events SET at = at - interval '1 day'`,
		"delete":   `DELETE FROM workspace_events`,
		"truncate": `TRUNCATE workspace_events`,
	}
	for name, sql := range refused {
		// The owner, who runs migrations and privileged work, is refused like everyone else.
		if _, err := pool.Exec(ctx, sql); err == nil || !strings.Contains(err.Error(), "append-only") {
			t.Fatalf("%s by the owner: %v, want the append-only refusal", name, err)
		}
		err := db.InWorkspace(ctx, md5UUID(t, pool, "ws-acme"), func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, sql)
			return err
		})
		if err == nil {
			t.Fatalf("%s by the app role succeeded", name)
		}
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workspace_events WHERE workspace_id=md5('ws-acme')::uuid AND kind='signed_up'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("signed_up rows after the refusals = %d (%v), want the one written", n, err)
	}
	// A kind outside the list and a source other than app or backfill are refused.
	for _, sql := range []string{
		`INSERT INTO workspace_events(workspace_id, kind, at, source) VALUES (md5('ws-acme')::uuid, 'activated', now(), 'app')`,
		`INSERT INTO workspace_events(workspace_id, kind, at, source) VALUES (md5('ws-acme')::uuid, 'paid', now(), 'script')`,
	} {
		if _, err := pool.Exec(ctx, sql); err == nil {
			t.Fatalf("accepted: %s", sql)
		}
	}
}

// The view's columns are a contract another program reads by name; a deleted workspace and a trial that
// ended keep their row, and paid_at and cancelled_at are the latest of each.
func TestWorkspaceMilestonesViewHasItsFixedColumnsAndOneRowPerWorkspace(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(ctx, `SELECT column_name||' '||data_type FROM information_schema.columns
		WHERE table_schema=current_schema() AND table_name='workspace_milestones' ORDER BY ordinal_position`)
	if err != nil {
		t.Fatal(err)
	}
	columns, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	wantColumns := "workspace_id uuid,signed_up_at timestamp with time zone,first_customer_message_at timestamp with time zone," +
		"import_finished_at timestamp with time zone,first_reply_at timestamp with time zone,teammate_invited_at timestamp with time zone," +
		"trial_ended_at timestamp with time zone,paid_at timestamp with time zone,cancelled_at timestamp with time zone"
	if strings.Join(columns, ",") != wantColumns {
		t.Fatalf("view columns = %s\nwant %s", strings.Join(columns, ","), wantColumns)
	}

	// Gone is a workspace retention deleted: its log stays, though no workspaces row does.
	if _, err := pool.Exec(ctx, `INSERT INTO workspace_events(workspace_id, kind, at, source) VALUES
		(md5('ws-gone')::uuid, 'signed_up', '2026-09-01T00:00:00Z', 'app'),
		(md5('ws-gone')::uuid, 'trial_ended', '2026-09-15T00:00:00Z', 'app'),
		(md5('ws-paid')::uuid, 'signed_up', '2026-09-02T00:00:00Z', 'app'),
		(md5('ws-paid')::uuid, 'paid', '2026-09-10T00:00:00Z', 'app'),
		(md5('ws-paid')::uuid, 'cancelled', '2026-09-20T00:00:00Z', 'app'),
		(md5('ws-paid')::uuid, 'paid', '2026-10-10T00:00:00Z', 'app'),
		(md5('ws-paid')::uuid, 'cancelled', '2026-11-01T00:00:00Z', 'app')`); err != nil {
		t.Fatal(err)
	}
	rows, err = pool.Query(ctx, `SELECT workspace_id::text||' '||concat_ws(' ', signed_up_at, first_customer_message_at, import_finished_at, first_reply_at,
		teammate_invited_at, trial_ended_at, paid_at, cancelled_at) FROM workspace_milestones ORDER BY signed_up_at`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		md5UUID(t, pool, "ws-gone") + " 2026-09-01 00:00:00+00 2026-09-15 00:00:00+00",
		md5UUID(t, pool, "ws-paid") + " 2026-09-02 00:00:00+00 2026-10-10 00:00:00+00 2026-11-01 00:00:00+00",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("view rows:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// The first occurrence of a first-time kind wins; a second one is not stored.
	if _, err := pool.Exec(ctx, `INSERT INTO workspace_events(workspace_id, kind, at, source) VALUES (md5('ws-gone')::uuid, 'trial_ended', '2026-10-01T00:00:00Z', 'app')`); err == nil {
		t.Fatal("a second trial_ended was stored")
	}
}
