package httpapi_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// Below an article, a reader finds the articles closest to it, from any collection, then the rest of
// its collection, as plain links; the rail still lists its collection, and the page's policy is the
// same as on every other page. An article with nothing close and alone in its collection has no list.
func TestAnArticleListsRelatedArticlesBelowItsText(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Related Pages", "admin@related-pages.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Related help", true)
	billing := kbCollection(t, api, admin, map[string]any{"name": "Billing"})
	account := kbCollection(t, api, admin, map[string]any{"name": "Account"})
	lonely := kbCollection(t, api, admin, map[string]any{"name": "Lonely"})
	article := func(collection, title, body string) kbArticleView {
		return kbArticle(t, api, admin, map[string]any{"collection_id": collection, "title": title, "status": "published", "body_md": body})
	}
	source := article(billing.ID, "Refund duplicate charge", "Press refund for the duplicate charge.")
	sibling := article(billing.ID, "Card expiry", "Update your card.")
	match := article(account.ID, "Duplicate charge refund", "Refund from the invoice.")
	alone := article(lonely.ID, "Webhooks", "Signing keys rotate.")
	path := func(a kbArticleView) string { return fmt.Sprintf("/help/%s/a/%d-%s", slug, a.Number, a.Slug) }

	page := visit(site, path(source), "192.0.2.80")
	wantPage(t, page, http.StatusOK, []string{`<section class="related" aria-labelledby="related-title">`, "In Billing"}, nil)
	if got := page.Header().Get("Content-Security-Policy"); got != pageCSP {
		t.Fatalf("Content-Security-Policy = %q, want %q", got, pageCSP)
	}
	body := page.Body.String()
	start := strings.Index(body, `<section class="related"`)
	section := body[start : start+strings.Index(body[start:], "</section>")]
	want := `<h2 id="related-title" class="section-label">Related articles</h2>` + "\n" +
		`<ul class="related-list"><li><a href="` + path(match) + `">Duplicate charge refund</a></li>` +
		`<li><a href="` + path(sibling) + `">Card expiry</a></li></ul>`
	if !strings.Contains(section, want) {
		t.Fatalf("related section = %s, want it to hold %s", section, want)
	}

	wantPage(t, visit(site, path(alone), "192.0.2.80"), http.StatusOK, []string{"Webhooks"}, []string{`class="related"`, "Related articles"})

	// The list is kept between visits only while the knowledge base stays as it was: once the
	// closest article is in the trash, the next reader of the page never sees its link.
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/articles/"+match.ID+"?updated_at="+url.QueryEscape(match.UpdatedAt), nil, admin, http.StatusNoContent, nil)
	wantPage(t, visit(site, path(source), "192.0.2.80"), http.StatusOK, []string{`<li><a href="` + path(sibling) + `">Card expiry</a></li>`}, []string{path(match)})
}
