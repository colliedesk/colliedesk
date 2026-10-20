package httpapi_test

import (
	"context"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// seedKBBudget gives a workspace a collection and count articles of about 9,000 characters, 20 of
// them at 100,000 characters (the longest text allowed). The words come from a vocabulary of 20,000,
// about what a help center's articles use.
func seedKBBudget(t *testing.T, ctx context.Context, exec func(sql string, args ...any) error, workspaceName string, count int) {
	t.Helper()
	// 100 distinct texts, each article taking one and a word of its own, keep the seed quick; reading a
	// vector costs the same whether or not another article has the same words.
	err := exec(`WITH w AS (SELECT id FROM workspaces WHERE name=$1),
		c AS (INSERT INTO kb_collections(workspace_id,name,slug) SELECT id,'Budget','budget' FROM w RETURNING id,workspace_id),
		t AS MATERIALIZED (SELECT n, (SELECT string_agg(substr(md5(((n*7919+k*104729) % 20000)::text),1,4+(k%5)),' ') FROM generate_series(1,16000) k) AS words
			FROM generate_series(0,99) n)
		INSERT INTO kb_articles(workspace_id,collection_id,number,title,slug,body_md,status,published_at)
		SELECT c.workspace_id,c.id,g,'Article '||g,'article-'||g,
			left('own'||g||' '||t.words, CASE WHEN g<=20 THEN 100000 ELSE 9000 END),
			CASE WHEN g%2=0 THEN 'published' ELSE 'draft' END, CASE WHEN g%2=0 THEN now() END
		FROM c, generate_series(1,$2::int) g JOIN t ON t.n = g % 100`, workspaceName, count)
	if err != nil {
		t.Fatalf("seed knowledge base of %s: %v", workspaceName, err)
	}
}

// TestKnowledgeBaseSearchLatencyBudget holds the article list and search to the inbox budget at the
// article cap: two workspaces of domain.MaxKBArticles articles each, a search with two words, a
// short prefix, a search that matches nothing, a title search, and the published articles, each at
// the largest page.
func TestKnowledgeBaseSearchLatencyBudget(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	token := createSession(t, api, service, "KB Budget A", "admin@kb-budget-a.example.com")
	_ = createSession(t, api, service, "KB Budget B", "admin@kb-budget-b.example.com")
	ctx := context.Background()
	exec := func(sql string, args ...any) error { _, err := pool.Exec(ctx, sql, args...); return err }
	seedKBBudget(t, ctx, exec, "KB Budget A", domain.MaxKBArticles)
	seedKBBudget(t, ctx, exec, "KB Budget B", domain.MaxKBArticles)
	if err := exec(`ANALYZE kb_articles; ANALYZE kb_article_versions`); err != nil {
		t.Fatal(err)
	}
	word := queryString(t, pool, `SELECT substr(md5('1234'),1,6)`)
	other := queryString(t, pool, `SELECT substr(md5('777'),1,5)`)
	paths := map[string]string{
		"two words":  "/api/v1/kb/articles?limit=200&q=" + word + "+" + other,
		"prefix":     "/api/v1/kb/articles?limit=200&q=" + word[:3],
		"no match":   "/api/v1/kb/articles?limit=200&q=zzzzqqqq",
		"title":      "/api/v1/kb/articles?limit=200&q=Article+1999",
		"published":  "/api/v1/kb/articles?limit=200&status=published",
		"collection": "/api/v1/kb/collections",
	}
	for name, path := range paths {
		p95 := inboxP95(t, api, token, path)
		t.Logf("knowledge base %s p95=%s", name, p95)
		if p95 > inboxLatencyBudget {
			t.Errorf("knowledge base %s p95 %s exceeds %s", name, p95, inboxLatencyBudget)
		}
	}
}
