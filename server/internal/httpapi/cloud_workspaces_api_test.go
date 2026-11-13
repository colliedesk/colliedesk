package httpapi_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// listCloudWorkspaces asks host for the workspaces of the identity token's verified email, with the browser's
// cookie when one is given.
func listCloudWorkspaces(t *testing.T, site http.Handler, host, token string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	request := newRequest(t, http.MethodGet, "/api/v1/auth/cloud/workspaces", nil, token, cookie)
	request.Host = host
	return serve(site, request)
}

// workspaceNames is "slug=name" for each workspace the answer lists, in order.
func workspaceNames(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	var answer struct {
		Workspaces []struct{ Slug, Name string } `json:"workspaces"`
	}
	decode(t, response, &answer)
	names := make([]string, 0, len(answer.Workspaces))
	for _, workspace := range answer.Workspaces {
		names = append(names, workspace.Slug+"="+workspace.Name)
	}
	return strings.Join(names, ",")
}

// Inside a workspace, the account menu asks which other workspaces its person can switch to. The answer
// comes from the identity provider's verified email, never from the workspace session: that email is not
// proven (an admin can invite any address and use the link). It lists only the verified email's own active
// workspaces, on the workspace's host and on APP_ORIGIN alike, and a token issued for another host's page
// is refused.
func TestTheVerifiedEmailListsOnlyItsOwnWorkspaces(t *testing.T) {
	fixture, site := switchFixture(t)
	alphaHost := "alpha-switch.example.net"
	onAlpha := func(claims map[string]any) string {
		if claims == nil {
			claims = map[string]any{}
		}
		if _, ok := claims["azp"]; !ok {
			claims["azp"] = "https://" + alphaHost
		}
		return fixture.token(t, claims)
	}

	response := listCloudWorkspaces(t, site, alphaHost, onAlpha(nil), nil)
	if got := workspaceNames(t, response); response.Code != http.StatusOK || got != "alpha-switch=Alpha Switch,beta-switch=Beta Switch" {
		t.Fatalf("workspaces of admin@cloud.example.com on alpha's host status=%d %q", response.Code, got)
	}
	if cache := response.Header().Get("Cache-Control"); cache != "no-store" {
		t.Errorf("Cache-Control=%q, want no-store", cache)
	}
	response = listCloudWorkspaces(t, site, testOriginHost, fixture.token(t, nil), nil)
	if got := workspaceNames(t, response); response.Code != http.StatusOK || got != "alpha-switch=Alpha Switch,beta-switch=Beta Switch" {
		t.Fatalf("workspaces on APP_ORIGIN status=%d %q", response.Code, got)
	}
	// Someone else's verified email lists their workspace only, even on alpha's host.
	response = listCloudWorkspaces(t, site, alphaHost, onAlpha(map[string]any{"email": "someone@other.example.com"}), nil)
	if got := workspaceNames(t, response); response.Code != http.StatusOK || got != "other-switch=Other Switch" {
		t.Fatalf("workspaces of someone@other.example.com status=%d %q", response.Code, got)
	}
	response = listCloudWorkspaces(t, site, alphaHost, onAlpha(map[string]any{"email": "nobody@example.org"}), nil)
	if got := workspaceNames(t, response); response.Code != http.StatusOK || got != "" {
		t.Fatalf("workspaces of an email with none status=%d %q", response.Code, got)
	}

	// A deactivated agent row is no workspace to switch to.
	if _, err := fixture.pool.Exec(context.Background(), `UPDATE agents a SET deactivated_at=now() FROM workspaces w
		WHERE w.id=a.workspace_id AND w.slug='beta-switch' AND a.email='admin@cloud.example.com'`); err != nil {
		t.Fatal(err)
	}
	response = listCloudWorkspaces(t, site, alphaHost, onAlpha(nil), nil)
	if got := workspaceNames(t, response); got != "alpha-switch=Alpha Switch" {
		t.Fatalf("workspaces after beta deactivated the person %q", got)
	}
}

// Nothing but a verified identity token of this host's page gets an answer: not the workspace session, not
// a Colliedesk agent token, not a forged, unverified or other host's token.
func TestOnlyAVerifiedIdentityTokenOfThisHostListsWorkspaces(t *testing.T) {
	fixture, site := switchFixture(t)
	alphaHost := "alpha-switch.example.net"
	signedIn := cloudExchangeOnAppHost(t, fixture, site, "beta-switch", nil)
	var beta struct {
		ContinueURL *string `json:"continue_url"`
	}
	decode(t, signedIn, &beta)
	if beta.ContinueURL == nil {
		t.Fatalf("sign-in into beta status=%d", signedIn.Code)
	}
	redeem := newRequest(t, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": handoffToken(t, *beta.ContinueURL), "session_type": "cookie"}, "", nil)
	redeem.Host = "beta-switch.example.net"
	redeem.Header.Set("Origin", "https://beta-switch.example.net")
	session := cookieSet(serve(site, redeem), "colliedesk_session")
	if session == nil || session.Value == "" {
		t.Fatal("no session on beta's host")
	}
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	forged := signJWT(t, otherKey, fixture.kid, map[string]any{"iss": fixture.issuer, "azp": "https://" + alphaHost, "email": "admin@cloud.example.com", "email_verified": true,
		"exp": 4102444800, "nbf": 0})

	for _, refused := range []struct {
		name, host, token string
		cookie            *http.Cookie
	}{
		{"the workspace session alone", "beta-switch.example.net", "", sessionCookieOf(session.Value)},
		{"a token signed by another key", alphaHost, forged, nil},
		{"an unverified email", alphaHost, fixture.token(t, map[string]any{"azp": "https://" + alphaHost, "email_verified": false}), nil},
		{"a token of beta's page on alpha's host", alphaHost, fixture.token(t, map[string]any{"azp": "https://beta-switch.example.net"}), nil},
		{"a token of an outside page", alphaHost, fixture.token(t, map[string]any{"azp": "https://evil.example"}), nil},
		{"a Colliedesk agent token", alphaHost, "cdk_agent_" + strings.Repeat("a", 43), nil},
	} {
		response := listCloudWorkspaces(t, site, refused.host, refused.token, refused.cookie)
		if response.Code != http.StatusUnauthorized || strings.Contains(response.Body.String(), "switch") {
			t.Errorf("%s: status=%d body=%s, want 401 naming no workspace", refused.name, response.Code, response.Body.String())
		}
	}
}

// A server without cloud sign-in has no such route to answer.
func TestListingCloudWorkspacesNeedsCloudSignIn(t *testing.T) {
	selfHosted, _, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.HelpCenterDomain = "example.net" })
	response := listCloudWorkspaces(t, helpCenterSite(selfHosted), testOriginHost, "some-token", nil)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", response.Code)
	}
}
