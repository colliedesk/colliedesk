package httpapi_test

import (
	"html"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// A search that finds nothing offers the team's contact link under "No results": a mailto: link starts
// an email about the words searched for, a contact page opens as it is, and with no contact link set up
// there is no block. Every page with the header, a missing one included, keeps the contact link in its
// footer.
func TestASearchWithNoResultsOffersTheTeamsContactLink(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Contact Desk", "admin@contact.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Contact help", true)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Reset your password", "status": "published", "body_md": "Open Settings."})
	base := "/help/" + slug
	words := `refund & "café" order`
	empty := base + "/search?q=" + url.QueryEscape(words)
	read := func(path string, status int) string {
		t.Helper()
		page := visit(site, path, "192.0.2.160")
		if page.Code != status {
			t.Fatalf("%s status=%d, want %d", path, page.Code, status)
		}
		return html.UnescapeString(page.Body.String())
	}

	// Nothing set up: no block.
	if body := read(empty, http.StatusOK); strings.Contains(body, "search-contact") {
		t.Fatalf("an empty search offers a contact that is not set up:\n%s", body)
	}

	kbCall(t, api, http.MethodPut, "/api/v1/help-center/theme", map[string]any{"contact_url": "mailto:help@contact.example", "custom_css": nil}, admin, http.StatusOK, nil)
	body := read(empty, http.StatusOK)
	for _, want := range []string{
		`<section class="search-contact" aria-labelledby="search-contact-title">`,
		`<h2 id="search-contact-title" class="search-contact-title">Can’t find it?</h2>`,
		`<a class="search-contact-link" href="mailto:help@contact.example?subject=Can%E2%80%99t%20find%3A%20refund%20%26%20%22caf%C3%A9%22%20order">Contact Contact Desk</a>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("empty search lacks %q:\n%s", want, body)
		}
	}
	// A search with results needs no block.
	if body := read(base+"/search?q=password", http.StatusOK); strings.Contains(body, "search-contact") {
		t.Fatalf("a search with results offers the contact block:\n%s", body)
	}

	kbCall(t, api, http.MethodPut, "/api/v1/help-center/theme", map[string]any{"contact_url": "https://contact.example/support", "custom_css": nil}, admin, http.StatusOK, nil)
	if body := read(empty, http.StatusOK); !strings.Contains(body, `<a class="search-contact-link" href="https://contact.example/support">Contact Contact Desk</a>`) {
		t.Fatalf("empty search lacks the contact page:\n%s", body)
	}

	// The footer of every page carries the contact link.
	for _, page := range []struct {
		path   string
		status int
	}{{base, http.StatusOK}, {base + "/c/guides", http.StatusOK}, {base + "/a/1-reset-your-password", http.StatusOK},
		{empty, http.StatusOK}, {base + "/a/99-gone", http.StatusNotFound}} {
		if body := read(page.path, page.status); !strings.Contains(body, `<a href="https://contact.example/support">Contact us</a>`) {
			t.Errorf("%s has no contact link in its footer:\n%s", page.path, body)
		}
	}
}
