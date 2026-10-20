package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
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

// kbBudget is the knowledge base at the article cap, seeded once per test binary and shared by the
// gates that read it: seeding is most of their time in CI, where every second counts against the
// 120-second budget. TestMain drops its schema.
var kbBudget struct {
	once   sync.Once
	ready  bool
	pool   *pgxpool.Pool
	schema string
	token  string
	slug   string
}

// sharedKBBudget returns an API on the shared fixture: workspaces "KB Budget A" and "KB Budget B", each
// with domain.MaxKBArticles articles (seedKBBudget) and its help center on; the session token and slug
// are workspace A's.
func sharedKBBudget(t *testing.T) (http.Handler, *pgxpool.Pool, string, string) {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is unset; skipping real PostgreSQL API test")
	}
	kbBudget.once.Do(func() {
		ctx := context.Background()
		config, err := pgxpool.ParseConfig(databaseURL)
		if err != nil {
			t.Fatal(err)
		}
		kbBudget.schema = fmt.Sprintf("test_kb_budget_%d", time.Now().UnixNano())
		config.ConnConfig.RuntimeParams["search_path"] = kbBudget.schema
		pool, err := pgxpool.NewWithConfig(ctx, config)
		if err != nil {
			t.Fatal(err)
		}
		kbBudget.pool = pool
		if _, err := pool.Exec(ctx, "CREATE SCHEMA "+kbBudget.schema); err != nil {
			t.Fatal(err)
		}
		api, service, _, _ := testAPIOnPool(t, pool, nil)
		kbBudget.token = createSession(t, api, service, "KB Budget A", "admin@kb-budget-a.example.com")
		_ = createSession(t, api, service, "KB Budget B", "admin@kb-budget-b.example.com")
		kbBudget.slug = workspaceSlug(t, api, kbBudget.token)
		exec := func(sql string, args ...any) error { _, err := pool.Exec(ctx, sql, args...); return err }
		seedKBBudget(t, ctx, exec, "KB Budget A", domain.MaxKBArticles)
		seedKBBudget(t, ctx, exec, "KB Budget B", domain.MaxKBArticles)
		if err := exec(`INSERT INTO help_centers(workspace_id,enabled,title,content_version,last_article_number,updated_at)
			SELECT id, true, name, 1, $1, now() FROM workspaces WHERE name LIKE 'KB Budget %'`, domain.MaxKBArticles); err != nil {
			t.Fatal(err)
		}
		if err := exec(`ANALYZE kb_articles; ANALYZE kb_article_versions; ANALYZE kb_collections; ANALYZE help_centers`); err != nil {
			t.Fatal(err)
		}
		kbBudget.ready = true
	})
	if !kbBudget.ready {
		t.Fatal("the shared knowledge base fixture did not seed; see the first test that used it")
	}
	api, _, _, _ := testAPIOnPool(t, kbBudget.pool, nil)
	return api, kbBudget.pool, kbBudget.token, kbBudget.slug
}

func TestMain(m *testing.M) {
	code := m.Run()
	if kbBudget.pool != nil {
		_, _ = kbBudget.pool.Exec(context.Background(), "DROP SCHEMA "+kbBudget.schema+" CASCADE")
		kbBudget.pool.Close()
	}
	os.Exit(code)
}

// TestKnowledgeBaseSearchLatencyBudget holds the article list and search to the inbox budget at the
// article cap: two workspaces of domain.MaxKBArticles articles each, a search with two words, a
// short prefix, a search that matches nothing, a title search, and the published articles, each at
// the largest page.
func TestKnowledgeBaseSearchLatencyBudget(t *testing.T) {
	api, pool, token, _ := sharedKBBudget(t)
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
