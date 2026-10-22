package httpapi_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// stylesheets lists the style sheet addresses a page links, in order.
func stylesheets(body string) []string {
	var found []string
	for rest := body; ; {
		start := strings.Index(rest, `<link rel="stylesheet" href="`)
		if start < 0 {
			return found
		}
		rest = rest[start+len(`<link rel="stylesheet" href="`):]
		found = append(found, rest[:strings.Index(rest, `"`)])
	}
}

// brandSheet is the address of the page's brand style sheet, or "".
func brandSheet(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	for _, sheet := range stylesheets(response.Body.String()) {
		if strings.Contains(sheet, "/_/brand-") {
			return sheet
		}
	}
	return ""
}

func TestHelpCenterPagesShowTheBrand(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Brand Pages", "admin@brand-pages.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Brand Answers", true)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Reset a password", "status": "published", "body_md": "pinewood"})
	logo := uploadKBImage(t, api, admin, kbPNG, http.StatusCreated)
	icon := uploadKBImage(t, api, admin, pngImage(9), http.StatusCreated)
	base := "/help/" + slug

	// An untouched help center sends what it always sent: no brand style sheet, logo or favicon.
	before := visit(site, base, "192.0.2.60")
	wantPage(t, before, http.StatusOK, []string{`<meta name="color-scheme" content="light dark">`, `<a class="brand" href="` + base + `">Brand Answers</a>`},
		[]string{"/_/brand-", `rel="icon"`, "/_/logo/", "tel:", "mailto:"})
	etag := before.Header().Get("ETag")

	theme := fullTheme(logo.ID)
	theme["favicon_media_id"] = icon.ID
	kbCall(t, api, http.MethodPut, themePath, theme, admin, http.StatusOK, nil)
	if again := visit(site, base, "192.0.2.60", "If-None-Match", etag); again.Code != http.StatusOK || again.Header().Get("ETag") == etag {
		t.Fatalf("home after a theme save with the old ETag status=%d etag=%q", again.Code, again.Header().Get("ETag"))
	}
	brandLink := `<link rel="stylesheet" href="` + base + `/_/brand-`
	for _, path := range []string{base, base + "/c/guides", fmt.Sprintf("%s/a/%d-%s", base, article.Number, article.Slug), base + "/search?q=pinewood", base + "/missing"} {
		status := http.StatusOK
		if strings.HasSuffix(path, "/missing") {
			status = http.StatusNotFound
		}
		response := visit(site, path, "192.0.2.60")
		wantPage(t, response, status, []string{
			brandLink,
			`<link rel="icon" href="` + base + `/_/icon/` + icon.ID + `">`,
			`<img class="brand-logo" src="` + base + `/_/logo/` + logo.ID + `" alt="Acme" width="2" height="2">`,
			`<a class="site-link" href="https://acme.example/" title="acme.example">acme.example</a>`,
			`<a href="mailto:help@acme.example">help@acme.example</a>`,
			// html/template writes + as &#43;, which a browser reads as +.
			`<a href="tel:&#43;15550102030">&#43;1 (555) 010-2030</a>`,
			// Brand colors are chosen for light pages.
			`<meta name="color-scheme" content="light">`,
		}, []string{"colliedesk", "style="})
		// The brand comes after the shared sheet, so its custom properties win.
		if sheets := stylesheets(response.Body.String()); len(sheets) != 2 || !strings.Contains(sheets[0], "/_/site-") || !strings.Contains(sheets[1], "/_/brand-") {
			t.Errorf("%s style sheets = %v", path, sheets)
		}
	}

	// An https contact page is a "Contact us" link; an empty logo text is the title.
	theme["contact_url"], theme["logo_alt"], theme["contact_phone"] = "https://acme.example/contact", "", nil
	kbCall(t, api, http.MethodPut, themePath, theme, admin, http.StatusOK, nil)
	wantPage(t, visit(site, base, "192.0.2.60"), http.StatusOK,
		[]string{`<a href="https://acme.example/contact">Contact us</a>`, `alt="Brand Answers"`}, []string{"tel:", "mailto:"})

	// Fonts alone keep the visitor's light or dark setting.
	kbCall(t, api, http.MethodPut, themePath, map[string]any{"body_font": "rounded-sans"}, admin, http.StatusOK, nil)
	fontsOnly := visit(site, base, "192.0.2.60")
	wantPage(t, fontsOnly, http.StatusOK, []string{brandLink, `<meta name="color-scheme" content="light dark">`}, []string{"/_/logo/", `rel="icon"`})
	if sheet := visit(site, brandSheet(t, fontsOnly), "192.0.2.60").Body.String(); strings.Contains(sheet, "--accent") || !strings.Contains(sheet, "--font-body:ui-rounded") {
		t.Fatalf("fonts-only brand sheet:\n%s", sheet)
	}
}

func TestHelpCenterBrandStyleSheetIsImmutableAndOnlyTheCurrentOne(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.HelpCenterDomain = "help.example.net" })
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Brand Sheet", "admin@brand-sheet.example.com")
	slug := workspaceSlug(t, api, admin)
	host := slug + ".help.example.net"
	enableHelpCenter(t, api, admin, "Sheet help", true)
	kbCall(t, api, http.MethodPut, themePath, fullTheme(""), admin, http.StatusOK, nil)

	sheet := brandSheet(t, visit(site, "/", "192.0.2.61", "Host", host))
	if !strings.HasPrefix(sheet, "/_/brand-") {
		t.Fatalf("brand sheet on the help center's host = %q", sheet)
	}
	response := visit(site, sheet, "192.0.2.61", "Host", host)
	header := response.Header()
	if response.Code != http.StatusOK || header.Get("Content-Type") != "text/css; charset=utf-8" || header.Get("X-Content-Type-Options") != "nosniff" ||
		header.Get("Cache-Control") != "public, max-age=31536000, immutable" || header.Get("Content-Length") != fmt.Sprint(response.Body.Len()) {
		t.Fatalf("brand sheet status=%d headers=%v", response.Code, header)
	}
	if body := response.Body.String(); !strings.Contains(body, "--accent:#0b5cad;") || !strings.Contains(body, "color-scheme:light;") {
		t.Fatalf("brand sheet body:\n%s", body)
	}
	// A name that is not the current theme's is the help center's 404, never an old look kept for good.
	theme := fullTheme("")
	theme["colors"].(map[string]any)["link"] = "#0a4f94"
	kbCall(t, api, http.MethodPut, themePath, theme, admin, http.StatusOK, nil)
	for _, stale := range []string{sheet, "/_/brand-000000000000.css", "/_/brand-.css"} {
		if response := visit(site, stale, "192.0.2.61", "Host", host); response.Code != http.StatusNotFound || response.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s status=%d cache=%q, want 404 no-store", stale, response.Code, response.Header().Get("Cache-Control"))
		}
	}
	if current := brandSheet(t, visit(site, "/", "192.0.2.61", "Host", host)); current == sheet || visit(site, current, "192.0.2.61", "Host", host).Code != http.StatusOK {
		t.Fatalf("brand sheet after a new link color = %q (was %q)", current, sheet)
	}
}

func TestHelpCenterLogoAndFaviconServeOnlyWhatTheThemeNames(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	adminA := createSession(t, api, service, "Brand Logo Alpha", "admin@brand-logo-alpha.example.com")
	adminB := createSession(t, api, service, "Brand Logo Bravo", "admin@brand-logo-bravo.example.com")
	slugA, slugB := workspaceSlug(t, api, adminA), workspaceSlug(t, api, adminB)
	enableHelpCenter(t, api, adminA, "Alpha brand", true)
	enableHelpCenter(t, api, adminB, "Bravo brand", true)
	logo := uploadKBImage(t, api, adminA, kbPNG, http.StatusCreated)
	icon := uploadKBImage(t, api, adminA, pngImage(9), http.StatusCreated)
	unnamed := uploadKBImage(t, api, adminA, pngImage(10), http.StatusCreated)
	bravoLogo := uploadKBImage(t, api, adminB, pngImage(11), http.StatusCreated)
	theme := fullTheme(logo.ID)
	theme["favicon_media_id"] = icon.ID
	kbCall(t, api, http.MethodPut, themePath, theme, adminA, http.StatusOK, nil)
	kbCall(t, api, http.MethodPut, themePath, fullTheme(bravoLogo.ID), adminB, http.StatusOK, nil)
	baseA, baseB := "/help/"+slugA, "/help/"+slugB

	for path, want := range map[string][]byte{baseA + "/_/logo/" + logo.ID: kbPNG, baseA + "/_/icon/" + icon.ID: pngImage(9), baseB + "/_/logo/" + bravoLogo.ID: pngImage(11)} {
		response := visit(site, path, "192.0.2.62")
		header := response.Header()
		if response.Code != http.StatusOK || response.Body.String() != string(want) || header.Get("Content-Type") != "image/png" ||
			header.Get("Cache-Control") != "public, max-age=31536000, immutable" || header.Get("Content-Security-Policy") != "default-src 'none'; sandbox" ||
			header.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s status=%d headers=%v", path, response.Code, header)
		}
	}
	for _, path := range []string{
		baseA + "/_/icon/" + logo.ID,      // the logo is not the favicon
		baseA + "/_/logo/" + icon.ID,      // nor the favicon the logo
		baseA + "/_/logo/" + unnamed.ID,   // an image the theme does not name
		baseA + "/_/logo/" + bravoLogo.ID, // another workspace's logo
		baseB + "/_/logo/" + logo.ID,      // and the other way round
		baseA + "/_/logo/" + strings.ToUpper(logo.ID),
		baseA + "/_/logo/" + logo.ID + "/x",
		baseA + "/_/logo/not-an-id",
	} {
		if response := visit(site, path, "192.0.2.62"); response.Code != http.StatusNotFound || strings.Contains(response.Body.String(), "\x89PNG") {
			t.Errorf("%s status=%d, want the help center's 404", path, response.Code)
		}
	}

	// A logo in the library's trash leaves the pages and its address, and comes back on restore.
	shown := visit(site, baseA, "192.0.2.62")
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/media/"+logo.ID, nil, adminA, http.StatusOK, nil)
	trashed := visit(site, baseA, "192.0.2.62", "If-None-Match", shown.Header().Get("ETag"))
	wantPage(t, trashed, http.StatusOK, []string{`<a class="brand" href="` + baseA + `">Alpha brand</a>`, "/_/icon/" + icon.ID}, []string{"/_/logo/"})
	if response := visit(site, baseA+"/_/logo/"+logo.ID, "192.0.2.62"); response.Code != http.StatusNotFound {
		t.Fatalf("trashed logo status=%d", response.Code)
	}
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/"+logo.ID+"/restore", nil, adminA, http.StatusOK, nil)
	wantPage(t, visit(site, baseA, "192.0.2.62", "If-None-Match", trashed.Header().Get("ETag")), http.StatusOK, []string{baseA + "/_/logo/" + logo.ID}, nil)
}
