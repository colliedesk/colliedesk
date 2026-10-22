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
	// Every draft of A used one of 10 articles: 2,000 drafts each this month.
	var article string
	if err := q.pool.QueryRow(ctx, `WITH col AS (INSERT INTO kb_collections(workspace_id,name,slug) VALUES($1,'Help','help') RETURNING id),
			art AS (INSERT INTO kb_articles(workspace_id,collection_id,number,title,slug,body_md,status,published_at)
				SELECT $1,col.id,g,'Article '||g,'article-'||g,'Text','published',now() FROM col, generate_series(1,10) g RETURNING id,number),
			src AS (INSERT INTO ai_draft_sources(workspace_id,draft_id,kb_article_id,created_at)
				SELECT d.workspace_id,d.id,art.id,d.created_at FROM ai_drafts d JOIN art ON art.number = 1 + abs(hashtext(d.id::text)) % 10 WHERE d.workspace_id=$1 RETURNING kb_article_id)
		SELECT kb_article_id::text FROM src LIMIT 1`, q.a.id).Scan(&article); err != nil {
		t.Fatal(err)
	}
	if _, err := q.pool.Exec(ctx, `ANALYZE ai_draft_sources`); err != nil {
		t.Fatal(err)
	}
	measure("article", func() error { _, err := q.service.ArticleDraftsThisMonth(ctx, q.a.id, article); return err })
	measure("get", func() error { _, err := q.service.GetDraft(ctx, q.a.id, target, draft); return err })
	measure("list", func() error {
		drafts, err := q.service.ListDrafts(ctx, q.a.id, target)
		if err == nil && len(drafts) != 20 {
			t.Fatalf("listed %d drafts", len(drafts))
		}
		return err
	})
}
