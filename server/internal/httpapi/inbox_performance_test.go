package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const inboxLatencyBudget = 100 * time.Millisecond

func TestInboxListLatencyBudget(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	tokenA := createSession(t, api, service, "Performance Workspace A", "admin-a@performance.example.com")
	_ = createSession(t, api, service, "Performance Workspace B", "admin-b@performance.example.com")
	seedInboxBudget(t, pool, "Performance Workspace A", "budget-needle")
	seedInboxBudget(t, pool, "Performance Workspace B", "other-workspace")

	statusP95 := inboxP95(t, api, tokenA, "/api/v1/conversations?status=open&page_size=100")
	searchP95 := inboxP95(t, api, tokenA, "/api/v1/conversations?search=budget-needle&page_size=100")
	t.Logf("inbox p95: status=%s search=%s", statusP95, searchP95)
	if statusP95 > inboxLatencyBudget || searchP95 > inboxLatencyBudget {
		t.Fatalf("inbox p95 exceeds %s: status=%s search=%s", inboxLatencyBudget, statusP95, searchP95)
	}
	var workspaceA, loadA, loadB, loadC string
	if err := pool.QueryRow(context.Background(), `SELECT w.id::text,
		(SELECT id::text FROM tags WHERE workspace_id=w.id AND name='load-a'),(SELECT id::text FROM tags WHERE workspace_id=w.id AND name='load-b'),
		(SELECT id::text FROM tags WHERE workspace_id=w.id AND name='load-c') FROM workspaces w WHERE w.name='Performance Workspace A'`).Scan(&workspaceA, &loadA, &loadB, &loadC); err != nil {
		t.Fatal(err)
	}
	// The newer filters and orders, each on the same 5,000 conversations. A filter that matches
	// nothing (no draft, no failed delivery, an unknown word) reads every row and is the slow case.
	for _, query := range []string{
		"awaiting=team&sort=waiting_longest",
		"tag=" + loadA + "," + loadB + "," + loadC + "&tag_match=all",
		"search=Load%20Contact",
		"search=nothing-matches-this",
		"sort=priority",
		"sort=created_asc&status=open,pending&priority=normal,high",
		"has_pending_draft=true",
		"delivery_failed=true",
		"channel=email,web_form&source=native&assignee=unassigned",
		"created_after=2020-01-01T00:00:00Z&updated_before=2100-01-01T00:00:00Z&organization=Nobody",
	} {
		p95 := inboxP95(t, api, tokenA, "/api/v1/conversations?page_size=100&"+query)
		t.Logf("inbox p95: %s=%s", query, p95)
		if p95 > inboxLatencyBudget {
			t.Fatalf("inbox p95 for %s exceeds %s: %s", query, inboxLatencyBudget, p95)
		}
	}
}

func seedInboxBudget(t *testing.T, pool *pgxpool.Pool, workspaceName, messageBody string) {
	t.Helper()
	ctx := context.Background()
	var workspaceID, contactID, firstTagID, secondTagID, thirdTagID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM workspaces WHERE name=$1`, workspaceName).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Load Contact',$2) RETURNING id::text`,
		workspaceID, fmt.Sprintf("load-%s@example.com", workspaceID)).Scan(&contactID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO tags(workspace_id,name) VALUES($1,'load-a') RETURNING id::text`, workspaceID).Scan(&firstTagID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO tags(workspace_id,name) VALUES($1,'load-b') RETURNING id::text`, workspaceID).Scan(&secondTagID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO tags(workspace_id,name) VALUES($1,'load-c') RETURNING id::text`, workspaceID).Scan(&thirdTagID); err != nil {
		t.Fatal(err)
	}
	_, err := pool.Exec(ctx, `WITH seeded AS (
		INSERT INTO conversations(workspace_id,contact_id,subject,status,updated_at)
		SELECT $1,$2,'Budget conversation '||n,'open',now()-(n||' seconds')::interval
		FROM generate_series(1,5000) AS n RETURNING id
	), seeded_messages AS (
		INSERT INTO messages(workspace_id,conversation_id,kind,body)
		SELECT $1,id,'incoming',$6||' '||message_number FROM seeded
		CROSS JOIN generate_series(1,3) AS message_number RETURNING conversation_id
	)
	INSERT INTO conversation_tags(workspace_id,conversation_id,tag_id)
	SELECT $1,id,tag_id FROM seeded CROSS JOIN (VALUES ($3::uuid),($4::uuid),($5::uuid)) AS tags(tag_id)`,
		workspaceID, contactID, firstTagID, secondTagID, thirdTagID, messageBody)
	if err != nil {
		t.Fatal(err)
	}
}

func inboxP95(t *testing.T, api http.Handler, token, path string) time.Duration {
	t.Helper()
	return apiPercentile(t, api, token, path, 50)
}

// apiPercentile calls the authenticated GET path calls times and returns the 95th percentile (the
// 48th of 50, the 19th of 20).
func apiPercentile(t *testing.T, api http.Handler, token, path string, calls int) time.Duration {
	t.Helper()
	durations := make([]time.Duration, calls)
	for index := range durations {
		started := time.Now()
		response := request(t, api, http.MethodGet, path, nil, token, nil)
		durations[index] = time.Since(started)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
	sort.Slice(durations, func(left, right int) bool { return durations[left] < durations[right] })
	return durations[(calls*95+99)/100-1]
}
