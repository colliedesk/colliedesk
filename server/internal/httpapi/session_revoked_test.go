package httpapi_test

import (
	"context"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// meOn asks host for /auth/me with session as the browser's cookie and returns the status and the error code.
func meOn(t *testing.T, site http.Handler, host, session string) (int, string) {
	t.Helper()
	request := newRequest(t, http.MethodGet, "/api/v1/auth/me", nil, "", sessionCookieOf(session))
	request.Host = host
	response := serve(site, request)
	if response.Code == http.StatusOK {
		return response.Code, ""
	}
	return response.Code, unauthorizedCode(t, response)
}

func revokeSessionRow(t *testing.T, f limitsFixture, session string) {
	t.Helper()
	hash := sha256.Sum256([]byte(session))
	if _, err := f.pool.Exec(context.Background(), `UPDATE sessions SET revoked_at=now() WHERE token_hash=$1`, hash[:]); err != nil {
		t.Fatal(err)
	}
}

// A browser whose own session someone revoked (Sessions, or the person's deactivation) is told so: 401
// session_revoked, so the web app ends the sign-in provider's session too and the next sign-in asks for a
// code. A session that expired, or a cookie the server does not know, is plain 401 unauthorized.
func TestARevokedSessionIsToldItWasRevokedAndAnExpiredOneIsNot(t *testing.T) {
	f := newLimitsFixture(t, false)

	if code, reason := meOn(t, f.api, testOriginHost, f.adminA); code != http.StatusOK {
		t.Fatalf("a live session status=%d %s", code, reason)
	}
	revokeSessionRow(t, f, f.adminA)
	if code, reason := meOn(t, f.api, testOriginHost, f.adminA); code != http.StatusUnauthorized || reason != "session_revoked" {
		t.Fatalf("a revoked session status=%d code=%q, want 401 session_revoked", code, reason)
	}
	// The bearer of a revoked session is not told: the reason is for the browser holding the cookie.
	if response := request(t, f.api, http.MethodGet, "/api/v1/auth/me", nil, f.adminA, nil); response.Code != http.StatusUnauthorized || unauthorizedCode(t, response) != "unauthorized" {
		t.Fatalf("a revoked session as bearer status=%d body=%s, want 401 unauthorized", response.Code, response.Body.String())
	}

	f.clock.set(f.clock.Now().Add(31 * 24 * time.Hour))
	if code, reason := meOn(t, f.api, testOriginHost, f.adminB); code != http.StatusUnauthorized || reason != "unauthorized" {
		t.Fatalf("an expired session status=%d code=%q, want 401 unauthorized", code, reason)
	}
	if code, reason := meOn(t, f.api, testOriginHost, "no-such-session"); code != http.StatusUnauthorized || reason != "unauthorized" {
		t.Fatalf("an unknown cookie status=%d code=%q, want 401 unauthorized", code, reason)
	}
}

// Deactivating a person revokes their sessions: their browser is told its session was revoked.
func TestADeactivatedPersonsSessionIsToldItWasRevoked(t *testing.T) {
	f := newLimitsFixture(t, false)
	if _, err := f.pool.Exec(context.Background(), `UPDATE agents SET deactivated_at=now() WHERE workspace_id=$1`, f.workspaceB); err != nil {
		t.Fatal(err)
	}
	if code, reason := meOn(t, f.api, testOriginHost, f.adminB); code != http.StatusUnauthorized || reason != "session_revoked" {
		t.Fatalf("a deactivated person's session status=%d code=%q, want 401 session_revoked", code, reason)
	}
}

// Only the workspace's own host says a session was revoked: another workspace's host answers a plain 401,
// so nothing about a session crosses to a host it does not belong to.
func TestAnotherWorkspaceHostNeverSaysASessionWasRevoked(t *testing.T) {
	f := newLimitsFixtureConfigured(t, false, nil, func(config *httpapi.Config) { config.HelpCenterDomain = "example.net" })
	site := helpCenterSite(f.api)
	alphaHost := workspaceSlug(t, f.api, f.adminA) + ".example.net"
	betaHost := workspaceSlug(t, f.api, f.adminB) + ".example.net"
	revokeSessionRow(t, f, f.adminA)
	if code, reason := meOn(t, site, betaHost, f.adminA); code != http.StatusUnauthorized || reason != "unauthorized" {
		t.Fatalf("alpha's revoked session on beta's host status=%d code=%q, want 401 unauthorized", code, reason)
	}
	if code, reason := meOn(t, site, alphaHost, f.adminA); code != http.StatusUnauthorized || reason != "session_revoked" {
		t.Fatalf("alpha's revoked session on alpha's host status=%d code=%q, want 401 session_revoked", code, reason)
	}
}

// unauthorizedCode is the error code of a 401 answer.
func unauthorizedCode(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct{ Code string } `json:"error"`
	}
	decode(t, response, &body)
	return body.Error.Code
}
