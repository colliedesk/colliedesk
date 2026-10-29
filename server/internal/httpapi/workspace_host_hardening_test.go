package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// appSession is a new session on APP_ORIGIN for the agent with email, signed in through a pairing link.
func appSession(t *testing.T, f limitsFixture, slug, email string) string {
	t.Helper()
	address, err := f.billing.service.Pair(context.Background(), slug, email)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Token string `json:"token"`
	}
	decode(t, request(t, f.api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, address)}, "", nil), &body)
	return body.Token
}

// redeemAs redeems token on host for a session of sessionType and returns the status.
func redeemAs(t *testing.T, site http.Handler, host, token, sessionType string) int {
	t.Helper()
	req := newRequest(t, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": token, "session_type": sessionType}, "", nil)
	req.Host = host
	req.Header.Set("Origin", "https://"+host)
	return serve(site, req).Code
}

// A hand-off signs a browser in on its workspace's host only, and only as a cookie session: on
// APP_ORIGIN, or asked for a bearer token, it is refused and stays unused. Pairing links still give a
// bearer token on APP_ORIGIN.
func TestAHandOffSignsInOnlyOnItsWorkspaceHostAsACookie(t *testing.T) {
	f := newLimitsFixtureConfigured(t, false, nil, func(config *httpapi.Config) { config.HelpCenterDomain = "example.net" })
	site := helpCenterSite(f.api)
	alphaHost := "limits-alpha.example.net"
	_, address := handoff(t, site, f.adminA, testOriginHost, "/")
	token := handoffToken(t, address)
	if code := redeemAs(t, site, testOriginHost, token, "bearer"); code != http.StatusUnauthorized {
		t.Fatalf("a hand-off as a bearer token on APP_ORIGIN status=%d", code)
	}
	if code := redeemAs(t, site, testOriginHost, token, "cookie"); code != http.StatusUnauthorized {
		t.Fatalf("a hand-off as a cookie on APP_ORIGIN status=%d", code)
	}
	if code := redeemAs(t, site, alphaHost, token, "bearer"); code != http.StatusUnauthorized {
		t.Fatalf("a hand-off as a bearer token on its workspace host status=%d", code)
	}
	if code := redeemAs(t, site, alphaHost, token, "cookie"); code != http.StatusOK {
		t.Fatalf("the refused hand-off as a cookie on its workspace host status=%d", code)
	}
	if session := appSession(t, f, "limits-alpha", "admin@limits-alpha.example.com"); session == "" {
		t.Fatal("a pairing link gave no bearer token on APP_ORIGIN")
	}
}

// A session sent to another workspace's host is refused before it is touched: its expiry is not pushed
// back, so a session that keeps being sent to the wrong host still ends on time.
func TestASessionRefusedOnAnotherWorkspaceHostIsNotExtended(t *testing.T) {
	f := newLimitsFixtureConfigured(t, false, nil, func(config *httpapi.Config) { config.HelpCenterDomain = "example.net" })
	site := helpCenterSite(f.api)
	read := func() (seen, expires time.Time) {
		t.Helper()
		if err := f.pool.QueryRow(context.Background(), `SELECT last_seen_at,expires_at FROM sessions WHERE workspace_id=$1 AND revoked_at IS NULL`, f.workspaceA).Scan(&seen, &expires); err != nil {
			t.Fatal(err)
		}
		return seen, expires
	}
	seenBefore, expiresBefore := read()
	f.clock.set(f.clock.Now().Add(time.Hour))
	req := newRequest(t, http.MethodGet, "/api/v1/auth/me", nil, "", sessionCookieOf(f.adminA))
	req.Host = "limits-bravo.example.net"
	if response := serve(site, req); response.Code != http.StatusUnauthorized {
		t.Fatalf("alpha's session on bravo's host status=%d", response.Code)
	}
	if seen, expires := read(); !seen.Equal(seenBefore) || !expires.Equal(expiresBefore) {
		t.Fatalf("refused session moved: last_seen %v -> %v, expires %v -> %v", seenBefore, seen, expiresBefore, expires)
	}
}

// Hand-offs live a minute; making a new one deletes the workspace's expired ones, so the table does not
// grow with every page load. Expired invites and used pairing links are kept: they show history.
func TestMakingAHandOffDeletesExpiredHandOffs(t *testing.T) {
	f := newLimitsFixtureConfigured(t, false, nil, func(config *httpapi.Config) { config.HelpCenterDomain = "example.net" })
	site := helpCenterSite(f.api)
	count := func(condition string) int {
		t.Helper()
		var n int
		if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM pairing_links WHERE workspace_id=$1 AND `+condition, f.workspaceA).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	createContractInvite(t, f.api, f.adminA, "later@limits-alpha.example.com", "agent")
	for range 3 {
		handoff(t, site, appSession(t, f, "limits-alpha", "admin@limits-alpha.example.com"), testOriginHost, "/")
	}
	pairings := count("kind='pairing'")
	f.clock.set(f.clock.Now().Add(30 * 24 * time.Hour))
	session := appSession(t, f, "limits-alpha", "admin@limits-alpha.example.com")
	handoff(t, site, session, testOriginHost, "/")
	if expired := count(fmt.Sprintf("kind='handoff' AND expires_at<='%s'", f.clock.Now().UTC().Format(time.RFC3339Nano))); expired != 0 {
		t.Fatalf("%d expired hand-off(s) left after a new one", expired)
	}
	if live := count("kind='handoff'"); live != 1 {
		t.Fatalf("hand-offs=%d, want the new one only", live)
	}
	if invites := count("kind='invite'"); invites != 1 {
		t.Fatalf("invites=%d, want the expired invite kept", invites)
	}
	if kept := count("kind='pairing'"); kept != pairings+1 {
		t.Fatalf("pairing links=%d, want %d", kept, pairings+1)
	}
}

// The page a sign-in on APP_ORIGIN opens is a path of the site; a control character can turn one into
// another host in a browser ("/\t/evil.com" reads as "//evil.com"), so such a next is dropped.
func TestASignInNextWithAControlCharacterIsDropped(t *testing.T) {
	fixture := newCloudAuthFixtureWith(t, "", func(config *httpapi.Config) { config.HelpCenterDomain = "example.net" })
	site := helpCenterSite(fixture.api)
	if _, _, err := fixture.service.CreateWorkspace(context.Background(), "Next Desk", "admin@cloud.example.com"); err != nil {
		t.Fatal(err)
	}
	for _, next := range []string{"/\t/evil.com", "/\x00x", "/a\x7f", "/\x1f/evil.com"} {
		req := newRequest(t, http.MethodPost, "/api/v1/auth/cloud/exchange", map[string]any{"next": next}, fixture.token(t, nil), nil)
		req.Header.Set("Origin", testOrigin)
		response := serve(site, req)
		var result struct {
			ContinueURL *string `json:"continue_url"`
		}
		decode(t, response, &result)
		if result.ContinueURL == nil || !strings.HasPrefix(*result.ContinueURL, "https://next-desk.example.net/sign-in#handoff=") {
			t.Errorf("next %q: status=%d continue_url=%v", next, response.Code, result.ContinueURL)
		}
	}
}

// The help center shares its host with the workspace's desk and its session, so no page under /help may
// run a script: every answer there carries script-src 'none'.
func TestTheHelpCenterOnTheWorkspaceHostRunsNoScript(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.HelpCenterDomain = "example.net" })
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Script Help", "admin@script-help.example.com")
	host := workspaceSlug(t, api, admin) + ".example.net"
	enableHelpCenter(t, api, admin, "Script help", true)
	guides := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": guides.ID, "title": "Start here", "status": "published", "body_md": "Hello"})
	for _, path := range []string{"/help", fmt.Sprintf("/help/a/%d-%s", article.Number, article.Slug), "/help/c/guides", "/help/search?q=start", "/help/a/999999-missing"} {
		response := onWorkspaceHost(site, http.MethodGet, host, path)
		if policy := response.Header().Get("Content-Security-Policy"); !strings.Contains(policy, "script-src 'none'") || !strings.Contains(policy, "default-src 'none'") {
			t.Errorf("GET %s status=%d Content-Security-Policy=%q", path, response.Code, policy)
		}
	}
}
