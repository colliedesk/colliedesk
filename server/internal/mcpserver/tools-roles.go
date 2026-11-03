package mcpserver

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

type restRole struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	System      *string  `json:"system"`
	Permissions []string `json:"permissions"`
	Editable    bool     `json:"editable"`
	Deletable   bool     `json:"deletable"`
	Assignable  bool     `json:"assignable"`
	Members     []struct {
		Name   string `json:"name"`
		Active bool   `json:"active"`
	} `json:"members"`
	Invites int `json:"invites"`
	Tokens  []struct {
		Label string `json:"label"`
		Owner string `json:"owner"`
	} `json:"tokens"`
	UpdatedBy *string `json:"updated_by"`
	UpdatedAt string  `json:"updated_at"`
	DeletedAt *string `json:"deleted_at"`
}

// roleView is a role as the tools show it: people and tokens by name, never only ids.
func roleView(role restRole) map[string]any {
	members := make([]string, len(role.Members))
	for index, member := range role.Members {
		members[index] = member.Name
		if !member.Active {
			members[index] += " (deactivated)"
		}
	}
	tokens := make([]string, len(role.Tokens))
	for index, token := range role.Tokens {
		tokens[index] = token.Label + " (" + token.Owner + ")"
	}
	permissions := role.Permissions
	if permissions == nil {
		permissions = []string{}
	}
	return map[string]any{"id": role.ID, "name": role.Name, "description": role.Description, "system": role.System, "permissions": permissions,
		"editable": role.Editable, "deletable": role.Deletable, "assignable": role.Assignable, "members": members, "invites": role.Invites,
		"tokens": tokens, "updated_by": role.UpdatedBy, "updated_at": role.UpdatedAt, "deleted_at": role.DeletedAt}
}

// rolePath is the address of one role; the server takes its id or its name.
func rolePath(c *call, key, rest string) string {
	key = strings.TrimSpace(key)
	c.notFound = "No role " + key + " in this workspace. List them with colliedesk_roles_list (deleted true for deleted ones)."
	return "/api/v1/roles/" + url.PathEscape(key) + rest
}

func (c *call) role(key string) (restRole, error) {
	var role restRole
	err := c.get(rolePath(c, key, ""), nil, &role)
	return role, err
}

func roleAnswer(c *call, method, path string, body any) (any, error) {
	var role restRole
	if err := c.send(method, path, body, &role); err != nil {
		return nil, err
	}
	return map[string]any{"role": roleView(role)}, nil
}

func rolesList(c *call, arguments map[string]any) (any, error) {
	query := url.Values{}
	if deleted, _ := arguments["deleted"].(bool); deleted {
		query.Set("deleted", "true")
	}
	var page struct {
		Items []restRole `json:"items"`
	}
	if err := c.get("/api/v1/roles", query, &page); err != nil {
		return nil, err
	}
	roles := make([]map[string]any, len(page.Items))
	for index, role := range page.Items {
		roles[index] = roleView(role)
	}
	return map[string]any{"roles": roles}, nil
}

// union adds extra to base, keeping each permission once, sorted. It is never nil: an empty list goes
// out as [], which the server reads as "no permissions", where null would mean "not sent".
func union(base, extra []string) []string {
	out := append([]string{}, base...)
	for _, item := range extra {
		if !slices.Contains(out, item) {
			out = append(out, item)
		}
	}
	slices.Sort(out)
	return out
}

func roleCreate(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		Name        string   `json:"name"`
		Description *string  `json:"description"`
		StartFrom   string   `json:"start_from"`
		Permissions []string `json:"permissions"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	permissions := union(nil, args.Permissions)
	if args.StartFrom != "" {
		from, err := c.role(args.StartFrom)
		if err != nil {
			return nil, err
		}
		permissions = union(from.Permissions, args.Permissions)
	}
	body := map[string]any{"name": strings.TrimSpace(args.Name), "permissions": permissions}
	if args.Description != nil {
		body["description"] = *args.Description
	}
	c.notFound = ""
	return roleAnswer(c, http.MethodPost, "/api/v1/roles", body)
}

func roleUpdate(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		Role        string    `json:"role"`
		Name        *string   `json:"name"`
		Description *string   `json:"description"`
		Permissions *[]string `json:"permissions"`
		Add         []string  `json:"add_permissions"`
		Remove      []string  `json:"remove_permissions"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	if args.Permissions != nil && (len(args.Add) > 0 || len(args.Remove) > 0) {
		return nil, &toolError{"Give either the whole list in permissions, or add_permissions and remove_permissions, not both."}
	}
	body := map[string]any{}
	if args.Name != nil {
		body["name"] = strings.TrimSpace(*args.Name)
	}
	if args.Description != nil {
		body["description"] = *args.Description
	}
	if args.Permissions != nil {
		body["permissions"] = union(nil, *args.Permissions)
	}
	if len(args.Add) > 0 || len(args.Remove) > 0 {
		// Read the role and send what it had with the change, saying which version it started from:
		// a person who changed it meanwhile is not overwritten (role_changed).
		current, err := c.role(args.Role)
		if err != nil {
			return nil, err
		}
		permissions := union(current.Permissions, args.Add)
		permissions = slices.DeleteFunc(permissions, func(item string) bool { return slices.Contains(args.Remove, item) })
		body["permissions"] = permissions
		body["expected_updated_at"] = current.UpdatedAt
	}
	if len(body) == 0 {
		return nil, &toolError{"Nothing to change. Give name, description, permissions, add_permissions or remove_permissions."}
	}
	return roleAnswer(c, http.MethodPatch, rolePath(c, args.Role, ""), body)
}

func roleDelete(c *call, arguments map[string]any) (any, error) {
	key, _ := arguments["role"].(string)
	current, err := c.role(key)
	if err != nil {
		return nil, err
	}
	if _, err := passOn(c, http.MethodDelete, rolePath(c, current.ID, ""), nil); err != nil {
		return nil, err
	}
	return map[string]any{"deleted": current.Name, "next": "Restore it with colliedesk_role_restore (role " + current.Name + ")."}, nil
}

func roleReset(c *call, arguments map[string]any) (any, error) {
	key, _ := arguments["role"].(string)
	if key == "" {
		key = "Agent"
	}
	return roleAnswer(c, http.MethodPost, rolePath(c, key, "/reset"), nil)
}

func roleRestore(c *call, arguments map[string]any) (any, error) {
	key, _ := arguments["role"].(string)
	return roleAnswer(c, http.MethodPost, rolePath(c, key, "/restore"), nil)
}

// foldIn reports whether names holds name, without regard to case, as the server matches role names.
func foldIn(names []string, name string) bool {
	return slices.ContainsFunc(names, func(item string) bool { return strings.EqualFold(strings.TrimSpace(item), strings.TrimSpace(name)) })
}

func memberRolesSet(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[struct {
		Member string   `json:"member"`
		Roles  []string `json:"roles"`
		Add    []string `json:"add_roles"`
		Remove []string `json:"remove_roles"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	if len(args.Roles) > 0 && (len(args.Add) > 0 || len(args.Remove) > 0) {
		return nil, &toolError{"Give either the whole list in roles, or add_roles and remove_roles, not both."}
	}
	member, err := c.memberByEmailOrID(args.Member)
	if err != nil {
		return nil, err
	}
	roles := args.Roles
	if len(roles) == 0 {
		for _, held := range roleNames(member.Roles) {
			if !foldIn(args.Remove, held) {
				roles = append(roles, held)
			}
		}
		for _, name := range args.Add {
			if !foldIn(roles, name) {
				roles = append(roles, name)
			}
		}
		for _, name := range args.Remove {
			if !foldIn(roleNames(member.Roles), name) {
				return nil, &toolError{fmt.Sprintf("%s does not hold %s; they hold %s.", memberName(member), name, strings.Join(roleNames(member.Roles), ", "))}
			}
		}
	}
	if len(roles) == 0 {
		return nil, &toolError{"Everyone keeps at least one role, so " + memberName(member) + " cannot lose every role. Give the role they should hold instead."}
	}
	c.notFound = "No team member " + args.Member + " in this workspace. List them with colliedesk_agents_list."
	var updated restMember
	if err := c.send(http.MethodPut, "/api/v1/agents/"+url.PathEscape(member.ID)+"/roles", map[string]any{"roles": roles}, &updated); err != nil {
		return nil, err
	}
	return map[string]any{"member": memberView(updated)}, nil
}

// memberName is a team member's display name, or their email when they have none.
func memberName(member restMember) string {
	if member.DisplayName != "" {
		return member.DisplayName
	}
	return member.Email
}
