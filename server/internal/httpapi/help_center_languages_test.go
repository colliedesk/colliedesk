package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

// languagesFixture is a help center with a French translation published, a Spanish draft, a
// German language hidden, and an article only in the primary language.
type languagesFixture struct {
	site                       http.Handler
	api                        http.Handler
	admin, slug, base, address string
	translated, untranslated   kbArticleView
	billing                    kbCollectionView
	frSeen                     string
	pool                       *pgxpool.Pool
	service                    *auth.Service
}

func newLanguagesFixture(t *testing.T) languagesFixture {
	t.Helper()
	api, service, pool, _ := testAPIWithStorage(t)
	f := languagesFixture{api: api, site: helpCenterSite(api), pool: pool, service: service}
	f.admin = createSession(t, api, service, "Help Languages", "admin@help-languages.example.com")
	f.slug = workspaceSlug(t, api, f.admin)
	f.base = "/help/" + f.slug
	f.address = "https://" + testOriginHost + f.base
	enableHelpCenter(t, api, f.admin, "Answers", true)
	kbCall(t, api, http.MethodPut, "/api/v1/help-center/languages", map[string]any{"languages": []map[string]any{
		{"locale": "fr", "visible": true}, {"locale": "es", "visible": true}, {"locale": "de", "visible": false}}}, f.admin, http.StatusOK, nil)
	f.billing = kbCollection(t, api, f.admin, map[string]any{"name": "Billing", "description": "Plans"})
	kbCall(t, api, http.MethodPut, "/api/v1/kb/collections/"+f.billing.ID+"/translations/fr", map[string]any{"name": "Facturation", "description": "Forfaits"}, f.admin, http.StatusOK, nil)
	image := uploadKBImage(t, api, f.admin, otherPNG(41), http.StatusCreated)
	f.translated = kbArticle(t, api, f.admin, map[string]any{"collection_id": f.billing.ID, "title": "Pay by card", "status": "published", "body_md": "Use any card: pinewood."})
	f.untranslated = kbArticle(t, api, f.admin, map[string]any{"collection_id": f.billing.ID, "title": "Refunds", "status": "published", "body_md": "Refunds take five days."})
	var fr kbTranslationView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+f.translated.ID+"/translations", map[string]any{"locale": "fr", "title": "Payer par carte", "status": "published",
		"body_md": "Toute carte convient : sapinette.\n\n![Carte](" + image.URL + ")", "seo_title": "Paiement par carte"}, f.admin, http.StatusCreated, &fr)
	f.frSeen = fr.UpdatedAt
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+f.translated.ID+"/translations", map[string]any{"locale": "es", "title": "Pagar con tarjeta", "body_md": "borrador secreto"},
		f.admin, http.StatusCreated, nil)
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+f.translated.ID+"/translations", map[string]any{"locale": "de", "title": "Mit Karte zahlen", "status": "published",
		"body_md": "versteckt"}, f.admin, http.StatusCreated, nil)
	return f
}

func TestHelpCenterShowsEachShownLanguageUnderItsOwnAddress(t *testing.T) {
	f := newLanguagesFixture(t)
	article := f.base + "/fr/a/1-payer-par-carte"
	page := visit(f.site, article, "198.51.100.91")
	wantPage(t, page, http.StatusOK, []string{`<html lang="fr">`, "Payer par carte", "sapinette", "<title>Paiement par carte</title>",
		`<link rel="canonical" href="` + f.address + `/fr/a/1-payer-par-carte">`,
		`<link rel="alternate" hreflang="en" href="` + f.address + `/a/1-pay-by-card">`,
		`<link rel="alternate" hreflang="fr" href="` + f.address + `/fr/a/1-payer-par-carte">`,
		`<link rel="alternate" hreflang="x-default" href="` + f.address + `/a/1-pay-by-card">`,
		`href="` + f.base + `/fr/c/billing">Facturation</a>`, "Mis à jour le", `aria-label="Langue"`, `aria-current="page">Français</a>`, `"inLanguage":"fr"`,
		`/images/`}, []string{"borrador", "versteckt", `hreflang="es" href=`, `hreflang="de"`, "Refunds"})
	// The primary article names its French version; nothing changed in its own address.
	wantPage(t, visit(f.site, f.base+"/a/1-pay-by-card", "198.51.100.91"), http.StatusOK,
		[]string{`<html lang="en">`, `hreflang="fr" href="` + f.address + `/fr/a/1-payer-par-carte"`, `hreflang="fr" lang="fr">Français</a>`}, nil)
	// An article without a French translation, a draft in Spanish and a hidden language lead to the primary page.
	// In one step, to the primary article's own address (302: a translation published later takes over).
	for path, want := range map[string]string{f.base + "/fr/a/2-refunds": f.base + "/a/2-refunds", f.base + "/es/a/1-pagar-con-tarjeta": f.base + "/a/1-pay-by-card",
		f.base + "/fr/a/2-refunds.md": f.base + "/a/2-refunds.md"} {
		response := visit(f.site, path, "198.51.100.91")
		if response.Code != http.StatusFound || response.Header().Get("Location") != want {
			t.Errorf("GET %s = %d %q, want 302 to %s", path, response.Code, response.Header().Get("Location"), want)
		}
	}
	if response := visit(f.site, f.base+"/de/a/1-mit-karte-zahlen", "198.51.100.91"); response.Code != http.StatusNotFound {
		t.Errorf("hidden language = %d, want 404", response.Code)
	}
	// Another slug, another case of the language and the primary language's prefix move to the address.
	wantMove(t, f.site, f.base+"/fr/a/1-old", http.StatusMovedPermanently, article)
	wantMove(t, f.site, f.base+"/FR/a/1-payer-par-carte", http.StatusMovedPermanently, article)
	wantMove(t, f.site, f.base+"/en/a/1-pay-by-card", http.StatusMovedPermanently, f.base+"/a/1-pay-by-card")
	// The French home and collection list only what is published in French, with French names.
	wantPage(t, visit(f.site, f.base+"/fr", "198.51.100.91"), http.StatusOK, []string{"Comment pouvons-nous vous aider ?", "Facturation", "Payer par carte",
		`action="` + f.base + `/fr/search"`}, []string{"Refunds", "Pay by card"})
	wantPage(t, visit(f.site, f.base+"/fr/c/billing", "198.51.100.91"), http.StatusOK, []string{"Facturation", "Forfaits", "Payer par carte"}, []string{"Refunds"})
	// The search of a language finds its translations, never the primary text or a draft.
	wantPage(t, visit(f.site, f.base+"/fr/search?q=sapinette", "198.51.100.91"), http.StatusOK, []string{"Payer par carte", "1 résultat pour « sapinette »"}, nil)
	wantPage(t, visit(f.site, f.base+"/fr/search?q=pinewood", "198.51.100.91"), http.StatusOK, []string{"Aucun résultat pour « pinewood »"}, nil)
	wantPage(t, visit(f.site, f.base+"/es/search?q=borrador", "198.51.100.91"), http.StatusOK, []string{"No hay resultados para «borrador»"}, nil)
	// The image a published translation shows is public.
	if !strings.Contains(page.Body.String(), f.base+"/images/") {
		t.Fatal("no image")
	}
}

func TestHelpCenterHomeFollowsTheBrowserLanguageOnlyAtItsRoot(t *testing.T) {
	f := newLanguagesFixture(t)
	response := visit(f.site, f.base, "198.51.100.92", "Accept-Language", "fr-CA,fr;q=0.9,en;q=0.5")
	if response.Code != http.StatusFound || response.Header().Get("Location") != f.base+"/fr" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("French browser = %d %q %q", response.Code, response.Header().Get("Location"), response.Header().Get("Cache-Control"))
	}
	for _, header := range [][]string{{}, {"Accept-Language", "en-US,en;q=0.9"}, {"Accept-Language", "de-DE"}, {"Accept-Language", "ja"}} {
		response := visit(f.site, f.base, "198.51.100.92", header...)
		if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Vary"), "Accept-Language") {
			t.Errorf("Accept-Language %v = %d (Vary %q), want the primary home", header, response.Code, response.Header().Get("Vary"))
		}
	}
	// The language menu's way back to the primary home keeps it, whatever the browser says.
	wantPage(t, visit(f.site, f.base+"?lang=en", "198.51.100.92", "Accept-Language", "fr"), http.StatusOK, []string{"How can we help?"}, nil)
	// An article address never moves by the browser's language.
	wantPage(t, visit(f.site, f.base+"/a/1-pay-by-card", "198.51.100.92", "Accept-Language", "fr"), http.StatusOK, []string{"Pay by card"}, nil)
}

// A language segment counts only when it names a language customers see: every path the help center
// had keeps its meaning.
func TestHelpCenterLanguagesLeaveEveryExistingPathAsItWas(t *testing.T) {
	f := newLanguagesFixture(t)
	for _, path := range []string{"/a/1-pay-by-card", "/c/billing", "/search?q=pinewood", "/sitemap.xml", "/llms.txt", "/a/1-pay-by-card.md"} {
		if response := visit(f.site, f.base+path, "198.51.100.93"); response.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, response.Code)
		}
	}
	// Files exist once, below the help center itself, never below a language.
	for _, path := range []string{"/fr/sitemap.xml", "/fr/llms.txt", "/fr/robots.txt", "/fr/images/x", "/fr/media/x", "/fr/_/site.css", "/xx/a/1-pay-by-card", "/de"} {
		if response := visit(f.site, f.base+path, "198.51.100.93"); response.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, response.Code)
		}
	}
	sitemap := visit(f.site, f.base+"/sitemap.xml", "198.51.100.93").Body.String()
	for _, want := range []string{`xmlns:xhtml="http://www.w3.org/1999/xhtml"`, "<loc>" + f.address + "/fr/a/1-payer-par-carte</loc>", "<loc>" + f.address + "/fr</loc>",
		"<loc>" + f.address + "/fr/c/billing</loc>", `<xhtml:link rel="alternate" hreflang="fr" href="` + f.address + `/fr/a/1-payer-par-carte"/>`} {
		if !strings.Contains(sitemap, want) {
			t.Errorf("sitemap lacks %q:\n%s", want, sitemap)
		}
	}
	for _, unwanted := range []string{"pagar", "mit-karte", "/fr/a/2-"} {
		if strings.Contains(sitemap, unwanted) {
			t.Errorf("sitemap holds %q:\n%s", unwanted, sitemap)
		}
	}
}

func TestOldFreshdeskAddressesInALanguageMoveToItsTranslation(t *testing.T) {
	f := newLanguagesFixture(t)
	ledgerRow(t, f.pool, f.slug, "freshdesk:account:1", "kb_article", "9001", f.translated.ID, "done", "2026-10-20 10:00:00+00")
	ledgerRow(t, f.pool, f.slug, "freshdesk:account:1", "kb_article", "9002", f.untranslated.ID, "done", "2026-10-20 10:00:00+00")
	wantMove(t, f.site, f.base+"/fr/support/solutions/articles/9001-payer", http.StatusMovedPermanently, f.address+"/fr/a/1-payer-par-carte")
	wantMove(t, f.site, f.base+"/fr/support/solutions/articles/9002-refunds", http.StatusMovedPermanently, f.address+"/a/2-refunds")
	wantMove(t, f.site, f.base+"/de/support/solutions/articles/9001", http.StatusMovedPermanently, f.address+"/a/1-pay-by-card")
	wantMove(t, f.site, f.base+"/support/solutions/articles/9001", http.StatusMovedPermanently, f.address+"/a/1-pay-by-card")
}

// Another workspace showing the same language never shows this one's translations, and the primary
// language cannot be one of the others.
func TestHelpCenterLanguagesStayInTheirWorkspace(t *testing.T) {
	f := newLanguagesFixture(t)
	api := f.api
	other := createSession(t, api, f.service, "Help Languages Other", "admin@help-languages-other.example.com")
	slug := workspaceSlug(t, api, other)
	enableHelpCenter(t, api, other, "Other", true)
	kbCall(t, api, http.MethodPut, "/api/v1/help-center/languages", map[string]any{"languages": []map[string]any{{"locale": "fr", "visible": true}}}, other, http.StatusOK, nil)
	for _, path := range []string{"/help/" + slug + "/fr/a/1-payer-par-carte", "/help/" + slug + "/fr/search?q=sapinette", "/help/" + slug + "/fr", "/help/" + slug + "/sitemap.xml"} {
		if body := visit(f.site, path, "198.51.100.94").Body.String(); strings.Contains(body, "Payer par carte") || strings.Contains(body, "Toute carte") {
			t.Errorf("GET %s shows the other workspace's translation", path)
		}
	}
	response := kbCall(t, api, http.MethodPut, "/api/v1/help-center", map[string]any{"enabled": true, "title": "Answers", "locale": "fr"}, f.admin, http.StatusBadRequest, nil)
	if !strings.Contains(response.Body.String(), "Remove it there first") {
		t.Fatalf("primary language among the others: %s", response.Body.String())
	}
}

// One rule on every page: the language menu lists every language customers see (this page in it, or
// its home); hreflang lists only the versions that exist. A shown language with nothing published
// says so and leads to the primary language.
func TestTheLanguageMenuIsTheSameOnEveryPage(t *testing.T) {
	f := newLanguagesFixture(t)
	for _, path := range []string{"/fr", "/fr/a/1-payer-par-carte", "/fr/c/billing", "/fr/search?q=carte", "", "/a/2-refunds", "/es"} {
		body := visit(f.site, f.base+path, "198.51.100.94").Body.String()
		for _, name := range []string{">English</a>", ">Français</a>", ">Español</a>"} {
			if !strings.Contains(body, name) {
				t.Errorf("GET %s: language menu lacks %s", path, name)
			}
		}
		if strings.Contains(body, "Deutsch") {
			t.Errorf("GET %s names the hidden language", path)
		}
	}
	// Spanish has no article of its own: the menu leads to its home, hreflang leaves it out.
	wantPage(t, visit(f.site, f.base+"/a/1-pay-by-card", "198.51.100.94"), http.StatusOK,
		[]string{`href="` + f.base + `/es" hreflang="es" lang="es">Español</a>`}, []string{`hreflang="es" href=`})
	wantPage(t, visit(f.site, f.base+"/es", "198.51.100.94"), http.StatusOK,
		[]string{`<html lang="es">`, "Aún no hay nada publicado", "Mientras tanto, lea el centro de ayuda en", `href="` + f.base + `/?lang=en" lang="en">English</a>`},
		[]string{`hreflang="es" href=`, "Pagar con tarjeta"})
	// A home with nothing of its own is not one for search engines; a language's home with articles is.
	if body := visit(f.site, f.base+"/es", "198.51.100.94").Body.String(); !strings.Contains(body, `<meta name="robots" content="noindex">`) {
		t.Error("the empty Spanish home lacks noindex")
	}
	if body := visit(f.site, f.base+"/fr", "198.51.100.94").Body.String(); strings.Contains(body, "noindex") {
		t.Error("the French home is noindex")
	}
}

// An image only a published translation in a hidden language shows stays private; shown, it is public.
func TestAnImageOfAHiddenLanguageIsNotPublic(t *testing.T) {
	f := newLanguagesFixture(t)
	image := uploadKBImage(t, f.api, f.admin, otherPNG(43), http.StatusCreated)
	var de kbTranslationView
	kbCall(t, f.api, http.MethodGet, "/api/v1/kb/articles/"+f.translated.ID+"/translations/de", nil, f.admin, http.StatusOK, &de)
	kbCall(t, f.api, http.MethodPatch, "/api/v1/kb/articles/"+f.translated.ID+"/translations/de", map[string]any{"updated_at": de.UpdatedAt,
		"body_md": "versteckt\n\n![Bild](" + image.URL + ")"}, f.admin, http.StatusOK, nil)
	public := f.base + "/images/" + image.ID
	if response := visit(f.site, public, "198.51.100.95"); response.Code != http.StatusNotFound {
		t.Fatalf("image of a hidden language = %d, want 404", response.Code)
	}
	kbCall(t, f.api, http.MethodPut, "/api/v1/help-center/languages", map[string]any{"languages": []map[string]any{
		{"locale": "fr", "visible": true}, {"locale": "es", "visible": true}, {"locale": "de", "visible": true}}}, f.admin, http.StatusOK, nil)
	if response := visit(f.site, public, "198.51.100.95"); response.Code != http.StatusOK {
		t.Fatalf("image once the language is shown = %d, want 200", response.Code)
	}
}
