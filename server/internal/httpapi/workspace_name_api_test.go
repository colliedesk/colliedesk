package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

func workspaceName(t *testing.T, api http.Handler, token string) string {
	t.Helper()
	response := request(t, api, http.MethodGet, "/api/v1/auth/me", nil, token, nil)
	var me struct {
		Workspace struct {
			Name string `json:"name"`
		} `json:"workspace"`
	}
	decode(t, response, &me)
	if response.Code != http.StatusOK {
		t.Fatalf("GET me status=%d body=%s", response.Code, response.Body.String())
	}
	return me.Workspace.Name
}

func renameWorkspace(t *testing.T, api http.Handler, token string, body map[string]any) (int, string) {
	t.Helper()
	response := request(t, api, http.MethodPatch, "/api/v1/workspace/name", body, token, nil)
	return response.Code, response.Body.String()
}

func TestAnAdminRenamesTheWorkspaceAndEveryoneSeesTheNewName(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "acceptance-20261015-b", "admin@rename-desk.example.com")
	agent := joinAsAgent(t, api, admin, "agent@rename-desk.example.com")

	status, body := renameWorkspace(t, api, admin, map[string]any{"name": "  Rename Desk "})
	if status != http.StatusOK || !strings.Contains(body, `"name":"Rename Desk"`) || !strings.Contains(body, `"slug":"acceptance-20261015-b"`) {
		t.Fatalf("rename status=%d body=%s", status, body)
	}
	for _, token := range []string{admin, agent} {
		if got := workspaceName(t, api, token); got != "Rename Desk" {
			t.Fatalf("workspace name read back = %q", got)
		}
	}

	invalid := []any{"", "   ", strings.Repeat("x", 101), "Acme\r\nDesk", "Tab\tname", "Spoof ‮gnp", 42, nil}
	for _, value := range invalid {
		status, body := renameWorkspace(t, api, admin, map[string]any{"name": value})
		if status != http.StatusBadRequest || !strings.Contains(body, `"code":"invalid_request"`) || !strings.Contains(body, "name") {
			t.Errorf("name %q status=%d body=%s, want 400 invalid_request", value, status, body)
		}
	}
	if status, body := renameWorkspace(t, api, admin, map[string]any{}); status != http.StatusBadRequest {
		t.Errorf("rename without a name status=%d body=%s, want 400", status, body)
	}
	if status, _ := renameWorkspace(t, api, agent, map[string]any{"name": "Agent Pick"}); status != http.StatusForbidden {
		t.Fatalf("agent rename status=%d, want 403", status)
	}
	if got := workspaceName(t, api, admin); got != "Rename Desk" {
		t.Fatalf("a refused rename changed the name to %q", got)
	}
}

func TestRenamingTheWorkspaceKeepsTheEmailSenderName(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Before Rename", "admin@keep-sender.example.com")
	if status, body := putChannel(t, api, admin, map[string]any{"support_address": "help@keep-sender.example.com"}); status != http.StatusOK {
		t.Fatalf("connect status=%d body=%s", status, body)
	}
	if status, body := renameWorkspace(t, api, admin, map[string]any{"name": "After Rename"}); status != http.StatusOK {
		t.Fatalf("rename status=%d body=%s", status, body)
	}
	if got := senderName(t, api, admin); got != "Before Rename" {
		t.Fatalf("the sender name followed the workspace rename: %q", got)
	}
}

func TestRenamingAWorkspaceLeavesOtherWorkspacesAlone(t *testing.T) {
	api, service := testAPI(t)
	adminA := createSession(t, api, service, "Rename Scope A", "admin@rename-scope-a.example.com")
	adminB := createSession(t, api, service, "Rename Scope B", "admin@rename-scope-b.example.com")
	if status, body := renameWorkspace(t, api, adminB, map[string]any{"name": "B Renamed"}); status != http.StatusOK {
		t.Fatalf("B rename status=%d body=%s", status, body)
	}
	if got := workspaceName(t, api, adminA); got != "Rename Scope A" {
		t.Fatalf("workspace A name = %q after B renamed itself", got)
	}
	if got := workspaceName(t, api, adminB); got != "B Renamed" {
		t.Fatalf("workspace B name = %q", got)
	}
}
