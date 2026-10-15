package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

func tokenAPICall(t *testing.T, env *trialEnv, method, path, token string, body string) *httptest.ResponseRecorder {
	t.Helper()
	handler := httpapi.New(env.pool, httpapi.Config{Origin: trialTestOrigin})
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func TestTokenCreatePrintsATokenThatAuthenticatesWithItsScope(t *testing.T) {
	env := newTrialEnv(t, "Token Desk")
	var hint bytes.Buffer
	previous := stderr
	stderr = &hint
	t.Cleanup(func() { stderr = previous })

	printed := env.mustRun("token", "create", "--workspace", "token-desk", "--email", "Admin@TokenDesk.example.com", "--scope", "draft", "--label", "Claude Code")
	token := strings.TrimSpace(printed)
	if token == "" || strings.Count(printed, "\n") != 1 || strings.ContainsAny(token, " \t") {
		t.Fatalf("stdout must hold only the token: %q", printed)
	}
	if !strings.Contains(hint.String(), "once") {
		t.Fatalf("hint does not say the token is shown once: %q", hint.String())
	}

	me := tokenAPICall(t, env, http.MethodGet, "/api/v1/auth/me", token, "")
	var identity struct {
		Agent struct {
			Email string `json:"email"`
			Role  string `json:"role"`
		} `json:"agent"`
	}
	if err := json.Unmarshal(me.Body.Bytes(), &identity); err != nil || me.Code != http.StatusOK ||
		identity.Agent.Email != "admin@tokendesk.example.com" || identity.Agent.Role != "agent" {
		t.Fatalf("token does not authenticate as the agent role: status=%d body=%s", me.Code, me.Body.String())
	}
	if list := tokenAPICall(t, env, http.MethodGet, "/api/v1/auth/sessions", token, ""); !strings.Contains(list.Body.String(), `"label":"Claude Code"`) ||
		!strings.Contains(list.Body.String(), `"scope":"draft"`) {
		t.Fatalf("session list lacks the token: %s", list.Body.String())
	}
	tag := tokenAPICall(t, env, http.MethodPost, "/api/v1/tags", token, `{"name":"x"}`)
	if tag.Code != http.StatusForbidden || !strings.Contains(tag.Body.String(), "scope_insufficient") {
		t.Fatalf("a draft token created a tag: status=%d body=%s", tag.Code, tag.Body.String())
	}
}

func TestTokenCreateRefusesBadInputAndCreatesNothing(t *testing.T) {
	env := newTrialEnv(t, "Token Refusal")
	cases := map[string]struct {
		args []string
		want string
	}{
		"unknown scope":     {[]string{"--scope", "admin", "--label", "x"}, "read, draft, send"},
		"missing scope":     {[]string{"--label", "x"}, "scope"},
		"missing label":     {[]string{"--scope", "read"}, "label"},
		"unknown agent":     {[]string{"--email", "nobody@tokenrefusal.example.com", "--scope", "read", "--label", "x"}, "nobody@tokenrefusal.example.com"},
		"unknown workspace": {[]string{"--workspace", "no-such-desk", "--scope", "read", "--label", "x"}, "no-such-desk"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			args := append([]string{"token", "create", "--workspace", "token-refusal", "--email", "admin@tokenrefusal.example.com"}, c.args...)
			var output bytes.Buffer
			err := run(context.Background(), args, &output, env.databaseURL, trialTestOrigin)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), c.want) {
				t.Fatalf("err=%v, want a message containing %q", err, c.want)
			}
			if output.Len() != 0 {
				t.Fatalf("a refused command printed %q", output.String())
			}
		})
	}
	if _, err := env.run("token"); err == nil || !strings.Contains(err.Error(), "usage: colliedesk token create") {
		t.Fatalf("token without a subcommand: %v", err)
	}
	var count int
	if err := env.pool.QueryRow(context.Background(), `SELECT count(*) FROM sessions WHERE scope IS NOT NULL`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("refused commands created %d tokens (err=%v)", count, err)
	}
}
