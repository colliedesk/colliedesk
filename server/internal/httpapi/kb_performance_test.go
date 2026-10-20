package httpapi_test

import (
	"context"
	"strings"
	"testing"
	"time"

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

// TestKnowledgeBaseSearchVectorCostsLinearTime writes and rewrites articles of 100,000 characters
// built from the runs that make the 'simple' parser quadratic (docs/internals/knowledge-base.md):
// unnormalized, each took seconds to tens of seconds of one database core. Every write path (API,
// MCP, import, approved request) computes the same generated column, so writing the row is the test.
func TestKnowledgeBaseSearchVectorCostsLinearTime(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	_ = createSession(t, api, service, "KB Parser", "admin@kb-parser.example.com")
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	// An unfixed parser fails here in seconds instead of hanging the run.
	if _, err := conn.Exec(ctx, `SET statement_timeout = '3s'`); err != nil {
		t.Fatal(err)
	}
	defer conn.Exec(ctx, `RESET statement_timeout`) //nolint:errcheck
	collection := ""
	if err := conn.QueryRow(ctx, `INSERT INTO kb_collections(workspace_id,name,slug)
		SELECT id,'Parser','parser' FROM workspaces WHERE name='KB Parser' RETURNING id`).Scan(&collection); err != nil {
		t.Fatal(err)
	}
	// Every ASCII punctuation character but '.' separates words; '.' keeps hosts and versions whole.
	var words string
	if err := conn.QueryRow(ctx, `SELECT kb_search_vector($1)::text`, "a!b\"c#d$e%f&g'h(i)j*k+l,m-n/o:p;q<r=s>t?u@v[w\\x]y^z_A`B{C|D}E~F 1.2.3").Scan(&words); err != nil {
		t.Fatal(err)
	}
	if want := `'1.2.3' 'a' 'b' 'c' 'd' 'e' 'f' 'g' 'h' 'i' 'j' 'k' 'l' 'm' 'n' 'o' 'p' 'q' 'r' 's' 't' 'u' 'v' 'w' 'x' 'y' 'z'`; words != want {
		t.Errorf("words = %s, want %s", words, want)
	}
	runs := []string{"_a", "a_", "_1", "~/", "/.", "a1@", "ab1@", "a-1-", "-aa-1", "a-a.", "e:a"}
	for index, run := range runs {
		other := runs[(index+1)%len(runs)]
		body, next := strings.Repeat(run, 100000/len(run)), strings.Repeat(other, 100000/len(other))
		started := time.Now()
		id := ""
		if err := conn.QueryRow(ctx, `INSERT INTO kb_articles(workspace_id,collection_id,number,title,slug,body_md)
			SELECT workspace_id,id,$2::bigint,'Run '||$2::text,'run-'||$2::text,$3 FROM kb_collections WHERE id=$1 RETURNING id`,
			collection, index+1, body).Scan(&id); err != nil {
			t.Fatalf("create with %q: %v", run, err)
		}
		created := time.Since(started)
		started = time.Now()
		if _, err := conn.Exec(ctx, `UPDATE kb_articles SET body_md=$2 WHERE id=$1`, id, next); err != nil {
			t.Fatalf("edit to %q: %v", other, err)
		}
		edited := time.Since(started)
		t.Logf("%q create=%s edit=%s", run, created, edited)
		if created > 200*time.Millisecond || edited > 200*time.Millisecond {
			t.Errorf("%q create %s, edit %s: want at most 200ms each", run, created, edited)
		}
	}
}
