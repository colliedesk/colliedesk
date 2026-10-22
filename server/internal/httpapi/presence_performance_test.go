package httpapi_test

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	presenceLatencyBudget = 20 * time.Millisecond
	presenceWarmup        = 10
	presenceSamples       = 200
)

// TestConversationPresenceLatencyBudget times the store call behind PUT
// /api/v1/conversations/{id}/presence, which every tab with a conversation open sends every 5 seconds:
// removing the workspace's expired rows, the announce, the cap per person and the read of who else
// is there, in one transaction. The fixture is three full teams of ten people on 2,000 conversations
// each, every person with three tabs and an agent token open: 40 live rows in the timed workspace,
// 16 of them on the timed conversation, and 40 expired rows waiting in each other workspace. As for
// the inbox activity budget, the HTTP and session hops are left to the inbox list budget.
func TestConversationPresenceLatencyBudget(t *testing.T) {
	_, _, pool := testAPIWithPool(t)
	ctx := context.Background()
	var target presenceFixture
	for index := range 3 {
		fixture := seedPresenceFixture(t, pool, index)
		if index == 0 {
			target = fixture
		}
	}
	if _, err := pool.Exec(ctx, `ANALYZE conversation_presence`); err != nil {
		t.Fatal(err)
	}

	// Each timed announce is a heartbeat: the clock moves past the minimum interval between two.
	clock := time.Now().UTC()
	data := store.New(pool).WithClock(func() time.Time { clock = clock.Add(domain.PresenceMinInterval); return clock })
	caller := store.PresenceCaller{AgentID: target.agents[0], SessionID: target.sessions[0]}
	announce := func() ([]domain.ConversationViewer, time.Duration) {
		started := time.Now()
		viewers, err := data.AnnouncePresence(ctx, target.workspace, target.conversation, caller,
			store.PresenceUpdate{ClientID: "budget-tab-1", Writing: true, TTL: domain.PresenceWritingTTL})
		elapsed := time.Since(started)
		if err != nil {
			t.Fatal(err)
		}
		return viewers, elapsed
	}
	for range presenceWarmup {
		announce()
	}
	durations := make([]time.Duration, presenceSamples)
	var sample []domain.ConversationViewer
	for index := range durations {
		sample, durations[index] = announce()
	}
	if len(sample) < 5 {
		t.Fatalf("fixture is not realistic: %d viewers on the timed conversation", len(sample))
	}
	sort.Slice(durations, func(left, right int) bool { return durations[left] < durations[right] })
	p95 := durations[presenceSamples*95/100-1]
	t.Logf("presence announce p95=%s (viewers=%d)", p95, len(sample))
	if p95 > presenceLatencyBudget {
		t.Fatalf("presence announce p95 exceeds %s: %s", presenceLatencyBudget, p95)
	}
}

type presenceFixture struct {
	workspace, conversation string
	agents, sessions        []string
}

// seedPresenceFixture writes one workspace with ten people signed in, 2,000 conversations, three tabs
// and a token per person spread over them, 16 of the rows on the first conversation; expired rows
// are left in every workspace but the first, whose announce removes its own.
func seedPresenceFixture(t *testing.T, pool *pgxpool.Pool, index int) presenceFixture {
	t.Helper()
	ctx := context.Background()
	var f presenceFixture
	if err := pool.QueryRow(ctx, `INSERT INTO workspaces(name,slug) VALUES($1,$2) RETURNING id::text`,
		fmt.Sprintf("Presence Budget %d", index), fmt.Sprintf("presence-budget-%d", index)).Scan(&f.workspace); err != nil {
		t.Fatal(err)
	}
	var contact string
	if err := pool.QueryRow(ctx, `INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Fixture','fixture@presence-budget.example.com') RETURNING id::text`, f.workspace).Scan(&contact); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `WITH made AS (INSERT INTO conversations(workspace_id,contact_id,subject,created_at)
			SELECT $1,$2,'Fixture '||n,now()-(n||' minutes')::interval FROM generate_series(1,2000) AS n RETURNING id,created_at)
		SELECT id::text FROM made ORDER BY created_at DESC LIMIT 1`, f.workspace, contact).Scan(&f.conversation); err != nil {
		t.Fatal(err)
	}
	for person := range 10 {
		var agent, session, token string
		if err := pool.QueryRow(ctx, `INSERT INTO agents(workspace_id,email,display_name,role) VALUES($1,$2,$3,'agent') RETURNING id::text`,
			f.workspace, fmt.Sprintf("person%d@presence-budget.example.com", person), fmt.Sprintf("Person %d", person)).Scan(&agent); err != nil {
			t.Fatal(err)
		}
		for kind, target := range map[string]*string{"browser": &session, "token": &token} {
			scope := any(nil)
			if kind == "token" {
				scope = "read"
			}
			if err := pool.QueryRow(ctx, `INSERT INTO sessions(workspace_id,agent_id,token_hash,role_ceiling,label,expires_at,scope)
				VALUES($1,$2,sha256(gen_random_uuid()::text::bytea),'agent',$3,now()+interval '30 days',$4) RETURNING id::text`, f.workspace, agent, kind, scope).Scan(target); err != nil {
				t.Fatal(err)
			}
		}
		f.agents, f.sessions = append(f.agents, agent), append(f.sessions, session)
		// Three tabs and the token, on the timed conversation for the first four people, elsewhere for the rest.
		expires := "now()+interval '1 hour'"
		if index > 0 {
			expires = "now()-interval '1 minute'"
		}
		if _, err := pool.Exec(ctx, `INSERT INTO conversation_presence(workspace_id,session_id,client_id,agent_id,conversation_id,writing_since,edited_at,seen_at,expires_at)
			SELECT $1,s.id,'fixture-tab-'||s.n,$2,
				CASE WHEN $4 < 4 THEN $3::uuid ELSE (SELECT id FROM conversations WHERE workspace_id=$1 ORDER BY id OFFSET ($4*4+s.n)*13 LIMIT 1) END,
				CASE WHEN s.n=1 THEN now() END,CASE WHEN s.n=1 THEN now() END,now(),`+expires+`
			FROM (SELECT $5::uuid AS id,n FROM generate_series(1,3) AS n UNION ALL SELECT $6::uuid,4) s`,
			f.workspace, agent, f.conversation, person, session, token); err != nil {
			t.Fatal(err)
		}
	}
	return f
}
