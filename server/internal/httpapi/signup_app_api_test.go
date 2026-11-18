package httpapi_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// registerApp registers an OAuth client the way an AI app's connector does and returns its client id.
func (f signupFixture) registerApp(t *testing.T, name, redirect string) string {
	t.Helper()
	response := request(t, f.api, http.MethodPost, "/oauth/register", map[string]any{
		"client_name": name, "redirect_uris": []string{redirect}, "token_endpoint_auth_method": "none",
	}, "", nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("register %s: status=%d body=%s", name, response.Code, response.Body.String())
	}
	var body struct {
		ClientID string `json:"client_id"`
	}
	decode(t, response, &body)
	return body.ClientID
}

func authorizeNext(clientID string) string {
	return "/oauth/authorize?" + url.Values{"client_id": {clientID}, "response_type": {"code"}, "state": {"abc"}}.Encode()
}

// A trial an AI app's connector started names that app, as one word from a fixed list, in trial_started
// (signup_app) and in the workspace's signed_up milestone (app). The app comes from the client id in the
// consent page's address: a client ID metadata document by its host (never fetched), a registration this
// server signed by its name. Anything else, a forged registration included, is "other"; a
// sign-up on the web names no app.
func TestATrialFromAnAIAppConnectorNamesTheApp(t *testing.T) {
	funnel := &recordingFunnel{}
	f := newSignupFixture(t, func(config *httpapi.Config) { config.Funnel = funnel })

	type signup struct{ next, want string }
	var cases []signup
	signUp := func(c signup) {
		t.Helper()
		i := len(cases)
		cases = append(cases, c)
		body := map[string]any{"workspace_name": fmt.Sprintf("App Test %d", i)}
		if c.next != "" {
			body["next"] = c.next
		}
		peer := fmt.Sprintf("203.0.113.%d:40000", 150+i)
		wantResponse(t, f.signUpWith(t, fmt.Sprintf("owner%d@app.example.com", i), peer, body), http.StatusCreated, "")
	}

	// Before any registration exists the server has no signing key: still a trial, from another app.
	signUp(signup{authorizeNext("cdk_client_e30.c2lnbmF0dXJl"), "other"})

	claudeCode := f.registerApp(t, "Claude Code (colliedesk)", "http://localhost:53682/callback")
	signUp(signup{authorizeNext(claudeCode), "claude_code"})
	signUp(signup{authorizeNext(f.registerApp(t, "Codex", "http://localhost/callback")), "codex"})
	signUp(signup{authorizeNext(f.registerApp(t, "Cursor", "cursor://anysphere.cursor-mcp/oauth/callback")), "cursor"})
	signUp(signup{authorizeNext(f.registerApp(t, "ChatGPT", "https://chatgpt.com/connector_platform_oauth_redirect")), "chatgpt"})
	signUp(signup{authorizeNext(f.registerApp(t, "Acme Helper", "http://127.0.0.1:9000/cb")), "other"})
	payload, _, _ := strings.Cut(claudeCode, ".")
	signUp(signup{authorizeNext(payload + ".AAAA"), "other"})

	signUp(signup{authorizeNext("https://claude.ai/oauth/mcp-oauth-client-metadata"), "claude"})
	signUp(signup{testOrigin + authorizeNext("https://claude.ai/oauth/mcp-oauth-client-metadata"), "claude"})
	// Claude Code and Codex publish their documents on the Claude and ChatGPT hosts, under their own path.
	signUp(signup{authorizeNext("https://claude.ai/oauth/claude-code-client-metadata"), "claude_code"})
	signUp(signup{authorizeNext("https://chatgpt.com/oauth/codex/client.json"), "codex"})
	signUp(signup{authorizeNext("https://glama.ai/mcp/oauth/client.json"), "glama"})
	signUp(signup{authorizeNext("https://claude.ai.attacker.example/client.json"), "other"})
	signUp(signup{authorizeNext("https://claude.ai:8443/client.json"), "other"})
	signUp(signup{"/oauth/authorize", "other"})
	signUp(signup{"/inbox", ""})
	signUp(signup{"", ""})

	events := funnel.named("trial_started")
	if len(events) != len(cases) {
		t.Fatalf("events=%+v, want one trial_started per sign-up", events)
	}
	for i, c := range cases {
		got, present := events[i].Properties["signup_app"]
		if c.want == "" && present {
			t.Errorf("next %q: signup_app=%#v, want none on a web sign-up", c.next, got)
		}
		if c.want != "" && got != c.want {
			t.Errorf("next %q: signup_app=%#v, want %q", c.next, got, c.want)
		}
		signedUp := onlyKind(milestonesOf(t, f.pool, events[i].Workspace), "signed_up")
		if len(signedUp) != 1 {
			t.Fatalf("next %q: signed_up=%+v, want one", c.next, signedUp)
		}
		app, present := signedUp[0].Detail["app"]
		if c.want == "" && present {
			t.Errorf("next %q: signed_up app=%#v, want none on a web sign-up", c.next, app)
		}
		if c.want != "" && app != c.want {
			t.Errorf("next %q: signed_up app=%#v, want %q (detail %v)", c.next, app, c.want, signedUp[0].Detail)
		}
	}
}
