package auth

import (
	"slices"
	"sort"
	"strings"
)

// ScopeCustom marks an agent token that holds a list of permissions instead of one of the four tiers.
// The tiers stay as presets: a preset token's permissions are worked out on every call, so a route a
// later release adds at the send tier opens for every send token, as it always has. A custom token
// holds exactly the permissions it was given.
const ScopeCustom = "custom"

// A token permission is resource:action. Resources are what a person sees in the app (conversations,
// the knowledge base, the team); actions are read, create, update and delete, plus a named action
// where the risk is not CRUD: send (reaches a customer), publish (reaches the public help center),
// export and share (data leaves the workspace), purge (no way back), manage_credentials (another
// service's key), manage_roles, revoke and invite (people's access).
//
// Each permission names its preset, the lowest tier that holds it; a tier holds every permission of
// its own preset and below. Every route of a resource whose old tiers differ is split into actions, so
// a preset token reaches exactly the routes its tier reached (TestEveryRouteIsOpenToTokensOnlyFromItsTier).
const (
	PermConversationsRead   = "conversations:read"
	PermConversationsUpdate = "conversations:update"

	PermMessagesRead = "messages:read"
	PermMessagesNote = "messages:note"
	PermMessagesSend = "messages:send"

	PermAIAssistRead   = "ai_assist:read"
	PermAIAssistCreate = "ai_assist:create"

	PermContactsRead   = "contacts:read"
	PermContactsCreate = "contacts:create"
	PermContactsUpdate = "contacts:update"

	PermTagsRead   = "tags:read"
	PermTagsCreate = "tags:create"
	PermTagsUpdate = "tags:update"
	PermTagsDelete = "tags:delete"

	PermSavedRepliesRead   = "saved_replies:read"
	PermSavedRepliesCreate = "saved_replies:create"
	PermSavedRepliesUpdate = "saved_replies:update"
	PermSavedRepliesDelete = "saved_replies:delete"

	PermSavedViewsRead   = "saved_views:read"
	PermSavedViewsCreate = "saved_views:create"
	PermSavedViewsUpdate = "saved_views:update"
	PermSavedViewsDelete = "saved_views:delete"

	PermKBArticlesRead    = "kb_articles:read"
	PermKBArticlesCreate  = "kb_articles:create"
	PermKBArticlesUpdate  = "kb_articles:update"
	PermKBArticlesDelete  = "kb_articles:delete"
	PermKBArticlesPublish = "kb_articles:publish"

	PermKBMediaRead   = "kb_media:read"
	PermKBMediaCreate = "kb_media:create"
	PermKBMediaUpdate = "kb_media:update"
	PermKBMediaDelete = "kb_media:delete"
	PermKBMediaPurge  = "kb_media:purge"

	PermHelpCenterRead   = "help_center:read"
	PermHelpCenterUpdate = "help_center:update"

	PermResultsRead  = "results:read"
	PermResultsShare = "results:share"

	PermAgentRequestsRead   = "agent_requests:read"
	PermAgentRequestsCancel = "agent_requests:cancel"

	PermProfileUpdate = "profile:update"

	PermTeamRead         = "team:read"
	PermTeamInvite       = "team:invite"
	PermTeamRevoke       = "team:revoke"
	PermTeamManageRoles  = "team:manage_roles"
	PermTeamCreateTokens = "team:create_tokens"

	PermChannelsRead              = "channels:read"
	PermChannelsUpdate            = "channels:update"
	PermChannelsManageCredentials = "channels:manage_credentials"

	PermWorkspaceUpdate = "workspace:update"
	PermWorkspaceExport = "workspace:export"

	PermBillingRead   = "billing:read"
	PermBillingManage = "billing:manage"

	PermImportsRead              = "imports:read"
	PermImportsRun               = "imports:run"
	PermImportsManageCredentials = "imports:manage_credentials"
	PermImportsUndo              = "imports:undo"

	PermAISettingsRead              = "ai_settings:read"
	PermAISettingsUpdate            = "ai_settings:update"
	PermAISettingsManageCredentials = "ai_settings:manage_credentials"

	PermAutofixRead                      = "autofix:read"
	PermAutofixUpdate                    = "autofix:update"
	PermAutofixSend                      = "autofix:send"
	PermAutofixSettingsRead              = "autofix_settings:read"
	PermAutofixSettingsUpdate            = "autofix_settings:update"
	PermAutofixSettingsManageCredentials = "autofix_settings:manage_credentials"
)

// TokenPermission describes one permission: its preset, and whether it reaches outside the workspace
// (a customer, the public, another service), which the token form marks.
type TokenPermission struct {
	Name     string `json:"name"`
	Resource string `json:"resource"`
	Action   string `json:"action"`
	Preset   string `json:"preset"`
	Outside  bool   `json:"outside"`
}

func permission(name, preset string, outside bool) TokenPermission {
	resource, action, _ := strings.Cut(name, ":")
	return TokenPermission{Name: name, Resource: resource, Action: action, Preset: preset, Outside: outside}
}

// tokenPermissions is every permission a token can hold, in the order the token form shows them.
var tokenPermissions = []TokenPermission{
	permission(PermConversationsRead, ScopeRead, false),
	permission(PermConversationsUpdate, ScopeDraft, false),
	permission(PermMessagesRead, ScopeRead, false),
	permission(PermMessagesNote, ScopeDraft, false),
	permission(PermMessagesSend, ScopeSend, true),
	permission(PermAIAssistRead, ScopeRead, false),
	permission(PermAIAssistCreate, ScopeDraft, false),
	permission(PermContactsRead, ScopeRead, false),
	permission(PermContactsCreate, ScopeSend, false),
	permission(PermContactsUpdate, ScopeSend, false),
	permission(PermTagsRead, ScopeRead, false),
	permission(PermTagsCreate, ScopeDraft, false),
	permission(PermTagsUpdate, ScopeSend, false),
	permission(PermTagsDelete, ScopeSend, false),
	permission(PermSavedRepliesRead, ScopeRead, false),
	permission(PermSavedRepliesCreate, ScopeSend, false),
	permission(PermSavedRepliesUpdate, ScopeSend, false),
	permission(PermSavedRepliesDelete, ScopeSend, false),
	permission(PermSavedViewsRead, ScopeRead, false),
	permission(PermSavedViewsCreate, ScopeDraft, false),
	permission(PermSavedViewsUpdate, ScopeDraft, false),
	permission(PermSavedViewsDelete, ScopeDraft, false),
	permission(PermKBArticlesRead, ScopeRead, false),
	permission(PermKBArticlesCreate, ScopeSend, false),
	permission(PermKBArticlesUpdate, ScopeSend, false),
	permission(PermKBArticlesDelete, ScopeSend, false),
	permission(PermKBArticlesPublish, ScopeSend, true),
	permission(PermKBMediaRead, ScopeRead, false),
	permission(PermKBMediaCreate, ScopeSend, false),
	permission(PermKBMediaUpdate, ScopeSend, false),
	permission(PermKBMediaDelete, ScopeSend, false),
	permission(PermKBMediaPurge, ScopeAdmin, false),
	permission(PermHelpCenterRead, ScopeRead, false),
	permission(PermHelpCenterUpdate, ScopeAdmin, true),
	permission(PermResultsRead, ScopeRead, false),
	permission(PermResultsShare, ScopeAdmin, true),
	permission(PermAgentRequestsRead, ScopeRead, false),
	permission(PermAgentRequestsCancel, ScopeRead, false),
	permission(PermProfileUpdate, ScopeDraft, false),
	permission(PermTeamRead, ScopeRead, false),
	permission(PermTeamInvite, ScopeAdmin, false),
	permission(PermTeamRevoke, ScopeAdmin, false),
	permission(PermTeamManageRoles, ScopeAdmin, false),
	permission(PermTeamCreateTokens, ScopeAdmin, false),
	permission(PermChannelsRead, ScopeRead, false),
	permission(PermChannelsUpdate, ScopeAdmin, false),
	permission(PermChannelsManageCredentials, ScopeAdmin, true),
	permission(PermWorkspaceUpdate, ScopeAdmin, false),
	permission(PermWorkspaceExport, ScopeAdmin, true),
	permission(PermBillingRead, ScopeAdmin, false),
	permission(PermBillingManage, ScopeAdmin, false),
	permission(PermImportsRead, ScopeAdmin, false),
	permission(PermImportsRun, ScopeAdmin, false),
	permission(PermImportsManageCredentials, ScopeAdmin, true),
	permission(PermImportsUndo, ScopeAdmin, false),
	permission(PermAISettingsRead, ScopeRead, false),
	permission(PermAISettingsUpdate, ScopeAdmin, false),
	permission(PermAISettingsManageCredentials, ScopeAdmin, true),
	permission(PermAutofixRead, ScopeRead, false),
	permission(PermAutofixUpdate, ScopeDraft, false),
	permission(PermAutofixSend, ScopeSend, true),
	permission(PermAutofixSettingsRead, ScopeRead, false),
	permission(PermAutofixSettingsUpdate, ScopeAdmin, false),
	permission(PermAutofixSettingsManageCredentials, ScopeAdmin, true),
}

var permissionPreset = func() map[string]string {
	presets := make(map[string]string, len(tokenPermissions))
	for _, item := range tokenPermissions {
		presets[item.Name] = item.Preset
	}
	return presets
}()

// TokenPermissions lists every permission a token can hold, in the order the token form shows them.
func TokenPermissions() []TokenPermission { return slices.Clone(tokenPermissions) }

// ValidPermission reports whether name is a permission a token can hold.
func ValidPermission(name string) bool { return permissionPreset[name] != "" }

// PermissionPreset is the lowest tier that holds the permission, or "" for an unknown one.
func PermissionPreset(name string) string {
	if slices.Contains(OperatorPermissions, name) {
		return ScopeOperator
	}
	return permissionPreset[name]
}

// PresetPermissions lists the permissions a tier holds, in the form's order.
func PresetPermissions(scope string) []string {
	var names []string
	for _, item := range tokenPermissions {
		if ScopeAllows(scope, item.Preset) && scope != "" {
			names = append(names, item.Name)
		}
	}
	return names
}

// needsAdmin reports whether a token holding these permissions acts with its creator's admin role:
// only an admin may create one, and it carries the admin role ceiling, like an admin token.
func needsAdmin(permissions []string) bool {
	return slices.ContainsFunc(permissions, func(name string) bool { return permissionPreset[name] == ScopeAdmin })
}

// NeedsAdmin reports whether creating a token of this tier or these permissions takes an admin.
func NeedsAdmin(scope string, permissions []string) bool {
	return scope == ScopeAdmin || needsAdmin(permissions)
}

// cleanPermissions sorts and dedupes a custom token's permissions, or says which one is unknown.
func cleanPermissions(names []string) ([]string, error) {
	seen := map[string]bool{}
	var clean []string
	for _, name := range names {
		name = strings.TrimSpace(name)
		if !ValidPermission(name) {
			return nil, &InvalidTokenError{"Unknown permission " + quote(name) + ". The list is in docs/user/sessions.md."}
		}
		if !seen[name] {
			seen[name] = true
			clean = append(clean, name)
		}
	}
	if len(clean) == 0 {
		return nil, &InvalidTokenError{"A custom token needs at least one permission."}
	}
	sort.Strings(clean)
	return clean, nil
}

func quote(text string) string { return `"` + text + `"` }

// Allows reports whether the principal may do what permission allows. A person's own session may do
// everything (the role still decides, per route); a preset token holds its tier's permissions; a
// custom token holds the ones it was given. An unknown permission is refused so a typo never opens a
// route.
func (p Principal) Allows(permission string) bool {
	switch p.Scope {
	case "":
		return true
	case ScopeCustom:
		return slices.Contains(p.Permissions, permission)
	}
	preset := permissionPreset[permission]
	return preset != "" && ScopeAllows(p.Scope, preset)
}

// IsToken reports whether the principal is an agent token rather than a person's own session.
func (p Principal) IsToken() bool { return p.Scope != "" }

// HeldPermissions lists what the principal's token holds: its tier's permissions or its own list.
func (p Principal) HeldPermissions() []string {
	if p.Scope == ScopeCustom {
		return slices.Clone(p.Permissions)
	}
	return PresetPermissions(p.Scope)
}
