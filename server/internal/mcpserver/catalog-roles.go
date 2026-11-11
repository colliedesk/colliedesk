package mcpserver

// The tools below manage roles: named sets of permissions a team member holds. A person holds every
// permission of each of their roles; an agent token follows one role of its person or holds a list
// picked when it was made, never more than its person holds. Admin always holds every permission and
// cannot change; Agent can change but not go. Nobody gives, makes or changes a role beyond what they
// hold, and nobody changes someone who holds more than they do.

import (
	"encoding/json"
	"slices"

	"github.com/colliedesk/colliedesk/server/internal/auth"
)

const (
	roleKeyProperty = `"role":{"type":"string","minLength":1,"maxLength":60,"description":"The role's name (any case) or id, from colliedesk_roles_list."}`

	roleSchema = `{"type":"object","required":["id","name","description","system","permissions","editable","deletable","assignable","members","invites","tokens"],"properties":{` +
		`"id":{"type":"string"},"name":{"type":"string"},"description":{"type":"string"},` +
		`"system":{"type":["string","null"],"description":"admin or agent for the two built-in roles; null for one the workspace made."},` +
		`"permissions":{"type":"array","items":{"type":"string"},"description":"resource:action; Admin lists every permission."},` +
		`"editable":{"type":"boolean","description":"You may change it: not Admin, and you hold every permission it has."},` +
		`"deletable":{"type":"boolean"},"assignable":{"type":"boolean","description":"You may give it to someone."},` +
		`"members":{"type":"array","items":{"type":"string"},"description":"Names of the team members who hold it (deactivated ones end with (deactivated))."},` +
		`"invites":{"type":"integer","description":"Pending invites that would give it."},` +
		`"tokens":{"type":"array","items":{"type":"string"},"description":"Live agent tokens that follow it, as label (owner)."},` +
		`"updated_by":{"type":["string","null"]},"updated_at":{"type":"string"},"deleted_at":{"type":["string","null"]}}}`

	roleOutput = `{"type":"object","required":["role"],"properties":{"role":` + roleSchema + `}}`
)

// permissionList is the JSON schema of a list of permissions, built from auth.TokenPermissions as data
// so a permission a release adds is offered at once.
func permissionList(description string) string {
	names := []string{}
	for _, item := range auth.TokenPermissions() {
		names = append(names, item.Name)
	}
	slices.Sort(names)
	encoded, err := json.Marshal(map[string]any{"type": "array", "maxItems": len(names), "uniqueItems": true,
		"items": map[string]any{"type": "string", "enum": names}, "description": description})
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func rolesListSpec() Spec {
	return Spec{
		Name:  "colliedesk_roles_list",
		Title: "List roles",
		Description: `List the workspace's roles, Admin and Agent first, then by name: each with its permissions, who holds it, the agent tokens that follow it, and whether you may ` +
			`change, delete or give it. With deleted true, only the deleted roles, to restore one with colliedesk_role_restore.`,
		REST:   []string{"GET /api/v1/roles"},
		Input:  `{"type":"object","additionalProperties":false,"properties":{"deleted":{"type":"boolean","default":false}}}`,
		Output: `{"type":"object","required":["roles"],"properties":{"roles":{"type":"array","items":` + roleSchema + `}}}`,
		run:    rolesList,
	}
}

func roleCreateSpec() Spec {
	return Spec{
		Name:  "colliedesk_role_create",
		Title: "Make a role",
		Description: `Make a role: a name unique in the workspace, an optional description, and its permissions (resource:action, each one you hold yourself). ` +
			`Start from another role with start_from (its permissions are copied, then permissions are added). Give it to people with colliedesk_member_roles_set; ` +
			`delete it with colliedesk_role_delete while nobody holds it.`,
		REST: []string{"POST /api/v1/roles", "GET /api/v1/roles/{id}"},
		Input: `{"type":"object","additionalProperties":false,"required":["name"],"properties":{"name":{"type":"string","minLength":1,"maxLength":60},` +
			`"description":{"type":"string","maxLength":300},"start_from":{"type":"string","minLength":1,"maxLength":60,"description":"Name or id of a role whose permissions to copy."},` +
			`"permissions":` + permissionList("Permissions of the role, on top of those of start_from.") + `}}`,
		Output: roleOutput,
		run:    roleCreate,
	}
}

func roleUpdateSpec() Spec {
	return Spec{
		Name:  "colliedesk_role_update",
		Title: "Change a role",
		Description: `Rename a role, change its description, or change its permissions: give the whole list in permissions, or add_permissions and remove_permissions to change ` +
			`only those. Everyone who holds it, and every agent token that follows it, gets the change at once: the answer names them, so tell the person you work for. ` +
			`You can only change a role whose every permission you hold, and only give permissions you hold. Admin cannot change; Agent goes back to its first list with colliedesk_role_reset.`,
		Idempotent: true,
		REST:       []string{"PATCH /api/v1/roles/{id}", "GET /api/v1/roles/{id}"},
		Input: `{"type":"object","additionalProperties":false,"required":["role"],"minProperties":2,"properties":{` + roleKeyProperty + `,` +
			`"name":{"type":"string","minLength":1,"maxLength":60},"description":{"type":"string","maxLength":300},` +
			`"permissions":` + permissionList("The whole new list; leave out to keep it.") + `,` +
			`"add_permissions":` + permissionList("Permissions to add to the list it has.") + `,` +
			`"remove_permissions":` + permissionList("Permissions to take off the list it has.") + `}}`,
		Output: roleOutput,
		run:    roleUpdate,
	}
}

func roleDeleteSpec() Spec {
	return Spec{
		Name:  "colliedesk_role_delete",
		Title: "Delete a role",
		Description: `Delete a role nobody uses: no team member holds it, no pending invite gives it and no live agent token follows it (otherwise refused with what still uses it). ` +
			`Admin and Agent cannot go. Bring it back with colliedesk_role_restore.`,
		REST:   []string{"DELETE /api/v1/roles/{id}", "GET /api/v1/roles/{id}"},
		Input:  `{"type":"object","additionalProperties":false,"required":["role"],"properties":{` + roleKeyProperty + `}}`,
		Output: `{"type":"object","required":["deleted","next"],"properties":{"deleted":{"type":"string","description":"Name of the deleted role."},"next":{"type":"string"}}}`,
		run:    roleDelete,
	}
}

func roleResetSpec() Spec {
	return Spec{
		Name:  "colliedesk_role_reset",
		Title: "Put the Agent role back to its first permissions",
		Description: `Put the built-in Agent role back to the permissions it starts with. Everyone who holds it and the agent tokens that follow it get the change at once. ` +
			`Only Agent can be reset.`,
		Idempotent: true,
		REST:       []string{"POST /api/v1/roles/{id}/reset"},
		Input:      `{"type":"object","additionalProperties":false,"properties":{"role":{"type":"string","minLength":1,"maxLength":60,"default":"Agent","description":"The Agent role, by its name or id."}}}`,
		Output:     roleOutput,
		run:        roleReset,
	}
}

func roleRestoreSpec() Spec {
	return Spec{
		Name:        "colliedesk_role_restore",
		Title:       "Restore a deleted role",
		Description: `Bring back a deleted role with its permissions (colliedesk_roles_list with deleted true lists them). Refused when a live role took its name meanwhile: rename that one first.`,
		REST:        []string{"POST /api/v1/roles/{id}/restore"},
		Input:       `{"type":"object","additionalProperties":false,"required":["role"],"properties":{` + roleKeyProperty + `}}`,
		Output:      roleOutput,
		run:         roleRestore,
	}
}

func memberRolesSetSpec() Spec {
	return Spec{
		Name:  "colliedesk_member_roles_set",
		Title: "Set a team member's roles",
		Description: `Set which roles a team member holds, by role name: the whole list in roles, or add_roles and remove_roles to change only those. Everyone keeps at least one role. ` +
			`You may give or take only roles whose every permission you hold, and only change someone who holds no permission you lack; the last active member with Admin keeps it. ` +
			`Agent tokens of the member that follow a role taken away stop at once, and start again when it is given back.`,
		Idempotent: true,
		REST:       []string{"PUT /api/v1/agents/{id}/roles", "GET /api/v1/agents"},
		Input: `{"type":"object","additionalProperties":false,"required":["member"],"minProperties":2,"properties":{` + memberProperty + `,` +
			`"roles":{"type":"array","minItems":1,"maxItems":50,"items":{"type":"string","minLength":1,"maxLength":60},"description":"The whole list of role names they should hold."},` +
			`"add_roles":{"type":"array","minItems":1,"maxItems":50,"items":{"type":"string","minLength":1,"maxLength":60},"description":"Role names to give, keeping the others."},` +
			`"remove_roles":{"type":"array","minItems":1,"maxItems":50,"items":{"type":"string","minLength":1,"maxLength":60},"description":"Role names to take away, keeping the others."}}}`,
		Output: `{"type":"object","required":["member"],"properties":{"member":` + memberSchema + `}}`,
		run:    memberRolesSet,
	}
}
