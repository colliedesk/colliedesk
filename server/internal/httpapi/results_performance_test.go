package httpapi_test

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/results"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	resultsComputeBudget = 2 * time.Second
	resultsReadBudget    = 20 * time.Millisecond
	resultsListBudget    = 100 * time.Millisecond
)

// TestResultsComputeAndReadStayInBudget holds the results page to docs/internals/performance.md. The
// background compute of one workspace: no screen waits for it, but it holds a connection, and a slow
// one on a busy workspace would back the worker up. The fixture is a busy team: 5,000 Colliedesk
// conversations over 30 days (a customer message and a reply each, half resolved), and 20,000 imported
// Freshdesk tickets in the baseline's window with stats and time logged on a quarter. Each compute
// (current numbers, baseline) is timed three times; the best must stay within 2 s. On the same data,
// the page's read (one stored snapshot of each kind) is held to 20 ms p95 and the list a number opens
// (the 5,000 conversations of the 30-day volume, a page of 50) to the inbox's 100 ms p95.
func TestResultsComputeAndReadStayInBudget(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	token := createSession(t, api, service, "Results Budget", "admin@results-budget.example.com")
	workspace := workspaceID(t, api, token)
	ctx := context.Background()
	now := time.Now().UTC()
	anchor := now.Add(-40 * results.Day)
	seedResultsFixture(t, pool, workspace, now, anchor)

	db := database.New(pool)
	best := func(name string, compute func(pgx.Tx) error) {
		var fastest time.Duration
		for run := range 3 {
			started := time.Now()
			if err := db.InWorkspace(ctx, workspace, compute); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if elapsed := time.Since(started); run == 0 || elapsed < fastest {
				fastest = elapsed
			}
		}
		t.Logf("results %s compute best of 3 = %s", name, fastest)
		if fastest > resultsComputeBudget {
			t.Fatalf("results %s compute exceeds %s: %s", name, resultsComputeBudget, fastest)
		}
	}
	best("current", func(tx pgx.Tx) error {
		_, err := results.ComputeCurrent(ctx, tx, workspace, now)
		return err
	})
	best("baseline", func(tx pgx.Tx) error { return results.ComputeBaseline(ctx, tx, workspace, now) })

	data := store.New(pool)
	p95 := func(name string, budget time.Duration, samples int, call func() error) {
		durations := make([]time.Duration, samples)
		for index := range durations {
			started := time.Now()
			if err := call(); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			durations[index] = time.Since(started)
		}
		sort.Slice(durations, func(left, right int) bool { return durations[left] < durations[right] })
		value := durations[samples*95/100-1]
		t.Logf("results %s p95 = %s", name, value)
		if value > budget {
			t.Fatalf("results %s p95 exceeds %s: %s", name, budget, value)
		}
	}
	var page results.Page
	p95("read", resultsReadBudget, 200, func() (err error) {
		page, err = data.ReadResults(ctx, workspace, results.Viewer{Admin: true, Now: now})
		return err
	})
	if page.Current == nil || page.Baseline == nil {
		t.Fatalf("read = %+v", page)
	}
	var listed store.ConversationPage
	p95("list of a number", resultsListBudget, 50, func() (err error) {
		listed, err = data.ListConversations(ctx, workspace, store.ConversationFilter{ResultSnapshotID: page.Current.ID, ResultMetric: "30d.volume", Page: 1, PageSize: 50})
		return err
	})
	if len(listed.Items) != 50 {
		t.Fatalf("list of a number has %d rows", len(listed.Items))
	}

	var answered, baselineAnswered int
	if err := pool.QueryRow(ctx, `SELECT (SELECT (data->'windows'->'30d'->'first_response'->>'answered')::int FROM results_snapshots WHERE workspace_id=$1 AND kind='current' ORDER BY computed_at DESC LIMIT 1),
		(SELECT (data->'windows'->'30d'->'first_response'->>'answered')::int FROM results_snapshots WHERE workspace_id=$1 AND kind='baseline')`, workspace).Scan(&answered, &baselineAnswered); err != nil {
		t.Fatal(err)
	}
	if answered < 4000 || baselineAnswered < 10000 {
		t.Fatalf("fixture is not realistic: %d answered in Colliedesk, %d in the baseline", answered, baselineAnswered)
	}
}

func seedResultsFixture(t *testing.T, pool *pgxpool.Pool, workspace string, now, anchor time.Time) {
	t.Helper()
	ctx := context.Background()
	steps := []struct {
		sql  string
		args []any
	}{
		{`UPDATE workspaces SET status_history_since=$2::timestamptz - interval '90 days' WHERE id=$1`, []any{workspace, now}},
		{`INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Budget customer','budget@customer.example')`, []any{workspace}},
		// Colliedesk: one conversation every ~8.6 minutes for 30 days.
		{`INSERT INTO conversations(workspace_id,contact_id,subject,channel,created_at,updated_at)
			SELECT $1,(SELECT id FROM contacts WHERE workspace_id=$1),'Live '||g,'email',$2::timestamptz - g*interval '518 seconds',$2::timestamptz - g*interval '518 seconds'
			FROM generate_series(1,5000) g`, []any{workspace, now}},
		// Imported: 20,000 tickets over the 37 days before the anchor, resolved after 5 hours.
		{`INSERT INTO conversations(workspace_id,contact_id,subject,channel,created_at,updated_at,origin)
			SELECT $1,(SELECT id FROM contacts WHERE workspace_id=$1),'Imported '||g,'imported',$2::timestamptz - g*interval '160 seconds',$2::timestamptz - g*interval '160 seconds',
				jsonb_build_object('system','freshdesk','external_id',g::text,'stats',jsonb_build_object('status','resolved','resolved_at',$2::timestamptz - g*interval '160 seconds' + interval '5 hours','first_responded_at',NULL,'closed_at',NULL))
			FROM generate_series(1,20000) g`, []any{workspace, anchor}},
		{`INSERT INTO import_items(workspace_id,source,entity,source_id,target_id,status)
			SELECT $1,'freshdesk:account:1','ticket',origin->>'external_id',id,'done' FROM conversations WHERE workspace_id=$1 AND channel='imported'`, []any{workspace}},
		// Half of the Colliedesk conversations resolved a day after they came in.
		{`INSERT INTO conversation_status_changes(workspace_id,conversation_id,from_status,to_status,changed_at)
			SELECT $1,id,'open','resolved',created_at + interval '1 day' FROM conversations
			WHERE workspace_id=$1 AND channel='email' AND right(subject,1) IN ('0','2','4','6','8')`, []any{workspace}},
		{`INSERT INTO time_entries(workspace_id,conversation_id,minutes,billable,executed_at)
			SELECT $1,id,12,false,created_at FROM conversations WHERE workspace_id=$1 AND channel='imported' AND (origin->>'external_id')::int % 4 = 0`, []any{workspace}},
		{`INSERT INTO results_state(workspace_id,baseline_source,baseline_anchor_at,baseline_import_status) VALUES($1,'freshdesk:account:1',$2,'OK')
			ON CONFLICT (workspace_id) DO UPDATE SET baseline_source=EXCLUDED.baseline_source,baseline_anchor_at=EXCLUDED.baseline_anchor_at`, []any{workspace, anchor}},
	}
	for _, step := range steps {
		if _, err := pool.Exec(ctx, step.sql, step.args...); err != nil {
			t.Fatalf("%s: %v", step.sql, err)
		}
	}
	// A customer message and a reply 47 minutes later on each, in slices: the messages triggers lock
	// every conversation of a statement, and 25,000 at once run out of the lock table.
	for slice := range 10 {
		if _, err := pool.Exec(ctx, `INSERT INTO messages(workspace_id,conversation_id,kind,body,created_at)
			SELECT $1,c.id,m.kind,'Text',c.created_at + m.after FROM conversations c
			CROSS JOIN (VALUES ('incoming',interval '0'),('outgoing',interval '47 minutes')) AS m(kind,after)
			WHERE c.workspace_id=$1 AND abs(hashtext(c.id::text)) % 10 = $2`, workspace, slice); err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range []string{"conversations", "messages", "conversation_status_changes", "import_items", "time_entries"} {
		if _, err := pool.Exec(ctx, `ANALYZE `+table); err != nil {
			t.Fatal(err)
		}
	}
}
