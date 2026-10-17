package httpapi_test

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

const pendingRequestsBudget = 20 * time.Millisecond

// TestPendingAgentRequestsLatencyBudget times the store query behind GET /api/v1/agent-requests?status=pending,
// which every open tab polls for the Requests badge and the reply drafts. The fixture is two workspaces with
// 5,000 requests each from one busy token: 20 still pending, the rest decided over the last week.
func TestPendingAgentRequestsLatencyBudget(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	tokenA := createSession(t, api, service, "Requests Budget A", "admin-a@requests-budget.example.com")
	tokenB := createSession(t, api, service, "Requests Budget B", "admin-b@requests-budget.example.com")
	readA := issueToken(t, api, tokenA, "busy agent", "read")
	readB := issueToken(t, api, tokenB, "busy agent", "read")
	workspaceA := workspaceID(t, api, tokenA)
	seedRequests(t, pool, workspaceA, readA.Session.ID)
	seedRequests(t, pool, workspaceID(t, api, tokenB), readB.Session.ID)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `ANALYZE agent_requests`); err != nil {
		t.Fatal(err)
	}
	data := store.New(pool)
	poll := func() (int, time.Duration) {
		started := time.Now()
		items, err := data.ListAgentRequests(ctx, workspaceA, store.AgentRequestFilter{Status: "pending"})
		elapsed := time.Since(started)
		if err != nil {
			t.Fatal(err)
		}
		return len(items), elapsed
	}
	for range activityWarmup {
		poll()
	}
	durations := make([]time.Duration, activitySamples)
	var pending int
	for index := range durations {
		pending, durations[index] = poll()
	}
	if pending != 20 {
		t.Fatalf("pending = %d, want the fixture's 20", pending)
	}
	sort.Slice(durations, func(left, right int) bool { return durations[left] < durations[right] })
	p95 := durations[activitySamples*95/100-1]
	t.Logf("pending agent requests p95=%s", p95)
	if p95 > pendingRequestsBudget {
		t.Fatalf("pending agent requests p95 exceeds %s: %s", pendingRequestsBudget, p95)
	}
}

func seedRequests(t *testing.T, pool *pgxpool.Pool, workspaceID, tokenID string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `INSERT INTO agent_requests(workspace_id,token_id,token_label,token_scope,requested_by_agent_id,kind,method,route,path,body,summary,status,decided_at,created_at,expires_at)
		SELECT $1,s.id,s.label,s.scope,s.agent_id,'action','PATCH','/api/v1/tags/{id}','/api/v1/tags/x','{"name":"n"}','Rename a tag: name n',
			CASE WHEN n <= 20 THEN 'pending' ELSE 'rejected' END,
			CASE WHEN n <= 20 THEN NULL ELSE now() - n * interval '2 minutes' END,
			now() - n * interval '2 minutes', now() - n * interval '2 minutes' + interval '7 days'
		FROM sessions s, generate_series(1, 5000) AS n WHERE s.workspace_id=$1 AND s.id=$2`, workspaceID, tokenID); err != nil {
		t.Fatal(err)
	}
}
