package mcpserver_test

import (
	"slices"
	"strings"
	"testing"
)

// An agent with team:manage_roles runs roles in one call each, by names only: it makes a role, changes
// its permissions, gives it to a member by email, takes it back, deletes and restores it, and resets
// Agent. Every answer names people and roles, and a role of another workspace is never found.
func TestRoleToolsAcceptNames(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Role Tools Desk", "admin@role-tools.example.com")
	other := e.newWorkspace("Other Role Desk", "admin@other-role.example.com")
	joinTeam(t, e, w, "sam@role-tools.example.com")
	admin := e.connect(w.adminTier, "")

	e.refused(e.connect(w.send, ""), "colliedesk_role_create", map[string]any{"name": "KB editor", "permissions": []string{"kb_articles:read"}})

	created := e.ok(admin, "colliedesk_role_create", map[string]any{"name": "KB editor", "description": "Writes help articles",
		"permissions": []string{"kb_articles:update", "kb_articles:read"}})
	if at(t, created, "role", "name") != "KB editor" || !slices.Equal(permissionsOf(t, created), []string{"kb_articles:read", "kb_articles:update"}) {
		t.Fatalf("created = %v", created)
	}
	updated := e.ok(admin, "colliedesk_role_update", map[string]any{"role": "kb editor", "add_permissions": []string{"kb_articles:publish"}, "remove_permissions": []string{"kb_articles:update"}})
	if !slices.Equal(permissionsOf(t, updated), []string{"kb_articles:publish", "kb_articles:read"}) {
		t.Fatalf("updated = %v", updated)
	}

	given := e.ok(admin, "colliedesk_member_roles_set", map[string]any{"member": "sam@role-tools.example.com", "add_roles": []string{"KB editor"}})
	if roles := at(t, given, "member", "roles").([]any); !slices.Contains(roles, any("Agent")) || !slices.Contains(roles, any("KB editor")) {
		t.Fatalf("given = %v, want Agent and KB editor by name", given)
	}
	listed := stringify(e.ok(admin, "colliedesk_roles_list", nil))
	if !strings.Contains(listed, "sam@role-tools.example.com") {
		t.Errorf("roles_list = %s, want the member who holds KB editor by name", listed)
	}
	if message := e.fails(admin, "colliedesk_role_delete", map[string]any{"role": "KB editor"}); !strings.Contains(message, "refused") {
		t.Errorf("deleting a role someone holds: %q", message)
	}
	taken := e.ok(admin, "colliedesk_member_roles_set", map[string]any{"member": "sam@role-tools.example.com", "remove_roles": []string{"kb editor"}})
	if roles := at(t, taken, "member", "roles").([]any); !slices.Equal(roles, []any{"Agent"}) {
		t.Fatalf("taken = %v", taken)
	}
	if message := e.fails(admin, "colliedesk_member_roles_set", map[string]any{"member": "sam@role-tools.example.com", "remove_roles": []string{"Agent"}}); !strings.Contains(message, "at least one role") {
		t.Errorf("taking the last role: %q", message)
	}

	if deleted := e.ok(admin, "colliedesk_role_delete", map[string]any{"role": "KB editor"}); deleted["deleted"] != "KB editor" || !strings.Contains(stringify(deleted), "colliedesk_role_restore") {
		t.Fatalf("deleted = %v", deleted)
	}
	if gone := stringify(e.ok(admin, "colliedesk_roles_list", map[string]any{"deleted": true})); !strings.Contains(gone, "KB editor") {
		t.Errorf("deleted roles = %s", gone)
	}
	if restored := e.ok(admin, "colliedesk_role_restore", map[string]any{"role": "KB editor"}); at(t, restored, "role", "name") != "KB editor" {
		t.Fatalf("restored = %v", restored)
	}

	e.ok(admin, "colliedesk_role_update", map[string]any{"role": "Agent", "remove_permissions": []string{"tags:delete"}})
	reset := e.ok(admin, "colliedesk_role_reset", map[string]any{})
	if at(t, reset, "role", "name") != "Agent" || !slices.Contains(permissionsOf(t, reset), "tags:delete") {
		t.Fatalf("reset = %v", reset)
	}

	theirs := e.connect(other.adminTier, "")
	e.ok(theirs, "colliedesk_role_create", map[string]any{"name": "Night shift", "permissions": []string{"conversations:read"}})
	if message := e.fails(admin, "colliedesk_role_update", map[string]any{"role": "Night shift", "name": "Mine now"}); !strings.Contains(message, "No role Night shift") {
		t.Errorf("another workspace's role: %q", message)
	}
	if message := e.fails(admin, "colliedesk_member_roles_set", map[string]any{"member": "admin@other-role.example.com", "roles": []string{"Agent"}}); !strings.Contains(message, "No team member") {
		t.Errorf("another workspace's member: %q", message)
	}
}

func permissionsOf(t *testing.T, out map[string]any) []string {
	t.Helper()
	names := []string{}
	for _, name := range at(t, out, "role", "permissions").([]any) {
		names = append(names, name.(string))
	}
	slices.Sort(names)
	return names
}
