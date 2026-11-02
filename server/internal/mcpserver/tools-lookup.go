package mcpserver

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// permissionsMeaning tells a model what its permissions mean; legacyMeaning is added for a token made
// before roles, which lost the actions that used to wait for a person.
const (
	permissionsMeaning = "You act as %s with these permissions: what this token was given, limited to what %s holds now. " +
		"A call outside them is refused with permission_missing, which names the permission and how to get it. Nothing is queued: a call you may make runs at once."
	legacyMeaning = " This token was made before roles, so some actions it could once ask a person for are refused. " +
		"A new token from Settings → External AI agents acts at once on everything it is given."
)

type whoamiToken struct {
	Kind       string   `json:"kind"`
	Role       *string  `json:"role"`
	Given      []string `json:"given"`
	LegacyTier *string  `json:"legacy_tier"`
}

type whoamiOut struct {
	Workspace struct {
		Name     string `json:"name"`
		Slug     string `json:"slug"`
		Timezone string `json:"timezone"`
	} `json:"workspace"`
	Agent struct {
		Name  string   `json:"name"`
		Email string   `json:"email"`
		Roles []string `json:"roles"`
	} `json:"agent"`
	Token              *whoamiToken `json:"token"`
	Permissions        []string     `json:"permissions"`
	PermissionsMeaning string       `json:"permissions_meaning"`
	WorkspaceLocked    bool         `json:"workspace_locked"`
	ToolsYouCanUse     []string     `json:"tools_you_can_use"`
}

func whoami(c *call, _ map[string]any) (any, error) {
	me, err := c.me()
	if err != nil {
		return nil, err
	}
	out := whoamiOut{WorkspaceLocked: me.Plan != nil && me.Plan.Locked, Permissions: me.Permissions}
	if out.Permissions == nil {
		out.Permissions = []string{}
	}
	out.Workspace.Name, out.Workspace.Slug, out.Workspace.Timezone = me.Workspace.Name, me.Workspace.Slug, me.Workspace.Timezone
	name := agentName(me.Agent)
	out.Agent.Name, out.Agent.Email, out.Agent.Roles = name, me.Agent.Email, roleNames(me.Agent.Roles)
	out.PermissionsMeaning = fmt.Sprintf(permissionsMeaning, name, name)
	if grant := me.Token; grant != nil {
		out.Token = &whoamiToken{Kind: grant.Kind, Given: grant.Permissions, LegacyTier: grant.LegacyTier}
		if out.Token.Given == nil {
			out.Token.Given = []string{}
		}
		if grant.Role != nil {
			out.Token.Role = &grant.Role.Name
		}
		if grant.Kind == "legacy" {
			out.PermissionsMeaning += legacyMeaning
		}
	}
	out.ToolsYouCanUse = []string{}
	for _, spec := range Catalog() {
		if toolOpen(spec, out.Permissions) {
			out.ToolsYouCanUse = append(out.ToolsYouCanUse, spec.Name)
		}
	}
	return out, nil
}

// roleNames are the names of roles, in the order the server gives them (Admin, Agent, then by name).
func roleNames(roles []restRoleRef) []string {
	names := make([]string, len(roles))
	for index, role := range roles {
		names[index] = role.Name
	}
	return names
}

type contactsArgs struct {
	Query  string `json:"query"`
	Limit  int    `json:"limit"`
	Detail string `json:"detail"`
}

type contactOutRow struct {
	ID                       string     `json:"id"`
	CreatedAt                string     `json:"created_at,omitempty"`
	UntrustedCustomerContent contactOut `json:"untrusted_customer_content"`
}

type contactsOut struct {
	Contacts  []contactOutRow `json:"contacts"`
	Truncated bool            `json:"truncated"`
}

func contactsFind(c *call, arguments map[string]any) (any, error) {
	args, err := decodeArguments[contactsArgs](arguments)
	if err != nil {
		return nil, err
	}
	if args.Limit == 0 {
		args.Limit = 10
	}
	if len(strings.TrimSpace(args.Query)) < 2 {
		return nil, &toolError{"The query needs at least 2 characters of a name or an email address."}
	}
	var found struct {
		Items   []restContact `json:"items"`
		HasMore bool          `json:"has_more"`
	}
	query := url.Values{"search": {strings.TrimSpace(args.Query)}, "limit": {strconv.Itoa(args.Limit)}}
	if err := c.get("/api/v1/contacts", query, &found); err != nil {
		return nil, err
	}
	out := contactsOut{Contacts: make([]contactOutRow, 0, len(found.Items)), Truncated: found.HasMore}
	for _, contact := range found.Items {
		row := contactOutRow{ID: contact.ID, UntrustedCustomerContent: contactOut{Name: contact.Name, Email: contact.Email}}
		if args.Detail == "full" {
			row.CreatedAt = utc(contact.CreatedAt)
			row.UntrustedCustomerContent.Organization = contact.Organization
		}
		out.Contacts = append(out.Contacts, row)
	}
	return out, nil
}

func tagsList(c *call, _ map[string]any) (any, error) {
	tags, err := c.tagList()
	if err != nil {
		return nil, err
	}
	return map[string]any{"tags": tags, "count": len(tags)}, nil
}

type agentRow struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Email string   `json:"email"`
	Roles []string `json:"roles"`
}

func agentsList(c *call, _ map[string]any) (any, error) {
	agents, err := c.agentList()
	if err != nil {
		return nil, err
	}
	rows := make([]agentRow, len(agents))
	for index, agent := range agents {
		rows[index] = agentRow{ID: agent.ID, Name: agentName(agent), Email: agent.Email, Roles: roleNames(agent.Roles)}
	}
	return map[string]any{"agents": rows, "count": len(rows)}, nil
}
