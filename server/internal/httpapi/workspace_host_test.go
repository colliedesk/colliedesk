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

// The root of a workspace's own host name is the front door to its desk: it leads to the sign-in page
// for that workspace, whether or not its help center is on. A slug no workspace has, and a reserved
// name, keep the help center's 404; the root accepts only GET and HEAD.
func TestTheWorkspaceHostRootLeadsToItsDeskSignIn(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.HelpCenterDomain = "help.example.net" })
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Front Door", "admin@front-door.example.com")
	slug := workspaceSlug(t, api, admin)
	host := slug + ".help.example.net"

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		response := onWorkspaceHost(site, method, host, "/")
		wantRedirect(t, response, http.StatusFound, "https://desk.example.com/sign-in?workspace="+slug)
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s / cache-control=%q", method, response.Header().Get("Cache-Control"))
		}
	}
	wantRedirect(t, onWorkspaceHost(site, http.MethodGet, strings.ToUpper(host)+":443", "/"), http.StatusFound, "https://desk.example.com/sign-in?workspace="+slug)
	if response := onWorkspaceHost(site, http.MethodPost, host, "/"); response.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST / status=%d", response.Code)
	}
	for _, name := range []string{"nobody-here.help.example.net", "www.help.example.net", "help.example.net", "a." + host} {
		response := onWorkspaceHost(site, http.MethodGet, name, "/")
		if response.Code != http.StatusNotFound || response.Header().Get("Location") != "" || !strings.HasPrefix(response.Header().Get("Content-Type"), "text/html") {
			t.Errorf("%s/ status=%d location=%q, want the help center's 404", name, response.Code, response.Header().Get("Location"))
		}
	}
}

// The help center on a workspace's host lives under /help: its links, canonical addresses and the
// address the API hands out carry /help, an address from before the move answers 301 to the same
// path under /help, robots.txt stays at the root, and the path address moves to the host under /help.
func TestTheHelpCenterOnTheWorkspaceHostLivesUnderHelp(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.HelpCenterDomain = "help.example.net" })
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Moved Help", "admin@moved-help.example.com")
	slug := workspaceSlug(t, api, admin)
	host := slug + ".help.example.net"
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
		"/helpdesk":            "/help/helpdesk",
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
	// Nothing of the app answers on the workspace's host.
	for _, path := range []string{"/api/v1/config", "/mcp"} {
		if response := onWorkspaceHost(site, http.MethodGet, host, path); response.Code == http.StatusOK {
			t.Errorf("%s on the workspace host status=%d", path, response.Code)
		}
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
// <slug>.example.com), the app's own host stays the app's, a workspace host leads to its desk, and a
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
	if response := onWorkspaceHost(site, http.MethodGet, testOriginHost, "/"); response.Code == http.StatusFound {
		t.Fatalf("the app host root redirects to %q", response.Header().Get("Location"))
	}
	wantRedirect(t, onWorkspaceHost(site, http.MethodGet, slug+".example.com", "/"), http.StatusFound, "https://desk.example.com/sign-in?workspace="+slug)
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
