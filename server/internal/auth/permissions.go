package auth

const (
	PermissionPublic             = "public"
	PermissionAuthenticated      = "authenticated"
	PermissionConversationsRead  = "conversations:read"
	PermissionConversationsWrite = "conversations:write"
	PermissionContactsRead       = "contacts:read"
	PermissionContactsWrite      = "contacts:write"
	PermissionAccessRead         = "access:read"
	PermissionAccessWrite        = "access:write"
	// PermissionBillingManage covers plan, checkout and the billing portal; admins only.
	PermissionBillingManage = "billing:manage"
	// PermissionChannelInbound marks routes authenticated by a channel secret instead of a session.
	PermissionChannelInbound = "channels:inbound"
)

var agentPermissions = map[string]bool{
	PermissionAuthenticated:      true,
	PermissionConversationsRead:  true,
	PermissionConversationsWrite: true,
	PermissionContactsRead:       true,
	PermissionContactsWrite:      true,
	PermissionAccessRead:         true,
}

func HasPermission(role, permission string) bool {
	if permission == PermissionPublic {
		return true
	}
	if role == "admin" {
		return true
	}
	return role == "agent" && agentPermissions[permission]
}

func CanDelegate(role, ceiling string) bool {
	if role != "admin" {
		return false
	}
	return ceiling == "admin" || ceiling == "agent"
}

func effectiveRole(agentRole, ceiling string) string {
	if agentRole == "agent" || ceiling == "agent" {
		return "agent"
	}
	return "admin"
}
