package httpapi_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// importJobs are jobs of one workspace in the states the import routes act on. The worker is not
// running in httpapi tests: the jobs are written as the worker would leave them.
type importJobs struct {
	previewed, waitingKey, finished string
}

func seedImportJobs(t *testing.T, pool *pgxpool.Pool, workspaceID string) importJobs {
	t.Helper()
	insert := func(state, extra string) string {
		t.Helper()
		var id string
		err := pool.QueryRow(context.Background(), `INSERT INTO import_jobs(workspace_id,kind,source,domain,account_source,state,phase,preview,report,report_markdown,status,started_at,finished_at)
			VALUES($1,'import','freshdesk','acme.freshdesk.com','freshdesk:account:777',$2,
				CASE WHEN $2='previewed' THEN 'preview' ELSE 'full' END,
				CASE WHEN $2='previewed' THEN $3::jsonb END,
				CASE WHEN $2='finished' THEN '{"status":"OK","tickets":{"source":4,"imported":4}}'::jsonb END,
				CASE WHEN $2='finished' THEN '# Freshdesk import: OK' END,
				CASE WHEN $2='finished' THEN 'OK' END,
				CASE WHEN $2<>'previewed' THEN now() END, CASE WHEN $2='finished' THEN now() END)
			RETURNING id::text`, workspaceID, state, extra).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	preview := `{"domain":"acme.freshdesk.com","account_id":777,"source":"freshdesk:account:777","agents":[{"id":101,"name":"Alice","email":"alice@acme.example","active":true}],` +
		`"companies":2,"contacts":3,"tickets_outside_archive":5,"highest_listed_ticket_id":6,"archive":"readable","canned_responses":"readable","knowledge_base":"none",` +
		`"rate_limit_per_minute":400,"api_calls":9,"checked_at":"2026-10-22T08:00:00Z"}`
	return importJobs{previewed: insert("previewed", preview), waitingKey: insert("waiting_key", "{}"), finished: insert("finished", "{}")}
}
