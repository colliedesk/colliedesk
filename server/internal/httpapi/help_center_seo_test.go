package httpapi_test

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// seoHost is the help center domain of the tests below.
const seoHost = "help.example.net"

// TestHelpCenterHostServesItsOwnRobotsSitemapAndLLMsFiles: robots.txt, sitemap.xml, llms.txt and the
// Markdown of each article belong to the workspace of the host that serves them and list only that
// host's addresses. A trial's robots.txt refuses every crawler and its files list nothing, as its
// pages are noindex.
func TestHelpCenterHostServesItsOwnRobotsSitemapAndLLMsFiles(t *testing.T) {
	f := newLimitsFixtureConfigured(t, true, nil, func(config *httpapi.Config) { config.HelpCenterDomain = seoHost })
	site := helpCenterSite(f.api)
	slugA, slugB := workspaceSlug(t, f.api, f.adminA), workspaceSlug(t, f.api, f.adminB)
	hostA, hostB := slugA+"."+seoHost, slugB+"."+seoHost
	enableHelpCenter(t, f.api, f.adminA, "Alpha help", true)
	enableHelpCenter(t, f.api, f.adminB, "Bravo help", true)
	guides := kbCollection(t, f.api, f.adminA, map[string]any{"name": "Guides"})
	image := uploadKBImage(t, f.api, f.adminA, kbPNG, http.StatusCreated)
	start := kbArticle(t, f.api, f.adminA, map[string]any{"collection_id": guides.ID, "title": "Start here", "status": "published",
		"body_md": "Sign in **first**.\n\n<!-- internal: ask Sam -->\n\n![Form](" + image.URL + ")"})
	draft := kbArticle(t, f.api, f.adminA, map[string]any{"collection_id": guides.ID, "title": "Secret draft", "body_md": "marigold"})
	bravo := kbCollection(t, f.api, f.adminB, map[string]any{"name": "Bravo shelf"})
	bravoArticle := kbArticle(t, f.api, f.adminB, map[string]any{"collection_id": bravo.ID, "title": "Bravo only", "status": "published", "body_md": "bravo"})
	f.upgrade(t, f.workspaceA, "sub_seo_alpha")

	startURL := fmt.Sprintf("https://%s/help/a/%d-%s", hostA, start.Number, start.Slug)
	robots := visit(site, "/robots.txt", "192.0.2.80", "Host", hostA)
	if robots.Code != http.StatusOK || robots.Body.String() != "User-agent: *\nDisallow:\n\nSitemap: https://"+hostA+"/help/sitemap.xml\n" {
		t.Fatalf("robots.txt status=%d body=%q", robots.Code, robots.Body.String())
	}
	sitemap := visit(site, "/help/sitemap.xml", "192.0.2.80", "Host", hostA)
	if !strings.HasPrefix(sitemap.Header().Get("Content-Type"), "application/xml") {
		t.Fatalf("sitemap type=%q", sitemap.Header().Get("Content-Type"))
	}
	wantPage(t, sitemap, http.StatusOK, []string{`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`,
		"<loc>https://" + hostA + "/help</loc>", "<loc>https://" + hostA + "/help/c/guides</loc>", "<loc>" + startURL + "</loc>", "<lastmod>"},
		[]string{hostB, testOriginHost, "Secret draft", fmt.Sprintf("/a/%d-", draft.Number) + draft.Slug, "Bravo"})
	if got := strings.Count(sitemap.Body.String(), "<url>"); got != 3 {
		t.Fatalf("sitemap lists %d addresses, want 3:\n%s", got, sitemap.Body.String())
	}
	llms := visit(site, "/help/llms.txt", "192.0.2.80", "Host", hostA)
	if !strings.HasPrefix(llms.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("llms.txt type=%q", llms.Header().Get("Content-Type"))
	}
	wantPage(t, llms, http.StatusOK, []string{"# Alpha help\n", "\n## Guides\n", "- [Start here](" + startURL + ".md)"},
		[]string{hostB, testOriginHost, "Secret draft", "Bravo"})

	md := visit(site, fmt.Sprintf("/help/a/%d-%s.md", start.Number, start.Slug), "192.0.2.80", "Host", hostA)
	if !strings.HasPrefix(md.Header().Get("Content-Type"), "text/markdown") || md.Header().Get("Link") != "<"+startURL+`>; rel="canonical"` {
		t.Fatalf("markdown type=%q link=%q", md.Header().Get("Content-Type"), md.Header().Get("Link"))
	}
	wantPage(t, md, http.StatusOK, []string{"# Start here\n", "Sign in **first**.", "![Form](https://" + hostA + "/help/images/" + image.ID + ")"},
		[]string{"/api/v1/", "internal: ask Sam", testOriginHost})
	// The number finds the article; another slug moves to the current Markdown address.
	if moved := visit(site, fmt.Sprintf("/help/a/%d-old.md", start.Number), "192.0.2.80", "Host", hostA); moved.Code != http.StatusMovedPermanently ||
		moved.Header().Get("Location") != fmt.Sprintf("/help/a/%d-%s.md", start.Number, start.Slug) {
		t.Fatalf("old markdown address status=%d location=%q", moved.Code, moved.Header().Get("Location"))
	}
	// Another workspace's article and a draft have no Markdown on this host.
	for _, path := range []string{fmt.Sprintf("/a/%d-%s.md", draft.Number, draft.Slug), fmt.Sprintf("/a/%d-%s.md", bravoArticle.Number+50, bravoArticle.Slug)} {
		wantPage(t, visit(site, "/help"+path, "192.0.2.80", "Host", hostA), http.StatusNotFound, nil, []string{"Bravo only", "marigold"})
	}

	// B is still on its trial: nothing for crawlers.
	if robots := visit(site, "/robots.txt", "192.0.2.81", "Host", hostB); robots.Body.String() != "User-agent: *\nDisallow: /\n" {
		t.Fatalf("trial robots.txt=%q", robots.Body.String())
	}
	trialMap := visit(site, "/help/sitemap.xml", "192.0.2.81", "Host", hostB)
	wantPage(t, trialMap, http.StatusOK, []string{"<urlset"}, []string{"<url>", "Bravo"})
	trialLLMs := visit(site, "/help/llms.txt", "192.0.2.81", "Host", hostB)
	wantPage(t, trialLLMs, http.StatusOK, []string{"# Bravo help"}, []string{"Bravo only"})
	trialMD := visit(site, fmt.Sprintf("/help/a/%d-%s.md", bravoArticle.Number, bravoArticle.Slug), "192.0.2.81", "Host", hostB)
	if trialMD.Code != http.StatusOK || trialMD.Header().Get("X-Robots-Tag") != "noindex" || trialMap.Header().Get("X-Robots-Tag") != "noindex" {
		t.Fatalf("trial markdown status=%d robots=%q, sitemap robots=%q", trialMD.Code, trialMD.Header().Get("X-Robots-Tag"), trialMap.Header().Get("X-Robots-Tag"))
	}
	// The path addresses move to the host, files included.
	if moved := visit(site, "/help/"+slugA+"/sitemap.xml", "192.0.2.82"); moved.Code != http.StatusMovedPermanently || moved.Header().Get("Location") != "https://"+hostA+"/help/sitemap.xml" {
		t.Fatalf("path sitemap status=%d location=%q", moved.Code, moved.Header().Get("Location"))
	}
}

// absoluteURL finds every absolute address a page names, in an attribute or in text.
var absoluteURL = regexp.MustCompile(`(?:https?:)?//[A-Za-z0-9.-]+(?::[0-9]+)?`)

// TestHelpCenterPagesNameOnlyTheirServingHost walks every page type of a help center on its slug host
// and on its custom domain: canonical, og:url, hreflang, internal links and files name only the host
// that serves them, never the app's host or another workspace's.
func TestHelpCenterPagesNameOnlyTheirServingHost(t *testing.T) {
	api, service, _, dns := testAPIWithConfig(t, func(config *httpapi.Config) { config.HelpCenterDomain = seoHost })
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Walk Alpha", "admin@walk-alpha.example.com")
	other := createSession(t, api, service, "Walk Bravo", "admin@walk-bravo.example.com")
	slug, otherSlug := workspaceSlug(t, api, admin), workspaceSlug(t, api, other)
	enableHelpCenter(t, api, admin, "Walk help", true)
	enableHelpCenter(t, api, other, "Other help", true)
	guides := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	cards := kbCollection(t, api, admin, map[string]any{"name": "Cards", "parent_id": guides.ID})
	image := uploadKBImage(t, api, admin, kbPNG, http.StatusCreated)
	article := kbArticle(t, api, admin, map[string]any{"collection_id": cards.ID, "title": "Pay by card", "status": "published",
		"body_md": "Pay by card.\n\n![Card](" + image.URL + ")"})
	otherCollection := kbCollection(t, api, other, map[string]any{"name": "Other shelf"})
	kbArticle(t, api, other, map[string]any{"collection_id": otherCollection.ID, "title": "Other article", "status": "published", "body_md": "other"})
	articlePath := fmt.Sprintf("/a/%d-%s", article.Number, article.Slug)
	pages := []string{"/", "/c/guides", "/c/guides/cards", articlePath, articlePath + ".md", "/search?q=card", "/nothing-here",
		"/robots.txt", "/sitemap.xml", "/llms.txt"}

	// base is where the help center lives on host: /help on the workspace's own host name, the root of a
	// custom domain. robots.txt is always at the root.
	walk := func(host, base string) {
		t.Helper()
		forbidden := []string{testOriginHost, otherSlug + "." + seoHost}
		if base == "" {
			forbidden = append(forbidden, "/help/")
		}
		at := func(path string) string {
			if path == "/robots.txt" {
				return path
			}
			if path == "/" && base != "" {
				return base
			}
			return base + path
		}
		home := "https://" + host + at("/")
		for _, path := range pages {
			response := visit(site, at(path), "192.0.2.90", "Host", host)
			if response.Code != http.StatusOK && path != "/nothing-here" {
				t.Fatalf("%s%s status=%d", host, path, response.Code)
			}
			body := response.Body.String()
			for _, found := range absoluteURL.FindAllString(body, -1) {
				if name := strings.TrimLeft(strings.TrimPrefix(strings.TrimPrefix(found, "https:"), "http:"), "/"); name != host && name != "www.sitemaps.org" && name != "schema.org" {
					t.Errorf("%s%s names %q", host, path, found)
				}
			}
			for _, unwanted := range forbidden {
				if strings.Contains(body, unwanted) {
					t.Errorf("%s%s names %q", host, path, unwanted)
				}
			}
		}
		homePage := visit(site, at("/"), "192.0.2.90", "Host", host).Body.String()
		for _, want := range []string{`<link rel="canonical" href="` + home + `">`, `<meta property="og:url" content="` + home + `">`,
			`<link rel="alternate" hreflang="en" href="` + home + `">`, `<link rel="alternate" hreflang="x-default" href="` + home + `">`} {
			if !strings.Contains(homePage, want) {
				t.Errorf("home on %s lacks %s:\n%s", host, want, homePage)
			}
		}
		page := visit(site, at(articlePath), "192.0.2.90", "Host", host).Body.String()
		for _, want := range []string{`<link rel="canonical" href="https://` + host + at(articlePath) + `">`,
			`<link rel="alternate" hreflang="en" href="https://` + host + at(articlePath) + `">`,
			`<link rel="alternate" type="text/markdown" href="https://` + host + at(articlePath) + `.md">`} {
			if !strings.Contains(page, want) {
				t.Errorf("article on %s lacks %s", host, want)
			}
		}
	}
	walk(slug+"."+seoHost, "/help")

	const custom = "help.walk-alpha.example"
	view := setDomain(t, api, admin, custom, http.StatusOK)
	dns.set("_help-center-verify."+custom, view.token())
	checkDomain(t, api, admin, http.StatusOK)
	walk(custom, "")
	// The workspace's host now moves its help center to the custom domain, every file with it.
	for old, path := range map[string]string{"/help": "/", "/robots.txt": "/robots.txt", "/help/sitemap.xml": "/sitemap.xml", "/help/llms.txt": "/llms.txt",
		"/help" + articlePath + ".md": articlePath + ".md"} {
		moved := visit(site, old, "192.0.2.91", "Host", slug+"."+seoHost)
		if moved.Code != http.StatusMovedPermanently || moved.Header().Get("Location") != "https://"+custom+path {
			t.Errorf("slug host %s status=%d location=%q", path, moved.Code, moved.Header().Get("Location"))
		}
	}
}

// TestHelpCenterNonCanonicalAddressesMove: every address that would show a page under another
// address answers 301 to the canonical one, never the same page with a canonical tag.
func TestHelpCenterNonCanonicalAddressesMove(t *testing.T) {
	api, service := testAPI(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Canon", "admin@canon.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Canon help", true)
	guides := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": guides.ID, "title": "Start here", "status": "published", "body_md": "Hello"})
	base := "/help/" + slug
	articlePath := fmt.Sprintf("%s/a/%d-%s", base, article.Number, article.Slug)
	for from, to := range map[string]string{
		base + "/":                                   base,
		base + "?utm_source=x":                       base,
		base + "/c/guides/":                          base + "/c/guides",
		base + "/c//guides":                          base + "/c/guides",
		base + "/c/guides?page=1":                    base + "/c/guides",
		base + "/c/guides?ref=mail":                  base + "/c/guides",
		articlePath + "?utm_source=x":                articlePath,
		articlePath + ".md?ref=x":                    articlePath + ".md",
		fmt.Sprintf("%s/a/%d", base, article.Number): articlePath,
	} {
		moved := visit(site, from, "192.0.2.95")
		if moved.Code != http.StatusMovedPermanently || moved.Header().Get("Location") != to {
			t.Errorf("%s status=%d location=%q, want 301 to %s", from, moved.Code, moved.Header().Get("Location"), to)
		}
	}
	for _, path := range []string{base, base + "/c/guides", articlePath, articlePath + ".md", base + "/search?q=start", base + "/sitemap.xml", base + "/llms.txt"} {
		if response := visit(site, path, "192.0.2.96"); response.Code != http.StatusOK {
			t.Errorf("%s status=%d", path, response.Code)
		}
	}
}
