package importjobs_test

import (
	"context"
	"sort"
	"testing"
	"time"
)

const (
	importStatusBudget = 20 * time.Millisecond
	importListBudget   = 100 * time.Millisecond
	importWarmup       = 10
	importSamples      = 200
)

// TestImportStatusLatencyBudget times the store calls behind GET /api/v1/imports/{id}, which the
// import screen polls every few seconds, and GET /api/v1/imports. The fixture is a workspace with 300
// jobs, each with a preview of 50 agents, and the newest one with a 4 MB report: the status never
// reads a report, so its size must not show. A second workspace has the same jobs.
func TestImportStatusLatencyBudget(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	for _, w := range []workspace{e.a, e.b} {
		e.exec(t, `INSERT INTO import_jobs(workspace_id,kind,source,domain,account_source,state,phase,preview,report,status,created_at,updated_at,finished_at)
			SELECT $1,'import','freshdesk','acme.freshdesk.com','freshdesk:account:777','finished','full',
				jsonb_build_object('agents',(SELECT jsonb_agg(jsonb_build_object('id',a,'name','Agent '||a,'email','agent'||a||'@acme.example','active',true)) FROM generate_series(1,50) a),'companies',20),
				CASE WHEN n=300 THEN jsonb_build_object('status','OK','warnings',(SELECT jsonb_agg(repeat('x',400)) FROM generate_series(1,10000))) END,
				'OK', now()-(301-n)*interval '1 minute', now()-(301-n)*interval '1 minute', now()-(301-n)*interval '1 minute'
			FROM generate_series(1,300) n`, w.id)
	}
	e.exec(t, `ANALYZE import_jobs`)
	var newest string
	if err := e.pool.QueryRow(ctx, `SELECT id::text FROM import_jobs WHERE workspace_id=$1 AND report IS NOT NULL`, e.a.id).Scan(&newest); err != nil {
		t.Fatal(err)
	}
	measure := func(name string, budget time.Duration, call func() error) {
		t.Helper()
		for range importWarmup {
			if err := call(); err != nil {
				t.Fatal(err)
			}
		}
		durations := make([]time.Duration, importSamples)
		for index := range durations {
			started := time.Now()
			if err := call(); err != nil {
				t.Fatal(err)
			}
			durations[index] = time.Since(started)
		}
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		p95 := durations[importSamples*95/100-1]
		t.Logf("%s p95=%s", name, p95)
		if p95 > budget {
			t.Fatalf("%s p95 exceeds %s: %s", name, budget, p95)
		}
	}
	measure("import status", importStatusBudget, func() error {
		job, err := e.service.Get(ctx, e.a.id, newest)
		if err == nil && !job.HasReport {
			t.Fatal("fixture: the newest job has no report")
		}
		return err
	})
	measure("import list", importListBudget, func() error {
		jobs, err := e.service.List(ctx, e.a.id, 100)
		if err == nil && len(jobs) != 100 {
			t.Fatalf("list = %d jobs", len(jobs))
		}
		return err
	})
}
