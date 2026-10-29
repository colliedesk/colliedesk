package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// handoff loads path on host with the session's cookie and returns the status and the Location.
func handoff(t *testing.T, site http.Handler, session, host, path string) (int, string) {
	t.Helper()
	req := newRequest(t, http.MethodGet, path, nil, "", sessionCookieOf(session))
	req.Host = host
	response := serve(site, req)
	return response.Code, response.Header().Get("Location")
}

// redeemOn redeems a one-time token for a cookie session on host.
func redeemOn(t *testing.T, site http.Handler, host, token string) int {
	t.Helper()
	req := newRequest(t, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": token, "session_type": "cookie"}, "", nil)
	req.Host = host
	req.Header.Set("Origin", "https://"+host)
	return serve(site, req).Code
}

// handoffToken is the token in the fragment of a hand-off address.
func handoffToken(t *testing.T, address string) string {
	t.Helper()
	parsed, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	token, ok := strings.CutPrefix(parsed.Fragment, "handoff=")
	if !ok || token == "" {
		t.Fatalf("hand-off address %q holds no token", address)
	}
	return token
}

// A page of the app loaded on APP_ORIGIN with a session moves to the same page on the workspace's host,
// with a one-time token in the address's fragment (never sent to a server or written to a log): the
// token signs in once, only on that workspace's host, and only for a minute. The API stays on
// APP_ORIGIN, and a server without workspace hosts keeps every page there.
func TestAnAppHostSessionHandsOffToItsWorkspaceHost(t *testing.T) {
	f := newLimitsFixtureConfigured(t, false, nil, func(config *httpapi.Config) { config.HelpCenterDomain = "example.net" })
	site := helpCenterSite(f.api)
	alphaHost := workspaceSlug(t, f.api, f.adminA) + ".example.net"
	betaHost := workspaceSlug(t, f.api, f.adminB) + ".example.net"

	code, address := handoff(t, site, f.adminA, testOriginHost, "/settings/billing?checkout=success")
	if code != http.StatusFound || !strings.HasPrefix(address, "https://"+alphaHost+"/sign-in?next=%2Fsettings%2Fbilling%3Fcheckout%3Dsuccess#handoff=") {
		t.Fatalf("page load status=%d location=%q", code, address)
	}
	token := handoffToken(t, address)
	if code := redeemOn(t, site, betaHost, token); code != http.StatusUnauthorized {
		t.Fatalf("alpha's hand-off on beta's host status=%d", code)
	}
	if code := redeemOn(t, site, alphaHost, token); code != http.StatusOK {
		t.Fatalf("hand-off on alpha's host status=%d", code)
	}
	if code := redeemOn(t, site, alphaHost, token); code != http.StatusUnauthorized {
		t.Fatalf("a hand-off used twice status=%d", code)
	}
	// Each hand-off revokes the APP_ORIGIN session it came from; every load below brings a new one.
	fresh := func() string { return appSession(t, f, "limits-alpha", "admin@limits-alpha.example.com") }
	if code, address := handoff(t, site, fresh(), testOriginHost, "/"); code != http.StatusFound || !strings.HasPrefix(address, "https://"+alphaHost+"/sign-in#handoff=") {
		t.Fatalf("root page load status=%d location=%q", code, address)
	}
	_, address = handoff(t, site, fresh(), testOriginHost, "/")
	f.clock.set(f.clock.Now().Add(2 * time.Minute))
	if code := redeemOn(t, site, alphaHost, handoffToken(t, address)); code != http.StatusUnauthorized {
		t.Fatalf("a hand-off after two minutes status=%d", code)
	}
	// The API, the sign-in page and the workspace host itself never hand off.
	for _, at := range []struct{ host, path string }{{testOriginHost, "/api/v1/auth/me"}, {testOriginHost, "/sign-in"}, {alphaHost, "/settings"}} {
		if code, address := handoff(t, site, fresh(), at.host, at.path); code == http.StatusFound {
			t.Errorf("%s%s handed off to %q", at.host, at.path, address)
		}
	}

	selfHosted, service, _, _ := testAPIWithConfig(t, nil)
	admin := createSession(t, selfHosted, service, "Self Host", "admin@self-host.example.com")
	if code, address := handoff(t, helpCenterSite(selfHosted), admin, testOriginHost, "/settings"); code == http.StatusFound {
		t.Fatalf("a server without workspace hosts handed off to %q", address)
	}
}

// Signing in on APP_ORIGIN (a link, cloud sign-in, a new team) answers where the browser continues: the
// workspace's host with the page asked for. On the workspace host there is nothing to continue to.
func TestASignInOnTheAppHostContinuesOnTheWorkspaceHost(t *testing.T) {
	fixture := newCloudAuthFixtureWith(t, "", func(config *httpapi.Config) { config.HelpCenterDomain = "example.net" })
	site := helpCenterSite(fixture.api)
	if _, _, err := fixture.service.CreateWorkspace(context.Background(), "Continue Desk", "admin@cloud.example.com"); err != nil {
		t.Fatal(err)
	}
	signIn := func(host string) string {
		origin := "https://" + host
		if host == testOriginHost {
			origin = testOrigin
		}
		req := newRequest(t, http.MethodPost, "/api/v1/auth/cloud/exchange", map[string]any{"next": "/settings/billing?buy=team_monthly"}, fixture.token(t, map[string]any{"azp": origin}), nil)
		req.Host = host
		req.Header.Set("Origin", origin)
		response := serve(site, req)
		var result struct {
			ContinueURL *string `json:"continue_url"`
		}
		decode(t, response, &result)
		if response.Code != http.StatusOK {
			t.Fatalf("sign-in on %s status=%d", host, response.Code)
		}
		if result.ContinueURL == nil {
			return ""
		}
		return *result.ContinueURL
	}
	address := signIn(testOriginHost)
	if !strings.HasPrefix(address, "https://continue-desk.example.net/sign-in?next=%2Fsettings%2Fbilling%3Fbuy%3Dteam_monthly#handoff=") {
		t.Fatalf("continue_url=%q", address)
	}
	if code := redeemOn(t, site, "continue-desk.example.net", handoffToken(t, address)); code != http.StatusOK {
		t.Fatalf("continue on the workspace host status=%d", code)
	}
	if address := signIn("continue-desk.example.net"); address != "" {
		t.Fatalf("sign-in on the workspace host continue_url=%q", address)
	}
}

// Links for agents name the workspace's host: an invite link is redeemed there.
func TestInviteLinksNameTheWorkspaceHost(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.HelpCenterDomain = "example.net" })
	admin := createSession(t, api, service, "Invite Host", "admin@invite-host.example.com")
	slug := workspaceSlug(t, api, admin)
	_, link := createContractInvite(t, api, admin, "teammate@invite-host.example.com", "agent")
	if !strings.HasPrefix(link, "https://"+slug+".example.net/sign-in?token=") {
		t.Fatalf("invite link=%q", link)
	}
	if code := redeemOn(t, helpCenterSite(api), slug+".example.net", linkToken(t, link)); code != http.StatusOK {
		t.Fatalf("invite on its workspace host status=%d", code)
	}
}

// Cloud sign-in on a workspace host signs in to that workspace only, whatever the browser asks: the
// workspace comes from the host. The identity token was issued to the workspace host's origin.
func TestCloudSignInOnAWorkspaceHostIsForItsWorkspaceOnly(t *testing.T) {
	fixture := newCloudAuthFixtureWith(t, "", func(config *httpapi.Config) { config.HelpCenterDomain = "example.net" })
	site := helpCenterSite(fixture.api)
	for _, name := range []string{"Cloud Alpha", "Cloud Beta"} {
		if _, _, err := fixture.service.CreateWorkspace(context.Background(), name, "admin@cloud.example.com"); err != nil {
			t.Fatal(err)
		}
	}
	signIn := func(host, slug string) (int, string) {
		origin := "https://" + host
		body := map[string]any{}
		if slug != "" {
			body["workspace_slug"] = slug
		}
		req := newRequest(t, http.MethodPost, "/api/v1/auth/cloud/exchange", body, fixture.token(t, map[string]any{"azp": origin}), nil)
		req.Host = host
		req.Header.Set("Origin", origin)
		response := serve(site, req)
		var result struct {
			Workspace struct {
				Slug string `json:"slug"`
			} `json:"workspace"`
		}
		if response.Code == http.StatusOK {
			decode(t, response, &result)
		}
		return response.Code, result.Workspace.Slug
	}
	// Two workspaces, no choice asked, and the browser's own choice is ignored.
	if code, slug := signIn("cloud-beta.example.net", ""); code != http.StatusOK || slug != "cloud-beta" {
		t.Fatalf("sign-in on beta's host status=%d workspace=%q", code, slug)
	}
	if code, slug := signIn("cloud-beta.example.net", "cloud-alpha"); code != http.StatusOK || slug != "cloud-beta" {
		t.Fatalf("sign-in on beta's host asking for alpha status=%d workspace=%q", code, slug)
	}
	if code, _ := signIn("nobody-here.example.net", ""); code != http.StatusForbidden {
		t.Fatalf("sign-in on a host of no workspace status=%d", code)
	}
	// A token issued to another workspace host is refused.
	req := newRequest(t, http.MethodPost, "/api/v1/auth/cloud/exchange", map[string]any{}, fixture.token(t, map[string]any{"azp": "https://cloud-alpha.example.net"}), nil)
	req.Host = "cloud-beta.example.net"
	req.Header.Set("Origin", "https://cloud-beta.example.net")
	if response := serve(site, req); response.Code != http.StatusUnauthorized {
		t.Fatalf("a token of alpha's host on beta's host status=%d", response.Code)
	}
}

// A provider sends the browser back to the fixed callback on APP_ORIGIN, where the workspace host's
// session cookie does not exist. The callback finds the workspace from the stored consent state and
// sends the browser, with the same query, to the same callback on that workspace's host, where the
// person who started the consent finishes it with their session there.
func TestAMailboxConsentFinishesOnTheWorkspaceHostThatStartedIt(t *testing.T) {
	desk := newGmailDeskWith(t, "Bounce Desk", func(config *httpapi.Config) { config.HelpCenterDomain = "example.net" })
	site := helpCenterSite(desk.api)
	host := "bounce-desk.example.net"
	consent := desk.startGmail(t, desk.admin)
	target, err := desk.fake.Consent(consent, "support@bounce-desk.example.com")
	if err != nil {
		t.Fatal(err)
	}
	path := strings.TrimPrefix(target, testOrigin)
	bounced := serve(site, newRequest(t, http.MethodGet, path, nil, "", nil))
	wantRedirect(t, bounced, http.StatusSeeOther, "https://"+host+path)

	finish := newRequest(t, http.MethodGet, path, nil, "", sessionCookieOf(desk.admin))
	finish.Host = host
	response := serve(site, finish)
	if response.Code != http.StatusSeeOther || !strings.HasSuffix(response.Header().Get("Location"), "?tab=replies&gmail=connected") {
		t.Fatalf("callback on the workspace host status=%d location=%q", response.Code, response.Header().Get("Location"))
	}
	// A made-up state is not sent anywhere.
	if response := serve(site, newRequest(t, http.MethodGet, "/api/v1/channels/email/gmail/callback?code=x&state=made-up", nil, "", nil)); response.Header().Get("Location") != "/settings/channels/type/email?tab=replies&gmail_error=signed_out" {
		t.Fatalf("a made-up state location=%q", response.Header().Get("Location"))
	}
}

// Links the server hands a workspace's agents lead back to the workspace host: the payment provider's
// return address, the page embedding its checkout, and the results share link.
func TestLinksForAgentsLeadToTheWorkspaceHost(t *testing.T) {
	f := newLimitsFixtureConfigured(t, true, nil, func(config *httpapi.Config) { config.HelpCenterDomain = "example.net" })
	site := helpCenterSite(f.api)
	host := "limits-alpha.example.net"
	call := func(method, path string, body any) *httptest.ResponseRecorder {
		req := newRequest(t, method, path, body, "", sessionCookieOf(f.adminA))
		req.Host = host
		req.Header.Set("Origin", "https://"+host)
		return serve(site, req)
	}
	if response := call(http.MethodPost, "/api/v1/billing/checkout", map[string]any{"plan": "team_monthly"}); response.Code != http.StatusOK {
		t.Fatalf("checkout status=%d body=%s", response.Code, response.Body.String())
	}
	checkouts := f.billing.fake.callsTo("/v1/checkouts/")
	if len(checkouts) != 1 || checkouts[0].Body["success_url"] != "https://limits-alpha.example.net/settings/billing?checkout=success" ||
		checkouts[0].Body["embed_origin"] != "https://limits-alpha.example.net" {
		t.Fatalf("checkout calls=%v", checkouts)
	}
	response := call(http.MethodPost, "/api/v1/results/share-link", nil)
	var share struct {
		URL string `json:"url"`
	}
	decode(t, response, &share)
	if !strings.HasPrefix(share.URL, "https://limits-alpha.example.net/r#") {
		t.Fatalf("share link status=%d url=%q", response.Code, share.URL)
	}
}
