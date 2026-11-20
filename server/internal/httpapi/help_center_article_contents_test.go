package httpapi_test

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// contentsArticleView is an article's on-this-page switch as the API answers it.
type contentsArticleView struct {
	ID           string `json:"id"`
	Number       int64  `json:"number"`
	Slug         string `json:"slug"`
	UpdatedAt    string `json:"updated_at"`
	ShowContents bool   `json:"show_contents"`
}

const fourSections = "Intro.\n\n## One\n\na\n\n## Two\n\nb\n\n## Three\n\nc\n\n## Four\n\nd"

// TestAnArticleWithThreeOrMoreHeadingsShowsItsContents: an article with 4 H2 headings lists 4 links
// to them, above the text and in the rail; one with 2 lists none; an editor turns the list off for
// the article in every language, and the version and the export keep the switch.
func TestAnArticleWithThreeOrMoreHeadingsShowsItsContents(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Contents Desk", "admin@contents.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Contents help", true)
	kbCall(t, api, http.MethodPut, "/api/v1/help-center/languages", map[string]any{"languages": []map[string]any{{"locale": "fr", "visible": true}}}, admin, http.StatusOK, nil)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	var long, short contentsArticleView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles", map[string]any{"collection_id": collection.ID, "title": "Long guide", "status": "published", "body_md": fourSections},
		admin, http.StatusCreated, &long)
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles", map[string]any{"collection_id": collection.ID, "title": "Short guide", "status": "published",
		"body_md": "## One\n\na\n\n## Two\n\nb"}, admin, http.StatusCreated, &short)
	if !long.ShowContents || !short.ShowContents {
		t.Fatalf("show_contents on create = %v, %v; want true by default", long.ShowContents, short.ShowContents)
	}
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+long.ID+"/translations", map[string]any{"locale": "fr", "title": "Guide long", "status": "published",
		"body_md": "## Un\n\na\n\n## Deux\n\nb\n\n## Trois\n\nc"}, admin, http.StatusCreated, nil)

	longPath := fmt.Sprintf("/help/%s/a/%d-%s", slug, long.Number, long.Slug)
	frPath := fmt.Sprintf("/help/%s/fr/a/%d-guide-long", slug, long.Number)
	links := []string{`<a href="#one">One</a>`, `<a href="#two">Two</a>`, `<a href="#three">Three</a>`, `<a href="#four">Four</a>`}
	page := visit(site, longPath, "192.0.2.130")
	wantPage(t, page, http.StatusOK, append([]string{`<h2 id="one">`, `<h2 id="four">`, `aria-labelledby="contents-title"`, `aria-labelledby="rail-contents-title"`,
		`>On this page</h2>`}, links...), nil)
	for _, link := range links {
		if got := strings.Count(page.Body.String(), link); got != 2 {
			t.Errorf("%s shows %d times, want 2 (above the text and in the rail)", link, got)
		}
	}
	wantPage(t, visit(site, frPath, "192.0.2.131"), http.StatusOK, []string{`<a href="#un">Un</a>`, `<a href="#trois">Trois</a>`, `>Sur cette page</h2>`}, nil)
	wantPage(t, visit(site, fmt.Sprintf("/help/%s/a/%d-%s", slug, short.Number, short.Slug), "192.0.2.132"), http.StatusOK,
		[]string{`<h2 id="one">`}, []string{`class="contents`, `href="#one"`})

	// Turned off, no language of the article lists its contents; the headings keep their anchors.
	var off contentsArticleView
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+long.ID, map[string]any{"updated_at": long.UpdatedAt, "show_contents": false}, admin, http.StatusOK, &off)
	if off.ShowContents {
		t.Fatal("show_contents stayed true after turning it off")
	}
	wantPage(t, visit(site, longPath, "192.0.2.133"), http.StatusOK, []string{`<h2 id="one">`}, []string{`class="contents`, `href="#one"`})
	wantPage(t, visit(site, frPath, "192.0.2.134"), http.StatusOK, []string{`<h2 id="un">`}, []string{`class="contents`, `href="#un"`})

	var versions struct {
		Items []struct {
			Version      int  `json:"version"`
			ShowContents bool `json:"show_contents"`
		} `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+long.ID+"/versions", nil, admin, http.StatusOK, &versions)
	if len(versions.Items) != 2 || versions.Items[0].ShowContents || !versions.Items[1].ShowContents {
		t.Fatalf("versions = %+v, want the newest off and the first on", versions.Items)
	}
	entries := unzip(t, request(t, api, http.MethodGet, "/api/v1/export", nil, admin, nil).Body.Bytes())
	for _, record := range ndjsonLines(t, entries["kb-articles.ndjson"]) {
		if want := record["id"] != long.ID; record["show_contents"] != want {
			t.Errorf("kb-articles.ndjson %v show_contents = %v, want %v", record["title"], record["show_contents"], want)
		}
	}
	if newest := ndjsonLines(t, entries["kb-article-versions.ndjson"]); len(newest) == 0 || newest[len(newest)-1]["show_contents"] == nil {
		t.Errorf("kb-article-versions.ndjson has no show_contents: %v", newest)
	}

	// Created off, it is off from the start.
	var created contentsArticleView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles", map[string]any{"collection_id": collection.ID, "title": "Plain guide", "status": "published",
		"body_md": fourSections, "show_contents": false}, admin, http.StatusCreated, &created)
	if created.ShowContents {
		t.Fatal("show_contents false on create was not kept")
	}
	wantPage(t, visit(site, fmt.Sprintf("/help/%s/a/%d-%s", slug, created.Number, created.Slug), "192.0.2.135"), http.StatusOK, nil, []string{`class="contents`})
}

// TestTheThemePreviewOfAnArticleListsItsContents: the theme preview shows the article as its page does.
func TestTheThemePreviewOfAnArticleListsItsContents(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "Contents Preview", "admin@contents-preview.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Long guide", "status": "published", "body_md": fourSections})
	article := preview(t, api, admin, "article", map[string]any{}, http.StatusOK)
	wantHTML(t, "article preview", article.HTML, []string{`aria-labelledby="contents-title"`, `<a href="#four">Four</a>`}, nil)
}

// TestAHeadingNamedMainLinksToItself: a heading "Main" is not given the id of the page's <main>, so
// the contents link and the skip link each lead where they say, and no id shows twice on the page.
func TestAHeadingNamedMainLinksToItself(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Main Desk", "admin@main-heading.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Main help", true)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	var article contentsArticleView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles", map[string]any{"collection_id": collection.ID, "title": "Layout guide", "status": "published",
		"body_md": "## Main\n\na\n\n## Results\n\nb\n\n## Topics\n\nc"}, admin, http.StatusCreated, &article)
	page := visit(site, fmt.Sprintf("/help/%s/a/%d-%s", slug, article.Number, article.Slug), "192.0.2.140")
	wantPage(t, page, http.StatusOK, []string{`<h2 id="main-1">Main</h2>`, `<a href="#main-1">Main</a>`, `<main id="main"`, `href="#main"`}, []string{`<h2 id="main">`})
	seen := map[string]bool{}
	for _, match := range regexp.MustCompile(`\sid="([^"]*)"`).FindAllStringSubmatch(page.Body.String(), -1) {
		if seen[match[1]] {
			t.Errorf("id %q shows twice on the page", match[1])
		}
		seen[match[1]] = true
	}
}

// TestAContentsLinkShowsTheHeadingsWords: an entity or an escaped character in a heading shows in its
// contents link and in the page's description as it does in the heading, escaped once in the page.
func TestAContentsLinkShowsTheHeadingsWords(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Words Desk", "admin@contents-words.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Words help", true)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	var article contentsArticleView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles", map[string]any{"collection_id": collection.ID, "title": "Words guide", "status": "published",
		"body_md": "## Tom &amp; Jerry\n\na\n\n## Use \\*stars\\*\n\nb\n\n## Three\n\nc"}, admin, http.StatusCreated, &article)
	page := visit(site, fmt.Sprintf("/help/%s/a/%d-%s", slug, article.Number, article.Slug), "192.0.2.141")
	wantPage(t, page, http.StatusOK, []string{`">Tom &amp; Jerry</a>`, `">Use *stars*</a>`,
		`<meta name="description" content="Tom &amp; Jerry a Use *stars* b Three c">`}, []string{`&amp;amp;`, `\\*`})
}
