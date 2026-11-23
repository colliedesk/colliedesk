package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

// strictHelpCenterPolicy is the help center's Content-Security-Policy on every page without the chat's
// script: no script runs, nothing is framed but the two video players.
const strictHelpCenterPolicy = "default-src 'none'; img-src 'self'; style-src 'self'; frame-src https://www.youtube-nocookie.com https://player.vimeo.com; media-src 'self'; script-src 'none'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'"

// An article prints from a page at its own address: the article alone, without the header, the rail,
// the footer or the chat box, and with the strict policy even while the chat runs on the article.
// Search engines keep the article itself.
func TestAnArticlePrintsFromItsOwnAddressWithoutScriptOrChrome(t *testing.T) {
	d := newChatDesk(t, "Print Lima", nil)
	article := d.base + "/a/" + num(d.article.Number) + "-" + d.article.Slug
	print := article + "/print"

	page := helpCenterCall(d.site, http.MethodGet, testOriginHost, article)
	wantPage(t, page, http.StatusOK, []string{`<a class="print-link" href="` + print + `">Print</a>`, `<script src="`}, nil)

	printed := helpCenterCall(d.site, http.MethodGet, testOriginHost, print)
	wantPage(t, printed, http.StatusOK,
		[]string{"<h1>Shipping times</h1>", "Orders ship in 2 days.", `<meta name="robots" content="noindex">`,
			`<link rel="canonical" href="` + testOrigin + article + `">`, `<a class="print-back" href="` + article + `">Back to the article</a>`},
		[]string{"<script", `class="masthead"`, `class="rail`, `class="foot"`, `class="chatbox"`, `class="helpful`, `class="print-link"`})
	if got := printed.Header().Get("Content-Security-Policy"); got != strictHelpCenterPolicy {
		t.Fatalf("print CSP = %q, want the help center's strict policy", got)
	}
	if got := printed.Header().Get("X-Robots-Tag"); got != "noindex" {
		t.Fatalf("print X-Robots-Tag = %q, want noindex", got)
	}

	// One address per print page: an old slug or a query moves to it.
	for _, old := range []string{d.base + "/a/" + num(d.article.Number) + "-old-name/print", print + "?utm_source=mail"} {
		moved := helpCenterCall(d.site, http.MethodGet, testOriginHost, old)
		if moved.Code != http.StatusMovedPermanently || moved.Header().Get("Location") != print {
			t.Fatalf("%s: %d to %q, want 301 to %s", old, moved.Code, moved.Header().Get("Location"), print)
		}
	}
	if sitemap := helpCenterCall(d.site, http.MethodGet, testOriginHost, d.base+"/sitemap.xml"); strings.Contains(sitemap.Body.String(), "/print</loc>") {
		t.Fatalf("the sitemap lists a print page:\n%s", sitemap.Body.String())
	}
	if missing := helpCenterCall(d.site, http.MethodGet, testOriginHost, d.base+"/a/99999-nothing/print"); missing.Code != http.StatusNotFound {
		t.Fatalf("print of no article: %d, want 404", missing.Code)
	}
}
