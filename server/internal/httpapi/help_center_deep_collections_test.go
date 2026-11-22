package httpapi_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// crumbNav is the breadcrumb navigation of a help center page.
func crumbNav(t *testing.T, page string) string {
	t.Helper()
	start := strings.Index(page, `<nav class="crumbs"`)
	if start < 0 {
		t.Fatalf("page has no breadcrumbs:\n%s", page)
	}
	end := strings.Index(page[start:], "</nav>")
	return page[start : start+end]
}

// TestHelpCenterFoldsTheMiddleOfADeepBreadcrumb: an article six collections deep shows Home, the
// first level and the last two on the line; the levels between fold into "…", a <details> that opens
// the full path without a script. Search engines still get every level. Three levels do not fold.
func TestHelpCenterFoldsTheMiddleOfADeepBreadcrumb(t *testing.T) {
	api, service := testAPI(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Deep HC", "admin@deep-hc.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Deep help", true)

	names := []string{"Level one", "Level two", "Level three", "Level four", "Level five", "Level six"}
	var parent any
	var ids []string
	for _, name := range names {
		collection := kbCollection(t, api, admin, map[string]any{"name": name, "parent_id": parent})
		parent, ids = collection.ID, append(ids, collection.ID)
	}
	deep := kbArticle(t, api, admin, map[string]any{"collection_id": ids[5], "title": "Deep answer", "status": "published", "body_md": "Found it."})
	shallow := kbArticle(t, api, admin, map[string]any{"collection_id": ids[2], "title": "Shallow answer", "status": "published", "body_md": "Here."})

	response := visit(site, fmt.Sprintf("/help/%s/a/%d-%s", slug, deep.Number, deep.Slug), "192.0.2.140")
	wantPage(t, response, http.StatusOK, nil, nil)
	page := response.Body.String()
	nav := crumbNav(t, page)
	open, close := strings.Index(nav, "<details"), strings.Index(nav, "</details>")
	if open < 0 || close < open {
		t.Fatalf("a six-level breadcrumb has no fold:\n%s", nav)
	}
	before, folded, after := nav[:open], nav[open:close], nav[close:]
	if !strings.Contains(before, ">Home<") || !strings.Contains(before, ">Level one<") || strings.Contains(before, "Level two") {
		t.Errorf("before the fold = %s, want Home and Level one only", before)
	}
	if !strings.Contains(folded, "<summary") || !strings.Contains(folded, "Show the full path") || !strings.Contains(folded, "…") {
		t.Errorf("fold = %s, want a summary labelled \"Show the full path\" showing …", folded)
	}
	for _, name := range names {
		if !strings.Contains(folded, ">"+name+"<") {
			t.Errorf("the full path lacks %q: %s", name, folded)
		}
	}
	if !strings.Contains(after, ">Level five<") || !strings.Contains(after, ">Level six<") || strings.Contains(after, "Level four") {
		t.Errorf("after the fold = %s, want Level five and Level six only", after)
	}
	items, _ := structuredData(t, page)["BreadcrumbList"]["itemListElement"].([]any)
	if len(items) != 8 {
		t.Errorf("BreadcrumbList has %d items, want 8 (home, six levels, the article): %v", len(items), items)
	}

	sixth := visit(site, "/help/"+slug+"/c/level-one/level-two/level-three/level-four/level-five/level-six", "192.0.2.140")
	wantPage(t, sixth, http.StatusOK, []string{"Deep answer", "<details"}, nil)

	short := visit(site, fmt.Sprintf("/help/%s/a/%d-%s", slug, shallow.Number, shallow.Slug), "192.0.2.140")
	wantPage(t, short, http.StatusOK, []string{">Level three<"}, nil)
	if strings.Contains(crumbNav(t, short.Body.String()), "<details") {
		t.Errorf("a three-level breadcrumb folds:\n%s", crumbNav(t, short.Body.String()))
	}
}
