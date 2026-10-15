package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// signupStart is when the signup tests begin; the 14 day trial of a workspace created then ends at
// 2026-11-03T10:00:00Z.
var signupStart = time.Date(2026, time.October, 20, 10, 0, 0, 0, time.UTC)

type signupFixture struct {
	*cloudAuthFixture
	clock *testClock
}

func newSignupFixture(t *testing.T, mutate func(*httpapi.Config)) signupFixture {
	t.Helper()
	clock := &testClock{now: signupStart}
	fake := newFakePolarAPI(t)
	fixture := newCloudAuthFixtureWith(t, "", func(config *httpapi.Config) {
		config.Now = clock.Now
		config.Billing = fake.config()
		config.SignupHashKey = testSignupKey
		hostedConfig(config)
		if mutate != nil {
			mutate(config)
		}
	})
	return signupFixture{cloudAuthFixture: fixture, clock: clock}
}

// testSignupKey is the SIGNUP_HASH_KEY of the signup tests: 32 bytes.
const testSignupKey = "0123456789abcdef0123456789abcdef"

const defaultPeer = "203.0.113.7:40000"

// signUp posts a signup with a Clerk token for email, from the given peer address.
func (f signupFixture) signUp(t *testing.T, email, name, peer string, forwardedFor ...string) *httptest.ResponseRecorder {
	t.Helper()
	return f.signUpToken(t, f.token(t, map[string]any{"email": email}), name, peer, forwardedFor...)
}

func (f signupFixture) signUpToken(t *testing.T, token, name, peer string, forwardedFor ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := newRequest(t, http.MethodPost, "/api/v1/auth/cloud/signup", map[string]any{"workspace_name": name}, token, nil)
	req.Header.Set("Origin", testOrigin)
	req.RemoteAddr = peer
	for _, line := range forwardedFor {
		req.Header.Add("X-Forwarded-For", line)
	}
	return serve(f.api, req)
}

func wantResponse(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status || (code != "" && !strings.Contains(response.Body.String(), `"`+code+`"`)) {
		t.Fatalf("status=%d body=%s, want %d %s", response.Code, response.Body.String(), status, code)
	}
}

func (f signupFixture) count(t *testing.T, table string) string {
	t.Helper()
	return queryString(t, f.pool, `SELECT count(*)::text FROM `+table)
}

func TestSignupCreatesTrialWorkspaceWithAdmin(t *testing.T) {
	f := newSignupFixture(t, nil)
	response := f.signUp(t, "Ada+x@Example.com", "Acme Support", defaultPeer)
	wantResponse(t, response, http.StatusCreated, "")
	var created struct {
		Workspace struct {
			Slug string `json:"slug"`
			Name string `json:"name"`
		} `json:"workspace"`
		Agent struct {
			Email string `json:"email"`
			Role  string `json:"role"`
		} `json:"agent"`
	}
	decode(t, response, &created)
	if created.Workspace.Slug != "acme-support" || created.Workspace.Name != "Acme Support" || created.Agent.Email != "ada+x@example.com" || created.Agent.Role != "admin" {
		t.Fatalf("created=%+v, want workspace acme-support / Acme Support with admin ada+x@example.com", created)
	}
	cookie := sessionCookie(t, response)
	billing := request(t, f.api, http.MethodGet, "/api/v1/billing", nil, "", cookie)
	var view planUsageView
	decode(t, billing, &view)
	if billing.Code != http.StatusOK || view.Plan != "trial" || view.TrialEndsAt != "2026-11-03T10:00:00Z" || view.Locked {
		t.Fatalf("billing status=%d %+v, want an unlocked trial ending 2026-11-03T10:00:00Z", billing.Code, view)
	}
}

func TestSignupAllowsOneTrialPerMailbox(t *testing.T) {
	f := newSignupFixture(t, nil)
	wantResponse(t, f.signUp(t, "Ana.B+test@gmail.com", "First", defaultPeer), http.StatusCreated, "")
	for _, alias := range []string{"anab@googlemail.com", "a.n.a.b@gmail.com", "ANAB+other@gmail.com"} {
		second := f.signUp(t, alias, "Second", "198.51.100.1:1")
		wantResponse(t, second, http.StatusConflict, "trial_already_used")
		if !strings.Contains(second.Body.String(), "Sign in") {
			t.Fatalf("%s: message does not point to sign-in: %s", alias, second.Body.String())
		}
	}
	// Dots are significant outside Gmail: these are two people.
	wantResponse(t, f.signUp(t, "ada.l@example.com", "Third", "198.51.100.2:1"), http.StatusCreated, "")
	wantResponse(t, f.signUp(t, "adal@example.com", "Fourth", "198.51.100.3:1"), http.StatusCreated, "")
	if got := f.count(t, "workspaces"); got != "3" {
		t.Fatalf("workspaces=%s, want 3", got)
	}
}

func TestSignupTrialSurvivesWorkspaceDeletion(t *testing.T) {
	f := newSignupFixture(t, nil)
	wantResponse(t, f.signUp(t, "ada@example.com", "Acme", defaultPeer), http.StatusCreated, "")
	if _, err := f.pool.Exec(context.Background(), `DELETE FROM workspaces`); err != nil {
		t.Fatal(err)
	}
	wantResponse(t, f.signUp(t, "ada@example.com", "Acme Again", "198.51.100.1:1"), http.StatusConflict, "trial_already_used")
}

func TestSignupLimitsThreePerClientIPPerDay(t *testing.T) {
	f := newSignupFixture(t, nil)
	for index, email := range []string{"a1@example.com", "a2@example.com", "a3@example.com"} {
		wantResponse(t, f.signUp(t, email, "Team "+string(rune('A'+index)), "203.0.113.7:40000"), http.StatusCreated, "")
	}
	wantResponse(t, f.signUp(t, "a4@example.com", "Team D", "203.0.113.7:40001"), http.StatusTooManyRequests, "rate_limited")
	if got := f.count(t, "workspaces"); got != "3" {
		t.Fatalf("workspaces=%s after a refused signup, want 3", got)
	}
	wantResponse(t, f.signUp(t, "a5@example.com", "Team E", "203.0.113.8:40000"), http.StatusCreated, "") // another address

	f.clock.set(signupStart.Add(24*time.Hour - time.Second))
	wantResponse(t, f.signUp(t, "a4@example.com", "Team D", "203.0.113.7:40002"), http.StatusTooManyRequests, "rate_limited")
	f.clock.set(signupStart.Add(24*time.Hour + time.Second))
	wantResponse(t, f.signUp(t, "a4@example.com", "Team D", "203.0.113.7:40003"), http.StatusCreated, "")
}

func TestSignupUsesTheRealClientBehindATrustedProxy(t *testing.T) {
	f := newSignupFixture(t, func(config *httpapi.Config) {
		config.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("172.18.0.0/16")}
	})
	proxy := "172.18.0.1:5000"
	for index, email := range []string{"p1@example.com", "p2@example.com", "p3@example.com"} {
		wantResponse(t, f.signUp(t, email, "Proxy "+string(rune('A'+index)), proxy, "198.51.100.9, 203.0.113.7"), http.StatusCreated, "")
	}
	wantResponse(t, f.signUp(t, "p4@example.com", "Proxy D", proxy, "198.51.100.77, 203.0.113.7"), http.StatusTooManyRequests, "rate_limited")
	wantResponse(t, f.signUp(t, "p5@example.com", "Proxy E", proxy, "198.51.100.9, 203.0.113.99"), http.StatusCreated, "") // a different client

	// The same header from a peer that is not a trusted proxy is attacker input: the peer counts.
	for index, email := range []string{"s1@example.com", "s2@example.com", "s3@example.com"} {
		wantResponse(t, f.signUp(t, email, "Spoof "+string(rune('A'+index)), "192.0.2.5:4000", "10.0.0."+string(rune('1'+index))), http.StatusCreated, "")
	}
	wantResponse(t, f.signUp(t, "s4@example.com", "Spoof D", "192.0.2.5:4000", "10.0.0.9"), http.StatusTooManyRequests, "rate_limited")
}

func TestSignupRefusesBrandNames(t *testing.T) {
	f := newSignupFixture(t, nil)
	for _, name := range []string{"PayPal Support", "pay pal", "G-O-O-G-L-E"} {
		wantResponse(t, f.signUp(t, "brand@example.com", name, defaultPeer), http.StatusUnprocessableEntity, "name_not_allowed")
	}
	if got := f.count(t, "cloud_signups"); got != "0" {
		t.Fatalf("cloud_signups=%s after refused names, want 0 (the email is not burnt)", got)
	}
	wantResponse(t, f.signUp(t, "brand@example.com", "Pineapple Studio", defaultPeer), http.StatusCreated, "")
}

func TestSignupValidatesTheName(t *testing.T) {
	f := newSignupFixture(t, nil)
	for _, name := range []string{"", " ", "A", strings.Repeat("x", 61)} {
		wantResponse(t, f.signUp(t, "name@example.com", name, defaultPeer), http.StatusBadRequest, "invalid_request")
	}
	wantResponse(t, f.signUp(t, "name@example.com", "  Spaced Out  ", defaultPeer), http.StatusCreated, "")
	if got := queryString(t, f.pool, `SELECT name FROM workspaces`); got != "Spaced Out" {
		t.Fatalf("stored name %q, want it trimmed", got)
	}
}

func TestSignupRequiresAVerifiedIdentityFromTheSameOrigin(t *testing.T) {
	f := newSignupFixture(t, nil)
	wantResponse(t, f.signUpToken(t, f.token(t, map[string]any{"email_verified": false}), "Acme", defaultPeer), http.StatusUnauthorized, "unauthorized")
	wantResponse(t, f.signUpToken(t, "not-a-token", "Acme", defaultPeer), http.StatusUnauthorized, "unauthorized")
	noBearer := newRequest(t, http.MethodPost, "/api/v1/auth/cloud/signup", map[string]any{"workspace_name": "Acme"}, "", nil)
	noBearer.Header.Set("Origin", testOrigin)
	wantResponse(t, serve(f.api, noBearer), http.StatusUnauthorized, "unauthorized")
	wrongOrigin := newRequest(t, http.MethodPost, "/api/v1/auth/cloud/signup", map[string]any{"workspace_name": "Acme"}, f.token(t, nil), nil)
	wrongOrigin.Header.Set("Origin", "https://evil.example.com")
	wantResponse(t, serve(f.api, wrongOrigin), http.StatusForbidden, "csrf_rejected")
	if got := f.count(t, "workspaces"); got != "0" {
		t.Fatalf("workspaces=%s after refused requests, want 0", got)
	}
}

func TestSignupRouteExistsOnlyOnTheCloud(t *testing.T) {
	billingOff := newCloudAuthFixture(t)
	req := newRequest(t, http.MethodPost, "/api/v1/auth/cloud/signup", map[string]any{"workspace_name": "Acme"}, billingOff.token(t, nil), nil)
	req.Header.Set("Origin", testOrigin)
	wantResponse(t, serve(billingOff.api, req), http.StatusNotFound, "")

	api, _ := testAPI(t) // self-hosted: no cloud sign-in, no billing
	wantResponse(t, request(t, api, http.MethodPost, "/api/v1/auth/cloud/signup", map[string]any{"workspace_name": "Acme"}, "x", nil), http.StatusNotFound, "")

	billingOnly := newLimitsFixture(t, true) // billing without Clerk
	wantResponse(t, request(t, billingOnly.api, http.MethodPost, "/api/v1/auth/cloud/signup", map[string]any{"workspace_name": "Acme"}, "x", nil), http.StatusNotFound, "")
}

func TestSignupSlugNeverCollides(t *testing.T) {
	f := newSignupFixture(t, nil)
	wantResponse(t, f.signUp(t, "one@example.com", "Acme", "198.51.100.1:1"), http.StatusCreated, "")
	second := f.signUp(t, "two@example.com", "ACME", "198.51.100.2:1")
	wantResponse(t, second, http.StatusCreated, "")
	var created struct {
		Workspace struct {
			Slug string `json:"slug"`
		} `json:"workspace"`
	}
	decode(t, second, &created)
	if !regexp.MustCompile(`^acme-[0-9a-f]{4}$`).MatchString(created.Workspace.Slug) {
		t.Fatalf("second slug %q, want acme-<4 hex>", created.Workspace.Slug)
	}
	// A reserved name would give a workspace whose forwarding address cannot exist.
	third := f.signUp(t, "three@example.com", "Support", "198.51.100.3:1")
	wantResponse(t, third, http.StatusCreated, "")
	decode(t, third, &created)
	if !regexp.MustCompile(`^support-[0-9a-f]{4}$`).MatchString(created.Workspace.Slug) {
		t.Fatalf("reserved-name slug %q, want support-<4 hex>", created.Workspace.Slug)
	}
	// A name with no letters or digits still gets a usable slug.
	fourth := f.signUp(t, "four@example.com", "!!!", "198.51.100.4:1")
	wantResponse(t, fourth, http.StatusCreated, "")
	decode(t, fourth, &created)
	if !regexp.MustCompile(`^workspace(-[0-9a-f]{4})?$`).MatchString(created.Workspace.Slug) {
		t.Fatalf("slug %q for a symbol-only name, want workspace", created.Workspace.Slug)
	}
}

// zeros makes the random slug suffix "0000" every time, so a test can occupy it.
type zeros struct{}

func (zeros) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestSignupSlugConflictTwiceIsAnErrorAndBurnsNothing(t *testing.T) {
	f := newSignupFixture(t, func(config *httpapi.Config) { config.Random = zeros{} })
	for _, name := range []string{"Acme", "Acme 0000"} {
		if _, _, err := f.service.CreateWorkspace(context.Background(), name, "owner@example.com"); err != nil {
			t.Fatal(err)
		}
	}
	// "acme" and "acme-0000" exist; the retry suffix is "0000" again.
	wantResponse(t, f.signUp(t, "late@example.com", "Acme", defaultPeer), http.StatusConflict, "slug_unavailable")
	if got := f.count(t, "cloud_signups"); got != "0" {
		t.Fatalf("cloud_signups=%s after a failed create, want 0", got)
	}
	if got := f.count(t, "workspaces"); got != "2" {
		t.Fatalf("workspaces=%s after a failed create, want 2", got)
	}
}

func TestSignupStoresKeyedHashesOnly(t *testing.T) {
	f := newSignupFixture(t, nil)
	wantResponse(t, f.signUp(t, "Ada@Example.com", "Acme", defaultPeer), http.StatusCreated, "")
	// HMAC-SHA256 with testSignupKey of "ada@example.com" and of "203.0.113.7", computed with openssl.
	const wantEmail = "e3e30b4d50224a4ba14084607085d072402144212a122eb4501daddd6bbe0ee3"
	const wantIP = "55a7c9ba39c762e973ffcf294361c78f7c5a830e44341d1686e3c0fcd1f191e3"
	if got := queryString(t, f.pool, `SELECT email_hash||'/'||ip_hash FROM cloud_signups`); got != wantEmail+"/"+wantIP {
		t.Fatalf("cloud_signups row = %s, want %s/%s", got, wantEmail, wantIP)
	}
}

func TestSignupAddressSweepClearsOnlyOldHashes(t *testing.T) {
	f := newSignupFixture(t, nil)
	seed := func(key string, age time.Duration) {
		if _, err := f.pool.Exec(context.Background(), `INSERT INTO cloud_signups(email_hash,ip_hash,created_at) VALUES($1,'someip',$2)`, key, signupStart.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}
	seed("old", 31*24*time.Hour)
	seed("recent", 29*24*time.Hour)
	cleared, err := f.service.WithClock(f.clock.Now).ClearSignupAddresses(context.Background())
	if err != nil || cleared != 1 {
		t.Fatalf("ClearSignupAddresses = %d, %v; want 1 row", cleared, err)
	}
	if got := queryString(t, f.pool, `SELECT COALESCE(ip_hash,'-') FROM cloud_signups WHERE email_hash='old'`); got != "-" {
		t.Fatalf("31 day old ip_hash = %q, want it cleared", got)
	}
	if got := queryString(t, f.pool, `SELECT COALESCE(ip_hash,'-') FROM cloud_signups WHERE email_hash='recent'`); got != "someip" {
		t.Fatalf("29 day old ip_hash = %q, want it kept", got)
	}
	if got := f.count(t, "cloud_signups WHERE email_hash='old'"); got != "1" {
		t.Fatalf("the old email hash row was deleted; it must stay so the trial is not handed out twice")
	}
}

func TestSignupMalformedEmailIsABadRequest(t *testing.T) {
	f := newSignupFixture(t, nil)
	for _, address := range []string{"a@b@c.com", "+tag@example.com", "two words@example.com"} {
		wantResponse(t, f.signUp(t, address, "Acme", defaultPeer), http.StatusBadRequest, "invalid_request")
	}
}

func TestSignupSlugIsAlwaysAValidHostedLabel(t *testing.T) {
	f := newSignupFixture(t, nil)
	response := f.signUp(t, "idn@example.com", "xn--shop", defaultPeer)
	wantResponse(t, response, http.StatusCreated, "")
	var created struct {
		Workspace struct {
			Slug string `json:"slug"`
		} `json:"workspace"`
	}
	decode(t, response, &created)
	if created.Workspace.Slug != "xn-shop" { // runs of hyphens collapse, so "xn--" never survives
		t.Fatalf("slug %q, want xn-shop", created.Workspace.Slug)
	}
}

func TestSignedUpWorkspaceCanBeReachedByCloudSignIn(t *testing.T) {
	f := newSignupFixture(t, nil)
	wantResponse(t, f.signUp(t, "ada@example.com", "Acme", defaultPeer), http.StatusCreated, "")
	response := exchange(t, f.api, f.token(t, map[string]any{"email": "ada@example.com"}), "", testOrigin)
	if response.Code != http.StatusOK {
		t.Fatalf("sign-in after signup: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestSlugRenameRefusesBrandsOnTheCloudOnly(t *testing.T) {
	cloud := newLimitsFixture(t, true)
	response := request(t, cloud.api, http.MethodPatch, "/api/v1/workspace/slug", map[string]any{"slug": "paypal-help"}, cloud.adminA, nil)
	wantResponse(t, response, http.StatusUnprocessableEntity, "name_not_allowed")
	wantResponse(t, request(t, cloud.api, http.MethodPatch, "/api/v1/workspace/slug", map[string]any{"slug": "pineapple-help"}, cloud.adminA, nil), http.StatusOK, "")

	selfHosted := newLimitsFixture(t, false)
	wantResponse(t, request(t, selfHosted.api, http.MethodPatch, "/api/v1/workspace/slug", map[string]any{"slug": "paypal-help"}, selfHosted.adminA, nil), http.StatusOK, "")
}
