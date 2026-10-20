package httpapi_test

import (
	"context"
	"testing"
)

// seedKBBudget gives a workspace a collection and 2,000 articles of about 9,000 characters of
// pseudo-random words each, 20 of them at 100,000 characters (the largest text allowed).
func seedKBBudget(t *testing.T, ctx context.Context, exec func(sql string, args ...any) error, workspaceName string) {
	t.Helper()
	err := exec(`WITH w AS (SELECT id FROM workspaces WHERE name=$1),
		c AS (INSERT INTO kb_collections(workspace_id,name,slug) SELECT id,'Budget','budget' FROM w RETURNING id,workspace_id)
		INSERT INTO kb_articles(workspace_id,collection_id,number,title,slug,body_md,status,published_at)
		SELECT c.workspace_id,c.id,g,'Article '||g||' '||md5(g::text),'article-'||g,
			left((SELECT string_agg(substr(md5((g*1000+k)::text||$1),1,5+(k%6)),' ') FROM generate_series(1, CASE WHEN g<=20 THEN 14000 ELSE 1200 END) k),
				CASE WHEN g<=20 THEN 100000 ELSE 9000 END),
			CASE WHEN g%2=0 THEN 'published' ELSE 'draft' END, CASE WHEN g%2=0 THEN now() END
		FROM c, generate_series(1,2000) g`, workspaceName)
	if err != nil {
		t.Fatalf("seed knowledge base of %s: %v", workspaceName, err)
	}
}

// TestKnowledgeBaseSearchLatencyBudget holds the article list and search to the inbox budget: two
// workspaces of 2,000 articles each, a search with two prefixes, a search that matches nothing, a
// title search, and the newest published articles, each at the largest page.
func TestKnowledgeBaseSearchLatencyBudget(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	token := createSession(t, api, service, "KB Budget A", "admin@kb-budget-a.example.com")
	_ = createSession(t, api, service, "KB Budget B", "admin@kb-budget-b.example.com")
	ctx := context.Background()
	exec := func(sql string, args ...any) error { _, err := pool.Exec(ctx, sql, args...); return err }
	seedKBBudget(t, ctx, exec, "KB Budget A")
	seedKBBudget(t, ctx, exec, "KB Budget B")
	if err := exec(`ANALYZE kb_articles; ANALYZE kb_article_versions`); err != nil {
		t.Fatal(err)
	}
	word := queryString(t, pool, `SELECT substr(md5((5*1000+3)::text||'KB Budget A'),1,4)`)
	paths := map[string]string{
		"search":     "/api/v1/kb/articles?limit=200&q=" + word + "+" + word[:3],
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
