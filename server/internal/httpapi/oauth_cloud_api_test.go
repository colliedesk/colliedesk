package httpapi_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// On Cloud every workspace has its own host and one /mcp serves them all on APP_ORIGIN. An AI app's
// consent page sends a signed-out browser to the sign-in page on APP_ORIGIN, which asks which workspace
// and continues to the consent page on that workspace's host; Allow there gives a code the app trades
// at APP_ORIGIN, for the workspace chosen and no other.
func TestOnCloudTheConsentRunsOnTheChosenWorkspaceHostAndTheCodeTradesOnTheAppHost(t *testing.T) {
	fixture := newCloudAuthFixtureWith(t, "", func(config *httpapi.Config) { config.HelpCenterDomain = "example.net" })
	site := helpCenterSite(fixture.api)
	for _, name := range []string{"Oauth Alpha", "Oauth Beta"} {
		if _, _, err := fixture.service.CreateWorkspace(context.Background(), name, "admin@cloud.example.com"); err != nil {
			t.Fatal(err)
		}
	}
	betaHost := "oauth-beta.example.net"
	f := oauthFixture{api: site}
	clientID := f.register(t, "Claude", "https://claude.ai/api/mcp/auth_callback")
	verifier, challenge := pkce(t)
	consent := "/oauth/authorize?" + url.Values{"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {"https://claude.ai/api/mcp/auth_callback"},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"}, "state": {"s1"}, "resource": {testOrigin + "/mcp"}}.Encode()

	exchange := func(body map[string]any) *http.Response {
		req := newRequest(t, http.MethodPost, "/api/v1/auth/cloud/exchange", body, fixture.token(t, map[string]any{"azp": testOrigin}), nil)
		req.Header.Set("Origin", testOrigin)
		return serve(site, req).Result()
	}
	if response := exchange(map[string]any{"next": consent}); response.StatusCode != http.StatusConflict {
		t.Fatalf("an email in two workspaces status=%d, want 409 with the choices", response.StatusCode)
	}
	recorder := serve(site, func() *http.Request {
		req := newRequest(t, http.MethodPost, "/api/v1/auth/cloud/exchange", map[string]any{"workspace_slug": "oauth-beta", "next": consent}, fixture.token(t, map[string]any{"azp": testOrigin}), nil)
		req.Header.Set("Origin", testOrigin)
		return req
	}())
	var signedIn struct {
		ContinueURL string `json:"continue_url"`
	}
	decode(t, recorder, &signedIn)
	continueTo, err := url.Parse(signedIn.ContinueURL)
	if err != nil || continueTo.Host != betaHost || continueTo.Query().Get("next") != consent {
		t.Fatalf("continue_url=%q, want beta's host with next exactly the consent page", signedIn.ContinueURL)
	}
	// The hand-off becomes a cookie session on beta's host.
	redeem := newRequest(t, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": handoffToken(t, signedIn.ContinueURL), "session_type": "cookie"}, "", nil)
	redeem.Host = betaHost
	redeem.Header.Set("Origin", "https://"+betaHost)
	redeemed := serve(site, redeem)
	var cookie *http.Cookie
	for _, set := range redeemed.Result().Cookies() {
		if set.Name == "colliedesk_session" {
			cookie = set
		}
	}
	if redeemed.Code != http.StatusOK || cookie == nil {
		t.Fatalf("hand-off on beta's host status=%d", redeemed.Code)
	}
	allow := newRequest(t, http.MethodPost, "/api/v1/oauth/authorize", map[string]any{"client_id": clientID, "redirect_uri": "https://claude.ai/api/mcp/auth_callback",
		"response_type": "code", "code_challenge": challenge, "code_challenge_method": "S256", "state": "s1", "resource": testOrigin + "/mcp"}, "", cookie)
	allow.Host = betaHost
	allow.Header.Set("Origin", "https://"+betaHost)
	back := redirectParams(t, serve(site, allow))
	if back.Get("iss") != testOrigin || back.Get("state") != "s1" || back.Get("code") == "" {
		t.Fatalf("Allow on beta's host answered %v, want a code, the state and APP_ORIGIN as iss", back)
	}
	pair := tokenPair(t, f.token(t, url.Values{"grant_type": {"authorization_code"}, "code": {back.Get("code")}, "code_verifier": {verifier},
		"client_id": {clientID}, "redirect_uri": {"https://claude.ai/api/mcp/auth_callback"}, "resource": {testOrigin + "/mcp"}}))
	var me struct {
		Workspace struct {
			Slug string `json:"slug"`
		} `json:"workspace"`
	}
	decode(t, request(t, site, http.MethodGet, "/api/v1/auth/me", nil, pair.Access, nil), &me)
	if me.Workspace.Slug != "oauth-beta" {
		t.Fatalf("the app acts in %q, want the workspace chosen (oauth-beta)", me.Workspace.Slug)
	}

	// Discovery: the authorization server is APP_ORIGIN only; each host names its own /mcp.
	onBeta := func(path string) *http.Response {
		req := newRequest(t, http.MethodGet, path, nil, "", nil)
		req.Host = betaHost
		return serve(site, req).Result()
	}
	if response := onBeta("/.well-known/oauth-authorization-server"); response.StatusCode != http.StatusNotFound {
		t.Fatalf("authorization server metadata on a workspace host status=%d, want 404", response.StatusCode)
	}
	resource := serve(site, func() *http.Request {
		req := newRequest(t, http.MethodGet, "/.well-known/oauth-protected-resource/mcp", nil, "", nil)
		req.Host = betaHost
		return req
	}())
	if !strings.Contains(resource.Body.String(), `"resource":"https://`+betaHost+`/mcp"`) || !strings.Contains(resource.Body.String(), `"authorization_servers":["`+testOrigin+`"]`) {
		t.Fatalf("resource metadata on beta's host: %s", resource.Body.String())
	}
}
