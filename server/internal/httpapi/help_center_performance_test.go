package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// TestHelpCenterPagesLatencyBudget holds every public help center page to the inbox budget, HTML
// rendering included, at the article cap: two workspaces of domain.MaxKBArticles articles each (half
// published, 20 of them 100,000 characters long), all in one collection. It times the home page, the
// first and last page of that collection, a short and a 100,000-character article, and searches.
func TestHelpCenterPagesLatencyBudget(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "HC Budget A", "admin@hc-budget-a.example.com")
	_ = createSession(t, api, service, "HC Budget B", "admin@hc-budget-b.example.com")
	ctx := context.Background()
	exec := func(sql string, args ...any) error { _, err := pool.Exec(ctx, sql, args...); return err }
	seedKBBudget(t, ctx, exec, "HC Budget A", domain.MaxKBArticles)
	seedKBBudget(t, ctx, exec, "HC Budget B", domain.MaxKBArticles)
	if err := exec(`INSERT INTO help_centers(workspace_id,enabled,title,content_version,last_article_number,updated_at)
		SELECT id, true, name, 1, $1, now() FROM workspaces WHERE name LIKE 'HC Budget %'`, domain.MaxKBArticles); err != nil {
		t.Fatal(err)
	}
	if err := exec(`ANALYZE kb_articles; ANALYZE kb_collections; ANALYZE help_centers`); err != nil {
		t.Fatal(err)
	}
	slug := workspaceSlug(t, api, admin)
	word := queryString(t, pool, `SELECT substr(md5('1234'),1,6)`)
	other := queryString(t, pool, `SELECT substr(md5('777'),1,5)`)
	base := "/help/" + slug
	paths := map[string]string{
		"home":               base,
		"collection":         base + "/c/budget",
		"collection last":    base + fmt.Sprintf("/c/budget?page=%d", domain.MaxKBArticles/2/50),
		"article":            base + "/a/1000-article-1000",
		"article 100k chars": base + "/a/2-article-2",
		"search two words":   base + "/search?q=" + word + "+" + other,
		"search prefix":      base + "/search?q=" + word[:3],
		"search no match":    base + "/search?q=zzzzqqqq",
		"search title":       base + "/search?q=Article+1998",
	}
	for name, path := range paths {
		p95 := helpCenterP95(t, site, path)
		t.Logf("help center %s p95=%s", name, p95)
		if p95 > inboxLatencyBudget {
			t.Errorf("help center %s p95 %s exceeds %s", name, p95, inboxLatencyBudget)
		}
	}
}

// helpCenterP95 loads path 50 times, each from its own address so the per-address limits stay out of
// the measurement, and returns the 95th percentile.
func helpCenterP95(t *testing.T, site http.Handler, path string) time.Duration {
	t.Helper()
	durations := make([]time.Duration, 50)
	for index := range durations {
		started := time.Now()
		response := visit(site, path, fmt.Sprintf("198.51.100.%d", index+1))
		durations[index] = time.Since(started)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%.300s", path, response.Code, response.Body.String())
		}
	}
	sort.Slice(durations, func(left, right int) bool { return durations[left] < durations[right] })
	return durations[47]
}
