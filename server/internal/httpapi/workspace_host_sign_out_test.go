package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// cookieSet is the cookie named name that response sets, or nil.
func cookieSet(response *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

// keptCookies are the cookies of response a browser keeps: set with a value and not expired.
func keptCookies(response *httptest.ResponseRecorder) []*http.Cookie {
	var kept []*http.Cookie
	for _, cookie := range response.Result().Cookies() {
		if cookie.Value != "" && cookie.MaxAge >= 0 {
			kept = append(kept, &http.Cookie{Name: cookie.Name, Value: cookie.Value})
		}
	}
	return kept
}

// A sign-in on APP_ORIGIN that continues on the workspace's host leaves no session on APP_ORIGIN: the
// one-time hand-off is the only credential that crosses over. Signing out on the workspace host therefore
// ends the sign-in, and a later page load of APP_ORIGIN with whatever the sign-in left there does not sign
// the browser back in. A bookmark of APP_ORIGIN still reaches the workspace host.
func TestSigningOutOnTheWorkspaceHostEndsTheAppHostSignIn(t *testing.T) {
	fixture := newCloudAuthFixtureWith(t, "", func(config *httpapi.Config) { config.HelpCenterDomain = "example.net" })
	site := helpCenterSite(fixture.api)
	if _, _, err := fixture.service.CreateWorkspace(context.Background(), "Leave Desk", "admin@cloud.example.com"); err != nil {
		t.Fatal(err)
	}
	host := "leave-desk.example.net"
	exchange := newRequest(t, http.MethodPost, "/api/v1/auth/cloud/exchange", map[string]any{"next": "/settings"}, fixture.token(t, nil), nil)
	exchange.Header.Set("Origin", testOrigin)
	signedIn := serve(site, exchange)
	var result struct {
		ContinueURL *string `json:"continue_url"`
	}
	decode(t, signedIn, &result)
	if signedIn.Code != http.StatusOK || result.ContinueURL == nil {
		t.Fatalf("sign-in on APP_ORIGIN status=%d continue_url=%v", signedIn.Code, result.ContinueURL)
	}
	if cookie := cookieSet(signedIn, "colliedesk_session"); cookie != nil && cookie.Value != "" && cookie.MaxAge >= 0 {
		t.Fatalf("a sign-in that continues on the workspace host set a session cookie on APP_ORIGIN: %v", cookie)
	}
	appCookies := keptCookies(signedIn)

	redeem := newRequest(t, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": handoffToken(t, *result.ContinueURL), "session_type": "cookie"}, "", nil)
	redeem.Host = host
	redeem.Header.Set("Origin", "https://"+host)
	redeemed := serve(site, redeem)
	hostSession := cookieSet(redeemed, "colliedesk_session")
	if redeemed.Code != http.StatusOK || hostSession == nil || hostSession.Value == "" {
		t.Fatalf("hand-off on the workspace host status=%d", redeemed.Code)
	}
	logout := newRequest(t, http.MethodPost, "/api/v1/auth/logout", nil, "", sessionCookieOf(hostSession.Value))
	logout.Host = host
	logout.Header.Set("Origin", "https://"+host)
	if response := serve(site, logout); response.Code != http.StatusNoContent {
		t.Fatalf("sign out on the workspace host status=%d", response.Code)
	}
	var live int
	if err := fixture.pool.QueryRow(context.Background(), `SELECT count(*) FROM sessions s JOIN workspaces w ON w.id=s.workspace_id
		WHERE w.slug='leave-desk' AND s.revoked_at IS NULL`).Scan(&live); err != nil {
		t.Fatal(err)
	}
	if live != 0 {
		t.Fatalf("%d session(s) still live after signing out on the workspace host", live)
	}

	for _, path := range []string{"/", "/settings"} {
		page := newRequest(t, http.MethodGet, path, nil, "", nil)
		for _, cookie := range appCookies {
			page.AddCookie(cookie)
		}
		response := serve(site, page)
		location := response.Header().Get("Location")
		if strings.Contains(location, "handoff=") {
			t.Fatalf("GET %s on APP_ORIGIN after signing out handed off again to %q", path, location)
		}
		// The bookmark still leads to the workspace host, which asks for a sign-in.
		if want := "https://" + host + path; response.Code != http.StatusFound || location != want {
			t.Errorf("GET %s on APP_ORIGIN status=%d location=%q, want 302 to %q", path, response.Code, location, want)
		}
	}
}

// A session on APP_ORIGIN from before the workspace moved to its own host hands off once: the page load
// that hands it off revokes it and clears its cookie, so signing out on the workspace host is the end.
func TestAnOlderAppHostSessionHandsOffOnlyOnce(t *testing.T) {
	f := newLimitsFixtureConfigured(t, false, nil, func(config *httpapi.Config) { config.HelpCenterDomain = "example.net" })
	site := helpCenterSite(f.api)
	code, address := handoff(t, site, f.adminA, testOriginHost, "/settings")
	if code != http.StatusFound || !strings.Contains(address, "#handoff=") {
		t.Fatalf("first page load status=%d location=%q", code, address)
	}
	page := newRequest(t, http.MethodGet, "/settings", nil, "", sessionCookieOf(f.adminA))
	if cookie := cookieSet(serve(site, page), "colliedesk_session"); cookie != nil {
		t.Fatalf("a revoked session's page load set %v", cookie)
	}
	if code, address := handoff(t, site, f.adminA, testOriginHost, "/settings"); strings.Contains(address, "handoff=") {
		t.Fatalf("second page load with the same session status=%d location=%q", code, address)
	}
	if response := request(t, f.api, http.MethodGet, "/api/v1/auth/me", nil, f.adminA, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("the handed-off session still answers /auth/me with status=%d", response.Code)
	}
}

// The page load that hands an older session off clears its cookie on APP_ORIGIN in the same response.
func TestTheHandOffOfAnOlderSessionClearsItsCookie(t *testing.T) {
	f := newLimitsFixtureConfigured(t, false, nil, func(config *httpapi.Config) { config.HelpCenterDomain = "example.net" })
	site := helpCenterSite(f.api)
	response := serve(site, newRequest(t, http.MethodGet, "/settings", nil, "", sessionCookieOf(f.adminA)))
	cookie := cookieSet(response, "colliedesk_session")
	if response.Code != http.StatusFound || cookie == nil || cookie.MaxAge >= 0 {
		t.Fatalf("hand-off status=%d session cookie=%v, want it cleared", response.Code, cookie)
	}
}
