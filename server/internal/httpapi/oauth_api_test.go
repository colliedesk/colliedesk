package httpapi_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/jackc/pgx/v5/pgxpool"
)

const oauthRedirect = "http://127.0.0.1:33418/callback"

type oauthFixture struct {
	api     http.Handler
	service *auth.Service
	pool    *pgxpool.Pool
	clock   *testClock
}

func newOAuthFixture(t *testing.T, mutate ...func(*httpapi.Config)) oauthFixture {
	t.Helper()
	clock := &testClock{now: time.Date(2026, time.November, 6, 9, 0, 0, 0, time.UTC)}
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		config.Now = clock.Now
		for _, change := range mutate {
			change(config)
		}
	})
	return oauthFixture{api: api, service: service.WithClock(clock.Now), pool: pool, clock: clock}
}

func (f oauthFixture) register(t *testing.T, name string, redirects ...string) string {
	t.Helper()
	response := request(t, f.api, http.MethodPost, "/oauth/register", map[string]any{
		"client_name": name, "redirect_uris": redirects, "token_endpoint_auth_method": "none",
		"grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}, "logo_uri": "https://example.com/logo.png",
	}, "", nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("register status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		ClientID string `json:"client_id"`
	}
	decode(t, response, &body)
	return body.ClientID
}

func pkce(t *testing.T) (string, string) {
	t.Helper()
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

func (f oauthFixture) allow(t *testing.T, person, clientID, redirect, challenge string, extra map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body := map[string]any{"client_id": clientID, "redirect_uri": redirect, "response_type": "code", "code_challenge": challenge,
		"code_challenge_method": "S256", "state": "xyz", "resource": testOrigin + "/mcp"}
	for key, value := range extra {
		body[key] = value
	}
	req := newRequest(t, http.MethodPost, "/api/v1/oauth/authorize", body, person, nil)
	req.Header.Set("Origin", testOrigin)
	return serve(f.api, req)
}

func redirectParams(t *testing.T, response *httptest.ResponseRecorder) url.Values {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("allow status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		RedirectTo string `json:"redirect_to"`
	}
	decode(t, response, &body)
	target, err := url.Parse(body.RedirectTo)
	if err != nil {
		t.Fatal(err)
	}
	return target.Query()
}

func (f oauthFixture) token(t *testing.T, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Host = testOriginHost
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return serve(f.api, req)
}

type oauthPair struct {
	Access  string `json:"access_token"`
	Refresh string `json:"refresh_token"`
	Type    string `json:"token_type"`
	Expires int    `json:"expires_in"`
}

func tokenPair(t *testing.T, response *httptest.ResponseRecorder) oauthPair {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("token status=%d body=%s", response.Code, response.Body.String())
	}
	var pair oauthPair
	decode(t, response, &pair)
	return pair
}

func oauthErrorCode(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &body)
	return body.Error
}

// connect runs the whole flow for person: register, allow, exchange.
func (f oauthFixture) connect(t *testing.T, person string) (oauthPair, string) {
	t.Helper()
	clientID := f.register(t, "Claude Code", "http://127.0.0.1/callback")
	verifier, challenge := pkce(t)
	code := redirectParams(t, f.allow(t, person, clientID, oauthRedirect, challenge, nil)).Get("code")
	pair := tokenPair(t, f.token(t, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {verifier},
		"client_id": {clientID}, "redirect_uri": {oauthRedirect}, "resource": {testOrigin + "/mcp"}}))
	return pair, clientID
}

func TestOAuthDiscoveryNamesThisServerAsTheAuthorizationServerOfMCP(t *testing.T) {
	f := newOAuthFixture(t)
	for _, path := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp"} {
		var resource struct {
			Resource             string   `json:"resource"`
			AuthorizationServers []string `json:"authorization_servers"`
			Documentation        string   `json:"resource_documentation"`
		}
		response := request(t, f.api, http.MethodGet, path, nil, "", nil)
		decode(t, response, &resource)
		if response.Code != http.StatusOK || resource.Resource != testOrigin+"/mcp" || len(resource.AuthorizationServers) != 1 || resource.AuthorizationServers[0] != testOrigin ||
			resource.Documentation != "https://colliedesk.com/docs/mcp" {
			t.Fatalf("%s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
	var server map[string]any
	response := request(t, f.api, http.MethodGet, "/.well-known/oauth-authorization-server", nil, "", nil)
	decode(t, response, &server)
	want := map[string]any{
		"issuer": testOrigin, "authorization_endpoint": testOrigin + "/oauth/authorize", "token_endpoint": testOrigin + "/oauth/token",
		"registration_endpoint": testOrigin + "/oauth/register", "client_id_metadata_document_supported": true,
		"service_documentation": "https://colliedesk.com/docs/mcp",
	}
	for key, value := range want {
		if server[key] != value {
			t.Errorf("%s=%v, want %v", key, server[key], value)
		}
	}
	for key, value := range map[string]string{"code_challenge_methods_supported": "S256", "token_endpoint_auth_methods_supported": "none"} {
		list, _ := server[key].([]any)
		if len(list) != 1 || list[0] != value {
			t.Errorf("%s=%v, want [%s] only", key, server[key], value)
		}
	}
}

func TestAnAIAppConnectsWithOAuthAndActsAsItsPersonButNeverAsAPerson(t *testing.T) {
	f := newOAuthFixture(t)
	admin := createSession(t, f.api, f.service, "OAuth Desk", "admin@oauth.example.com")
	pair, _ := f.connect(t, admin)
	if !strings.HasPrefix(pair.Access, "cdk_oat_") || !strings.HasPrefix(pair.Refresh, "cdk_ort_") || pair.Type != "Bearer" || pair.Expires != 3600 {
		t.Fatalf("pair=%+v, want prefixed access and refresh tokens, an hour long", pair)
	}
	var me struct {
		Permissions []string `json:"permissions"`
	}
	decode(t, request(t, f.api, http.MethodGet, "/api/v1/auth/me", nil, pair.Access, nil), &me)
	if len(me.Permissions) != len(auth.AllPermissions()) {
		t.Fatalf("an admin's app may do %d permissions, want every one the admin holds", len(me.Permissions))
	}
	if created := request(t, f.api, http.MethodPost, "/api/v1/contacts", map[string]any{"name": "Through OAuth", "email": "c@oauth.example.com"}, pair.Access, nil); created.Code != http.StatusCreated {
		t.Fatalf("create contact status=%d body=%s", created.Code, created.Body.String())
	}
	// Person-only routes refuse it: no token making, no allowing other apps.
	tokens := request(t, f.api, http.MethodPost, "/api/v1/auth/tokens", map[string]any{"label": "x", "role_id": "Admin"}, pair.Access, nil)
	if tokens.Code != http.StatusForbidden || !strings.Contains(tokens.Body.String(), "person_only") {
		t.Fatalf("token creation by an app status=%d body=%s, want 403 person_only", tokens.Code, tokens.Body.String())
	}
	_, challenge := pkce(t)
	if allowed := f.allow(t, pair.Access, f.register(t, "Other", oauthRedirect), oauthRedirect, challenge, nil); allowed.Code != http.StatusForbidden {
		t.Fatalf("an app allowing another app status=%d, want 403", allowed.Code)
	}
	// Never as a cookie, and the code and refresh token are not bearers.
	if response := request(t, f.api, http.MethodGet, "/api/v1/auth/me", nil, "", &http.Cookie{Name: "colliedesk_session", Value: pair.Access}); response.Code != http.StatusUnauthorized {
		t.Fatalf("access token as a cookie status=%d, want 401", response.Code)
	}
	if response := request(t, f.api, http.MethodGet, "/api/v1/auth/me", nil, pair.Refresh, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("refresh token as a bearer status=%d, want 401", response.Code)
	}
}

func TestOAuthAccessTokensCannotBeFoundByThePlainHashAnOlderImageUses(t *testing.T) {
	f := newOAuthFixture(t)
	admin := createSession(t, f.api, f.service, "OAuth Hash", "admin@hash.example.com")
	pair, _ := f.connect(t, admin)
	for _, raw := range []string{pair.Access, pair.Refresh} {
		plain := sha256.Sum256([]byte(raw))
		var found int
		if err := f.pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM sessions WHERE token_hash=$1)+(SELECT count(*) FROM oauth_tokens WHERE hash=$1)`, plain[:]).Scan(&found); err != nil {
			t.Fatal(err)
		}
		if found != 0 {
			t.Fatalf("%d rows match the plain SHA-256 of %s…: an older image would accept it", found, raw[:8])
		}
	}
}

func TestOAuthCodeWorksOnceWithItsVerifierAndASecondUseEndsTheGrant(t *testing.T) {
	f := newOAuthFixture(t)
	admin := createSession(t, f.api, f.service, "OAuth Code", "admin@code.example.com")
	clientID := f.register(t, "Codex", "http://localhost/callback")
	redirect := "http://localhost:51234/callback"
	verifier, challenge := pkce(t)
	code := redirectParams(t, f.allow(t, admin, clientID, redirect, challenge, nil)).Get("code")
	exchange := func(code, verifier, redirect string) *httptest.ResponseRecorder {
		return f.token(t, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {verifier}, "client_id": {clientID}, "redirect_uri": {redirect}})
	}
	other, _ := pkce(t)
	if response := exchange(code, other, redirect); oauthErrorCode(t, response) != "invalid_grant" {
		t.Fatalf("wrong verifier: %s, want invalid_grant", response.Body.String())
	}
	if response := exchange(code, verifier, "http://localhost:9/callback"); oauthErrorCode(t, response) != "invalid_grant" {
		t.Fatalf("another redirect: %s, want invalid_grant", response.Body.String())
	}
	pair := tokenPair(t, exchange(code, verifier, redirect))
	if response := exchange(code, verifier, redirect); oauthErrorCode(t, response) != "invalid_grant" {
		t.Fatalf("second use: %s, want invalid_grant", response.Body.String())
	}
	if status := meStatus(t, f.api, pair.Access); status != http.StatusUnauthorized {
		t.Fatalf("after the code was replayed the grant's access token status=%d, want 401", status)
	}
}

func TestOAuthCodeExpiresAfterFiveMinutes(t *testing.T) {
	f := newOAuthFixture(t)
	admin := createSession(t, f.api, f.service, "OAuth Expiry", "admin@expiry.example.com")
	clientID := f.register(t, "Codex", oauthRedirect)
	verifier, challenge := pkce(t)
	code := redirectParams(t, f.allow(t, admin, clientID, oauthRedirect, challenge, nil)).Get("code")
	f.clock.set(f.clock.Now().Add(5*time.Minute + time.Second))
	response := f.token(t, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {verifier}, "client_id": {clientID}, "redirect_uri": {oauthRedirect}})
	if oauthErrorCode(t, response) != "invalid_grant" || !strings.Contains(response.Body.String(), "codex mcp login") {
		t.Fatalf("expired code: %s, want invalid_grant that says how to reconnect", response.Body.String())
	}
}

func TestOAuthRefreshRotatesAndARetryWithinTheGraceKeepsEveryoneConnected(t *testing.T) {
	f := newOAuthFixture(t)
	admin := createSession(t, f.api, f.service, "OAuth Refresh", "admin@refresh.example.com")
	first, clientID := f.connect(t, admin)
	refresh := func(token string) *httptest.ResponseRecorder {
		return f.token(t, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {token}, "client_id": {clientID}})
	}
	second := tokenPair(t, refresh(first.Refresh))
	if second.Refresh == first.Refresh || second.Access == first.Access {
		t.Fatal("a refresh must rotate both tokens")
	}
	// The answer was lost: the app retries with the old refresh token within the grace.
	f.clock.set(f.clock.Now().Add(time.Minute))
	retry := tokenPair(t, refresh(first.Refresh))
	for _, access := range []string{first.Access, second.Access, retry.Access} {
		if status := meStatus(t, f.api, access); status != http.StatusOK {
			t.Fatalf("access token status=%d, want 200 for every pair inside the grace", status)
		}
	}
	if next := tokenPair(t, refresh(second.Refresh)); next.Access == "" {
		t.Fatal("the pair from before the retry must still refresh")
	}
	// Long after, the old refresh token is a replay: the grant ends for everyone.
	f.clock.set(f.clock.Now().Add(10 * time.Minute))
	if response := refresh(first.Refresh); oauthErrorCode(t, response) != "invalid_grant" {
		t.Fatalf("replay: %s, want invalid_grant", response.Body.String())
	}
	if status := meStatus(t, f.api, retry.Access); status != http.StatusUnauthorized {
		t.Fatalf("after a replay the grant's access token status=%d, want 401", status)
	}
}

func TestTwoRefreshesAtOnceWithOneTokenBothSucceed(t *testing.T) {
	f := newOAuthFixture(t)
	admin := createSession(t, f.api, f.service, "OAuth Race", "admin@race.example.com")
	pair, clientID := f.connect(t, admin)
	var wg sync.WaitGroup
	results := make([]*httptest.ResponseRecorder, 2)
	for index := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[index] = f.token(t, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {pair.Refresh}, "client_id": {clientID}})
		}()
	}
	wg.Wait()
	for _, response := range results {
		next := tokenPair(t, response)
		if status := meStatus(t, f.api, next.Access); status != http.StatusOK {
			t.Fatalf("access token of a concurrent refresh status=%d, want 200", status)
		}
	}
}

func TestOAuthAccessTokenLastsAnHourAndTheGrantSlidesThirtyDays(t *testing.T) {
	f := newOAuthFixture(t)
	admin := createSession(t, f.api, f.service, "OAuth Life", "admin@life.example.com")
	pair, clientID := f.connect(t, admin)
	f.clock.set(f.clock.Now().Add(time.Hour + time.Second))
	if status := meStatus(t, f.api, pair.Access); status != http.StatusUnauthorized {
		t.Fatalf("access token after an hour status=%d, want 401", status)
	}
	f.clock.set(f.clock.Now().Add(29 * 24 * time.Hour))
	next := tokenPair(t, f.token(t, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {pair.Refresh}, "client_id": {clientID}}))
	f.clock.set(f.clock.Now().Add(30*24*time.Hour + time.Second))
	if response := f.token(t, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {next.Refresh}, "client_id": {clientID}}); oauthErrorCode(t, response) != "invalid_grant" {
		t.Fatalf("a grant unused for 30 days refreshed: %s", response.Body.String())
	}
}

func TestDisconnectInSettingsStopsTheAppAtOnce(t *testing.T) {
	f := newOAuthFixture(t)
	admin := createSession(t, f.api, f.service, "OAuth Disconnect", "admin@disconnect.example.com")
	pair, clientID := f.connect(t, admin)
	var sessions struct {
		Items []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
			Token *struct {
				Kind string `json:"kind"`
			} `json:"token"`
			App *struct {
				ClientID          string `json:"client_id"`
				UserAgent         string `json:"user_agent"`
				NeedsReconnecting bool   `json:"needs_reconnecting"`
			} `json:"app"`
		} `json:"items"`
	}
	decode(t, request(t, f.api, http.MethodGet, "/api/v1/auth/sessions", nil, admin, nil), &sessions)
	grantID := ""
	for _, session := range sessions.Items {
		if session.App != nil {
			if session.Label != "Claude Code" || session.Token == nil || session.Token.Kind != "oauth" || session.App.ClientID != clientID || session.App.NeedsReconnecting {
				t.Fatalf("connected app row=%+v", session)
			}
			grantID = session.ID
		}
	}
	if grantID == "" {
		t.Fatalf("no connected app in %+v", sessions)
	}
	if response := request(t, f.api, http.MethodDelete, "/api/v1/auth/sessions/"+grantID, nil, admin, nil); response.Code != http.StatusNoContent && response.Code != http.StatusOK {
		t.Fatalf("disconnect status=%d body=%s", response.Code, response.Body.String())
	}
	if status := meStatus(t, f.api, pair.Access); status != http.StatusUnauthorized {
		t.Fatalf("access token after Disconnect status=%d, want 401", status)
	}
	if response := f.token(t, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {pair.Refresh}}); oauthErrorCode(t, response) != "invalid_grant" {
		t.Fatalf("refresh after Disconnect: %s, want invalid_grant", response.Body.String())
	}
	decode(t, request(t, f.api, http.MethodGet, "/api/v1/auth/sessions", nil, admin, nil), &sessions)
	for _, session := range sessions.Items {
		if session.ID == grantID && (session.App == nil || !session.App.NeedsReconnecting) {
			t.Fatalf("disconnected app row=%+v, want needs_reconnecting", session)
		}
	}
}

func TestAGrantForOneWorkspaceNeverReadsAnother(t *testing.T) {
	f := newOAuthFixture(t)
	adminA := createSession(t, f.api, f.service, "Grant A", "a@grant-a.example.com")
	adminB := createSession(t, f.api, f.service, "Grant B", "b@grant-b.example.com")
	pairA, _ := f.connect(t, adminA)
	contactB := create(t, f.api, adminB, "/api/v1/contacts", map[string]any{"name": "Only B", "email": "only@grant-b.example.com"})
	if response := request(t, f.api, http.MethodGet, "/api/v1/contacts/"+contactB, nil, pairA.Access, nil); response.Code != http.StatusNotFound {
		t.Fatalf("A's app reading B's contact status=%d, want 404", response.Code)
	}
	listed := request(t, f.api, http.MethodGet, "/api/v1/contacts", nil, pairA.Access, nil)
	if listed.Code != http.StatusOK || strings.Contains(listed.Body.String(), "Only B") {
		t.Fatalf("A's app listing contacts status=%d, lists B's: %v", listed.Code, strings.Contains(listed.Body.String(), "Only B"))
	}
	var sessions struct {
		Items []struct {
			App *struct{} `json:"app"`
		} `json:"items"`
	}
	decode(t, request(t, f.api, http.MethodGet, "/api/v1/auth/sessions", nil, adminB, nil), &sessions)
	for _, session := range sessions.Items {
		if session.App != nil {
			t.Fatal("B lists A's connected app")
		}
	}
	// B's admin cannot disconnect A's grant by id either.
	var grantA string
	if err := f.pool.QueryRow(context.Background(), `SELECT id::text FROM sessions WHERE scope='oauth'`).Scan(&grantA); err != nil {
		t.Fatal(err)
	}
	request(t, f.api, http.MethodDelete, "/api/v1/auth/sessions/"+grantA, nil, adminB, nil)
	if status := meStatus(t, f.api, pairA.Access); status != http.StatusOK {
		t.Fatalf("A's app after B tried to disconnect it status=%d, want 200", status)
	}
}

func TestAGrantFromASessionLimitedToAgentStaysLimited(t *testing.T) {
	f := newOAuthFixture(t)
	_, pairingURL, err := f.service.CreateWorkspace(context.Background(), "OAuth Limited", "admin@limited.example.com")
	if err != nil {
		t.Fatal(err)
	}
	var redeemed struct {
		Token string `json:"token"`
	}
	decode(t, request(t, f.api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, pairingURL), "requested_role": "agent"}, "", nil), &redeemed)
	pair, _ := f.connect(t, redeemed.Token)
	invite := request(t, f.api, http.MethodPost, "/api/v1/auth/invites", map[string]any{"email": "x@example.com", "roles": []string{"Agent"}}, pair.Access, nil)
	if invite.Code != http.StatusForbidden {
		t.Fatalf("an app allowed from an Agent-limited session invited someone: status=%d", invite.Code)
	}
}

func TestDeactivatingThePersonEndsTheirAppsGrants(t *testing.T) {
	f := newOAuthFixture(t)
	admin := createSession(t, f.api, f.service, "OAuth Deactivate", "admin@deact.example.com")
	invite := request(t, f.api, http.MethodPost, "/api/v1/auth/invites", map[string]any{"email": "ann@deact.example.com", "roles": []string{"Agent"}}, admin, nil)
	var link struct {
		URL string `json:"url"`
	}
	decode(t, invite, &link)
	var ann struct {
		Token string `json:"token"`
		Agent struct {
			ID string `json:"id"`
		} `json:"agent"`
	}
	decode(t, request(t, f.api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, link.URL)}, "", nil), &ann)
	pair, clientID := f.connect(t, ann.Token)
	if response := request(t, f.api, http.MethodPatch, "/api/v1/agents/"+ann.Agent.ID, map[string]any{"active": false}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("deactivate status=%d body=%s", response.Code, response.Body.String())
	}
	if status := meStatus(t, f.api, pair.Access); status != http.StatusUnauthorized {
		t.Fatalf("app of a deactivated person status=%d, want 401", status)
	}
	if response := f.token(t, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {pair.Refresh}, "client_id": {clientID}}); oauthErrorCode(t, response) != "invalid_grant" {
		t.Fatalf("refresh of a deactivated person: %s", response.Body.String())
	}
}

func TestConsentRefusesAnUnregisteredRedirectAndPlainPKCE(t *testing.T) {
	f := newOAuthFixture(t)
	admin := createSession(t, f.api, f.service, "OAuth Consent", "admin@consent.example.com")
	clientID := f.register(t, "Claude", "https://claude.ai/api/mcp/auth_callback", "http://localhost/cb")
	describe := func(clientID, redirect string) *httptest.ResponseRecorder {
		return request(t, f.api, http.MethodGet, "/api/v1/oauth/client?"+url.Values{"client_id": {clientID}, "redirect_uri": {redirect}}.Encode(), nil, "", nil)
	}
	var app struct {
		Name string `json:"name"`
		Host string `json:"host"`
	}
	ok := describe(clientID, "https://claude.ai/api/mcp/auth_callback")
	decode(t, ok, &app)
	if ok.Code != http.StatusOK || app.Name != "Claude" || app.Host != "claude.ai" {
		t.Fatalf("describe status=%d body=%s", ok.Code, ok.Body.String())
	}
	if response := describe(clientID, "http://localhost:4567/cb"); response.Code != http.StatusOK {
		t.Fatalf("a loopback redirect on another port status=%d, want 200", response.Code)
	}
	for _, redirect := range []string{"https://evil.example/api/mcp/auth_callback", "https://claude.ai/api/mcp/auth_callback/x", "http://127.0.0.1:4567/cb"} {
		if response := describe(clientID, redirect); response.Code != http.StatusBadRequest {
			t.Fatalf("redirect %s status=%d, want 400", redirect, response.Code)
		}
	}
	if response := describe(clientID+"x", "https://claude.ai/api/mcp/auth_callback"); response.Code != http.StatusBadRequest {
		t.Fatalf("a forged client id status=%d, want 400", response.Code)
	}
	if response := f.allow(t, admin, clientID, "https://evil.example/cb", "c", nil); response.Code != http.StatusBadRequest {
		t.Fatalf("allow to an unregistered redirect status=%d, want 400 (never a redirect)", response.Code)
	}
	_, challenge := pkce(t)
	refused := redirectParams(t, f.allow(t, admin, clientID, "https://claude.ai/api/mcp/auth_callback", challenge, map[string]any{"code_challenge_method": "plain"}))
	if refused.Get("error") != "invalid_request" || refused.Get("code") != "" || refused.Get("state") != "xyz" || refused.Get("iss") != testOrigin {
		t.Fatalf("plain PKCE answer=%v, want invalid_request back to the app with state and iss", refused)
	}
	other := redirectParams(t, f.allow(t, admin, clientID, "https://claude.ai/api/mcp/auth_callback", challenge, map[string]any{"resource": "https://other.example/mcp"}))
	if other.Get("error") != "invalid_target" {
		t.Fatalf("a token for another resource answer=%v, want invalid_target", other)
	}
	req := newRequest(t, http.MethodPost, "/api/v1/oauth/authorize", map[string]any{"client_id": clientID}, admin, nil)
	req.Header.Set("Origin", "https://evil.example")
	if response := serve(f.api, req); response.Code != http.StatusForbidden {
		t.Fatalf("allow from another origin status=%d, want 403", response.Code)
	}
}

func TestRegistrationRefusesUnsafeRedirectsAndIsLimitedPerAddress(t *testing.T) {
	f := newOAuthFixture(t)
	for _, redirects := range [][]string{{"http://evil.example/cb"}, {"javascript:alert(1)"}, {}, {"https://ok.example/cb#frag"}} {
		response := request(t, f.api, http.MethodPost, "/oauth/register", map[string]any{"client_name": "x", "redirect_uris": redirects}, "", nil)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("redirects %v status=%d, want 400", redirects, response.Code)
		}
	}
	for _, scheme := range []string{"cursor://anysphere.cursor-mcp/oauth/callback", "vscode://vscode.github-authentication/did-authenticate"} {
		f.register(t, "Desktop app", scheme)
	}
	// Refused registrations are not counted: they store nothing and sign nothing.
	for range 50 {
		request(t, f.api, http.MethodPost, "/oauth/register", map[string]any{"client_name": "x", "redirect_uris": []string{"http://evil.example/cb"}}, "", nil)
	}
	// Two valid registrations above, 998 here: 1000 in the hour.
	for index := range 998 {
		response := request(t, f.api, http.MethodPost, "/oauth/register", map[string]any{"client_name": "x", "redirect_uris": []string{oauthRedirect}}, "", nil)
		if response.Code != http.StatusCreated {
			t.Fatalf("valid registration %d from one address in an hour status=%d, want 201", index+3, response.Code)
		}
	}
	if response := request(t, f.api, http.MethodPost, "/oauth/register", map[string]any{"client_name": "x", "redirect_uris": []string{oauthRedirect}}, "", nil); response.Code != http.StatusTooManyRequests {
		t.Fatalf("valid registration 1001 from one address in an hour status=%d, want 429", response.Code)
	}
	f.clock.set(f.clock.Now().Add(time.Hour + time.Second))
	f.register(t, "After an hour", oauthRedirect)
}

func TestClientMetadataDocumentIsFetchedSafely(t *testing.T) {
	var documentURL string
	site := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/client.json":
			_ = json.NewEncoder(response).Encode(map[string]any{"client_id": documentURL, "client_name": "Claude Code", "redirect_uris": []string{"http://localhost/callback"}})
		case "/liar.json":
			_ = json.NewEncoder(response).Encode(map[string]any{"client_id": "https://someone-else.example/client.json", "redirect_uris": []string{"http://localhost/callback"}})
		case "/moved.json":
			http.Redirect(response, request, "/client.json", http.StatusFound)
		}
	}))
	defer site.Close()
	documentURL = site.URL + "/client.json"
	trusting := site.Client()
	trusting.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	f := newOAuthFixture(t, func(config *httpapi.Config) { config.OAuthMetadataClient = trusting })
	describe := func(clientID string) *httptest.ResponseRecorder {
		return request(t, f.api, http.MethodGet, "/api/v1/oauth/client?"+url.Values{"client_id": {clientID}, "redirect_uri": {"http://localhost:5555/callback"}}.Encode(), nil, "", nil)
	}
	var app struct {
		Name       string `json:"name"`
		Verified   bool   `json:"verified"`
		ClientHost string `json:"client_host"`
	}
	response := describe(documentURL)
	decode(t, response, &app)
	// The document names itself "Claude Code" on a host that is not Anthropic's: the name is not verified,
	// and the page says which host published it.
	if response.Code != http.StatusOK || app.Name != "Claude Code" || app.Verified || app.ClientHost != "127.0.0.1" {
		t.Fatalf("metadata document status=%d body=%s", response.Code, response.Body.String())
	}
	for _, path := range []string{"/liar.json", "/moved.json", "/missing.json"} {
		if response := describe(site.URL + path); response.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d body=%s, want 400", path, response.Code, response.Body.String())
		}
	}
	// The guarded client (no test override) never reaches this computer's own addresses.
	guarded := newOAuthFixture(t)
	refused := request(t, guarded.api, http.MethodGet, "/api/v1/oauth/client?"+url.Values{"client_id": {documentURL}, "redirect_uri": {"http://localhost/callback"}}.Encode(), nil, "", nil)
	if refused.Code != http.StatusBadRequest || !strings.Contains(refused.Body.String(), "not a public address") {
		t.Fatalf("metadata on a loopback address status=%d body=%s, want 400 not a public address", refused.Code, refused.Body.String())
	}
}

func TestARoleChangeAppliesToTheAppAtOnce(t *testing.T) {
	f := newOAuthFixture(t)
	admin := createSession(t, f.api, f.service, "OAuth Roles", "admin@roles-oauth.example.com")
	pair, _ := f.connect(t, admin)
	var me struct {
		Agent struct {
			ID string `json:"id"`
		} `json:"agent"`
	}
	decode(t, request(t, f.api, http.MethodGet, "/api/v1/auth/me", nil, admin, nil), &me)
	// A second admin keeps the workspace administered while this one becomes an Agent.
	second := request(t, f.api, http.MethodPost, "/api/v1/auth/invites", map[string]any{"email": "second@roles-oauth.example.com", "roles": []string{"Admin"}}, admin, nil)
	var link struct {
		URL string `json:"url"`
	}
	decode(t, second, &link)
	request(t, f.api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, link.URL)}, "", nil)
	if response := request(t, f.api, http.MethodPut, "/api/v1/agents/"+me.Agent.ID+"/roles", map[string]any{"roles": []string{"Agent"}}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("set roles status=%d body=%s", response.Code, response.Body.String())
	}
	if response := request(t, f.api, http.MethodPost, "/api/v1/auth/invites", map[string]any{"email": "y@example.com", "roles": []string{"Agent"}}, pair.Access, nil); response.Code != http.StatusForbidden {
		t.Fatalf("the app of a person who lost Admin invited someone: status=%d", response.Code)
	}
	if status := meStatus(t, f.api, pair.Access); status != http.StatusOK {
		t.Fatalf("the app still works with what the person holds: status=%d", status)
	}
}

// A repeat of one press of Allow (the same Idempotency-Key) makes nothing new and never shows the code
// again: the app is told to start again.
func TestARepeatedAllowNeverShowsTheCodeAgain(t *testing.T) {
	f := newOAuthFixture(t)
	admin := createSession(t, f.api, f.service, "OAuth Repeat", "admin@repeat.example.com")
	clientID := f.register(t, "Claude", oauthRedirect)
	_, challenge := pkce(t)
	press := func() *httptest.ResponseRecorder {
		req := newRequest(t, http.MethodPost, "/api/v1/oauth/authorize", map[string]any{"client_id": clientID, "redirect_uri": oauthRedirect, "response_type": "code",
			"code_challenge": challenge, "code_challenge_method": "S256", "state": "xyz"}, admin, nil)
		req.Header.Set("Origin", testOrigin)
		req.Header.Set("Idempotency-Key", "allow-press-1")
		return serve(f.api, req)
	}
	if first := redirectParams(t, press()); first.Get("code") == "" {
		t.Fatalf("first press answered %v, want a code", first)
	}
	again := redirectParams(t, press())
	if again.Get("code") != "" || again.Get("error") != "server_error" || again.Get("state") != "xyz" {
		t.Fatalf("repeat answered %v, want no code and an error for the app", again)
	}
	var grants int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM sessions WHERE scope='oauth'`).Scan(&grants); err != nil || grants != 1 {
		t.Fatalf("%d grants after a repeated press (err %v), want 1", grants, err)
	}
}
