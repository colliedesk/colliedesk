package httpapi_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// The header of every help center page has Home and Solutions, as a Freshdesk portal has its tabs.
// aria-current marks Home on the home page and Solutions on the Solutions page and on every page inside
// it (a collection, an article); a search and a missing page are neither, so neither link is marked.
func TestEveryHelpCenterPageHeaderLinksHomeAndSolutions(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Tabs Desk", "admin@tabs.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Tabs help", true)
	guides := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	kbArticle(t, api, admin, map[string]any{"collection_id": guides.ID, "title": "Reset your password", "status": "published", "body_md": "Open Settings."})

	base := "/help/" + slug
	home := fmt.Sprintf(`<a href="%s" aria-current="page">Home</a>`, base)
	homeLink := fmt.Sprintf(`<a href="%s">Home</a>`, base)
	solutions := fmt.Sprintf(`<a href="%s/c/" aria-current="page">Solutions</a>`, base)
	solutionsLink := fmt.Sprintf(`<a href="%s/c/">Solutions</a>`, base)
	for _, page := range []struct {
		name, path string
		status     int
		holds      []string
	}{
		{"home", base, http.StatusOK, []string{home, solutionsLink}},
		{"solutions", base + "/c/", http.StatusOK, []string{homeLink, solutions}},
		{"collection", base + "/c/guides", http.StatusOK, []string{homeLink, solutions}},
		{"article", base + "/a/1-reset-your-password", http.StatusOK, []string{homeLink, solutions}},
		{"search", base + "/search?q=nothing+matches+this", http.StatusOK, []string{homeLink, solutionsLink}},
		{"not found", base + "/a/99-gone", http.StatusNotFound, []string{homeLink, solutionsLink}},
	} {
		t.Run(page.name, func(t *testing.T) {
			response := visit(site, page.path, "192.0.2.150")
			wantPage(t, response, page.status, append([]string{`<nav class="site-nav" aria-label="Help center">`}, page.holds...), nil)
			if got := strings.Count(response.Body.String(), `aria-current="page"`); page.name == "search" || page.name == "not found" {
				if got != 0 {
					t.Errorf("%s marks %d links as the current page, want none", page.name, got)
				}
			}
		})
	}
}

// The Solutions page lists every collection with a published article, sections included, and their
// published articles; a draft, an article for agents only and a collection for agents only stay off it.
func TestTheSolutionsPageListsEveryPublishedCollectionAndItsArticles(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Solutions Desk", "admin@solutions.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Solutions help", true)
	guides := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	billing := kbCollection(t, api, admin, map[string]any{"name": "Billing", "parent_id": guides.ID})
	staff := kbCollection(t, api, admin, map[string]any{"name": "Staff handbook", "visibility": "agents"})
	kbArticle(t, api, admin, map[string]any{"collection_id": guides.ID, "title": "Reset your password", "status": "published", "body_md": "Open Settings."})
	kbArticle(t, api, admin, map[string]any{"collection_id": billing.ID, "title": "Change your card", "status": "published", "body_md": "Open Billing."})
	kbArticle(t, api, admin, map[string]any{"collection_id": guides.ID, "title": "Secret roadmap", "status": "draft", "body_md": "Not yet."})
	kbArticle(t, api, admin, map[string]any{"collection_id": guides.ID, "title": "Refund rules for agents", "status": "published", "visibility": "agents", "body_md": "Inside."})
	kbArticle(t, api, admin, map[string]any{"collection_id": staff.ID, "title": "Holiday rota", "status": "published", "body_md": "Inside."})

	base := "/help/" + slug
	wantPage(t, visit(site, base+"/c/", "192.0.2.151"), http.StatusOK, []string{
		`<title>Solutions · Solutions help</title>`,
		`<h1>Solutions</h1>`,
		fmt.Sprintf(`<link rel="canonical" href="https://desk.example.com%s/c/">`, base),
		fmt.Sprintf(`<a href="%s/c/guides" data-count="1">Guides</a>`, base),
		fmt.Sprintf(`<a href="%s/c/guides/billing" data-count="1">Billing</a>`, base),
		fmt.Sprintf(`<a href="%s/a/1-reset-your-password">Reset your password</a>`, base),
		fmt.Sprintf(`<a href="%s/a/2-change-your-card">Change your card</a>`, base),
	}, []string{"Secret roadmap", "Refund rules for agents", "Staff handbook", "Holiday rota"})

}
