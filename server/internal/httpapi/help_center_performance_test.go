package httpapi_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestHelpCenterPagesLatencyBudget holds every public help center page to the inbox budget, HTML
// rendering included, at the article cap: the shared fixture of TestKnowledgeBaseSearchLatencyBudget,
// two workspaces of domain.MaxKBArticles articles each (half published, 20 of them 100,000 characters
// long), all in one collection, help centers on, workspace A's with a full theme. It times the home
// page, the first and last page of that collection, a short and a 100,000-character article,
// searches, the brand style sheet and the logo.
func TestHelpCenterPagesLatencyBudget(t *testing.T) {
	_, pool, _, slug := sharedKBBudget(t)
	storage, err := attachments.NewLocalDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	api, _, _, _ := testAPIOnPool(t, pool, func(config *httpapi.Config) { config.Attachments = storage })
	site := helpCenterSite(api)
	word := queryString(t, pool, `SELECT substr(md5('1234'),1,6)`)
	other := queryString(t, pool, `SELECT substr(md5('777'),1,5)`)
	base := "/help/" + slug
	logo := brandKBBudget(t, pool, storage, slug)
	brand := brandSheet(t, visit(site, base, "198.51.100.1"))
	if brand == "" {
		t.Fatal("the themed help center links no brand style sheet")
	}
	paths := map[string]string{
		"brand style sheet":  brand,
		"logo":               base + "/_/logo/" + logo,
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

// brandColumns are the columns of help_centers brandKBBudget changes and puts back.
const brandColumns = `logo_media_id, favicon_media_id, logo_alt, linkback_url, contact_url, contact_phone, color_primary, color_header,
	color_header_text, color_background, color_text, color_link, color_footer, body_font, heading_font, content_version`

// brandKBBudget gives workspace A of the shared fixture a full theme whose logo and favicon are an
// image of its own, and returns that image's id. The cleanup removes the image and puts A's help
// center back as it was, so the other gates on the fixture see it as seeded whatever order they run in.
func brandKBBudget(t *testing.T, pool *pgxpool.Pool, storage attachments.Storage, slug string) string {
	t.Helper()
	ctx := context.Background()
	workspaceID := queryString(t, pool, `SELECT id::text FROM workspaces WHERE slug=$1`, slug)
	saved := queryString(t, pool, `SELECT to_jsonb(h)::text FROM help_centers h WHERE workspace_id=$1`, workspaceID)
	object, err := storage.Put(ctx, workspaceID, bytes.NewReader(kbPNG), int64(len(kbPNG)))
	if err != nil {
		t.Fatal(err)
	}
	logo := queryString(t, pool, `INSERT INTO kb_media(workspace_id,storage_key,sha256,size,content_type,name,width,height,inspected_at)
		VALUES ($1,$2,$3,$4,'image/png','Budget logo',2,2,now()) RETURNING id::text`, workspaceID, object.Key, object.SHA256, object.Size)
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, `UPDATE help_centers SET (`+brandColumns+`) = (SELECT `+brandColumns+` FROM jsonb_populate_record(NULL::help_centers, $2::jsonb))
			WHERE workspace_id=$1`, workspaceID, saved); err != nil {
			t.Errorf("put the help center back: %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM kb_media WHERE id=$1`, logo); err != nil {
			t.Errorf("remove the logo: %v", err)
		}
	})
	if _, err := pool.Exec(ctx, `UPDATE help_centers SET logo_media_id=$2, favicon_media_id=$2, logo_alt='KB Budget', linkback_url='https://budget.example/',
		contact_url='mailto:help@budget.example', contact_phone='+1 (555) 010-2030', color_primary='#0b5cad', color_header='#0b5cad',
		color_header_text='#ffffff', color_background='#ffffff', color_text='#1f2328', color_link='#0b5cad', color_footer='#f6f8fa',
		body_font='humanist-sans', heading_font='transitional-serif', content_version=content_version+1 WHERE workspace_id=$1`, workspaceID, logo); err != nil {
		t.Fatal(err)
	}
	return logo
}

// TestHelpCenterHostileArticlesLatencyBudget holds an article page to the same budget, its first
// (cold) render included, when its text is built to be slow to parse (docs/internals/help-center.md,
// Traps): deep nesting, open links, same-named headings and Linkify's e-mail search are tamed.
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
		"strong":         strings.Repeat("**a", 33333),
	}
	for name, body := range bodies {
		article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": name, "body_md": body, "status": "published"})
		path := fmt.Sprintf("/help/%s/a/%d-%s", slug, article.Number, article.Slug)
		// The first visit renders the text cold: it is held to the budget too, and must be the
		// rendered page, not the text shown while a render runs late.
		started := time.Now()
		first := visit(site, path, "198.51.100.250").Body.String()
		cold := time.Since(started)
		t.Logf("help center hostile %s cold=%s", name, cold)
		if cold > inboxLatencyBudget || strings.Contains(first, `class="plain-note"`) {
			t.Errorf("help center hostile %s rendered cold in %s (budget %s)", name, cold, inboxLatencyBudget)
		}
		// 20 visits each, not 50, since this gate shares its CI runner with every other latency gate.
		p95 := helpCenterPercentile(t, site, path, 20)
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
	return helpCenterPercentile(t, site, path, 50)
}

// helpCenterPercentile loads path visits times and returns the 95th percentile.
func helpCenterPercentile(t *testing.T, site http.Handler, path string, visits int) time.Duration {
	t.Helper()
	durations := make([]time.Duration, visits)
	for index := range durations {
		started := time.Now()
		response := visit(site, path, fmt.Sprintf("198.51.100.%d", index+1))
		durations[index] = time.Since(started)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%.300s", path, response.Code, response.Body.String())
		}
	}
	sort.Slice(durations, func(left, right int) bool { return durations[left] < durations[right] })
	return durations[(visits*95+99)/100-1]
}
