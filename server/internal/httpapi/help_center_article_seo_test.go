package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// seoArticleView is an article's search title and meta description as the API answers them.
type seoArticleView struct {
	ID             string `json:"id"`
	Number         int64  `json:"number"`
	Slug           string `json:"slug"`
	UpdatedAt      string `json:"updated_at"`
	SEOTitle       string `json:"seo_title"`
	SEODescription string `json:"seo_description"`
}

// TestArticleSearchTitleAndDescriptionReachTheHelpCenter: an article's own search title and meta
// description are saved, read, versioned and exported, and its help center page shows them; left
// empty, the page uses the title and the start of the text.
func TestArticleSearchTitleAndDescriptionReachTheHelpCenter(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "SEO Fields", "admin@seo-fields.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Fields help", true)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	var article seoArticleView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles", map[string]any{"collection_id": collection.ID, "title": "Reset your password", "status": "published",
		"body_md": "Open **Settings**, then Password.\n\nMore text.", "seo_title": "  Reset an Acme password  ", "seo_description": "Choose a new\npassword in two steps."},
		admin, http.StatusCreated, &article)
	if article.SEOTitle != "Reset an Acme password" || article.SEODescription != "Choose a new password in two steps." {
		t.Fatalf("saved seo fields = %q, %q", article.SEOTitle, article.SEODescription)
	}
	path := fmt.Sprintf("/help/%s/a/%d-%s", slug, article.Number, article.Slug)
	wantPage(t, visit(site, path, "192.0.2.120"), http.StatusOK,
		[]string{"<title>Reset an Acme password</title>", `<meta name="description" content="Choose a new password in two steps.">`,
			`<meta property="og:title" content="Reset an Acme password">`, "<h1>Reset your password</h1>"}, []string{"Open Settings, then Password."})

	// The contract says the same; the server refuses past it too, never cutting.
	for _, bad := range []map[string]any{{"seo_title": strings.Repeat("t", 121)}, {"seo_description": strings.Repeat("d", 321)}} {
		bad["updated_at"] = article.UpdatedAt
		if response := request(t, api, http.MethodPatch, "/api/v1/kb/articles/"+article.ID, bad, admin, nil); response.Code != http.StatusBadRequest {
			t.Fatalf("too long %v status=%d", bad, response.Code)
		}
	}
	var cleared seoArticleView
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+article.ID, map[string]any{"updated_at": article.UpdatedAt, "seo_title": "", "seo_description": ""}, admin, http.StatusOK, &cleared)
	if cleared.SEOTitle != "" || cleared.SEODescription != "" {
		t.Fatalf("cleared seo fields = %q, %q", cleared.SEOTitle, cleared.SEODescription)
	}
	wantPage(t, visit(site, path, "192.0.2.121"), http.StatusOK,
		[]string{"<title>Reset your password · Fields help</title>", `<meta name="description" content="Open Settings, then Password. More text.">`}, []string{"Acme"})

	// The version that saved them keeps them, and restoring it brings them back.
	var versions struct {
		Items []struct {
			Version  int    `json:"version"`
			SEOTitle string `json:"seo_title"`
		} `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+article.ID+"/versions", nil, admin, http.StatusOK, &versions)
	first := versions.Items[len(versions.Items)-1]
	if first.Version != 1 || first.SEOTitle != "Reset an Acme password" {
		t.Fatalf("versions = %+v", versions.Items)
	}
	var restored seoArticleView
	kbCall(t, api, http.MethodPost, fmt.Sprintf("/api/v1/kb/articles/%s/versions/1/restore", article.ID), map[string]any{"updated_at": cleared.UpdatedAt}, admin, http.StatusOK, &restored)
	if restored.SEOTitle != "Reset an Acme password" || restored.SEODescription != "Choose a new password in two steps." {
		t.Fatalf("restored seo fields = %q, %q", restored.SEOTitle, restored.SEODescription)
	}

	response := request(t, api, http.MethodGet, "/api/v1/export", nil, admin, nil)
	entries := unzip(t, response.Body.Bytes())
	articles := ndjsonLines(t, entries["kb-articles.ndjson"])
	if len(articles) != 1 || articles[0]["seo_title"] != "Reset an Acme password" || articles[0]["seo_description"] != "Choose a new password in two steps." {
		t.Fatalf("kb-articles.ndjson = %v", articles)
	}
	if versions := ndjsonLines(t, entries["kb-article-versions.ndjson"]); versions[0]["seo_title"] != "Reset an Acme password" {
		t.Fatalf("kb-article-versions.ndjson = %v", versions)
	}
}

// TestArticleSearchFieldsOfAPublishedArticleNeedASendToken: a published article's search title
// reaches the public help center, so a token that may edit articles but not publish files an agent
// request; on a draft it changes nothing public and goes through.
func TestArticleSearchFieldsOfAPublishedArticleNeedASendToken(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "SEO Tiers", "admin@seo-tiers.example.com")
	draftToken := issueCustomToken(t, api, admin, "Editor", "kb_articles:read", "kb_articles:update").Token
	send := issueToken(t, api, admin, "Publisher", "send").Token
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	var published, draft seoArticleView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles", map[string]any{"collection_id": collection.ID, "title": "Live", "body_md": "x", "status": "published"}, admin, http.StatusCreated, &published)
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles", map[string]any{"collection_id": collection.ID, "title": "Draft", "body_md": "x"}, admin, http.StatusCreated, &draft)

	change := map[string]any{"updated_at": published.UpdatedAt, "seo_title": "Proposed title"}
	if response := request(t, api, http.MethodPatch, "/api/v1/kb/articles/"+published.ID, change, draftToken, nil); response.Code != http.StatusAccepted {
		t.Fatalf("editor token on a published article status=%d body=%s, want 202", response.Code, response.Body.String())
	}
	var unchanged seoArticleView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+published.ID, nil, admin, http.StatusOK, &unchanged)
	if unchanged.SEOTitle != "" {
		t.Fatalf("a request changed the article before approval: %q", unchanged.SEOTitle)
	}
	var sent seoArticleView
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+published.ID, change, send, http.StatusOK, &sent)
	if sent.SEOTitle != "Proposed title" {
		t.Fatalf("send token seo title = %q", sent.SEOTitle)
	}
	var drafted seoArticleView
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+draft.ID, map[string]any{"updated_at": draft.UpdatedAt, "seo_description": "Draft description"}, draftToken, http.StatusOK, &drafted)
	if drafted.SEODescription != "Draft description" {
		t.Fatalf("editor token on a draft seo description = %q", drafted.SEODescription)
	}
}

// ldScripts are the JSON-LD blocks of a page.
var ldScripts = regexp.MustCompile(`(?s)<script type="application/ld\+json">(.*?)</script>`)

// structuredData decodes every JSON-LD block of a page by its @type; a block that is not valid JSON fails.
func structuredData(t *testing.T, page string) map[string]map[string]any {
	t.Helper()
	found := map[string]map[string]any{}
	for _, match := range ldScripts.FindAllStringSubmatch(page, -1) {
		var data map[string]any
		if err := json.Unmarshal([]byte(match[1]), &data); err != nil {
			t.Fatalf("JSON-LD is not valid JSON: %v\n%s", err, match[1])
		}
		if data["@context"] != "https://schema.org" {
			t.Fatalf("JSON-LD @context = %v", data["@context"])
		}
		kind, _ := data["@type"].(string)
		found[kind] = data
	}
	return found
}

// TestHelpCenterPagesCarryStructuredData: every page has a BreadcrumbList of absolute addresses on
// its own host, an article an Article, and an article that is a list of questions a FAQPage. Nothing
// an author writes can close the script element.
func TestHelpCenterPagesCarryStructuredData(t *testing.T) {
	api, service := testAPI(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "SEO LD", "admin@seo-ld.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "LD help", true)
	guides := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	base := testOrigin + "/help/" + slug
	plain := kbArticle(t, api, admin, map[string]any{"collection_id": guides.ID, "title": "Start here", "status": "published", "body_md": "## Why\n\nBecause.\n\nSee below."})
	faq := kbArticle(t, api, admin, map[string]any{"collection_id": guides.ID, "title": "Billing questions", "status": "published",
		"body_md": "Answers about billing.\n\n## How do I pay?\n\nBy **card** or transfer.\n\nWe send a receipt.\n\n## Can I get a refund?\n\nYes, within 30 days.\n"})
	hostile := kbArticle(t, api, admin, map[string]any{"collection_id": guides.ID, "title": `Break </script><script>alert(1)</script> out`, "status": "published",
		"body_md": "## Is </script><b>this</b> safe?\n\nIt is </script><img src=x onerror=alert(1)>.\n\n## And   this?\n\nYes."})

	home := visit(site, "/help/"+slug, "192.0.2.130").Body.String()
	if crumbs := structuredData(t, home)["BreadcrumbList"]; crumbs == nil || !strings.Contains(fmt.Sprint(crumbs["itemListElement"]), base) {
		t.Fatalf("home BreadcrumbList = %v\n%s", crumbs, home)
	}
	collection := visit(site, "/help/"+slug+"/c/guides", "192.0.2.130").Body.String()
	if crumbs := structuredData(t, collection)["BreadcrumbList"]; crumbs == nil || !strings.Contains(fmt.Sprint(crumbs["itemListElement"]), base+"/c/guides") {
		t.Fatalf("collection BreadcrumbList = %v", crumbs)
	}

	page := visit(site, fmt.Sprintf("/help/%s/a/%d-%s", slug, plain.Number, plain.Slug), "192.0.2.130").Body.String()
	data := structuredData(t, page)
	articleLD, crumbs := data["Article"], data["BreadcrumbList"]
	canonical := fmt.Sprintf("%s/a/%d-%s", base, plain.Number, plain.Slug)
	if articleLD == nil || articleLD["headline"] != "Start here" || articleLD["mainEntityOfPage"] != canonical || articleLD["dateModified"] == nil || articleLD["inLanguage"] != "en" {
		t.Fatalf("Article = %v", articleLD)
	}
	items, _ := crumbs["itemListElement"].([]any)
	if len(items) != 3 || !strings.Contains(fmt.Sprint(items[2]), canonical) || !strings.Contains(fmt.Sprint(items[1]), "Guides") {
		t.Fatalf("article BreadcrumbList = %v", items)
	}
	if data["FAQPage"] != nil {
		t.Fatalf("an article with one heading that is not a question has a FAQPage: %v", data["FAQPage"])
	}

	faqPage := structuredData(t, visit(site, fmt.Sprintf("/help/%s/a/%d-%s", slug, faq.Number, faq.Slug), "192.0.2.130").Body.String())["FAQPage"]
	questions, _ := faqPage["mainEntity"].([]any)
	if len(questions) != 2 {
		t.Fatalf("FAQPage = %v", faqPage)
	}
	first, _ := questions[0].(map[string]any)
	answer, _ := first["acceptedAnswer"].(map[string]any)
	if first["@type"] != "Question" || first["name"] != "How do I pay?" || answer["text"] != "By card or transfer. We send a receipt." {
		t.Fatalf("first question = %v", first)
	}

	raw := visit(site, fmt.Sprintf("/help/%s/a/%d-%s", slug, hostile.Number, hostile.Slug), "192.0.2.130").Body.String()
	for _, block := range ldScripts.FindAllStringSubmatch(raw, -1) {
		if strings.Contains(strings.ToLower(block[1]), "<") || strings.Contains(block[1], " ") {
			t.Fatalf("JSON-LD holds an unescaped character an author wrote:\n%s", block[1])
		}
	}
	if strings.Contains(raw, "<script>alert") || strings.Contains(raw, "<img src=x") {
		t.Fatalf("the article's text escaped into the page:\n%s", raw)
	}
	hostileData := structuredData(t, raw)
	if hostileData["Article"]["headline"] != `Break </script><script>alert(1)</script> out` || hostileData["FAQPage"] == nil {
		t.Fatalf("hostile article structured data = %v", hostileData)
	}
}

// TestArticleSearchFieldsStayInTheirWorkspace: another workspace can neither read nor change an
// article's search title by its id, and its help center never shows it.
func TestArticleSearchFieldsStayInTheirWorkspace(t *testing.T) {
	api, service := testAPI(t)
	site := helpCenterSite(api)
	owner := createSession(t, api, service, "SEO Owner", "admin@seo-owner.example.com")
	other := createSession(t, api, service, "SEO Other", "admin@seo-other.example.com")
	otherSlug := workspaceSlug(t, api, other)
	enableHelpCenter(t, api, owner, "Owner help", true)
	enableHelpCenter(t, api, other, "Other help", true)
	collection := kbCollection(t, api, owner, map[string]any{"name": "Guides"})
	var article seoArticleView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles", map[string]any{"collection_id": collection.ID, "title": "Private", "body_md": "x", "status": "published",
		"seo_title": "Owner search title", "seo_description": "Owner description"}, owner, http.StatusCreated, &article)

	if response := request(t, api, http.MethodGet, "/api/v1/kb/articles/"+article.ID, nil, other, nil); response.Code != http.StatusNotFound {
		t.Fatalf("other workspace read status=%d, want 404", response.Code)
	}
	change := map[string]any{"updated_at": article.UpdatedAt, "seo_title": "Taken over"}
	if response := request(t, api, http.MethodPatch, "/api/v1/kb/articles/"+article.ID, change, other, nil); response.Code != http.StatusNotFound {
		t.Fatalf("other workspace patch status=%d, want 404", response.Code)
	}
	var unchanged seoArticleView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+article.ID, nil, owner, http.StatusOK, &unchanged)
	if unchanged.SEOTitle != "Owner search title" {
		t.Fatalf("seo title after another workspace's patch = %q", unchanged.SEOTitle)
	}
	path := fmt.Sprintf("/help/%s/a/%d-%s", otherSlug, article.Number, article.Slug)
	wantPage(t, visit(site, path, "192.0.2.140"), http.StatusNotFound, nil, []string{"Owner search title", "Owner description"})
}
