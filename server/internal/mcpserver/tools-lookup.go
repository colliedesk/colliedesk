package mcpserver

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

var tierMeaning = map[string]string{
	"read":  "You can read the inbox. You cannot change anything.",
	"draft": "You can read, change status, priority, assignee and tags, and add private notes. Nothing you do reaches a customer.",
	"send":  "You may do everything a team member can, including replying to customers by email. A reply cannot be called back: confirm it with the user first.",
	"custom": "This token holds its own list of permissions (permissions below): use only the tools listed in tools_you_can_use. " +
		"Anything else, and anything that waits for a person's approval, comes back as a request a person decides.",
	"admin": "You may also change workspace settings, agents, channels, revoke invites and agent tokens, as the admin who created this token, while that person is an admin. " +
		"Creating tokens or invites, any change to an admin or making one, signing out another person, export, billing, disconnecting email, a new inbound secret and the workspace address still wait for a person's approval.",
}

type whoamiOut struct {
	Workspace struct {
		Name     string `json:"name"`
		Slug     string `json:"slug"`
		Timezone string `json:"timezone"`
	} `json:"workspace"`
	Agent struct {
		Name  string `json:"name"`
		Email string `json:"email"`
		Role  string `json:"role"`
	} `json:"agent"`
	TokenTier       string   `json:"token_tier"`
	TierMeaning     string   `json:"tier_meaning"`
	Permissions     []string `json:"permissions"`
	WorkspaceLocked bool     `json:"workspace_locked"`
	ToolsYouCanUse  []string `json:"tools_you_can_use"`
}

func whoami(c *call, _ map[string]any) (any, error) {
	me, err := c.me()
	if err != nil {
		return nil, err
	}
	out := whoamiOut{WorkspaceLocked: me.Plan != nil && me.Plan.Locked}
	scope := ""
	if me.Scope != nil {
		scope = *me.Scope
		out.TokenTier = scope
	}
	out.TierMeaning = tierMeaning[out.TokenTier]
	out.Workspace.Name, out.Workspace.Slug, out.Workspace.Timezone = me.Workspace.Name, me.Workspace.Slug, me.Workspace.Timezone
	out.Agent.Name, out.Agent.Email, out.Agent.Role = agentName(me.Agent), me.Agent.Email, me.Agent.Role
	out.Permissions = me.Permissions
	if out.Permissions == nil {
		out.Permissions = []string{}
	}
	who := auth.Principal{Scope: scope, Permissions: me.Permissions}
	out.ToolsYouCanUse = []string{}
	for _, spec := range Catalog() {
		if toolOpen(spec, who) {
			out.ToolsYouCanUse = append(out.ToolsYouCanUse, spec.Name)
		}
	}
	return out, nil
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
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

func agentsList(c *call, _ map[string]any) (any, error) {
	agents, err := c.agentList()
	if err != nil {
		return nil, err
	}
	rows := make([]agentRow, len(agents))
	for index, agent := range agents {
		rows[index] = agentRow{ID: agent.ID, Name: agentName(agent), Email: agent.Email, Role: agent.Role}
	}
	return map[string]any{"agents": rows, "count": len(rows)}, nil
}

// routeAccess is how each route is classified for tokens (httpapi's table), read once.
var routeAccess = httpapi.TokenAccessTable()

// toolOpen reports whether a token can use the tool: a preset token by its tier, as the tool states
// it; a custom token when it holds the permission of every route the tool calls (for a route the
// handler decides by the body, the least it needs). A route that always waits for a person is open
// to call: the call becomes a request.
func toolOpen(spec Spec, who auth.Principal) bool {
	if spec.Tier == "any" {
		return true
	}
	if who.Scope != auth.ScopeCustom {
		return auth.ScopeAllows(who.Scope, spec.Tier)
	}
	for _, key := range spec.REST {
		access := routeAccess[key]
		if !access.Approval && access.Permission != "" && !who.Allows(access.Permission) {
			return false
		}
	}
	return true
}
