package httpapi_test

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// aiRetrievalBudget is the time an AI task may spend finding its sources (docs/internals/performance.md).
// No screen waits for it: drafts and suggestions run in the background.
const aiRetrievalBudget = 200 * time.Millisecond

// seedAIRetrievalBudget gives the workspace resolved conversations of about 3,500 characters and
// published articles of about 9,000, 20 of them at 100,000, already built. Every text holds the word "everyword" and every other one "halfword", the worst case for a
// search: half the window matches. 100 distinct texts with a word of their own keep the seed quick.
func seedAIRetrievalBudget(t *testing.T, pool *pgxpool.Pool, workspaceName string, conversations, articles int) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `WITH w AS (SELECT id FROM workspaces WHERE name=$1),
		t0 AS MATERIALIZED (SELECT n, (SELECT string_agg(substr(md5(((n*7919+k*104729) % 20000)::text),1,4+(k%5)),' ') FROM generate_series(1,16000) k) AS words
			FROM generate_series(0,99) n),
		t AS MATERIALIZED (SELECT n, words, kb_search_vector(left(words, 3500)) AS short, kb_search_vector(left(words, 9000)) AS long, kb_search_vector(words) AS whole FROM t0),
		ct AS (INSERT INTO contacts(workspace_id,name,email) SELECT id,'Budget Person','budget@ai.example' FROM w RETURNING id,workspace_id),
		c AS (INSERT INTO conversations(workspace_id,contact_id,subject,status,created_at,updated_at)
			SELECT ct.workspace_id,ct.id,'Budget conversation '||g,'resolved',now()-g*interval '1 minute',now()-g*interval '1 minute' FROM ct, generate_series(1,$2::int) g
			RETURNING id,workspace_id,updated_at,subject),
		col AS (INSERT INTO kb_collections(workspace_id,name,slug) SELECT id,'AI budget','ai-budget' FROM w RETURNING id,workspace_id),
		a AS (INSERT INTO kb_articles(workspace_id,collection_id,number,title,slug,body_md,status,published_at)
			SELECT col.workspace_id,col.id,g,'Budget article '||g,'budget-article-'||g,
				left('own'||g||' everyword '||CASE WHEN g%2=0 THEN 'halfword ' ELSE '' END||t.words, CASE WHEN g<=20 THEN 100000 ELSE 9000 END),'published',now()
			FROM col, generate_series(1,$3::int) g JOIN t ON t.n = g % 100 RETURNING id,workspace_id,number,title,body_md,updated_at),
		dc AS (INSERT INTO ai_retrieval_docs(workspace_id,conversation_id,title,body,folded,tokens,updated_at)
			SELECT c.workspace_id,c.id,c.subject,x.body,kb_search_vector(c.subject||' customer everyword '||x.half)||x.short,length(x.body)/4,c.updated_at
			FROM c, LATERAL (SELECT CASE WHEN right(c.subject,1) IN ('0','2','4','6','8') THEN 'halfword' ELSE '' END AS half, t.short,
				'Customer: everyword '||left(t.words, 3500) AS body FROM t WHERE t.n = (hashtext(c.id::text) & 1023) % 100) x RETURNING 1)
		INSERT INTO ai_retrieval_docs(workspace_id,kb_article_id,title,body,folded,tokens,updated_at)
		SELECT a.workspace_id,a.id,a.title,a.body_md,
			kb_search_vector(lower(a.title)||' own'||a.number||' everyword '||CASE WHEN a.number%2=0 THEN 'halfword' ELSE '' END)||CASE WHEN a.number<=20 THEN t.whole ELSE t.long END,
			length(a.body_md)/4,a.updated_at FROM a JOIN t ON t.n = a.number % 100`, workspaceName, conversations, articles)
	if err != nil {
		t.Fatalf("seed AI retrieval budget of %s: %v", workspaceName, err)
	}
	// The rows are built already; what the triggers queued would only be built again.
	if _, err := pool.Exec(ctx, `DELETE FROM ai_retrieval_pending WHERE workspace_id=(SELECT id FROM workspaces WHERE name=$1)`, workspaceName); err != nil {
		t.Fatal(err)
	}
}

// TestAIRetrievalLatencyBudget holds a full search (knowledge base and conversations) to the budget
// past the size of the search window, in two workspaces, with 16 words of which two match every text or
// half of them.
func TestAIRetrievalLatencyBudget(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	adminA := createSession(t, api, service, "AI Budget A", "admin@ai-budget-a.example.com")
	_ = createSession(t, api, service, "AI Budget B", "admin@ai-budget-b.example.com")
	started := time.Now()
	// More conversations than the search window, and the article cap; the other workspace is smaller.
	seedAIRetrievalBudget(t, pool, "AI Budget A", 12000, domain.MaxKBArticles)
	seedAIRetrievalBudget(t, pool, "AI Budget B", 2000, 200)
	if _, err := pool.Exec(context.Background(), `ANALYZE ai_retrieval_docs; ANALYZE kb_articles; ANALYZE conversations`); err != nil {
		t.Fatal(err)
	}
	t.Logf("seeded in %s", time.Since(started))
	workspace := workspaceID(t, api, adminA)
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
