package auth

import (
	"slices"
	"sort"
	"strings"
)

// A permission is resource:action; roles hold them, and agent tokens are given them. Resources are what a person sees in the app (conversations,
// the knowledge base, the team); actions are read, create, update and delete, plus a named action
// where the risk is not CRUD: send (reaches a customer), publish (reaches the public help center),
// export and share (data leaves the workspace), purge (no way back), manage_credentials (another
// service's key), manage_roles, revoke and invite (people's access).
// httpapi.routeAccess names the one permission each route needs, for people and tokens alike.
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

// TokenPermission describes one permission, and whether it reaches outside the workspace (a customer,
// the public, another service), which the permission matrix marks.
type TokenPermission struct {
	Name     string `json:"name"`
	Resource string `json:"resource"`
	Action   string `json:"action"`
	Outside  bool   `json:"outside"`
}

func permission(name string, outside bool) TokenPermission {
	resource, action, _ := strings.Cut(name, ":")
	return TokenPermission{Name: name, Resource: resource, Action: action, Outside: outside}
}

// tokenPermissions is every permission, in the order the permission matrix shows them.
var tokenPermissions = []TokenPermission{
	permission(PermConversationsRead, false),
	permission(PermConversationsUpdate, false),
	permission(PermMessagesRead, false),
	permission(PermMessagesNote, false),
	permission(PermMessagesSend, true),
	permission(PermAIAssistRead, false),
	permission(PermAIAssistCreate, false),
	permission(PermContactsRead, false),
	permission(PermContactsCreate, false),
	permission(PermContactsUpdate, false),
	permission(PermTagsRead, false),
	permission(PermTagsCreate, false),
	permission(PermTagsUpdate, false),
	permission(PermTagsDelete, false),
	permission(PermSavedRepliesRead, false),
	permission(PermSavedRepliesCreate, false),
	permission(PermSavedRepliesUpdate, false),
	permission(PermSavedRepliesDelete, false),
	permission(PermSavedViewsRead, false),
	permission(PermSavedViewsCreate, false),
	permission(PermSavedViewsUpdate, false),
	permission(PermSavedViewsDelete, false),
	permission(PermKBArticlesRead, false),
	permission(PermKBArticlesCreate, false),
	permission(PermKBArticlesUpdate, false),
	permission(PermKBArticlesDelete, false),
	permission(PermKBArticlesPublish, true),
	permission(PermKBMediaRead, false),
	permission(PermKBMediaCreate, false),
	permission(PermKBMediaUpdate, false),
	permission(PermKBMediaDelete, false),
	permission(PermKBMediaPurge, false),
	permission(PermHelpCenterRead, false),
	permission(PermHelpCenterUpdate, true),
	permission(PermResultsRead, false),
	permission(PermResultsShare, true),
	permission(PermProfileUpdate, false),
	permission(PermTeamRead, false),
	permission(PermTeamInvite, false),
	permission(PermTeamRevoke, false),
	permission(PermTeamManageRoles, false),
	permission(PermTeamCreateTokens, false),
	permission(PermChannelsRead, false),
	permission(PermChannelsUpdate, false),
	permission(PermChannelsManageCredentials, true),
	permission(PermWorkspaceUpdate, false),
	permission(PermWorkspaceExport, true),
	permission(PermBillingRead, false),
	permission(PermBillingManage, false),
	permission(PermImportsRead, false),
	permission(PermImportsRun, false),
	permission(PermImportsManageCredentials, true),
	permission(PermImportsUndo, false),
	permission(PermAISettingsRead, false),
	permission(PermAISettingsUpdate, false),
	permission(PermAISettingsManageCredentials, true),
	permission(PermAutofixRead, false),
	permission(PermAutofixUpdate, false),
	permission(PermAutofixSend, true),
	permission(PermAutofixSettingsRead, false),
	permission(PermAutofixSettingsUpdate, false),
	permission(PermAutofixSettingsManageCredentials, true),
}

var validPermission = func() map[string]bool {
	valid := make(map[string]bool, len(tokenPermissions))
	for _, item := range tokenPermissions {
		valid[item.Name] = true
	}
	return valid
}()

// TokenPermissions lists every permission, in the order the permission matrix shows them.
func TokenPermissions() []TokenPermission { return slices.Clone(tokenPermissions) }

// ValidPermission reports whether name is a permission of the workspace.
func ValidPermission(name string) bool { return validPermission[name] }

// cleanPermissions sorts and dedupes a list of permissions, or says which one is unknown.
func cleanPermissions(names []string) ([]string, error) {
	seen := map[string]bool{}
	var clean []string
	for _, name := range names {
		name = strings.TrimSpace(name)
		if !ValidPermission(name) {
			return nil, &InvalidTokenError{"Unknown permission " + quote(name) + ". The list is in docs/user/roles-and-permissions.md."}
		}
		if !seen[name] {
			seen[name] = true
			clean = append(clean, name)
		}
	}
	sort.Strings(clean)
	return clean, nil
}

// CleanPermissions is cleanPermissions for roles: an empty list is fine.
func CleanPermissions(names []string) ([]string, error) {
	clean, err := cleanPermissions(names)
	if clean == nil && err == nil {
		clean = []string{}
	}
	return clean, err
}

func quote(text string) string { return `"` + text + `"` }
