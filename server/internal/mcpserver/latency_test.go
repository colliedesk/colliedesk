package mcpserver_test

import (
	"context"
	"sort"
	"testing"
	"time"
)

// mcpLatencyBudget is the per-call share of the 200 ms a person waits; the REST inbox gate uses the same.
const mcpLatencyBudget = 100 * time.Millisecond

func TestInboxSearchLatencyBudget(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Latency Desk", "admin@latency.example.com")
	if _, err := e.pool.Exec(context.Background(), `WITH contacts_made AS (
			INSERT INTO contacts(workspace_id,name,email) SELECT $1,'Load '||n,'load-'||n||'@example.com' FROM generate_series(1,50) AS n RETURNING id, email
		), numbered AS (
			SELECT id, row_number() OVER (ORDER BY email) AS slot FROM contacts_made
		), seeded AS (
			INSERT INTO conversations(workspace_id,contact_id,subject,status,updated_at)
			SELECT $1,(SELECT id FROM numbered WHERE slot=(n % 50)+1),'Budget conversation '||n,'open',now()-(n||' seconds')::interval
			FROM generate_series(1,5000) AS n RETURNING id
		)
		INSERT INTO messages(workspace_id,conversation_id,kind,body)
		SELECT $1,id,'incoming','budget-needle '||message_number FROM seeded CROSS JOIN generate_series(1,3) AS message_number`, w.id); err != nil {
		t.Fatal(err)
	}
	session := e.connect(w.read, "")
	for name, args := range map[string]map[string]any{
		"status page": {"status": "open", "limit": 50},
		"text search": {"q": "budget-needle", "limit": 50},
	} {
		durations := make([]time.Duration, 50)
		for index := range durations {
			started := time.Now()
			out := e.call(session, "colliedesk_inbox_search", args)
			durations[index] = time.Since(started)
			if out.IsError {
				t.Fatalf("%s: %s", name, text(out))
			}
		}
		sort.Slice(durations, func(left, right int) bool { return durations[left] < durations[right] })
		t.Logf("%s p95=%s", name, durations[47])
		if durations[47] > mcpLatencyBudget {
			t.Errorf("%s: p95 %s exceeds %s", name, durations[47], mcpLatencyBudget)
		}
	}
}
