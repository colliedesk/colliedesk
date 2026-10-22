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
// does not own. The fixture is a busy workspace: 5,000 conversations, one every 20 seconds, a tenth of
// them assigned to the caller by a teammate, a customer message on every third of them, plus a second workspace of the same size.
// Who has a conversation open is read on every poll: the teammate has 30 conversations open (three
// tabs each, one writing) and 300 expired rows wait for the next announce to remove them.
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
	session := seedActivityPresence(t, pool, workspaceA, caller, teammate)
	if _, err := pool.Exec(ctx, `ANALYZE conversations`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ANALYZE messages`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ANALYZE conversation_presence`); err != nil {
		t.Fatal(err)
	}

	data := store.New(pool)
	since := time.Now().UTC().Add(-30 * time.Minute)
	poll := func() (store.InboxActivity, time.Duration) {
		started := time.Now()
		activity, err := data.InboxActivity(ctx, workspaceA, store.PresenceCaller{AgentID: caller, SessionID: session}, &since)
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
	if len(sample.NewConversations) == 0 || len(sample.NewConversations) >= store.InboxActivityLimit || len(sample.AssignedToMe) == 0 || len(sample.Updated) == 0 || len(sample.Updated) >= store.InboxActivityLimit ||
		len(sample.Viewing) != 30 {
		t.Fatalf("fixture is not realistic: new=%d assigned=%d updated=%d viewing=%d", len(sample.NewConversations), len(sample.AssignedToMe), len(sample.Updated), len(sample.Viewing))
	}
	sort.Slice(durations, func(left, right int) bool { return durations[left] < durations[right] })
	p95 := durations[activitySamples*95/100-1]
	t.Logf("inbox activity p95=%s (new=%d assigned=%d updated=%d viewing=%d)", p95, len(sample.NewConversations), len(sample.AssignedToMe), len(sample.Updated), len(sample.Viewing))
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
		SELECT $1,$2,'Fixture conversation '||n,now()-(n*20||' seconds')::interval,now()-(n*20||' seconds')::interval,
			CASE WHEN $3<>'' AND n%10=0 THEN $3::uuid END,
			CASE WHEN $3<>'' AND n%10=0 THEN now()-(n*20||' seconds')::interval END,
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

// seedActivityPresence opens 30 conversations of the workspace in three tabs of the teammate (one of
// them writing), the caller's own tab on one, and leaves 300 expired rows; it returns the caller's session.
func seedActivityPresence(t *testing.T, pool *pgxpool.Pool, workspace, caller, teammate string) string {
	t.Helper()
	ctx := context.Background()
	sessions := map[string]string{}
	for _, agent := range []string{caller, teammate} {
		var session string
		if err := pool.QueryRow(ctx, `SELECT id::text FROM sessions WHERE workspace_id=$1 AND agent_id=$2 ORDER BY created_at LIMIT 1`, workspace, agent).Scan(&session); err != nil {
			t.Fatal(err)
		}
		sessions[agent] = session
	}
	if _, err := pool.Exec(ctx, `INSERT INTO conversation_presence(workspace_id,session_id,client_id,agent_id,conversation_id,writing_since,edited_at,seen_at,expires_at)
		SELECT $1,$2,'fixture-tab-'||c.n||'-'||k,$3,c.id,CASE WHEN k=1 THEN now() END,CASE WHEN k=1 THEN now() END,now(),
			CASE WHEN c.n<=30 THEN now()+interval '1 hour' ELSE now()-interval '1 minute' END
		FROM (SELECT id,row_number() OVER (ORDER BY id) AS n FROM conversations WHERE workspace_id=$1 ORDER BY id LIMIT 130) c, generate_series(1,3) AS k`,
		workspace, sessions[teammate], teammate); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO conversation_presence(workspace_id,session_id,client_id,agent_id,conversation_id,seen_at,expires_at)
		SELECT $1,$2,'fixture-own-tab',$3,id,now(),now()+interval '1 hour' FROM conversations WHERE workspace_id=$1 ORDER BY id LIMIT 1`,
		workspace, sessions[caller], caller); err != nil {
		t.Fatal(err)
	}
	return sessions[caller]
}
