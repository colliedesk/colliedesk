package auth

import (
	"slices"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// personRolesColumn selects, for the query that joins agents a, the person's roles with their
// permissions as JSON: Admin first, then Agent, then by name. A deleted role is held by nobody.
const personRolesColumn = `COALESCE((SELECT json_agg(json_build_object('id',r.id,'name',r.name,'system',r.system,'permissions',r.permissions)
		ORDER BY CASE r.system WHEN 'admin' THEN 0 WHEN 'agent' THEN 1 ELSE 2 END, lower(r.name), r.id)
	FROM agent_roles ar JOIN roles r ON r.workspace_id=ar.workspace_id AND r.id=ar.role_id
	WHERE ar.workspace_id=a.workspace_id AND ar.agent_id=a.id AND r.deleted_at IS NULL),'[]'::json)`

// roleRow is a role as personRolesColumn and the session's own role read it.
type roleRow struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	System      *string  `json:"system"`
	Permissions []string `json:"permissions"`
}

func (r roleRow) ref() domain.RoleRef {
	return domain.RoleRef{ID: r.ID, Name: r.Name, System: r.System}
}

// permissions of a role: every one for Admin.
func (r roleRow) permissions() PermissionSet {
	if r.System != nil && *r.System == domain.SystemRoleAdmin {
		return NewPermissionSet(AllPermissions())
	}
	return NewPermissionSet(r.Permissions)
}

// rolesPermissions is what someone holding these roles may do.
func rolesPermissions(roles []roleRow) PermissionSet {
	held := PermissionSet{}
	for _, role := range roles {
		for name := range role.permissions() {
			held[name] = true
		}
	}
	return held
}

// Principal is who makes a call: a person's session, or an agent token acting as its person.
type Principal struct {
	Workspace domain.Workspace
	// Agent is the person; Agent.Roles are the roles they hold.
	Agent     domain.Agent
	SessionID string
	ViaCookie bool
	// Token is what an agent token was given; nil for a person's own session.
	Token *domain.TokenGrant
	// LimitedTo is the one role a person's session is limited to (a pairing link that asked for the
	// Agent role); nil otherwise.
	LimitedTo *domain.RoleRef
	// Access is the workspace's plan at the time of the request, derived inside the transaction that
	// authenticated the session so the lock check costs no transaction of its own. The zero value
	// (self-hosted, no plan limits) is never locked.
	Access domain.Access
	// person is what the person may do in this session: their roles, limited to LimitedTo if set.
	person PermissionSet
	// given is what the token was given; nil for a person's session.
	given PermissionSet
}

// TokenGrant is what an agent token was given (domain.TokenGrant).
type TokenGrant = domain.TokenGrant

// setAccess works out the principal's permissions from what Authenticate read. scope, permissions and
// legacy are the session's columns; limit is the role of sessions.role_id (empty ID when none).
func (p *Principal) setAccess(roles []roleRow, limit roleRow, scope string, permissions []string, legacy bool) {
	p.Agent.Roles = make([]domain.RoleRef, len(roles))
	for index, role := range roles {
		p.Agent.Roles[index] = role.ref()
	}
	p.person = rolesPermissions(roles)
	if scope == "" {
		if limit.ID != "" {
			ref := limit.ref()
			p.LimitedTo = &ref
			p.person = p.person.Intersect(limit.permissions())
		}
		return
	}
	switch {
	case legacy:
		p.given = legacyGiven(scope, permissions)
		tier := scope
		p.Token = &domain.TokenGrant{Kind: domain.TokenKindLegacy, LegacyTier: &tier}
	case scope == ScopeRole:
		ref := limit.ref()
		p.given = limit.permissions()
		p.Token = &domain.TokenGrant{Kind: domain.TokenKindRole, Role: &ref, RoleHeld: p.person.HoldsAll(p.given), Permissions: p.given.Sorted()}
		if !p.Token.RoleHeld {
			// The person no longer holds every permission of the role (they may follow a role they do not
			// hold, such as an admin's token that follows Agent): the token is stopped until they do.
			p.given = PermissionSet{}
		}
		return
	default:
		p.given = NewPermissionSet(permissions)
		p.Token = &domain.TokenGrant{Kind: domain.TokenKindCustom}
	}
	p.Token.Permissions = p.given.Sorted()
}

// IsToken reports whether the principal is an agent token rather than a person's own session.
func (p Principal) IsToken() bool { return p.Token != nil }

// Can reports whether this call may do what permission allows: the person holds it, and an agent token
// was given it. An unknown permission is refused so a typo never opens a route.
func (p Principal) Can(permission string) bool {
	if !p.person[permission] {
		return false
	}
	return p.given == nil || p.given[permission]
}

// PersonCan reports whether the person behind the call holds the permission, whatever the token was
// given; a refusal says which of the two lacks it.
func (p Principal) PersonCan(permission string) bool { return p.person[permission] }

// Effective lists what this call may do, in the matrix order.
func (p Principal) Effective() []string { return p.effective().Sorted() }

func (p Principal) effective() PermissionSet {
	if p.given == nil {
		return p.person
	}
	return p.person.Intersect(p.given)
}

// Missing lists the names this call may not do.
func (p Principal) Missing(names []string) []string { return p.effective().Missing(names) }

// HoldsAll reports whether this call may do every one of names.
func (p Principal) HoldsAll(names []string) bool { return len(p.Missing(names)) == 0 }

// IsAdmin reports whether the person holds the built-in Admin role (used only for the seat order and
// the last-admin rule, never to decide a permission).
func (p Principal) IsAdmin() bool {
	return slices.ContainsFunc(p.Agent.Roles, func(role domain.RoleRef) bool { return role.IsAdmin() })
}

// PrincipalForTest builds a person's principal holding exactly permissions, for tests of code that only
// asks Can. Production principals come from Authenticate.
func PrincipalForTest(workspace domain.Workspace, agent domain.Agent, permissions []string) Principal {
	return Principal{Workspace: workspace, Agent: agent, person: NewPermissionSet(permissions)}
}

// WithTokenForTest gives a test principal a custom token holding permissions.
func (p Principal) WithTokenForTest(permissions []string) Principal {
	p.given = NewPermissionSet(permissions)
	p.Token = &domain.TokenGrant{Kind: domain.TokenKindCustom, Permissions: p.given.Sorted()}
	return p
}
