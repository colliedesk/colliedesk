package httpapi_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// helpCenterSite is the handler a server mounts in front of everything else.
func helpCenterSite(api http.Handler) http.Handler {
	return api.(*httpapi.Server).WrapHelpCenter(api)
}

// visit loads a public help center address with no session, from client (an IP address).
func visit(site http.Handler, path, client string, header ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = client + ":4242"
	for index := 0; index+1 < len(header); index += 2 {
		if strings.EqualFold(header[index], "Host") {
			req.Host = header[index+1]
			continue
		}
		req.Header.Set(header[index], header[index+1])
	}
	return serve(site, req)
}

func wantPage(t *testing.T, response *httptest.ResponseRecorder, status int, holds []string, lacks []string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status=%d, want %d; body=%s", response.Code, status, response.Body.String())
	}
	body := response.Body.String()
	for _, want := range holds {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q:\n%s", want, body)
		}
	}
	for _, unwanted := range lacks {
		if strings.Contains(body, unwanted) {
			t.Errorf("page shows %q:\n%s", unwanted, body)
		}
	}
}

// otherPNG is an image of the same type other than kbPNG.
func otherPNG(n byte) []byte { return pngImage(100 + n) }

func enableHelpCenter(t *testing.T, api http.Handler, admin, title string, enabled bool) {
	t.Helper()
	kbCall(t, api, http.MethodPut, "/api/v1/help-center", map[string]any{"enabled": enabled, "title": title, "locale": "en"}, admin, http.StatusOK, nil)
}

func TestHelpCenterShowsOnlyPublishedArticlesOfItsOwnWorkspace(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	adminA := createSession(t, api, service, "Help Alpha", "admin@help-alpha.example.com")
	adminB := createSession(t, api, service, "Help Bravo", "admin@help-bravo.example.com")
	slugA, slugB := workspaceSlug(t, api, adminA), workspaceSlug(t, api, adminB)
	enableHelpCenter(t, api, adminA, "Alpha Answers", true)
	enableHelpCenter(t, api, adminB, "Bravo Answers", true)

	billing := kbCollection(t, api, adminA, map[string]any{"name": "Billing", "description": "Plans and invoices"})
	cards := kbCollection(t, api, adminA, map[string]any{"name": "Cards", "parent_id": billing.ID})
	internal := kbCollection(t, api, adminA, map[string]any{"name": "Internal roadmap"})
	shown := uploadKBImage(t, api, adminA, kbPNG, http.StatusCreated)
	hidden := uploadKBImage(t, api, adminA, otherPNG(1), http.StatusCreated)
	published := kbArticle(t, api, adminA, map[string]any{"collection_id": cards.ID, "title": "Pay by card", "status": "published",
		"body_md": "Use **any card**: pinewood.\n\n![Card form](" + shown.URL + ")"})
	draft := kbArticle(t, api, adminA, map[string]any{"collection_id": cards.ID, "title": "Draft pricing", "body_md": "marigold\n\n![Secret](" + hidden.URL + ")"})
	kbArticle(t, api, adminA, map[string]any{"collection_id": internal.ID, "title": "Next quarter", "body_md": "marigold"})
	trashed := kbArticle(t, api, adminA, map[string]any{"collection_id": billing.ID, "title": "Old refunds", "status": "published", "body_md": "juniper"})
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/articles/"+trashed.ID+"?updated_at="+url.QueryEscape(trashed.UpdatedAt), nil, adminA, http.StatusNoContent, nil)
	bravo := kbCollection(t, api, adminB, map[string]any{"name": "Bravo shelf"})
	bravoArticle := kbArticle(t, api, adminB, map[string]any{"collection_id": bravo.ID, "title": "Bravo only", "status": "published", "body_md": "pinewood bravo"})
	bravoImage := uploadKBImage(t, api, adminB, otherPNG(2), http.StatusCreated)
	kbArticle(t, api, adminB, map[string]any{"collection_id": bravo.ID, "title": "Bravo picture", "status": "published", "body_md": "![x](" + bravoImage.URL + ")"})

	secrets := []string{"Draft pricing", "Internal roadmap", "Next quarter", "marigold", "Old refunds", "Bravo", hidden.ID, "/api/v1/"}
	base := "/help/" + slugA
	wantPage(t, visit(site, base, "192.0.2.10"), http.StatusOK, []string{"Alpha Answers", "Billing", "Plans and invoices", "Cards"}, secrets)
	wantPage(t, visit(site, base+"/c/billing", "192.0.2.10"), http.StatusOK, []string{"Cards", "1 article"}, secrets)
	wantPage(t, visit(site, base+"/c/billing/cards", "192.0.2.10"), http.StatusOK, []string{"Pay by card"}, secrets)
	articlePath := fmt.Sprintf("%s/a/%d-%s", base, published.Number, published.Slug)
	wantPage(t, visit(site, articlePath, "192.0.2.10"), http.StatusOK,
		[]string{"Pay by card", "<strong>any card</strong>", base + "/images/" + shown.ID, `href="` + base + `/c/billing/cards"`}, secrets)
	wantPage(t, visit(site, base+"/search?q=pinewood", "192.0.2.10"), http.StatusOK, []string{"1 result", "Pay by card"}, secrets)
	wantPage(t, visit(site, base+"/search?q=marigold", "192.0.2.10"), http.StatusOK, []string{"No results for “marigold”"}, []string{"Draft pricing", "Next quarter"})
	wantPage(t, visit(site, base+"/search?q=bravo", "192.0.2.10"), http.StatusOK, []string{"No results"}, secrets)

	for _, path := range []string{
		fmt.Sprintf("%s/a/%d-%s", base, draft.Number, draft.Slug),
		fmt.Sprintf("%s/a/%d-%s", base, trashed.Number, trashed.Slug),
		fmt.Sprintf("%s/a/%d", base, bravoArticle.Number+100),
		base + "/c/internal-roadmap",
		base + "/c/cards",
		base + "/images/" + hidden.ID,
		base + "/images/" + bravoImage.ID,
		base + "/nothing-here",
	} {
		wantPage(t, visit(site, path, "192.0.2.10"), http.StatusNotFound, nil, secrets)
	}
	image := visit(site, base+"/images/"+shown.ID, "192.0.2.10")
	if image.Code != http.StatusOK || image.Body.String() != string(kbPNG) || image.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" ||
		image.Header().Get("Content-Security-Policy") != "default-src 'none'; sandbox" || image.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("published image status=%d headers=%v", image.Code, image.Header())
	}
	// Workspace B's own help center shows its articles and image, and nothing of A.
	wantPage(t, visit(site, "/help/"+slugB, "192.0.2.10"), http.StatusOK, []string{"Bravo shelf", "Bravo only"}, []string{"Billing", "Pay by card", "Alpha"})
	if response := visit(site, "/help/"+slugB+"/images/"+bravoImage.ID, "192.0.2.10"); response.Code != http.StatusOK {
		t.Fatalf("workspace B image status=%d", response.Code)
	}
	if response := visit(site, "/help/"+slugB+"/images/"+shown.ID, "192.0.2.10"); response.Code != http.StatusNotFound {
		t.Fatalf("workspace A image on B's help center status=%d", response.Code)
	}

	// An image the current text no longer shows is not public, even though an older version named it.
	edited := kbArticle(t, api, adminA, map[string]any{"collection_id": cards.ID, "title": "Edited", "status": "published", "body_md": "![](" + hidden.URL + ")"})
	if response := visit(site, base+"/images/"+hidden.ID, "192.0.2.10"); response.Code != http.StatusOK {
		t.Fatalf("image of a published article status=%d", response.Code)
	}
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+edited.ID, map[string]any{"updated_at": edited.UpdatedAt, "body_md": "No picture now."}, adminA, http.StatusOK, nil)
	if response := visit(site, base+"/images/"+hidden.ID, "192.0.2.10"); response.Code != http.StatusNotFound {
		t.Fatalf("image dropped from the text status=%d, want 404", response.Code)
	}
	// The address carries the slug, but the number finds the article.
	moved := visit(site, fmt.Sprintf("%s/a/%d-an-old-title?x=1", base, published.Number), "192.0.2.10")
	if moved.Code != http.StatusMovedPermanently || moved.Header().Get("Location") != articlePath {
		t.Fatalf("wrong slug status=%d location=%q, want 301 to %s", moved.Code, moved.Header().Get("Location"), articlePath)
	}
	var center struct {
		URL string `json:"url"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/help-center", nil, adminA, http.StatusOK, &center)
	if center.URL != testOrigin+base {
		t.Fatalf("help center url=%q, want %q", center.URL, testOrigin+base)
	}
	// The app and the API still answer around the help center.
	if response := request(t, site, http.MethodGet, "/api/v1/config", nil, "", nil); response.Code != http.StatusOK {
		t.Fatalf("API behind the help center status=%d", response.Code)
	}
}

func TestHelpCenterUnknownOffAndLockedAreOneNotFound(t *testing.T) {
	f := newLimitsFixture(t, true)
	site := helpCenterSite(f.api)
	slugA, slugB := workspaceSlug(t, f.api, f.adminA), workspaceSlug(t, f.api, f.adminB)
	enableHelpCenter(t, f.api, f.adminA, "Alpha help", true)
	collection := kbCollection(t, f.api, f.adminA, map[string]any{"name": "Guides"})
	kbArticle(t, f.api, f.adminA, map[string]any{"collection_id": collection.ID, "title": "Start here", "status": "published", "body_md": "Hello"})

	trial := visit(site, "/help/"+slugA, "192.0.2.20")
	wantPage(t, trial, http.StatusOK, []string{`<meta name="robots" content="noindex">`, "Start here"}, nil)
	if trial.Header().Get("X-Robots-Tag") != "noindex" {
		t.Fatalf("trial X-Robots-Tag=%q, want noindex", trial.Header().Get("X-Robots-Tag"))
	}
	// B pays: its pages may be indexed, and a page cached during the trial is stale.
	enableHelpCenter(t, f.api, f.adminB, "Bravo help", true)
	bravo := kbCollection(t, f.api, f.adminB, map[string]any{"name": "Bravo guides"})
	kbArticle(t, f.api, f.adminB, map[string]any{"collection_id": bravo.ID, "title": "Bravo start", "status": "published", "body_md": "Hi"})
	bravoTrial := visit(site, "/help/"+slugB, "192.0.2.20")
	f.upgrade(t, f.workspaceB, "sub_help_bravo")
	paid := visit(site, "/help/"+slugB, "192.0.2.20", "If-None-Match", bravoTrial.Header().Get("ETag"))
	wantPage(t, paid, http.StatusOK, []string{"Bravo start"}, []string{"noindex"})
	if paid.Header().Get("X-Robots-Tag") != "" {
		t.Fatalf("paid X-Robots-Tag=%q", paid.Header().Get("X-Robots-Tag"))
	}

	// B turns its help center off; A's trial ends unpaid, which locks it.
	enableHelpCenter(t, f.api, f.adminB, "Bravo help", false)
	off := visit(site, "/help/"+slugB, "192.0.2.21", "If-None-Match", paid.Header().Get("ETag"))
	unknown := visit(site, "/help/no-such-workspace", "192.0.2.22")
	malformed := visit(site, "/help/Not_A-Slug!", "192.0.2.23")
	f.clock.set(trialEnd)
	locked := visit(site, "/help/"+slugA, "192.0.2.24", "If-None-Match", trial.Header().Get("ETag"))
	lockedArticle := visit(site, "/help/"+slugA+"/a/1-start-here", "192.0.2.24")
	for name, response := range map[string]*httptest.ResponseRecorder{"off": off, "locked": locked, "locked article": lockedArticle, "malformed": malformed} {
		if response.Code != http.StatusNotFound || response.Body.String() != unknown.Body.String() {
			t.Errorf("%s: status=%d body=%s, want the unknown workspace's 404", name, response.Code, response.Body.String())
		}
		if fmt.Sprint(response.Header()) != fmt.Sprint(unknown.Header()) {
			t.Errorf("%s headers=%v, want %v", name, response.Header(), unknown.Header())
		}
	}
	if unknown.Header().Get("ETag") != "" || unknown.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("404 headers=%v, want no ETag and no-store", unknown.Header())
	}
}

func TestHelpCenterPagesRevalidateByContentVersion(t *testing.T) {
	api, service := testAPI(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Help Cache", "admin@help-cache.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Cache help", true)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	oldest := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Oldest guide", "status": "published", "body_md": "a"})
	kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Newest guide", "status": "published", "body_md": "b"})

	first := visit(site, "/help/"+slug, "192.0.2.30")
	etag := first.Header().Get("ETag")
	if first.Code != http.StatusOK || etag == "" || first.Header().Get("Cache-Control") != "public, max-age=0, must-revalidate" {
		t.Fatalf("home status=%d headers=%v", first.Code, first.Header())
	}
	if again := visit(site, "/help/"+slug, "192.0.2.30", "If-None-Match", etag); again.Code != http.StatusNotModified || again.Body.Len() != 0 {
		t.Fatalf("unchanged home status=%d, want 304", again.Code)
	}
	// Unpublishing the oldest article changes nothing a max(updated_at) would see; the version moves.
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+oldest.ID, map[string]any{"updated_at": oldest.UpdatedAt, "status": "draft"}, admin, http.StatusOK, nil)
	after := visit(site, "/help/"+slug, "192.0.2.30", "If-None-Match", etag)
	wantPage(t, after, http.StatusOK, []string{"Newest guide"}, []string{"Oldest guide"})
	if after.Header().Get("ETag") == etag {
		t.Fatal("the ETag stayed after an article was unpublished")
	}
	article := visit(site, fmt.Sprintf("/help/%s/a/%d-%s", slug, oldest.Number, oldest.Slug), "192.0.2.30", "If-None-Match", etag)
	if article.Code != http.StatusNotFound || article.Header().Get("ETag") != "" {
		t.Fatalf("unpublished article with an old ETag status=%d etag=%q, want 404 without one", article.Code, article.Header().Get("ETag"))
	}
	// Turning the help center off takes every page down at once, cached or not.
	enableHelpCenter(t, api, admin, "Cache help", false)
	if response := visit(site, "/help/"+slug, "192.0.2.30", "If-None-Match", after.Header().Get("ETag")); response.Code != http.StatusNotFound {
		t.Fatalf("help center turned off status=%d", response.Code)
	}
}

func TestHelpCenterPagesForbidScriptsFramingAndBranding(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Help Safe", "admin@help-safe.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "", true)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Tricky <b>title</b>", "status": "published",
		"body_md": "<script>alert(1)</script>\n\n[x](javascript:alert(1))\n\n<img src=x onerror=alert(1)>\n\n![a](https://tracker.example/pixel.gif)\n\n<iframe src=\"https://www.youtube.com/embed/jNQXAC9IVRw\" onload=\"alert(1)\"></iframe>"})
	base := "/help/" + slug
	page := visit(site, base, "192.0.2.40")
	logo := uploadKBImage(t, api, admin, kbPNG, http.StatusCreated)
	// Every guard holds with the default look and with every setting of a theme used.
	for _, themed := range []bool{false, true} {
		if themed {
			kbCall(t, api, http.MethodPut, themePath, fullTheme(logo.ID), admin, http.StatusOK, nil)
		}
		styles := map[string]bool{}
		for _, path := range []string{base, base + "/c/guides", fmt.Sprintf("%s/a/%d-%s", base, article.Number, article.Slug), base + "/search?q=tricky", base + "/missing", "/help/nobody"} {
			response := visit(site, path, "192.0.2.40")
			header := response.Header()
			if got := header.Get("Content-Security-Policy"); got != "default-src 'none'; img-src 'self'; style-src 'self'; frame-src https://www.youtube-nocookie.com https://player.vimeo.com; media-src 'self'; script-src 'none'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'" {
				t.Errorf("themed=%v %s CSP=%q", themed, path, got)
			}
			if header.Get("X-Content-Type-Options") != "nosniff" || header.Get("X-Frame-Options") != "DENY" || header.Get("Content-Type") != "text/html; charset=utf-8" {
				t.Errorf("themed=%v %s headers=%v", themed, path, header)
			}
			body := strings.ToLower(response.Body.String())
			for _, bad := range []string{"<script", "onerror", "javascript:", "style=", "colliedesk", "<b>title", "<img src=\"https://"} {
				if strings.Contains(body, bad) {
					t.Errorf("themed=%v %s holds %q:\n%s", themed, path, bad, body)
				}
			}
			if strings.Contains(path, "/a/") && !strings.Contains(body, `<iframe src="https://www.youtube-nocookie.com/embed/jnqxac9ivrw" title="youtube video" allow="encrypted-media; picture-in-picture" sandbox="allow-scripts allow-same-origin allow-presentation allow-popups" referrerpolicy="strict-origin-when-cross-origin" loading="lazy"></iframe>`) {
				t.Errorf("themed=%v %s shows no sandboxed player:\n%s", themed, path, body)
			}
			sheets := stylesheets(body)
			if len(sheets) == 0 {
				t.Errorf("themed=%v %s has no style sheet", themed, path)
			}
			for _, sheet := range sheets {
				styles[sheet] = true
			}
		}
		if brand := len(styles) > 2; brand != themed {
			t.Errorf("themed=%v style sheets %v", themed, styles)
		}
		for style := range styles {
			sheet := visit(site, style, "192.0.2.40")
			body := strings.ToLower(sheet.Body.String())
			if sheet.Code != http.StatusOK || sheet.Header().Get("Content-Type") != "text/css; charset=utf-8" ||
				sheet.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" || strings.Contains(body, "colliedesk") ||
				strings.Contains(body, "url(") || strings.Contains(body, "@import") || strings.Contains(body, "<") {
				t.Fatalf("style sheet %s status=%d headers=%v", style, sheet.Code, sheet.Header())
			}
		}
	}
	// The workspace name stands in for an empty title.
	wantPage(t, page, http.StatusOK, []string{"Help Safe"}, nil)
	if response := serve(site, httptest.NewRequest(http.MethodPost, base+"/search", nil)); response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status=%d, want 405", response.Code)
	}
}

func TestHelpCenterOnItsOwnHostAndTheCertificateQuestion(t *testing.T) {
	f := newLimitsFixtureConfigured(t, true, nil, func(config *httpapi.Config) { config.HelpCenterDomain = "help.example.net" })
	site := helpCenterSite(f.api)
	slugA, slugB := workspaceSlug(t, f.api, f.adminA), workspaceSlug(t, f.api, f.adminB)
	enableHelpCenter(t, f.api, f.adminA, "Alpha help", true)
	collection := kbCollection(t, f.api, f.adminA, map[string]any{"name": "Guides"})
	article := kbArticle(t, f.api, f.adminA, map[string]any{"collection_id": collection.ID, "title": "Start here", "status": "published", "body_md": "Hello"})
	host := slugA + ".help.example.net"

	wantPage(t, visit(site, "/", "192.0.2.50", "Host", host), http.StatusOK, []string{"Alpha help", `href="/c/guides"`}, nil)
	wantPage(t, visit(site, fmt.Sprintf("/a/%d-%s", article.Number, article.Slug), "192.0.2.50", "Host", strings.ToUpper(host)+":443"), http.StatusOK, []string{"Start here"}, nil)
	robots := visit(site, "/robots.txt", "192.0.2.50", "Host", host)
	if robots.Code != http.StatusOK || robots.Body.String() != "User-agent: *\nDisallow: /\n" {
		t.Fatalf("trial robots.txt status=%d body=%q", robots.Code, robots.Body.String())
	}
	// Nothing of the app is reachable on a help center host.
	for _, path := range []string{"/api/v1/config", "/mcp", "/inbox"} {
		if response := visit(site, path, "192.0.2.50", "Host", host); response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "Alpha help") {
			t.Errorf("%s on the help center host status=%d", path, response.Code)
		}
	}
	moved := visit(site, fmt.Sprintf("/help/%s/a/%d-%s?q=1", slugA, article.Number, article.Slug), "192.0.2.50")
	if want := fmt.Sprintf("https://%s/a/%d-%s?q=1", host, article.Number, article.Slug); moved.Code != http.StatusMovedPermanently || moved.Header().Get("Location") != want {
		t.Fatalf("path address status=%d location=%q, want 301 to %s", moved.Code, moved.Header().Get("Location"), want)
	}
	if home := visit(site, "/help/"+slugA, "192.0.2.50"); home.Header().Get("Location") != "https://"+host+"/" {
		t.Fatalf("path home location=%q", home.Header().Get("Location"))
	}
	var center struct {
		URL string `json:"url"`
	}
	kbCall(t, f.api, http.MethodGet, "/api/v1/help-center", nil, f.adminA, http.StatusOK, &center)
	if center.URL != "https://"+host {
		t.Fatalf("help center url=%q", center.URL)
	}

	ask := func(domain string) int {
		t.Helper()
		path := "/api/v1/public/help-center/tls-ask?domain=" + url.QueryEscape(domain)
		req := newRequest(t, http.MethodGet, path, nil, "", nil)
		validateContractRequest(t, req)
		response := serve(site, req)
		validateContractResponse(t, newRequest(t, http.MethodGet, path, nil, "", nil), response)
		return response.Code
	}
	for domain, want := range map[string]int{
		host:                             http.StatusOK,
		strings.ToUpper(host):            http.StatusOK,
		slugB + ".help.example.net":      http.StatusNotFound, // off
		"nobody.help.example.net":        http.StatusNotFound,
		"www.help.example.net":           http.StatusNotFound, // reserved
		"a." + host:                      http.StatusNotFound,
		"help.example.net":               http.StatusNotFound,
		slugA + ".help.example.net.evil": http.StatusNotFound,
		slugA + ".example.org":           http.StatusNotFound,
		"":                               http.StatusNotFound,
	} {
		if got := ask(domain); got != want {
			t.Errorf("tls-ask %q = %d, want %d", domain, got, want)
		}
	}
	// A workspace whose slug is now reserved (here: an own DNS record) keeps signing in and keeps its
	// help center on the path address; it never gets a host name or a certificate.
	staging := createSession(t, f.api, f.billing.service, "Staging", "admin@staging-team.example.com")
	if slug := workspaceSlug(t, f.api, staging); slug != "staging" {
		t.Fatalf("slug=%q, want staging", slug)
	}
	enableHelpCenter(t, f.api, staging, "Staging help", true)
	kbCall(t, f.api, http.MethodGet, "/api/v1/help-center", nil, staging, http.StatusOK, &center)
	if center.URL != testOrigin+"/help/staging" {
		t.Fatalf("reserved slug help center url=%q", center.URL)
	}
	wantPage(t, visit(site, "/help/staging", "192.0.2.52"), http.StatusOK, []string{"Staging help"}, nil)
	if got := ask("staging.help.example.net"); got != http.StatusNotFound {
		t.Fatalf("tls-ask for a reserved slug = %d", got)
	}
	f.clock.set(trialEnd)
	if got := ask(host); got != http.StatusNotFound {
		t.Fatalf("tls-ask for a locked workspace = %d, want 404", got)
	}
	if response := visit(site, "/", "192.0.2.51", "Host", host); response.Code != http.StatusNotFound || strings.Contains(response.Body.String(), "Alpha") {
		t.Fatalf("locked help center host status=%d", response.Code)
	}
}

func TestHelpCenterTLSAskAnswersNothingWithoutADomain(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Help Plain", "admin@help-plain.example.com")
	enableHelpCenter(t, api, admin, "", true)
	slug := workspaceSlug(t, api, admin)
	for _, domain := range []string{slug, slug + ".example.com", "desk.example.com"} {
		if response := request(t, api, http.MethodGet, "/api/v1/public/help-center/tls-ask?domain="+domain, nil, "", nil); response.Code != http.StatusNotFound {
			t.Errorf("tls-ask %q without HELP_CENTER_DOMAIN status=%d", domain, response.Code)
		}
	}
}

func TestHelpCenterSearchIsLimitedPerAddress(t *testing.T) {
	api, service := testAPI(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Help Busy", "admin@help-busy.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "", true)
	for index := range 30 {
		if response := visit(site, "/help/"+slug+"/search?q=word", "192.0.2.60"); response.Code != http.StatusOK {
			t.Fatalf("search %d status=%d", index, response.Code)
		}
	}
	busy := visit(site, "/help/"+slug+"/search?q=word", "192.0.2.60")
	if busy.Code != http.StatusTooManyRequests || busy.Header().Get("Retry-After") == "" {
		t.Fatalf("31st search status=%d headers=%v, want 429 with Retry-After", busy.Code, busy.Header())
	}
	if response := visit(site, "/help/"+slug+"/search?q=word", "192.0.2.61"); response.Code != http.StatusOK {
		t.Fatalf("another address status=%d", response.Code)
	}
	if response := visit(site, "/help/"+slug, "192.0.2.60"); response.Code != http.StatusOK {
		t.Fatalf("a page after the search limit status=%d", response.Code)
	}
}

func TestHelpCenterSearchTakesAnyTextAndPagesDescribeThemselves(t *testing.T) {
	api, service := testAPI(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Help Meta", "admin@help-meta.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Meta help", true)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides", "description": "How to start"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Start here", "status": "published", "body_md": "Sign in as an **admin**.\n\nThen open it."})
	base := "/help/" + slug
	// Postgres refuses NUL and invalid UTF-8; the visitor gets a page, never a 500.
	for _, query := range []string{"%00", "a%00b", "%ff", "start%ff%00"} {
		wantPage(t, visit(site, base+"/search?q="+query, "192.0.2.70"), http.StatusOK, nil, []string{"Something went wrong"})
	}
	wantPage(t, visit(site, base+"/search?q=start%00", "192.0.2.70"), http.StatusOK, []string{"Start here", "Sign in as an admin. Then open it.", `<meta name="robots" content="noindex">`}, nil)
	articleAddress := fmt.Sprintf("%s%s/a/%d-%s", testOrigin, base, article.Number, article.Slug)
	wantPage(t, visit(site, fmt.Sprintf("%s/a/%d-%s", base, article.Number, article.Slug), "192.0.2.70"), http.StatusOK,
		[]string{`<link rel="canonical" href="` + articleAddress + `">`, `<meta name="description" content="Sign in as an admin. Then open it.">`}, []string{"noindex"})
	wantPage(t, visit(site, base+"/c/guides", "192.0.2.70"), http.StatusOK,
		[]string{`<link rel="canonical" href="` + testOrigin + base + `/c/guides">`, `<meta name="description" content="How to start">`}, nil)
	wantPage(t, visit(site, base, "192.0.2.70"), http.StatusOK, []string{`<link rel="canonical" href="` + testOrigin + base + `">`}, nil)
}

// Every answer that carries an article carries its public address, the one the help center serves it
// at, on the path of a self-hosted server and on the workspace's own host name when the server has a
// help center domain. A person or an AI agent puts that address in a reply.
func TestEveryArticleAnswerCarriesTheAddressTheHelpCenterServesItAt(t *testing.T) {
	for _, mode := range []struct {
		name, domain string
	}{{"path", ""}, {"host", "help.example.net"}} {
		t.Run(mode.name, func(t *testing.T) {
			f := newLimitsFixtureConfigured(t, true, nil, func(config *httpapi.Config) { config.HelpCenterDomain = mode.domain })
			site := helpCenterSite(f.api)
			slugA, slugB := workspaceSlug(t, f.api, f.adminA), workspaceSlug(t, f.api, f.adminB)
			home := testOrigin + "/help/" + slugA
			if mode.domain != "" {
				home = "https://" + slugA + "." + mode.domain
			}
			enableHelpCenter(t, f.api, f.adminA, "Alpha help", true)
			collection := kbCollection(t, f.api, f.adminA, map[string]any{"name": "Guides"})
			created := kbArticle(t, f.api, f.adminA, map[string]any{"collection_id": collection.ID, "title": "Đặt lại mật khẩu", "status": "published", "body_md": "Steps"})
			want := fmt.Sprintf("%s/a/%d-%s", home, created.Number, url.PathEscape(created.Slug))
			if created.URL != want {
				t.Fatalf("create url=%q, want %q", created.URL, want)
			}
			address, err := url.Parse(created.URL)
			if err != nil {
				t.Fatal(err)
			}
			wantPage(t, visit(site, address.RequestURI(), "192.0.2.60", "Host", address.Host), http.StatusOK, []string{"Đặt lại mật khẩu", "Steps"}, nil)

			var got kbArticleView
			kbCall(t, f.api, http.MethodGet, "/api/v1/kb/articles/"+created.ID, nil, f.adminA, http.StatusOK, &got)
			var page struct {
				Items []kbArticleView `json:"items"`
			}
			kbCall(t, f.api, http.MethodGet, "/api/v1/kb/articles?status=published&q="+url.QueryEscape("mật"), nil, f.adminA, http.StatusOK, &page)
			if got.URL != want || len(page.Items) != 1 || page.Items[0].URL != want {
				t.Fatalf("get url=%q, list=%+v, want %q", got.URL, page.Items, want)
			}
			// A new slug is a new address; the answer to the change already has it.
			var renamed kbArticleView
			kbCall(t, f.api, http.MethodPatch, "/api/v1/kb/articles/"+created.ID, map[string]any{"updated_at": created.UpdatedAt, "slug": "reset"}, f.adminA, http.StatusOK, &renamed)
			if want := fmt.Sprintf("%s/a/%d-reset", home, created.Number); renamed.URL != want {
				t.Fatalf("update url=%q, want %q", renamed.URL, want)
			}
			var restored kbArticleView
			kbCall(t, f.api, http.MethodPost, "/api/v1/kb/articles/"+created.ID+"/versions/1/restore", map[string]any{"updated_at": renamed.UpdatedAt}, f.adminA, http.StatusOK, &restored)
			if restored.URL != want {
				t.Fatalf("version restore url=%q, want %q", restored.URL, want)
			}
			kbCall(t, f.api, http.MethodDelete, "/api/v1/kb/articles/"+created.ID+"?updated_at="+url.QueryEscape(restored.UpdatedAt), nil, f.adminA, http.StatusNoContent, nil)
			var trash struct {
				Items []kbTrashItem `json:"items"`
			}
			kbCall(t, f.api, http.MethodGet, "/api/v1/kb/articles/trash", nil, f.adminA, http.StatusOK, &trash)
			if len(trash.Items) != 1 || trash.Items[0].URL != want {
				t.Fatalf("trash = %+v, want url %q", trash.Items, want)
			}
			var back kbArticleView
			kbCall(t, f.api, http.MethodPost, "/api/v1/kb/articles/"+created.ID+"/restore", map[string]any{"updated_at": trash.Items[0].UpdatedAt}, f.adminA, http.StatusOK, &back)
			if back.URL != want {
				t.Fatalf("undelete url=%q, want %q", back.URL, want)
			}

			// Each workspace's articles carry its own address, never another workspace's.
			betaCollection := kbCollection(t, f.api, f.adminB, map[string]any{"name": "Guides"})
			beta := kbArticle(t, f.api, f.adminB, map[string]any{"collection_id": betaCollection.ID, "title": "Beta page", "body_md": "x"})
			if !strings.Contains(beta.URL, slugB) || strings.Contains(beta.URL, slugA) {
				t.Fatalf("beta url=%q, want beta's slug %q only", beta.URL, slugB)
			}
		})
	}
}
