package httpapi_test

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// TestHelpCenterPagesLatencyBudget holds every public help center page to the inbox budget, HTML
// rendering included, at the article cap: the shared fixture of TestKnowledgeBaseSearchLatencyBudget,
// two workspaces of domain.MaxKBArticles articles each (half published, 20 of them 100,000 characters
// long), all in one collection, help centers on. It times the home page, the
// first and last page of that collection, a short and a 100,000-character article, and searches.
func TestHelpCenterPagesLatencyBudget(t *testing.T) {
	api, pool, _, slug := sharedKBBudget(t)
	site := helpCenterSite(api)
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

// TestHelpCenterHostileArticlesLatencyBudget holds an article page to the same budget when its text is
// built to be slow to parse: one line of 100,000 quote or list markers nests that deep, which the
// Markdown parser handles in quadratic time (2.7 s for ">" on its own). Its excerpt is parsed too.
func TestHelpCenterHostileArticlesLatencyBudget(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Help Hostile", "admin@help-hostile.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "", true)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Hostile"})
	bodies := map[string]string{
		"quotes":         strings.Repeat(">", 100000),
		"spaced quotes":  strings.Repeat("> ", 50000),
		"ordered lists":  strings.Repeat("1. ", 33333),
		"bullet lists":   strings.Repeat("+ ", 50000),
		"mixed":          strings.Repeat("> 1. - ", 14285),
		"quoted players": strings.Repeat("> <iframe src=\"https://player.vimeo.com/video/1\"></iframe>\n", 1500),
	}
	for name, body := range bodies {
		article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": name, "body_md": body, "status": "published"})
		p95 := helpCenterP95(t, site, fmt.Sprintf("/help/%s/a/%d-%s", slug, article.Number, article.Slug))
		t.Logf("help center hostile %s p95=%s", name, p95)
		if p95 > inboxLatencyBudget {
			t.Errorf("help center hostile %s p95 %s exceeds %s", name, p95, inboxLatencyBudget)
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
