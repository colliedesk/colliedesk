package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

type attributedMessage struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Body       string `json:"body"`
	AgentToken *struct {
		ID        string `json:"id"`
		Label     string `json:"label"`
		Scope     string `json:"scope"`
		AgentName string `json:"agent_name"`
	} `json:"agent_token"`
}

func listAttributed(t *testing.T, api http.Handler, token, conversation string) map[string]attributedMessage {
	t.Helper()
	var list struct {
		Items []attributedMessage `json:"items"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/conversations/"+conversation+"/messages", nil, token, nil), &list)
	byBody := map[string]attributedMessage{}
	for _, item := range list.Items {
		byBody[item.Body] = item
	}
	return byBody
}

func TestMessageNamesTheTokenThatWroteItAndNobodyElse(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Attribution Desk", "admin@attribution.example.com")
	if _, err := pool.Exec(context.Background(), `UPDATE agents SET display_name='Ada Admin' WHERE email='admin@attribution.example.com'`); err != nil {
		t.Fatal(err)
	}
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Cy", "email": "cy@attribution.example.com"})
	conversation := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Who wrote this"})
	path := "/api/v1/conversations/" + conversation + "/messages"

	drafter := issueToken(t, api, admin, "Claude Code", "draft")
	sender := issueToken(t, api, admin, "Support bot", "send")
	post := func(token, kind, body string) {
		t.Helper()
		if response := request(t, api, http.MethodPost, path, map[string]any{"kind": kind, "body": body}, token, nil); response.Code != http.StatusCreated {
			t.Fatalf("post %q status=%d body=%s", body, response.Code, response.Body.String())
		}
	}
	post(drafter.Token, "note", "note by the draft token")
	post(sender.Token, "outgoing", "reply by the send token")
	post(admin, "note", "note by the person")

	got := listAttributed(t, api, admin, conversation)
	note, reply, person := got["note by the draft token"], got["reply by the send token"], got["note by the person"]
	if note.AgentToken == nil || note.AgentToken.ID != drafter.Session.ID || note.AgentToken.Label != "Claude Code" ||
		note.AgentToken.Scope != "draft" || note.AgentToken.AgentName != "Ada Admin" {
		t.Fatalf("note by a draft token: agent_token=%+v, want id %s, label Claude Code, scope draft, agent Ada Admin", note.AgentToken, drafter.Session.ID)
	}
	if reply.AgentToken == nil || reply.AgentToken.ID != sender.Session.ID || reply.AgentToken.Label != "Support bot" || reply.AgentToken.Scope != "send" {
		t.Fatalf("reply by a send token: agent_token=%+v, want id %s, label Support bot, scope send", reply.AgentToken, sender.Session.ID)
	}
	if person.AgentToken != nil {
		t.Fatalf("a person's note names a token: %+v", person.AgentToken)
	}

	if response := request(t, api, http.MethodDelete, "/api/v1/auth/sessions/"+sender.Session.ID, nil, admin, nil); response.Code != http.StatusNoContent {
		t.Fatalf("revoke status=%d", response.Code)
	}
	if again := listAttributed(t, api, admin, conversation)["reply by the send token"]; again.AgentToken == nil || again.AgentToken.Label != "Support bot" {
		t.Fatalf("a revoked token must stay named on what it wrote: %+v", again.AgentToken)
	}
}

func TestMessageResponseToTheTokenAlreadyNamesIt(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Attribution Echo", "admin@echo.example.com")
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Cy", "email": "cy@echo.example.com"})
	conversation := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Echo"})
	token := issueToken(t, api, admin, "Echo agent", "draft")
	response := request(t, api, http.MethodPost, "/api/v1/conversations/"+conversation+"/messages", map[string]any{"kind": "note", "body": "hi"}, token.Token, nil)
	var created attributedMessage
	decode(t, response, &created)
	if created.AgentToken == nil || created.AgentToken.Label != "Echo agent" {
		t.Fatalf("201 body agent_token=%+v, want the label Echo agent", created.AgentToken)
	}
}

func TestAnotherWorkspaceNeverSeesTheTokensOfThisOne(t *testing.T) {
	api, service := testAPI(t)
	adminA := createSession(t, api, service, "Attribution Alpha", "admin@attr-alpha.example.com")
	adminB := createSession(t, api, service, "Attribution Beta", "admin@attr-beta.example.com")
	contactA := create(t, api, adminA, "/api/v1/contacts", map[string]any{"name": "Cy", "email": "cy@attr-alpha.example.com"})
	conversationA := create(t, api, adminA, "/api/v1/conversations", map[string]any{"contact_id": contactA, "subject": "Alpha"})
	tokenA := issueToken(t, api, adminA, "Alpha only label", "draft")
	request(t, api, http.MethodPost, "/api/v1/conversations/"+conversationA+"/messages", map[string]any{"kind": "note", "body": "alpha"}, tokenA.Token, nil)

	response := request(t, api, http.MethodGet, "/api/v1/conversations/"+conversationA+"/messages", nil, adminB, nil)
	if response.Code != http.StatusNotFound || strings.Contains(response.Body.String(), "Alpha only label") {
		t.Fatalf("workspace B reading A's timeline: status=%d body=%s, want 404 without the label", response.Code, response.Body.String())
	}
}
