package auth

import (
	"slices"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

// The store works out the Admin role and role checks from the same catalog.
func init() {
	store.SetPermissionCatalog(AllPermissions())
	store.SetAgentRoleDefaults(AgentRolePermissions())
}

// What a route of Routes() needs to be reached at all. Which permission a signed-in caller needs is
// in httpapi.routeAccess, the one table for people and agent tokens.
const (
	// PermissionPublic: no session (sign-in, the public help center, a share link).
	PermissionPublic = "public"
	// PermissionSession: a person's session or an agent token of the workspace.
	PermissionSession = "session"
	// PermissionChannelInbound marks routes authenticated by a channel secret instead of a session.
	PermissionChannelInbound = "channels:inbound"
)

// agentRolePermissions is what the built-in Agent role starts with: everything the agent role could do
// before roles (every permission below the admin tier of agent tokens), and team:create_tokens, since a
// person only ever gives a token what they hold. Migration 20261102120000-roles writes the same list
// (colliedesk_agent_role_permissions); TestTheAgentRoleStartsAsTheMigrationMadeIt keeps them equal.
// A permission a later release adds does not join it: the workspace decides.
var agentRolePermissions = []string{
	PermConversationsRead, PermConversationsUpdate, PermMessagesRead, PermMessagesNote, PermMessagesSend,
	PermAIAssistRead, PermAIAssistCreate, PermContactsRead, PermContactsCreate, PermContactsUpdate, PermTagsRead,
	PermTagsCreate, PermTagsUpdate, PermTagsDelete, PermSavedRepliesRead, PermSavedRepliesCreate,
	PermSavedRepliesUpdate, PermSavedRepliesDelete, PermSavedViewsRead, PermSavedViewsCreate,
	PermSavedViewsUpdate, PermSavedViewsDelete, PermKBArticlesRead, PermKBArticlesCreate, PermKBArticlesUpdate,
	PermKBArticlesDelete, PermKBArticlesPublish, PermKBMediaRead, PermKBMediaCreate, PermKBMediaUpdate,
	PermKBMediaDelete, PermHelpCenterRead, PermResultsRead, PermProfileUpdate, PermTeamRead, PermChannelsRead,
	PermAISettingsRead, PermAutofixRead, PermAutofixUpdate, PermAutofixSend, PermAutofixSettingsRead,
	PermTeamCreateTokens,
}

// AgentRolePermissions lists what the built-in Agent role starts with (and goes back to on a reset).
func AgentRolePermissions() []string { return slices.Clone(agentRolePermissions) }

// AllPermissions lists every permission, in the order the permission matrix shows them; the built-in
// Admin role holds all of them.
func AllPermissions() []string {
	names := make([]string, len(tokenPermissions))
	for index, item := range tokenPermissions {
		names[index] = item.Name
	}
	return names
}

// PermissionSet is a set of permissions.
type PermissionSet map[string]bool

// NewPermissionSet holds the valid names among names.
func NewPermissionSet(names []string) PermissionSet {
	set := PermissionSet{}
	for _, name := range names {
		if ValidPermission(name) {
			set[name] = true
		}
	}
	return set
}

// Sorted lists the set in the matrix order.
func (s PermissionSet) Sorted() []string {
	names := make([]string, 0, len(s))
	for _, item := range tokenPermissions {
		if s[item.Name] {
			names = append(names, item.Name)
		}
	}
	return names
}

// Intersect is the permissions in both sets.
func (s PermissionSet) Intersect(other PermissionSet) PermissionSet {
	both := PermissionSet{}
	for name := range s {
		if other[name] {
			both[name] = true
		}
	}
	return both
}

// HoldsAll reports whether this set holds every permission of other.
func (s PermissionSet) HoldsAll(other PermissionSet) bool {
	for name := range other {
		if !s[name] {
			return false
		}
	}
	return true
}

// Missing lists the names this set does not hold, in the matrix order.
func (s PermissionSet) Missing(names []string) []string {
	want := NewPermissionSet(names)
	var missing []string
	for _, name := range want.Sorted() {
		if !s[name] {
			missing = append(missing, name)
		}
	}
	return missing
}
