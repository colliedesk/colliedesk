package httpapi_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

const searchConsolePath = "/api/v1/help-center/search-console"

const (
	googleToken = "Ab3dEf6hIj9lMn2pQr5tUv8xYz1-Ab3dEf6hIj9lMn2"
	bingToken   = "0A1B2C3D4E5F60718293A4B5C6D7E8F9"
)

type searchConsoleView struct {
	Google         *string `json:"google_site_verification"`
	Bing           *string `json:"bing_site_verification"`
	ContentVersion int64   `json:"content_version"`
}

func googleMeta(token string) string {
	return `<meta name="google-site-verification" content="` + token + `">`
}

func bingMeta(token string) string {
	return `<meta name="msvalidate.01" content="` + token + `">`
}

// A verification token set in Settings is a <meta> tag on every page of that workspace's help center,
// on its own host and on its custom domain, and nowhere else: not on another workspace's host, not on
// the app.
func TestSearchConsoleTokensAreMetaTagsOnlyOnTheirOwnHelpCenter(t *testing.T) {
	api, service, _, dns := testAPIWithConfig(t, func(config *httpapi.Config) { config.HelpCenterDomain = seoHost })
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Console Alpha", "admin@console-alpha.example.com")
	other := createSession(t, api, service, "Console Bravo", "admin@console-bravo.example.com")
	slug, otherSlug := workspaceSlug(t, api, admin), workspaceSlug(t, api, other)
	hostA, hostB := slug+"."+seoHost, otherSlug+"."+seoHost
	enableHelpCenter(t, api, admin, "Alpha help", true)
	enableHelpCenter(t, api, other, "Bravo help", true)
	guides := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": guides.ID, "title": "Start here", "status": "published", "body_md": "Sign in."})
	articlePath := fmt.Sprintf("/a/%d-%s", article.Number, article.Slug)
	pages := []string{"/", "/c/guides", articlePath, "/search?q=start", "/no-such-page"}

	var before searchConsoleView
	kbCall(t, api, http.MethodGet, searchConsolePath, nil, admin, http.StatusOK, &before)
	if before.Google != nil || before.Bing != nil {
		t.Fatalf("a new workspace has tokens: %+v", before)
	}
	for _, path := range pages {
		wantPage(t, visit(site, path, "192.0.2.110", "Host", hostA), statusOf(path), nil, []string{"google-site-verification", "msvalidate.01"})
	}

	var saved searchConsoleView
	kbCall(t, api, http.MethodPut, searchConsolePath, map[string]any{"google_site_verification": googleToken, "bing_site_verification": bingToken}, admin, http.StatusOK, &saved)
	if saved.Google == nil || *saved.Google != googleToken || saved.Bing == nil || *saved.Bing != bingToken || saved.ContentVersion == 0 {
		t.Fatalf("saved = %+v", saved)
	}
	for _, path := range pages {
		wantPage(t, visit(site, path, "192.0.2.110", "Host", hostA), statusOf(path), []string{googleMeta(googleToken), bingMeta(bingToken)}, nil)
		// Bravo has no such collection or article, so some of these are its 404 page, which is a page too.
		other := visit(site, path, "192.0.2.111", "Host", hostB).Body.String()
		for _, unwanted := range []string{"google-site-verification", "msvalidate.01", googleToken, bingToken} {
			if strings.Contains(other, unwanted) {
				t.Errorf("%s%s carries %q of another workspace", hostB, path, unwanted)
			}
		}
	}
	// The app's own pages and the API never carry it.
	for _, path := range []string{"/", "/settings", "/api/v1/config"} {
		if response := visit(site, path, "192.0.2.112"); strings.Contains(response.Body.String(), googleToken) || strings.Contains(response.Body.String(), bingToken) {
			t.Errorf("app %s carries a workspace's verification token", path)
		}
	}
	// The other workspace reads and sets its own tokens only.
	var theirs searchConsoleView
	kbCall(t, api, http.MethodGet, searchConsolePath, nil, other, http.StatusOK, &theirs)
	if theirs.Google != nil || theirs.Bing != nil {
		t.Fatalf("another workspace reads %+v", theirs)
	}

	// The same pages on a custom domain, and again nowhere else once the domain is removed.
	const custom = "help.console-alpha.example"
	view := setDomain(t, api, admin, custom, http.StatusOK)
	dns.set("_help-center-verify."+custom, view.token())
	checkDomain(t, api, admin, http.StatusOK)
	for _, path := range pages {
		wantPage(t, visit(site, path, "192.0.2.113", "Host", custom), statusOf(path), []string{googleMeta(googleToken), bingMeta(bingToken)}, nil)
	}
	wantPage(t, visit(site, "/", "192.0.2.114", "Host", hostA), http.StatusMovedPermanently, nil, []string{googleToken})
}

// statusOf is what a page of the walk answers.
func statusOf(path string) int {
	if path == "/no-such-page" {
		return http.StatusNotFound
	}
	return http.StatusOK
}

// The token is stored and served only as a verification token: a value that could close the attribute,
// open a tag or carry markup is refused and nothing is saved.
func TestSearchConsoleRefusesATokenThatIsNotOne(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Console Refuse", "admin@console-refuse.example.com")
	kbCall(t, api, http.MethodPut, searchConsolePath, map[string]any{"google_site_verification": googleToken}, admin, http.StatusOK, nil)

	for _, bad := range []string{
		`"><script>alert(1)</script>`,
		googleToken + `" onload="x`,
		`<meta name="google-site-verification" content="` + googleToken + `">`,
		"short",
		googleToken + " " + googleToken,
		googleToken + "\n",
		strings.Repeat("a", 101),
		"tök" + googleToken,
	} {
		for _, field := range []string{"google_site_verification", "bing_site_verification"} {
			response := request(t, api, http.MethodPut, searchConsolePath, map[string]any{field: bad}, admin, nil)
			if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), `"fields"`) || !strings.Contains(response.Body.String(), field) {
				t.Errorf("%s = %q: status=%d body=%s, want 422 naming the field", field, bad, response.Code, response.Body.String())
			}
		}
	}
	var after searchConsoleView
	kbCall(t, api, http.MethodGet, searchConsolePath, nil, admin, http.StatusOK, &after)
	if after.Google == nil || *after.Google != googleToken || after.Bing != nil {
		t.Fatalf("a refused save changed the tokens: %+v", after)
	}
	if response := request(t, api, http.MethodPut, searchConsolePath, map[string]any{"token": googleToken}, admin, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status=%d", response.Code)
	}
}

// A field left out stays as it is; null or an empty string removes the tag. Each change makes a new
// version of every page, so a cache or crawler holding the old page asks again.
func TestSearchConsoleLeftOutKeepsNullClearsAndChangesThePagesETag(t *testing.T) {
	api, service := testAPI(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Console Clear", "admin@console-clear.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Clear help", true)
	home := "/help/" + slug

	kbCall(t, api, http.MethodPut, searchConsolePath, map[string]any{"google_site_verification": googleToken, "bing_site_verification": bingToken}, admin, http.StatusOK, nil)
	first := visit(site, home, "192.0.2.113")
	wantPage(t, first, http.StatusOK, []string{googleMeta(googleToken), bingMeta(bingToken)}, nil)

	var kept searchConsoleView
	kbCall(t, api, http.MethodPut, searchConsolePath, map[string]any{"bing_site_verification": nil}, admin, http.StatusOK, &kept)
	if kept.Google == nil || *kept.Google != googleToken || kept.Bing != nil {
		t.Fatalf("after clearing Bing: %+v", kept)
	}
	second := visit(site, home, "192.0.2.113", "If-None-Match", first.Header().Get("ETag"))
	wantPage(t, second, http.StatusOK, []string{googleMeta(googleToken)}, []string{"msvalidate.01"})
	if second.Header().Get("ETag") == first.Header().Get("ETag") {
		t.Fatalf("the page kept its ETag after a token was removed")
	}
	kbCall(t, api, http.MethodPut, searchConsolePath, map[string]any{"google_site_verification": ""}, admin, http.StatusOK, nil)
	wantPage(t, visit(site, home, "192.0.2.113"), http.StatusOK, nil, []string{"google-site-verification"})
}

// Setting a token changes the public help center, so it needs the publish permission; without it a
// token files a request a person approves, and an agent session cannot set it at all.
func TestSearchConsoleNeedsPublishPermission(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Console Publish", "admin@console-publish.example.com")
	agent := agentSession(t, api, admin, "agent@console-publish.example.com")
	writer := issueCustomToken(t, api, admin, "Settings only", auth.PermHelpCenterRead, auth.PermHelpCenterUpdate).Token
	reader := issueCustomToken(t, api, admin, "Reader", auth.PermHelpCenterRead).Token
	publisher := issueCustomToken(t, api, admin, "Publisher", auth.PermHelpCenterRead, auth.PermHelpCenterUpdate, auth.PermKBArticlesPublish).Token

	if response := request(t, api, http.MethodPut, searchConsolePath, map[string]any{"google_site_verification": googleToken}, agent, nil); response.Code != http.StatusForbidden {
		t.Fatalf("an agent session set the token: status=%d", response.Code)
	}
	kbCall(t, api, http.MethodGet, searchConsolePath, nil, agent, http.StatusOK, nil)
	kbCall(t, api, http.MethodGet, searchConsolePath, nil, reader, http.StatusOK, nil)
	if response := request(t, api, http.MethodPut, searchConsolePath, map[string]any{"google_site_verification": googleToken}, reader, nil); response.Code != http.StatusAccepted {
		t.Fatalf("a read token's save: status=%d, want 202 and a request for a person", response.Code)
	}
	filed := accepted(t, api, http.MethodPut, searchConsolePath, map[string]any{"google_site_verification": googleToken}, writer)
	var unchanged searchConsoleView
	kbCall(t, api, http.MethodGet, searchConsolePath, nil, admin, http.StatusOK, &unchanged)
	if unchanged.Google != nil {
		t.Fatalf("token set before approval: %+v", unchanged)
	}
	// A request carries only the fields the route takes.
	if response := request(t, api, http.MethodPut, searchConsolePath, map[string]any{"google_site_verification": googleToken, "api_key": "sk-hidden"}, writer, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("an extra field was filed: status=%d", response.Code)
	}
	if response := approve(t, api, admin, filed.RequestID, nil); response.Code != http.StatusOK {
		t.Fatalf("approve status=%d body=%s", response.Code, response.Body.String())
	}
	var approved searchConsoleView
	kbCall(t, api, http.MethodGet, searchConsolePath, nil, admin, http.StatusOK, &approved)
	if approved.Google == nil || *approved.Google != googleToken {
		t.Fatalf("after approval: %+v", approved)
	}
	var published searchConsoleView
	kbCall(t, api, http.MethodPut, searchConsolePath, map[string]any{"bing_site_verification": bingToken}, publisher, http.StatusOK, &published)
	if published.Bing == nil || *published.Bing != bingToken || published.Google == nil {
		t.Fatalf("publisher save: %+v", published)
	}
}

// The export carries both tokens, so a move away keeps them; a workspace without any exports null.
func TestExportCarriesTheSearchConsoleTokens(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Console Export", "admin@console-export.example.com")
	enableHelpCenter(t, api, admin, "Export help", true)
	export := func() map[string]any {
		response := request(t, api, http.MethodGet, "/api/v1/export", nil, admin, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("export status=%d body=%s", response.Code, response.Body.String())
		}
		lines := ndjsonLines(t, unzip(t, response.Body.Bytes())["help-center.ndjson"])
		if len(lines) != 1 {
			t.Fatalf("help-center.ndjson = %v", lines)
		}
		return lines[0]
	}
	if line := export(); line["google_site_verification"] != nil || line["bing_site_verification"] != nil {
		t.Fatalf("a new help center exports tokens: %v", line)
	}
	kbCall(t, api, http.MethodPut, searchConsolePath, map[string]any{"google_site_verification": googleToken, "bing_site_verification": bingToken}, admin, http.StatusOK, nil)
	if line := export(); line["google_site_verification"] != googleToken || line["bing_site_verification"] != bingToken {
		t.Fatalf("export = %v, want both tokens", line)
	}
}
