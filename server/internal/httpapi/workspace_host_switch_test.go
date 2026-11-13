package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// switchFixture is a cloud server with workspace hosts where one person (admin@cloud.example.com) is an
// agent of two workspaces, and someone else has a third.
func switchFixture(t *testing.T) (*cloudAuthFixture, http.Handler) {
	t.Helper()
	fixture := newCloudAuthFixtureWith(t, "", func(config *httpapi.Config) { config.HelpCenterDomain = "example.net" })
	for _, workspace := range []struct{ name, email string }{
		{"Alpha Switch", "admin@cloud.example.com"},
		{"Beta Switch", "admin@cloud.example.com"},
		{"Other Switch", "someone@other.example.com"},
	} {
		if _, _, err := fixture.service.CreateWorkspace(context.Background(), workspace.name, workspace.email); err != nil {
			t.Fatal(err)
		}
	}
	return fixture, helpCenterSite(fixture.api)
}

// cloudExchangeOnAppHost signs the fixture's person in on APP_ORIGIN with the browser's cookies there.
func cloudExchangeOnAppHost(t *testing.T, fixture *cloudAuthFixture, site http.Handler, slug string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	body := map[string]any{}
	if slug != "" {
		body["workspace_slug"] = slug
	}
	exchange := newRequest(t, http.MethodPost, "/api/v1/auth/cloud/exchange", body, fixture.token(t, nil), nil)
	exchange.Header.Set("Origin", testOrigin)
	for _, cookie := range cookies {
		exchange.AddCookie(cookie)
	}
	return serve(site, exchange)
}

// appPageLoad loads path on APP_ORIGIN with cookies and returns the status and the Location.
func appPageLoad(t *testing.T, site http.Handler, path string, cookies []*http.Cookie) (int, string) {
	t.Helper()
	page := newRequest(t, http.MethodGet, path, nil, "", nil)
	for _, cookie := range cookies {
		page.AddCookie(cookie)
	}
	response := serve(site, page)
	return response.Code, response.Header().Get("Location")
}

// A person with several workspaces is asked on APP_ORIGIN which one to open, every time: a sign-in into one
// of them leaves no workspace hint there (and clears one an earlier sign-in left), so opening APP_ORIGIN
// later reaches the chooser instead of being sent into the last workspace. The chooser lists only that
// person's workspaces.
func TestAPersonWithSeveralWorkspacesIsNotSentIntoTheLastOneFromTheAppHost(t *testing.T) {
	fixture, site := switchFixture(t)

	asked := cloudExchangeOnAppHost(t, fixture, site, "", nil)
	var choices struct {
		Workspaces []struct{ Slug string } `json:"workspaces"`
	}
	decode(t, asked, &choices)
	var slugs []string
	for _, workspace := range choices.Workspaces {
		slugs = append(slugs, workspace.Slug)
	}
	if asked.Code != http.StatusConflict || strings.Join(slugs, ",") != "alpha-switch,beta-switch" {
		t.Fatalf("sign-in without a workspace status=%d workspaces=%v, want 409 with alpha-switch and beta-switch only", asked.Code, slugs)
	}

	// A hint an earlier sign-in left (before this person had a second workspace) is cleared.
	stale := &http.Cookie{Name: "colliedesk_workspace", Value: "beta-switch"}
	signedIn := cloudExchangeOnAppHost(t, fixture, site, "alpha-switch", []*http.Cookie{stale})
	var result struct {
		ContinueURL *string `json:"continue_url"`
	}
	decode(t, signedIn, &result)
	if signedIn.Code != http.StatusOK || result.ContinueURL == nil || !strings.HasPrefix(*result.ContinueURL, "https://alpha-switch.example.net/sign-in#handoff=") {
		t.Fatalf("sign-in into alpha status=%d continue_url=%v", signedIn.Code, result.ContinueURL)
	}
	hint := cookieSet(signedIn, "colliedesk_workspace")
	if hint == nil || hint.MaxAge >= 0 {
		t.Fatalf("sign-in of a person with several workspaces left the workspace hint %v, want it cleared", hint)
	}
	for _, path := range []string{"/", "/settings"} {
		if code, location := appPageLoad(t, site, path, keptCookies(signedIn)); code == http.StatusFound && strings.Contains(location, ".example.net") {
			t.Errorf("GET %s on APP_ORIGIN sent a person with several workspaces to %q", path, location)
		}
	}
}

// Switching workspace: signed in on alpha's host, the person opens the sign-in page on APP_ORIGIN (also with
// a hint left from before), chooses beta and lands on beta's host through a one-time hand-off that signs in
// on beta's host only. Alpha's session on its own host stays as it was.
func TestSwitchingWorkspaceGoesThroughTheChooserAndAHandOffToTheOtherHost(t *testing.T) {
	fixture, site := switchFixture(t)
	alphaHost, betaHost := "alpha-switch.example.net", "beta-switch.example.net"

	signedIn := cloudExchangeOnAppHost(t, fixture, site, "alpha-switch", nil)
	var alpha struct {
		ContinueURL *string `json:"continue_url"`
	}
	decode(t, signedIn, &alpha)
	if alpha.ContinueURL == nil {
		t.Fatalf("sign-in into alpha status=%d gave no continue_url", signedIn.Code)
	}
	redeem := newRequest(t, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": handoffToken(t, *alpha.ContinueURL), "session_type": "cookie"}, "", nil)
	redeem.Host = alphaHost
	redeem.Header.Set("Origin", "https://"+alphaHost)
	alphaSession := cookieSet(serve(site, redeem), "colliedesk_session")
	if alphaSession == nil || alphaSession.Value == "" {
		t.Fatal("the hand-off gave no session on alpha's host")
	}

	stale := &http.Cookie{Name: "colliedesk_workspace", Value: "alpha-switch"}
	if code, location := appPageLoad(t, site, "/sign-in", []*http.Cookie{stale}); code == http.StatusFound {
		t.Fatalf("the sign-in page on APP_ORIGIN redirected to %q", location)
	}
	switched := cloudExchangeOnAppHost(t, fixture, site, "beta-switch", []*http.Cookie{stale})
	var beta struct {
		ContinueURL *string `json:"continue_url"`
	}
	decode(t, switched, &beta)
	if switched.Code != http.StatusOK || beta.ContinueURL == nil || !strings.HasPrefix(*beta.ContinueURL, "https://"+betaHost+"/sign-in#handoff=") {
		t.Fatalf("choosing beta status=%d continue_url=%v", switched.Code, beta.ContinueURL)
	}
	token := handoffToken(t, *beta.ContinueURL)
	if code := redeemOn(t, site, alphaHost, token); code != http.StatusUnauthorized {
		t.Fatalf("beta's hand-off on alpha's host status=%d, want 401", code)
	}
	if code := redeemOn(t, site, betaHost, token); code != http.StatusOK {
		t.Fatalf("beta's hand-off on beta's host status=%d", code)
	}

	me := newRequest(t, http.MethodGet, "/api/v1/auth/me", nil, "", sessionCookieOf(alphaSession.Value))
	me.Host = alphaHost
	if response := serve(site, me); response.Code != http.StatusOK {
		t.Fatalf("alpha's session after switching to beta status=%d", response.Code)
	}
}

// A deactivated agent row is no workspace to choose: a person left with one active workspace goes straight
// in, and APP_ORIGIN keeps sending its bookmarks there, as for anyone with one workspace.
func TestADeactivatedWorkspaceIsNoChoiceAndOneWorkspaceGoesStraightIn(t *testing.T) {
	fixture, site := switchFixture(t)
	if _, err := fixture.pool.Exec(context.Background(), `UPDATE agents a SET deactivated_at=now() FROM workspaces w
		WHERE w.id=a.workspace_id AND w.slug='beta-switch' AND a.email='admin@cloud.example.com'`); err != nil {
		t.Fatal(err)
	}
	signedIn := cloudExchangeOnAppHost(t, fixture, site, "", nil)
	var result struct {
		ContinueURL *string `json:"continue_url"`
	}
	decode(t, signedIn, &result)
	if signedIn.Code != http.StatusOK || result.ContinueURL == nil || !strings.HasPrefix(*result.ContinueURL, "https://alpha-switch.example.net/") {
		t.Fatalf("sign-in with one active workspace status=%d continue_url=%v", signedIn.Code, result.ContinueURL)
	}
	if code, location := appPageLoad(t, site, "/", keptCookies(signedIn)); code != http.StatusFound || location != "https://alpha-switch.example.net/" {
		t.Fatalf("GET / on APP_ORIGIN status=%d location=%q, want 302 to alpha's host", code, location)
	}
}
