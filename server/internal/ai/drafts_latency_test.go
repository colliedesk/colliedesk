package ai_test

import (
	"context"
	"sort"
	"testing"
	"time"
)

// aiDraftReadBudget is what reading a draft (the poll of the reply box) may take in the store, like the
// other per-conversation reads (docs/internals/performance.md).
const aiDraftReadBudget = 20 * time.Millisecond

// TestAIDraftReadLatencyBudget reads a draft and lists a conversation's drafts in a workspace with
// 20,000 drafts and 20,000 tasks, 200 on the conversation read, beside another workspace as large.
func TestAIDraftReadLatencyBudget(t *testing.T) {
	q := newQueueEnv(t, nil)
	ctx := context.Background()
	var target, draft string
	for _, w := range []workspace{q.a, q.b} {
		if _, err := q.pool.Exec(ctx, `WITH ct AS (INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Budget','budget@example.com') RETURNING id),
			c AS (INSERT INTO conversations(workspace_id,contact_id,subject) SELECT $1,ct.id,'Budget '||g FROM ct, generate_series(1,100) g RETURNING id),
			cs AS (SELECT id, row_number() OVER () AS n FROM c),
			t AS (INSERT INTO ai_tasks(workspace_id,kind,conversation_id,state,created_at,finished_at)
				SELECT $1,'draft',cs.id,'done',now()-g*interval '1 minute',now() FROM cs, generate_series(1,200) g RETURNING id,conversation_id,created_at)
			INSERT INTO ai_drafts(workspace_id,conversation_id,task_id,status,prompt,text,created_at,ready_at,decided_at,outcome)
			SELECT $1,t.conversation_id,t.id,'ready','say hi',repeat('Hello there. ',80),t.created_at,t.created_at,t.created_at,'discarded' FROM t`, w.id); err != nil {
			t.Fatal(err)
		}
	}
	if err := q.pool.QueryRow(ctx, `SELECT conversation_id::text, id::text FROM ai_drafts WHERE workspace_id=$1 ORDER BY created_at DESC LIMIT 1`, q.a.id).Scan(&target, &draft); err != nil {
		t.Fatal(err)
	}
	if _, err := q.pool.Exec(ctx, `ANALYZE ai_drafts; ANALYZE ai_tasks`); err != nil {
		t.Fatal(err)
	}
	measure := func(name string, read func() error) {
		durations := make([]time.Duration, 30)
		for i := range durations {
			started := time.Now()
			if err := read(); err != nil {
				t.Fatal(err)
			}
			durations[i] = time.Since(started)
		}
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		p95 := durations[len(durations)*95/100]
		t.Logf("%s p95 %s", name, p95)
		if p95 > aiDraftReadBudget {
			t.Fatalf("%s p95 %s exceeds %s", name, p95, aiDraftReadBudget)
		}
	}
	measure("get", func() error { _, err := q.service.GetDraft(ctx, q.a.id, target, draft); return err })
	measure("list", func() error {
		drafts, err := q.service.ListDrafts(ctx, q.a.id, target)
		if err == nil && len(drafts) != 20 {
			t.Fatalf("listed %d drafts", len(drafts))
		}
		return err
	})
}
