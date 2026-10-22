package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// The app, the API, MCP and attachments answer only on APP_ORIGIN's host, and on loopback and IP
// address hosts for health checks and the proxy's certificate question. Every other host name is the
// help center's: an active custom domain serves it, and any other name, a removed custom domain
// included, gets the help center's 404 and never the app, even with a valid session.
func TestTheAppAnswersOnlyOnItsOwnHost(t *testing.T) {
	api, service, _, dns := testAPIWithConfig(t, func(config *httpapi.Config) { config.HelpCenterDomain = "help.example.net" })
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Own Host", "admin@own-host.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Own help", true)
	const host = "help.own-host.example"
	view := setDomain(t, api, admin, host, http.StatusOK)
	dns.set("_help-center-verify."+host, view.token())
	checkDomain(t, api, admin, http.StatusOK)
	kbCall(t, api, http.MethodDelete, domainPath, nil, admin, http.StatusOK, nil)

	appPaths := []string{"/api/v1/config", "/api/v1/auth/me", "/mcp", "/", "/settings", "/help/" + slug,
		"/api/v1/attachments/0b6f8a8e-3c4d-4e5f-8a9b-0c1d2e3f4a5b"}
	for _, name := range []string{host, "HELP.Own-Host.Example.:443", "never-claimed.example", "help.example.net", "a.b.help.example.net", "intranet"} {
		for _, path := range appPaths {
			req := newRequest(t, http.MethodGet, path, nil, admin, nil)
			req.Host = name
			response := serve(site, req)
			if response.Code != http.StatusNotFound || !strings.HasPrefix(response.Header().Get("Content-Type"), "text/html") || strings.Contains(response.Body.String(), "Own help") {
				t.Errorf("%s%s status=%d type=%q, want the help center's 404", name, path, response.Code, response.Header().Get("Content-Type"))
			}
		}
		req := newRequest(t, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": "x"}, "", nil)
		req.Host = name
		if response := serve(site, req); response.Code != http.StatusNotFound {
			t.Errorf("POST on %s status=%d", name, response.Code)
		}
	}
	for _, name := range []string{"desk.example.com", "DESK.example.com:443", "127.0.0.1:8080", "127.0.0.1", "[::1]:8080", "localhost:8080", "10.0.0.5:8080"} {
		req := newRequest(t, http.MethodGet, "/api/v1/config", nil, "", nil)
		req.Host = name
		if response := serve(site, req); response.Code != http.StatusOK || !strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") {
			t.Errorf("/api/v1/config on %s status=%d type=%q", name, response.Code, response.Header().Get("Content-Type"))
		}
	}
	// The proxy's certificate question on loopback reaches the API.
	req := newRequest(t, http.MethodGet, "/api/v1/public/help-center/tls-ask?domain="+slug+".help.example.net", nil, "", nil)
	req.Host = "127.0.0.1:8080"
	if response := serve(site, req); response.Code != http.StatusOK {
		t.Errorf("tls-ask on loopback status=%d", response.Code)
	}
}
