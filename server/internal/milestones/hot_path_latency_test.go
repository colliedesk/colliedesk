package milestones_test

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/milestones"
	"github.com/jackc/pgx/v5"
)

const (
	hotPathBudget  = 5 * time.Millisecond
	hotPathWarmup  = 10
	hotPathSamples = 200
)

// TestMilestoneHotPathLatencyBudget times the two checks that run on every inbound message and every reply
// (RecordCustomerMessage, RecordReply) in the case that happens almost always: the workspace has recorded the
// milestone already, so the check reads and writes nothing. The fixture is a workspace with 5,000 live
// inbound messages and 5,000 delivered replies, 500 conversations, 50 agents, and a second workspace of
// the same shape. Each check must stay at or below 5 ms p95, inside the 200 ms of the request that calls it.
func TestMilestoneHotPathLatencyBudget(t *testing.T) {
	ctx := context.Background()
	pool := databasetest.Pool(t)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	service := auth.New(pool, "https://desk.example.com")
	var ids []string
	for _, name := range []string{"Hot Path One", "Hot Path Two"} {
		workspace, _, err := service.CreateWorkspace(ctx, name, "admin@"+name[len(name)-3:]+".example")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, workspace.ID)
		for _, statement := range []string{
			`INSERT INTO agents(workspace_id,email,role) SELECT $1,'agent'||n||'@hot.example','agent' FROM generate_series(1,50) n`,
			`INSERT INTO channels(workspace_id,type,name) VALUES($1,'email','Email')`,
			`INSERT INTO contacts(workspace_id,name,email) SELECT $1,'Customer '||n,'customer'||n||'@customer.example' FROM generate_series(1,500) n`,
			`INSERT INTO conversations(workspace_id,contact_id,subject,channel_id)
				SELECT $1,c.id,'Subject '||row_number() OVER (),(SELECT id FROM channels WHERE workspace_id=$1 LIMIT 1)
				FROM contacts c WHERE c.workspace_id=$1 AND c.email LIKE 'customer%'`,
			`INSERT INTO messages(workspace_id,conversation_id,kind,body,channel_id,email_meta,created_at)
				SELECT $1,c.id,'incoming','question '||g,(SELECT id FROM channels WHERE workspace_id=$1 LIMIT 1),
					jsonb_build_object('from','customer@customer.example','authentication','pass'),now()-(g||' minutes')::interval
				FROM (SELECT id,row_number() OVER () rn FROM conversations WHERE workspace_id=$1) c, generate_series(1,10) g`,
			`WITH r AS (INSERT INTO messages(workspace_id,conversation_id,author_agent_id,kind,body,created_at)
				SELECT $1,c.id,(SELECT id FROM agents WHERE workspace_id=$1 ORDER BY email LIMIT 1),'outgoing','answer '||g,now()-(g||' minutes')::interval
				FROM conversations c, generate_series(1,10) g WHERE c.workspace_id=$1 RETURNING id)
			INSERT INTO channel_outbox(workspace_id,message_id,channel_type,channel_id,status,created_at)
				SELECT $1,id,'email',(SELECT id FROM channels WHERE workspace_id=$1 LIMIT 1),'sent',now() FROM r`,
		} {
			if _, err := pool.Exec(ctx, statement, workspace.ID); err != nil {
				t.Fatalf("fixture: %v", err)
			}
		}
	}
	for _, table := range []string{"messages", "channel_outbox", "workspace_events", "conversations", "agents"} {
		if _, err := pool.Exec(ctx, `ANALYZE `+table); err != nil {
			t.Fatal(err)
		}
	}
	workspace := ids[0]
	var inbound, reply string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM messages WHERE workspace_id=$1 AND kind='incoming' ORDER BY created_at DESC LIMIT 1`, workspace).Scan(&inbound); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT id::text FROM messages WHERE workspace_id=$1 AND kind='outgoing' ORDER BY created_at DESC LIMIT 1`, workspace).Scan(&reply); err != nil {
		t.Fatal(err)
	}
	// The milestones are recorded already: the check finds them and writes nothing.
	for _, kind := range []milestones.Kind{milestones.FirstCustomerMessage, milestones.FirstReply} {
		if _, err := pool.Exec(ctx, `INSERT INTO workspace_events(workspace_id,kind,at,source) VALUES($1,$2,now(),'app')`, workspace, string(kind)); err != nil {
			t.Fatal(err)
		}
	}
	measure := func(name string, check func(context.Context, pgx.Tx) error) {
		t.Helper()
		call := func() error {
			tx, err := pool.Begin(ctx)
			if err != nil {
				return err
			}
			defer tx.Rollback(ctx) //nolint:errcheck
			return check(ctx, tx)
		}
		for range hotPathWarmup {
			if err := call(); err != nil {
				t.Fatal(err)
			}
		}
		durations := make([]time.Duration, hotPathSamples)
		for index := range durations {
			started := time.Now()
			if err := call(); err != nil {
				t.Fatal(err)
			}
			durations[index] = time.Since(started)
		}
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		p95 := durations[hotPathSamples*95/100-1]
		t.Logf("%s p95=%s", name, p95)
		if p95 > hotPathBudget {
			t.Fatalf("%s p95 exceeds %s: %s", name, hotPathBudget, p95)
		}
	}
	measure("RecordCustomerMessage", func(ctx context.Context, tx pgx.Tx) error {
		return milestones.RecordCustomerMessage(ctx, tx, workspace, inbound)
	})
	measure("RecordReply", func(ctx context.Context, tx pgx.Tx) error {
		return milestones.RecordReply(ctx, tx, workspace, reply)
	})
}
