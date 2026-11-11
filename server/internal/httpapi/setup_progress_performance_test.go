package httpapi_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	setupLatencyBudget = 50 * time.Millisecond
	setupWarmup        = 10
	setupSamples       = 50
)

// TestSetupProgressLatencyBudget times the store call behind GET /api/v1/workspace/setup, which the inbox reads
// on every load and the setup flow polls every 5 seconds, on its slowest path: an admin of a workspace whose
// reply targets are off, so the median first reply of the imported history is read. The fixture is a busy
// team after a Freshdesk import: 10,000 imported conversations opened in the last 30 days and 2,000 older
// ones, each with a customer message, a received notice and a reply, and a second workspace with 4,000 of them.
// Seeding more (30,000 conversations of five messages each) took 70 s of the 120-second CI budget. Same method
// as the inbox activity budget: the HTTP and session hops are the inbox list budget's.
func TestSetupProgressLatencyBudget(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	tokenA := createSession(t, api, service, "Setup Budget A", "admin-a@setup-budget.example.com")
	tokenB := createSession(t, api, service, "Setup Budget B", "admin-b@setup-budget.example.com")
	workspaceA := workspaceID(t, api, tokenA)
	ctx := context.Background()
	for token, conversations := range map[string]int{tokenA: 12000, tokenB: 4000} {
		insertImport(t, pool, workspaceSlug(t, api, token), "finished")
		seedSetupHistory(t, pool, workspaceID(t, api, token), conversations)
	}
	for _, table := range []string{"conversations", "messages"} {
		if _, err := pool.Exec(ctx, `ANALYZE `+table); err != nil {
			t.Fatal(err)
		}
	}

	data := store.New(pool)
	read := func() (store.SetupProgress, time.Duration) {
		started := time.Now()
		progress, err := data.SetupProgress(ctx, workspaceA, time.Now().UTC(), store.SetupOptions{ServerDrafts: true, CanSetTargets: true})
		elapsed := time.Since(started)
		if err != nil {
			t.Fatal(err)
		}
		return progress, elapsed
	}
	for range setupWarmup {
		read()
	}
	durations := make([]time.Duration, setupSamples)
	var sample store.SetupProgress
	for index := range durations {
		sample, durations[index] = read()
	}
	if sample.ReplyTargetSuggestion == nil || sample.ReplyTargetSuggestion.Basis != "history" {
		t.Fatalf("the fixture must take the median path: %+v", sample.ReplyTargetSuggestion)
	}
	slices.Sort(durations)
	p95 := durations[setupSamples*95/100-1]
	t.Logf("setup progress p95=%s (median %d min)", p95, *sample.ReplyTargetSuggestion.MedianFirstReplyMinutes)
	if p95 > setupLatencyBudget {
		t.Fatalf("setup progress p95 exceeds %s: %s", setupLatencyBudget, p95)
	}
}

// seedSetupHistory writes imported conversations, one every 260 seconds back from now (10,000 in the last 30
// days), each with its customer message, a received notice and a reply.
func seedSetupHistory(t *testing.T, pool *pgxpool.Pool, workspace string, conversations int) {
	t.Helper()
	ctx := context.Background()
	var contact string
	if err := pool.QueryRow(ctx, `INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Imported Contact',$2) RETURNING id::text`,
		workspace, "imported-"+workspace+"@example.com").Scan(&contact); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO conversations(workspace_id,contact_id,subject,status,origin,channel,created_at,updated_at)
		SELECT $1,$2,'Imported '||n,'resolved','{"source":"freshdesk"}','imported',now()-(n*260||' seconds')::interval,now()-(n*260||' seconds')::interval
		FROM generate_series(1,$3::int) AS n`, workspace, contact, conversations); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages(workspace_id,conversation_id,kind,body,notice_kind,origin,created_at)
		SELECT c.workspace_id,c.id,m.kind,'Imported message',m.notice,'{"source":"freshdesk"}',c.created_at+(m.after||' minutes')::interval
		FROM conversations c, (VALUES ('incoming',NULL,0),('outgoing','received',1),('outgoing',NULL,90)) AS m(kind,notice,after)
		WHERE c.workspace_id=$1`, workspace); err != nil {
		t.Fatal(err)
	}
}
