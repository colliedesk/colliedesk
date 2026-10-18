package mcpserver_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestDisplayNameToolsSetClearAndStayInTheirWorkspace(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Name Tools Desk", "admin@name-tools.example.com")
	other := e.newWorkspace("Other Name Desk", "admin@other-name.example.com")
	joinTeam(t, e, w, "agent@name-tools.example.com")
	joinTeam(t, e, other, "agent@other-name.example.com")

	draft := e.connect(w.draft, "")
	if mine := e.ok(draft, "colliedesk_my_display_name_set", map[string]any{"display_name": "  Ada Admin "}); mine["your_display_name"] != "Ada Admin" {
		t.Fatalf("set my display name = %v", mine)
	}
	if who := e.ok(draft, "colliedesk_whoami", nil); at(t, who, "agent", "name") != "Ada Admin" {
		t.Fatalf("whoami after the rename = %v", who)
	}
	if mine := e.ok(draft, "colliedesk_my_display_name_set", map[string]any{"display_name": ""}); mine["your_display_name"] != "" {
		t.Fatalf("clear my display name = %v", mine)
	}
	if who := e.ok(draft, "colliedesk_whoami", nil); at(t, who, "agent", "name") != "admin@name-tools.example.com" {
		t.Fatalf("whoami after clearing shows %v, want the email", who)
	}
	if message := e.fails(draft, "colliedesk_my_display_name_set", map[string]any{"display_name": "Line\nbreak"}); !strings.Contains(message, "80 characters") {
		t.Errorf("a control character: %q", message)
	}
	requestedForTier(t, e, w.read, "colliedesk_my_display_name_set", map[string]any{"display_name": "Asked"})

	admin := e.connect(w.adminTier, "")
	renamed := e.ok(admin, "colliedesk_team_member_update", map[string]any{"member": "agent@name-tools.example.com", "display_name": "Sam Support"})
	if at(t, renamed, "member", "display_name") != "Sam Support" || at(t, renamed, "member", "role") != "agent" {
		t.Fatalf("admin rename = %v", renamed)
	}
	_, team := e.rest(http.MethodGet, "/api/v1/agents", nil, w.admin)
	if !strings.Contains(stringify(team), "Sam Support") {
		t.Fatalf("team list after the rename = %v", team)
	}
	if message := e.fails(admin, "colliedesk_team_member_update", map[string]any{"member": "agent@other-name.example.com", "display_name": "Taken"}); !strings.Contains(message, "No ") {
		t.Errorf("renaming a member of another workspace: %q", message)
	}
	_, theirs := e.rest(http.MethodGet, "/api/v1/agents", nil, other.admin)
	if strings.Contains(stringify(theirs), "Taken") {
		t.Fatalf("another workspace's member was renamed: %v", theirs)
	}
}
