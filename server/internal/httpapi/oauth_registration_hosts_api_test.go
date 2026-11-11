package httpapi_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// Anyone can call dynamic registration, so an https redirect it signs must be a known AI app's own
// callback. Any other web app proves its host with a client ID metadata document instead.
func TestRegistrationTakesHttpsRedirectsOnlyOnKnownAIAppHosts(t *testing.T) {
	f := newOAuthFixture(t)
	for _, redirect := range []string{
		"https://attacker-alpha.example.com/cb",
		"https://claude.ai.attacker.example/cb",
		"https://notclaude.ai/cb",
		"https://claude.ai:8443/api/mcp/auth_callback",
	} {
		response := request(t, f.api, http.MethodPost, "/oauth/register", map[string]any{"client_name": "ProbeAlpha", "redirect_uris": []string{redirect}}, "", nil)
		if response.Code != http.StatusBadRequest || oauthErrorCode(t, response) != "invalid_redirect_uri" {
			t.Fatalf("redirect %s status=%d body=%s, want 400 invalid_redirect_uri", redirect, response.Code, response.Body.String())
		}
	}
	mixed := request(t, f.api, http.MethodPost, "/oauth/register", map[string]any{"client_name": "x",
		"redirect_uris": []string{"https://claude.ai/api/mcp/auth_callback", "https://attacker-alpha.example.com/cb"}}, "", nil)
	if mixed.Code != http.StatusBadRequest {
		t.Fatalf("a known callback with another host status=%d, want 400", mixed.Code)
	}
	f.register(t, "Claude", "https://claude.ai/api/mcp/auth_callback")
	f.register(t, "ChatGPT", "https://chatgpt.com/connector_platform_oauth_redirect")
	f.register(t, "Codex", "http://localhost/callback")
	f.register(t, "Cursor", "cursor://anysphere.cursor-mcp/oauth/callback")
}

// A client id signed before the rule above still carries its redirect: the consent page and Allow refuse
// it, so a registration made earlier cannot be used either.
func TestARegistrationSignedForAnotherHostIsRefusedAtConsent(t *testing.T) {
	f := newOAuthFixture(t)
	admin := createSession(t, f.api, f.service, "OAuth Hosts", "admin@hosts.example.com")
	f.register(t, "Codex", oauthRedirect) // makes the signing key
	const redirect = "https://attacker-alpha.example.com/cb"
	clientID := signedClientID(t, f, "ProbeAlpha", redirect)
	describe := request(t, f.api, http.MethodGet, "/api/v1/oauth/client?"+url.Values{"client_id": {clientID}, "redirect_uri": {redirect}}.Encode(), nil, "", nil)
	if describe.Code != http.StatusBadRequest || errorCode(t, describe) != "invalid_client" || !strings.Contains(describe.Body.String(), "not a known AI app") {
		t.Fatalf("describe status=%d body=%s, want 400 invalid_client saying the redirect is not a known AI app's", describe.Code, describe.Body.String())
	}
	_, challenge := pkce(t)
	if response := f.allow(t, admin, clientID, redirect, challenge, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("allow status=%d body=%s, want 400 and never a redirect", response.Code, response.Body.String())
	}
}

// signedClientID makes a client id the way a registration signs one, as a server before this rule did.
func signedClientID(t *testing.T, f oauthFixture, name string, redirects ...string) string {
	t.Helper()
	var key []byte
	if err := f.pool.QueryRow(context.Background(), `SELECT value FROM server_secrets WHERE name='oauth_client_key'`).Scan(&key); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{"n": name, "r": redirects})
	if err != nil {
		t.Fatal(err)
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payload))
	return "cdk_client_" + payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
