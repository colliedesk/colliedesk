package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
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
	// Agent tokens and their sessions are on Settings → AI & agents; Settings → Sessions lists devices.
	if !strings.Contains(hint.String(), "Revoke it under Settings → AI & agents") {
		t.Fatalf("hint does not say where the token is revoked: %q", hint.String())
	}

	me := tokenAPICall(t, env, http.MethodGet, "/api/v1/auth/me", token, "")
	var identity struct {
		Agent struct {
			Email string `json:"email"`
		} `json:"agent"`
		Token struct {
			Kind        string   `json:"kind"`
			Permissions []string `json:"permissions"`
		} `json:"token"`
	}
	if err := json.Unmarshal(me.Body.Bytes(), &identity); err != nil || me.Code != http.StatusOK ||
		identity.Agent.Email != "admin@tokendesk.example.com" || identity.Token.Kind != "custom" || !slices.Contains(identity.Token.Permissions, "tags:create") ||
		slices.Contains(identity.Token.Permissions, "messages:send") {
		t.Fatalf("a --scope draft token must hold the old draft list of permissions: status=%d body=%s", me.Code, me.Body.String())
	}
	if !strings.Contains(hint.String(), "deprecated") {
		t.Fatalf("--scope must say it is deprecated: %q", hint.String())
	}
	var label string
	if err := env.pool.QueryRow(context.Background(), `SELECT label FROM sessions WHERE permissions IS NOT NULL`).Scan(&label); err != nil || label != "Claude Code" {
		t.Fatalf("stored token label = %q (err=%v), want Claude Code", label, err)
	}
	if created := tokenAPICall(t, env, http.MethodPost, "/api/v1/tags", token, `{"name":"x"}`); created.Code != http.StatusCreated {
		t.Fatalf("a draft token could not create a tag: status=%d body=%s", created.Code, created.Body.String())
	}
	// A permission the token was not given is refused and named; nothing waits for a person.
	if sent := tokenAPICall(t, env, http.MethodPost, "/api/v1/conversations", token, `{"contact_id":"00000000-0000-4000-8000-000000000000","subject":"x"}`); sent.Code != http.StatusForbidden || !strings.Contains(sent.Body.String(), `"messages:send"`) {
		t.Fatalf("a draft token's conversation must be refused naming the permission: status=%d body=%s", sent.Code, sent.Body.String())
	}
}

// A token follows a role by name, and only a role its person holds.
func TestTokenCreateFollowsARoleThePersonHolds(t *testing.T) {
	env := newTrialEnv(t, "Role Token Desk")
	var hint bytes.Buffer
	previous := stderr
	stderr = &hint
	t.Cleanup(func() { stderr = previous })
	token := strings.TrimSpace(env.mustRun("token", "create", "--workspace", "role-token-desk", "--email", "admin@roletokendesk.example.com", "--role", "admin", "--label", "Ops"))
	me := tokenAPICall(t, env, http.MethodGet, "/api/v1/auth/me", token, "")
	if me.Code != http.StatusOK || !strings.Contains(me.Body.String(), `"kind":"role"`) || !strings.Contains(me.Body.String(), `"name":"Admin"`) {
		t.Fatalf("role token identity: status=%d body=%s, want a token following Admin", me.Code, me.Body.String())
	}
	if !strings.Contains(hint.String(), "follows the role Admin") {
		t.Fatalf("hint does not name the role: %q", hint.String())
	}
	token = strings.TrimSpace(env.mustRun("token", "create", "--workspace", "role-token-desk", "--email", "admin@roletokendesk.example.com",
		"--permission", "conversations:read", "--permission", "messages:read", "--label", "Reader"))
	me = tokenAPICall(t, env, http.MethodGet, "/api/v1/auth/me", token, "")
	if me.Code != http.StatusOK || !strings.Contains(me.Body.String(), `"kind":"custom","role":null,"role_held":false,"permissions":["conversations:read","messages:read"]`) {
		t.Fatalf("permission token identity: status=%d body=%s", me.Code, me.Body.String())
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
	if _, err := env.run("token", "create", "--workspace", "admin-token-desk", "--email", "helper@admintokendesk.example.com", "--scope", "admin", "--label", "x"); err == nil || !strings.Contains(err.Error(), "helper@admintokendesk.example.com does not hold ") || !strings.Contains(err.Error(), "workspace:export") {
		t.Fatalf("an admin token for an agent: err=%v, want a refusal naming what the agent lacks", err)
	}
	token := strings.TrimSpace(env.mustRun("token", "create", "--workspace", "admin-token-desk", "--email", "admin@admintokendesk.example.com", "--scope", "admin", "--label", "Ops"))
	me := tokenAPICall(t, env, http.MethodGet, "/api/v1/auth/me", token, "")
	if me.Code != http.StatusOK || !strings.Contains(me.Body.String(), `"kind":"role"`) || !strings.Contains(me.Body.String(), `"name":"Admin"`) {
		t.Fatalf("admin token identity: status=%d body=%s, want a token following the Admin role", me.Code, me.Body.String())
	}
}

func TestTokenCreateRefusesBadInputAndCreatesNothing(t *testing.T) {
	env := newTrialEnv(t, "Token Refusal")
	cases := map[string]struct {
		args []string
		want string
	}{
		"unknown scope":      {[]string{"--scope", "owner", "--label", "x"}, "--role"},
		"missing access":     {[]string{"--label", "x"}, "--permission"},
		"unknown role":       {[]string{"--role", "Owner", "--label", "x"}, `has no role "owner"`},
		"unknown permission": {[]string{"--permission", "tickets:eat", "--label", "x"}, "tickets:eat"},
		"missing label":      {[]string{"--scope", "read"}, "label"},
		"unknown agent":      {[]string{"--email", "nobody@tokenrefusal.example.com", "--scope", "read", "--label", "x"}, "nobody@tokenrefusal.example.com"},
		"unknown workspace":  {[]string{"--workspace", "no-such-desk", "--scope", "read", "--label", "x"}, "no-such-desk"},
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
	if err := env.pool.QueryRow(context.Background(), `SELECT count(*) FROM sessions WHERE permissions IS NOT NULL OR role_id IS NOT NULL`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("refused commands created %d tokens (err=%v)", count, err)
	}
}
