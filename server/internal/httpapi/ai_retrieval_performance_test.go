package httpapi_test

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// aiRetrievalBudget is the time an AI task may spend finding its sources (docs/internals/performance.md).
// No screen waits for it: drafts and suggestions run in the background.
const aiRetrievalBudget = 200 * time.Millisecond

// seedAIRetrievalBudget builds, in a workspace of the shared knowledge base fixture (sharedKBBudget:
// 2,000 articles of about 9,000 characters, 20 at 100,000, half published), the published articles and
// conversations resolved conversations of about 3,500 characters, as the worker would. The article
// vectors are the fixture's own (its words are lower case ASCII, so folding changes nothing). Every
// text holds "everyword" and every other one "halfword", the worst case for a search. 100 distinct
// texts with a word of their own keep the seed quick, and the shared fixture keeps it from slowing
// the other latency gates that run beside it.
func seedAIRetrievalBudget(t *testing.T, pool *pgxpool.Pool, workspaceName string, conversations int) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `WITH w AS (SELECT id FROM workspaces WHERE name=$1),
		t AS MATERIALIZED (SELECT n, words, kb_search_vector(words) AS vector FROM (
			SELECT n, (SELECT string_agg(substr(md5(((n*7919+k*104729) % 20000)::text),1,4+(k%5)),' ') FROM generate_series(1,600) k) AS words FROM generate_series(0,99) n) x),
		ct AS (INSERT INTO contacts(workspace_id,name,email) SELECT id,'Budget Person','budget@ai.example' FROM w RETURNING id,workspace_id),
		c AS (INSERT INTO conversations(workspace_id,contact_id,subject,status,created_at,updated_at)
			SELECT ct.workspace_id,ct.id,'Budget conversation '||g,'resolved',now()-g*interval '1 minute',now()-g*interval '1 minute' FROM ct, generate_series(1,$2::int) g
			RETURNING id,workspace_id,updated_at,subject),
		dc AS (INSERT INTO ai_retrieval_docs(workspace_id,conversation_id,title,body,folded,tokens,updated_at)
			SELECT c.workspace_id,c.id,c.subject,'Customer: everyword '||t.words,
				kb_search_vector(lower(c.subject)||' customer everyword '||CASE WHEN right(c.subject,1) IN ('0','2','4','6','8') THEN 'halfword' ELSE '' END)||t.vector,
				length(t.words)/4,c.updated_at
			FROM c JOIN t ON t.n = (hashtext(c.id::text) & 1023) % 100 RETURNING 1)
		INSERT INTO ai_retrieval_docs(workspace_id,kb_article_id,title,body,folded,tokens,updated_at)
		SELECT a.workspace_id,a.id,a.title,a.body_md,
			kb_search_vector(lower(a.title)||' everyword '||CASE WHEN a.number%4=0 THEN 'halfword' ELSE '' END)||a.search,length(a.body_md)/4,a.updated_at
		FROM kb_articles a JOIN w ON a.workspace_id=w.id WHERE a.status='published'`, workspaceName, conversations)
	if err != nil {
		t.Fatalf("seed AI retrieval budget of %s: %v", workspaceName, err)
	}
	// The rows are built already; what the triggers queued would only be built again.
	if _, err := pool.Exec(ctx, `DELETE FROM ai_retrieval_pending WHERE workspace_id=(SELECT id FROM workspaces WHERE name=$1)`, workspaceName); err != nil {
		t.Fatal(err)
	}
}

// TestAIRetrievalLatencyBudget holds a full search (knowledge base and conversations) to the budget,
// with more resolved conversations than the search window and the knowledge base at its cap, in two
// workspaces, with 16 words of which two match every text or half of them.
func TestAIRetrievalLatencyBudget(t *testing.T) {
	_, pool, _, _ := sharedKBBudget(t)
	started := time.Now()
	seedAIRetrievalBudget(t, pool, "KB Budget A", 6000)
	seedAIRetrievalBudget(t, pool, "KB Budget B", 1000)
	if _, err := pool.Exec(context.Background(), `ANALYZE ai_retrieval_docs; ANALYZE conversations`); err != nil {
		t.Fatal(err)
	}
	t.Logf("seeded in %s", time.Since(started))
	workspace := queryString(t, pool, `SELECT id::text FROM workspaces WHERE name='KB Budget A'`)
	words := []string{"everyword", "halfword"}
	for k := 1; len(words) < 16; k += 1237 {
		words = append(words, queryString(t, pool, `SELECT substr(md5($1::int::text),1,4+($2::int%5))`, (k*104729)%20000, k))
	}
	query := store.AIRetrievalQuery{Text: strings.Join(words, " "), KBLimit: 5, ConversationLimit: 3}
	db := database.New(pool)
	// Warm: the budget is for a search whose rows are in memory, as they are after the first one.
	durations := make([]time.Duration, 25)
	for i := range durations {
		started := time.Now()
		var docs []store.AIRetrievedDoc
		err := db.InWorkspace(context.Background(), workspace, func(tx pgx.Tx) error {
			var err error
			docs, err = store.AIRetrieve(context.Background(), tx, workspace, query)
			return err
		})
		durations[i] = time.Since(started)
		if err != nil {
			t.Fatal(err)
		}
		if len(docs) != 8 {
			t.Fatalf("search returned %d documents, want 5 articles and 3 conversations", len(docs))
		}
	}
	durations = durations[5:]
	sort.Slice(durations, func(a, b int) bool { return durations[a] < durations[b] })
	p95 := durations[18]
	t.Logf("AI retrieval p95=%s (min %s)", p95, durations[0])
	if p95 > aiRetrievalBudget {
		t.Errorf("AI retrieval p95 %s exceeds %s", p95, aiRetrievalBudget)
	}
}
