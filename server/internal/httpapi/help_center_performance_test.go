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
// built to be slow to parse (docs/internals/help-center.md, Traps): deep nesting, open links and
// same-named headings are tamed; what is left (emphasis runs, about 0.7 s) renders once, and the page
// is kept for the next visits.
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
		"same headings":  strings.Repeat("# a\n", 25000),
		"setext":         strings.Repeat("a\n---\n", 16666),
		"open links":     strings.Repeat("[a](", 25000),
		"open images":    strings.Repeat("![](", 20000),
		"emphasis":       strings.Repeat("*a", 50000),
		"strikethrough":  strings.Repeat("~a", 50000),
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

// The rendered pages kept in memory are per workspace: two help centers with the same article text
// each show their own image addresses.
func TestHelpCenterRenderedPagesAreNeverSharedAcrossWorkspaces(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	body := "![Screen](/api/v1/kb/images/0b6f8a8e-3c4d-4e5f-8a9b-0c1d2e3f4a5b)"
	slugs := map[string]string{}
	for _, name := range []string{"Render One", "Render Two"} {
		admin := createSession(t, api, service, name, "admin@"+strings.ReplaceAll(strings.ToLower(name), " ", "-")+".example.com")
		slugs[name] = workspaceSlug(t, api, admin)
		enableHelpCenter(t, api, admin, "", true)
		collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
		kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Same", "body_md": body, "status": "published"})
	}
	for round := range 2 {
		for name, slug := range slugs {
			page := visit(site, "/help/"+slug+"/a/1-same", fmt.Sprintf("198.51.100.%d", 200+round)).Body.String()
			for other, otherSlug := range slugs {
				if holds := strings.Contains(page, "/help/"+otherSlug+"/images/"); holds != (other == name) {
					t.Errorf("round %d: %s's page holding %s's image address = %v", round, name, other, holds)
				}
			}
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
