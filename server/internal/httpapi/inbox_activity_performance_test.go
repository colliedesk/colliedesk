package httpapi_test

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	activityLatencyBudget = 20 * time.Millisecond
	activityWarmup        = 10
	activitySamples       = 200
)

// TestInboxActivityLatencyBudget times the store query behind GET /api/v1/inbox/activity, which every
// open inbox tab polls. The HTTP and session lookup hops are shared with every endpoint and measured
// by the inbox list budget; timing the query itself keeps a noisy CI runner from failing a budget it
// does not own. The fixture is a busy workspace: 5,000 conversations spread over a week, a tenth of
// them assigned to the caller by a teammate, a customer message on every third of them, plus a second workspace of the same size.
func TestInboxActivityLatencyBudget(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	tokenA := createSession(t, api, service, "Activity Budget A", "admin-a@activity-budget.example.com")
	_ = createSession(t, api, service, "Activity Budget B", "admin-b@activity-budget.example.com")
	_, inviteURL := createContractInvite(t, api, tokenA, "teammate@activity-budget.example.com", "agent")
	teammate := agentID(t, api, redeemLabelled(t, api, inviteURL, "Teammate"))
	caller := agentID(t, api, tokenA)
	workspaceA := workspaceID(t, api, tokenA)
	ctx := context.Background()
	seedActivityFixture(t, pool, "Activity Budget A", caller, teammate)
	seedActivityFixture(t, pool, "Activity Budget B", "", "")
	if _, err := pool.Exec(ctx, `ANALYZE conversations`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ANALYZE messages`); err != nil {
		t.Fatal(err)
	}

	data := store.New(pool)
	since := time.Now().UTC().Add(-30 * time.Minute)
	poll := func() (store.InboxActivity, time.Duration) {
		started := time.Now()
		activity, err := data.InboxActivity(ctx, workspaceA, caller, &since)
		elapsed := time.Since(started)
		if err != nil {
			t.Fatal(err)
		}
		return activity, elapsed
	}
	for range activityWarmup {
		poll()
	}
	durations := make([]time.Duration, activitySamples)
	var sample store.InboxActivity
	for index := range durations {
		sample, durations[index] = poll()
	}
	if len(sample.NewConversations) == 0 || len(sample.NewConversations) >= store.InboxActivityLimit || len(sample.AssignedToMe) == 0 || len(sample.Updated) == 0 || len(sample.Updated) >= store.InboxActivityLimit {
		t.Fatalf("fixture is not realistic: new=%d assigned=%d updated=%d", len(sample.NewConversations), len(sample.AssignedToMe), len(sample.Updated))
	}
	sort.Slice(durations, func(left, right int) bool { return durations[left] < durations[right] })
	p95 := durations[activitySamples*95/100-1]
	t.Logf("inbox activity p95=%s (new=%d assigned=%d updated=%d)", p95, len(sample.NewConversations), len(sample.AssignedToMe), len(sample.Updated))
	if p95 > activityLatencyBudget {
		t.Fatalf("inbox activity p95 exceeds %s: %s", activityLatencyBudget, p95)
	}
}

func seedActivityFixture(t *testing.T, pool *pgxpool.Pool, workspaceName, caller, teammate string) {
	t.Helper()
	ctx := context.Background()
	var workspace, contact string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM workspaces WHERE name=$1`, workspaceName).Scan(&workspace); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Fixture Contact',$2) RETURNING id::text`,
		workspace, "fixture-"+workspace+"@example.com").Scan(&contact); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO conversations(workspace_id,contact_id,subject,created_at,updated_at,assignee_id,assigned_at,assigned_by_agent_id)
		SELECT $1,$2,'Fixture conversation '||n,now()-(n*2||' minutes')::interval,now()-(n*2||' minutes')::interval,
			CASE WHEN $3<>'' AND n%10=0 THEN $3::uuid END,
			CASE WHEN $3<>'' AND n%10=0 THEN now()-(n*2||' minutes')::interval END,
			CASE WHEN $3<>'' AND n%10=0 THEN $4::uuid END
		FROM generate_series(1,5000) AS n`, workspace, contact, caller, nullIfEmpty(teammate)); err != nil {
		t.Fatal(err)
	}
	// Every conversation carries a short history; every third one ends with a customer message.
	if _, err := pool.Exec(ctx, `INSERT INTO messages(workspace_id,conversation_id,kind,body,created_at)
		SELECT c.workspace_id,c.id,CASE WHEN k=1 AND c.n%3=0 THEN 'incoming' ELSE 'note' END,'Fixture message',c.updated_at-((2-k)||' seconds')::interval
		FROM (SELECT id,workspace_id,updated_at,row_number() OVER (ORDER BY created_at) AS n FROM conversations WHERE workspace_id=$1) c, generate_series(0,1) AS k`, workspace); err != nil {
		t.Fatal(err)
	}
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}
