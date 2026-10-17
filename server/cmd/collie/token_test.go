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
	if !strings.HasPrefix(token, "cdk_agent_") || strings.Count(printed, "\n") != 1 || strings.ContainsAny(token, " \t") {
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
		Scope string `json:"scope"`
	}
	if err := json.Unmarshal(me.Body.Bytes(), &identity); err != nil || me.Code != http.StatusOK ||
		identity.Agent.Email != "admin@tokendesk.example.com" || identity.Agent.Role != "agent" || identity.Scope != "draft" {
		t.Fatalf("token does not authenticate as the agent role with the draft scope: status=%d body=%s", me.Code, me.Body.String())
	}
	var label string
	if err := env.pool.QueryRow(context.Background(), `SELECT label FROM sessions WHERE scope='draft'`).Scan(&label); err != nil || label != "Claude Code" {
		t.Fatalf("stored token label = %q (err=%v), want Claude Code", label, err)
	}
	if created := tokenAPICall(t, env, http.MethodPost, "/api/v1/tags", token, `{"name":"x"}`); created.Code != http.StatusCreated {
		t.Fatalf("a draft token could not create a tag: status=%d body=%s", created.Code, created.Body.String())
	}
	// Above the draft scope the call waits for a person as an agent request instead of running.
	if sent := tokenAPICall(t, env, http.MethodPost, "/api/v1/conversations", token, `{"contact_id":"00000000-0000-4000-8000-000000000000","subject":"x"}`); sent.Code != http.StatusAccepted || !strings.Contains(sent.Body.String(), `"request_id"`) {
		t.Fatalf("a draft token's conversation must wait for a person: status=%d body=%s", sent.Code, sent.Body.String())
	}
}

// An admin token acts with the admin role, so only an admin may be given one.
func TestTokenCreateGivesTheAdminTierOnlyToAnAdmin(t *testing.T) {
	env := newTrialEnv(t, "Admin Token Desk")
	var hint bytes.Buffer
	previous := stderr
	stderr = &hint
	t.Cleanup(func() { stderr = previous })
	if _, err := env.pool.Exec(context.Background(), `INSERT INTO agents(workspace_id,email,role) SELECT id,'helper@admintokendesk.example.com','agent' FROM workspaces WHERE slug='admin-token-desk'`); err != nil {
		t.Fatal(err)
	}
	if _, err := env.run("token", "create", "--workspace", "admin-token-desk", "--email", "helper@admintokendesk.example.com", "--scope", "admin", "--label", "x"); err == nil || !strings.Contains(err.Error(), "admin") {
		t.Fatalf("an admin token for an agent: err=%v, want a refusal naming the admin role", err)
	}
	token := strings.TrimSpace(env.mustRun("token", "create", "--workspace", "admin-token-desk", "--email", "admin@admintokendesk.example.com", "--scope", "admin", "--label", "Ops"))
	me := tokenAPICall(t, env, http.MethodGet, "/api/v1/auth/me", token, "")
	if me.Code != http.StatusOK || !strings.Contains(me.Body.String(), `"role":"admin"`) || !strings.Contains(me.Body.String(), `"scope":"admin"`) {
		t.Fatalf("admin token identity: status=%d body=%s, want the admin role and scope", me.Code, me.Body.String())
	}
}

func TestTokenCreateRefusesBadInputAndCreatesNothing(t *testing.T) {
	env := newTrialEnv(t, "Token Refusal")
	cases := map[string]struct {
		args []string
		want string
	}{
		"unknown scope":     {[]string{"--scope", "owner", "--label", "x"}, "read, draft, send, admin"},
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
	if _, err := env.run("token"); err == nil || !strings.Contains(err.Error(), "usage: collie token create") {
		t.Fatalf("token without a subcommand: %v", err)
	}
	var count int
	if err := env.pool.QueryRow(context.Background(), `SELECT count(*) FROM sessions WHERE scope IS NOT NULL`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("refused commands created %d tokens (err=%v)", count, err)
	}
}
