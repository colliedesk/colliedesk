package httpapi_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// onWorkspaceHost sends a request with no session to path on a workspace's host name.
func onWorkspaceHost(site http.Handler, method, host, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr, req.Host = "192.0.2.70:4242", host
	return serve(site, req)
}

func wantRedirect(t *testing.T, response *httptest.ResponseRecorder, status int, location string) {
	t.Helper()
	if response.Code != status || response.Header().Get("Location") != location {
		t.Fatalf("status=%d location=%q, want %d to %q", response.Code, response.Header().Get("Location"), status, location)
	}
}

// The workspace's own host name runs the desk: the app and its API answer there, as on APP_ORIGIN. A
// slug no workspace has, and a reserved name, keep the help center's 404 for the app's pages.
func TestTheWorkspaceHostRunsTheDesk(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.HelpCenterDomain = "example.net" })
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Front Door", "admin@front-door.example.com")
	slug := workspaceSlug(t, api, admin)
	host := slug + ".example.net"

	config := onWorkspaceHost(site, http.MethodGet, host, "/api/v1/config")
	if config.Code != http.StatusOK || !strings.HasPrefix(config.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("/api/v1/config on the workspace host status=%d type=%q", config.Code, config.Header().Get("Content-Type"))
	}
	for _, path := range []string{"/", "/settings", "/conversations/0b6f8a8e-3c4d-4e5f-8a9b-0c1d2e3f4a5b", "/sign-in"} {
		app := onWorkspaceHost(site, http.MethodGet, testOriginHost, path)
		got := onWorkspaceHost(site, http.MethodGet, host, path)
		if got.Code != app.Code || got.Header().Get("Location") != "" || got.Header().Get("Content-Type") != app.Header().Get("Content-Type") {
			t.Errorf("%s on the workspace host status=%d type=%q, want the app's %d %q", path, got.Code, got.Header().Get("Content-Type"), app.Code, app.Header().Get("Content-Type"))
		}
	}
	me := newRequest(t, http.MethodGet, "/api/v1/auth/me", nil, "", sessionCookieOf(admin))
	me.Host = host
	if response := serve(site, me); response.Code != http.StatusOK {
		t.Fatalf("me on the workspace host status=%d body=%s", response.Code, response.Body.String())
	}
	for _, name := range []string{"nobody-here.example.net", "www.example.net", "example.net", "a." + host} {
		response := onWorkspaceHost(site, http.MethodGet, name, "/")
		if response.Code != http.StatusNotFound || !strings.HasPrefix(response.Header().Get("Content-Type"), "text/html") {
			t.Errorf("%s/ status=%d type=%q, want the help center's 404", name, response.Code, response.Header().Get("Content-Type"))
		}
	}
}

// A session belongs to the host it was made on: on a workspace host only that workspace's sessions and
// agent tokens act, and another workspace's session is refused and its cookie cleared. Writes made with
// the cookie must come from the workspace host's own origin.
func TestAWorkspaceHostActsOnlyForItsOwnWorkspace(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.HelpCenterDomain = "example.net" })
	site := helpCenterSite(api)
	alpha := createSession(t, api, service, "Host Alpha", "admin@host-alpha.example.com")
	beta := createSession(t, api, service, "Host Beta", "admin@host-beta.example.com")
	alphaHost := workspaceSlug(t, api, alpha) + ".example.net"
	betaHost := workspaceSlug(t, api, beta) + ".example.net"

	for name, req := range map[string]*http.Request{
		"cookie": newRequest(t, http.MethodGet, "/api/v1/auth/me", nil, "", sessionCookieOf(alpha)),
		"bearer": newRequest(t, http.MethodGet, "/api/v1/auth/me", nil, alpha, nil),
	} {
		req.Host = betaHost
		response := serve(site, req)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("alpha's %s on beta's host status=%d", name, response.Code)
		}
		if name == "cookie" && !strings.Contains(response.Header().Get("Set-Cookie"), "colliedesk_session=;") {
			t.Errorf("alpha's cookie on beta's host was not cleared: %q", response.Header().Get("Set-Cookie"))
		}
	}
	// APP_ORIGIN keeps every session working.
	if response := request(t, site, http.MethodGet, "/api/v1/auth/me", nil, "", sessionCookieOf(alpha)); response.Code != http.StatusOK {
		t.Fatalf("alpha's cookie on the app host status=%d", response.Code)
	}

	write := func(origin string) int {
		req := newRequest(t, http.MethodPatch, "/api/v1/workspace/name", map[string]any{"name": "Host Alpha"}, "", sessionCookieOf(alpha))
		req.Host = alphaHost
		req.Header.Set("Origin", origin)
		return serve(site, req).Code
	}
	if code := write("https://" + alphaHost); code != http.StatusOK {
		t.Fatalf("a write from the workspace host's own origin status=%d", code)
	}
	for _, origin := range []string{testOrigin, "https://" + betaHost, "https://evil.example"} {
		if code := write(origin); code != http.StatusForbidden {
			t.Errorf("a write on alpha's host from %s status=%d, want 403", origin, code)
		}
	}
}

// The help center on a workspace's host lives under /help: its links, canonical addresses and the
// address the API hands out carry /help, an address from before the move answers 301 to the same
// path under /help, robots.txt stays at the root, and the path address moves to the host under /help.
func TestTheHelpCenterOnTheWorkspaceHostLivesUnderHelp(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.HelpCenterDomain = "example.net" })
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Moved Help", "admin@moved-help.example.com")
	slug := workspaceSlug(t, api, admin)
	host := slug + ".example.net"
	enableHelpCenter(t, api, admin, "Moved help", true)
	guides := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": guides.ID, "title": "Start here", "status": "published", "body_md": "Hello"})
	articlePath := fmt.Sprintf("/a/%d-%s", article.Number, article.Slug)

	wantPage(t, onWorkspaceHost(site, http.MethodGet, host, "/help"), http.StatusOK,
		[]string{"Moved help", `href="/help/c/guides"`, `href="/help` + articlePath + `"`, `action="/help/search"`}, nil)
	wantPage(t, onWorkspaceHost(site, http.MethodGet, host, "/help"+articlePath), http.StatusOK,
		[]string{"Start here", `<link rel="canonical" href="https://` + host + `/help` + articlePath + `">`}, nil)
	wantPage(t, onWorkspaceHost(site, http.MethodGet, host, "/help/c/guides"), http.StatusOK, []string{"Guides"}, nil)
	wantPage(t, onWorkspaceHost(site, http.MethodGet, host, "/help/search?q=start"), http.StatusOK, []string{"Start here"}, nil)
	wantRedirect(t, onWorkspaceHost(site, http.MethodGet, host, "/help/"), http.StatusMovedPermanently, "/help")

	for old, want := range map[string]string{
		articlePath + "?ref=1": "/help" + articlePath + "?ref=1",
		"/c/guides":            "/help/c/guides",
		"/search?q=start":      "/help/search?q=start",
		"/sitemap.xml":         "/help/sitemap.xml",
		"/llms.txt":            "/help/llms.txt",
		"/images/x":            "/help/images/x",
		"/media/x":             "/help/media/x",
	} {
		wantRedirect(t, onWorkspaceHost(site, http.MethodGet, host, old), http.StatusMovedPermanently, want)
	}
	// An old Freshdesk address reaches its page in one step.
	wantRedirect(t, onWorkspaceHost(site, http.MethodGet, host, "/support/solutions"), http.StatusMovedPermanently, "https://"+host+"/help")
	robots := onWorkspaceHost(site, http.MethodGet, host, "/robots.txt")
	if robots.Code != http.StatusOK || robots.Body.String() != "User-agent: *\nDisallow:\n\nSitemap: https://"+host+"/help/sitemap.xml\n" {
		t.Fatalf("robots.txt status=%d body=%q", robots.Code, robots.Body.String())
	}
	if response := onWorkspaceHost(site, http.MethodGet, host, "/help/robots.txt"); response.Code != http.StatusNotFound {
		t.Errorf("/help/robots.txt status=%d, want 404", response.Code)
	}

	moved := visit(site, "/help/"+slug+articlePath+"?q=1", "192.0.2.71")
	wantRedirect(t, moved, http.StatusMovedPermanently, "https://"+host+"/help"+articlePath+"?q=1")
	wantRedirect(t, visit(site, "/help/"+slug, "192.0.2.71"), http.StatusMovedPermanently, "https://"+host+"/help")
	var center struct {
		URL string `json:"url"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/help-center", nil, admin, http.StatusOK, &center)
	if center.URL != "https://"+host+"/help" {
		t.Fatalf("help center url=%q", center.URL)
	}
}

// With the help center domain at the apex of APP_ORIGIN's host (app on desk.example.com, workspaces on
// <slug>.example.com), the app's own host stays the app's, a workspace host runs its desk, and a
// claim's CNAME target hc-<claim>.example.com stays a claim, never a workspace.
func TestTheWorkspaceHostBesideTheAppOnTheSameApex(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.HelpCenterDomain = "example.com" })
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Apex Team", "admin@apex-team.example.org")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Apex help", true)

	config := onWorkspaceHost(site, http.MethodGet, testOriginHost, "/api/v1/config")
	if config.Code != http.StatusOK || !strings.HasPrefix(config.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("/api/v1/config on the app host status=%d type=%q", config.Code, config.Header().Get("Content-Type"))
	}
	me := newRequest(t, http.MethodGet, "/api/v1/auth/me", nil, admin, nil)
	me.Host = slug + ".example.com"
	if response := serve(site, me); response.Code != http.StatusOK {
		t.Fatalf("me on the workspace host status=%d", response.Code)
	}
	wantPage(t, onWorkspaceHost(site, http.MethodGet, slug+".example.com", "/help"), http.StatusOK, []string{"Apex help"}, nil)

	target := setDomain(t, api, admin, "help.apex-team.example.org", http.StatusOK).cnameTarget()
	if !strings.HasPrefix(target, "hc-") || !strings.HasSuffix(target, ".example.com") {
		t.Fatalf("claim target=%q", target)
	}
	wantRedirect(t, onWorkspaceHost(site, http.MethodGet, target, "/c/guides"), http.StatusMovedPermanently, "https://"+slug+".example.com/help/c/guides")
	if response := onWorkspaceHost(site, http.MethodGet, "hc-0123456789abcdef01234567.example.com", "/"); response.Code != http.StatusNotFound {
		t.Fatalf("an unheld claim target status=%d location=%q", response.Code, response.Header().Get("Location"))
	}
}

// Staging runs the app on the help center domain itself (APP_ORIGIN https://staging.example.com,
// HELP_CENTER_DOMAIN staging.example.com): that host stays the app, and <slug>.staging.example.com runs
// each workspace's desk.
func TestTheAppHostCanBeTheHelpCenterDomainItself(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		config.Origin = "https://staging.example.com"
		config.HelpCenterDomain = "staging.example.com"
	})
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Stage Team", "admin@stage-team.example.org")
	me := newRequest(t, http.MethodGet, "/api/v1/auth/me", nil, admin, nil)
	me.Host = "staging.example.com"
	if response := serve(site, me); response.Code != http.StatusOK {
		t.Fatalf("me on the app host status=%d", response.Code)
	}
	slug := workspaceSlug(t, api, admin)
	me = newRequest(t, http.MethodGet, "/api/v1/auth/me", nil, admin, nil)
	me.Host = slug + ".staging.example.com"
	if response := serve(site, me); response.Code != http.StatusOK {
		t.Fatalf("me on the workspace host status=%d", response.Code)
	}
	var center struct {
		URL string `json:"url"`
	}
	enableHelpCenter(t, api, admin, "Stage help", true)
	kbCall(t, api, http.MethodGet, "/api/v1/help-center", nil, admin, http.StatusOK, &center)
	if center.URL != "https://"+slug+".staging.example.com/help" {
		t.Fatalf("help center url=%q", center.URL)
	}
}

// Renaming the workspace's address on its host moves the desk to the new host: the answer says where the
// browser continues, signed in there, and the old host's cookie no longer signs in.
func TestRenamingTheWorkspaceAddressMovesTheDeskToItsNewHost(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.HelpCenterDomain = "example.net" })
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Rename Desk", "admin@rename-desk.example.com")
	req := newRequest(t, http.MethodPatch, "/api/v1/workspace/slug", map[string]any{"slug": "renamed-desk", "next": "/settings/channels/type/email"}, "", sessionCookieOf(admin))
	req.Host = "rename-desk.example.net"
	req.Header.Set("Origin", "https://rename-desk.example.net")
	response := serve(site, req)
	var renamed struct {
		Slug        string  `json:"slug"`
		ContinueURL *string `json:"continue_url"`
	}
	decode(t, response, &renamed)
	if response.Code != http.StatusOK || renamed.Slug != "renamed-desk" || renamed.ContinueURL == nil ||
		!strings.HasPrefix(*renamed.ContinueURL, "https://renamed-desk.example.net/sign-in?next=%2Fsettings%2Fchannels%2Ftype%2Femail#handoff=") {
		t.Fatalf("rename status=%d body=%s", response.Code, response.Body.String())
	}
	if code := redeemOn(t, site, "renamed-desk.example.net", handoffToken(t, *renamed.ContinueURL)); code != http.StatusOK {
		t.Fatalf("continue on the new host status=%d", code)
	}
	me := newRequest(t, http.MethodGet, "/api/v1/auth/me", nil, "", sessionCookieOf(admin))
	me.Host = "rename-desk.example.net"
	if response := serve(site, me); response.Code != http.StatusUnauthorized {
		t.Fatalf("the old host's cookie after the rename status=%d", response.Code)
	}
}
