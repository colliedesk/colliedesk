package mcpserver_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// connectWithOAuth runs an AI app's OAuth sign-in for the person of session: register, Allow, trade the code.
func (e *env) connectWithOAuth(session string) string {
	e.t.Helper()
	status, registered := e.rest(http.MethodPost, "/oauth/register", map[string]any{"client_name": "Claude Code", "redirect_uris": []string{"http://localhost/callback"}}, "")
	if status != http.StatusCreated {
		e.t.Fatalf("register status=%d body=%v", status, registered)
	}
	clientID := registered["client_id"].(string)
	verifier := strings.Repeat("v", 50)
	sum := sha256.Sum256([]byte(verifier))
	body, _ := json.Marshal(map[string]any{"client_id": clientID, "redirect_uri": "http://localhost:7777/callback", "response_type": "code",
		"code_challenge": base64.RawURLEncoding.EncodeToString(sum[:]), "code_challenge_method": "S256", "resource": testOrigin + "/mcp"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/oauth/authorize", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+session)
	req.Header.Set("Origin", testOrigin)
	recorder := httptest.NewRecorder()
	e.api.ServeHTTP(recorder, req)
	var allowed struct {
		RedirectTo string `json:"redirect_to"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &allowed); err != nil || recorder.Code != http.StatusOK {
		e.t.Fatalf("allow status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	back, _ := url.Parse(allowed.RedirectTo)
	form := url.Values{"grant_type": {"authorization_code"}, "code": {back.Query().Get("code")}, "code_verifier": {verifier},
		"client_id": {clientID}, "redirect_uri": {"http://localhost:7777/callback"}}
	tokenRequest := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	tokenRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder = httptest.NewRecorder()
	e.api.ServeHTTP(recorder, tokenRequest)
	var pair struct {
		Access string `json:"access_token"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &pair); err != nil || pair.Access == "" {
		e.t.Fatalf("token status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	return pair.Access
}

func postMCP(t *testing.T, address, bearer string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, address, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	return response, string(body)
}

// An app that connects with OAuth finds its sign-in from the 401 alone, and its access token works on /mcp.
func TestMCPNamesItsProtectedResourceMetadataAndTakesAnOAuthAccessToken(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("OAuth MCP", "admin@oauth-mcp.example.com")
	response, body := postMCP(t, e.mcpURL(), "")
	challenge := response.Header.Get("WWW-Authenticate")
	host := strings.TrimPrefix(e.server.URL, "http://")
	if response.StatusCode != http.StatusUnauthorized || !strings.Contains(challenge, `resource_metadata="https://`+host+`/.well-known/oauth-protected-resource/mcp"`) {
		t.Fatalf("status=%d WWW-Authenticate=%q, want 401 naming the resource metadata", response.StatusCode, challenge)
	}
	if !strings.Contains(body, "codex mcp login colliedesk") || !strings.Contains(body, "Authenticate") {
		t.Fatalf("401 body %s does not say how to connect again", body)
	}
	access := e.connectWithOAuth(w.admin)
	if response, body := postMCP(t, e.mcpURL(), access); response.StatusCode != http.StatusOK || !strings.Contains(body, "colliedesk_whoami") {
		t.Fatalf("OAuth access token on /mcp status=%d body=%.200s", response.StatusCode, body)
	}
	// Each answer holds to its tool's output schema (a real client refuses one that does not).
	session := e.connect(access, "")
	if kind := e.ok(session, "colliedesk_whoami", nil)["token"].(map[string]any)["kind"]; kind != "oauth" {
		t.Fatalf("whoami token kind=%v, want oauth", kind)
	}
	listed := false
	for _, row := range e.ok(session, "colliedesk_sessions_list", nil)["sessions"].([]any) {
		if app, ok := row.(map[string]any)["app"].(map[string]any); ok && app["needs_reconnecting"] == false && row.(map[string]any)["this_token"] == true {
			listed = true
		}
	}
	if !listed {
		t.Fatal("colliedesk_sessions_list does not show the app's own connection with its app details")
	}
}
