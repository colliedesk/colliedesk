package database

// Privilege names why a transaction must see more than one workspace. The list is closed: each
// value is one reviewed path, documented in docs/internals/workspace-isolation.md, and a guard test
// pins every call site. Keep a privileged transaction to the lookup that finds the workspace, then
// continue in InWorkspace.
type Privilege string

const (
	// PrivilegeMigrate applies schema migrations.
	PrivilegeMigrate Privilege = "migrate"
	// PrivilegeCreateWorkspace inserts a new workspace with its first admin and pairing link.
	PrivilegeCreateWorkspace Privilege = "create-workspace"
	// PrivilegeWorkspaceBySlug resolves the workspace a CLI command names (pair, import, undo).
	PrivilegeWorkspaceBySlug Privilege = "workspace-by-slug"
	// PrivilegeSessionToken resolves the workspace of a session from its token hash.
	PrivilegeSessionToken Privilege = "session-token"
	// PrivilegeLinkToken resolves the workspace of a pairing or invite link from its token hash.
	PrivilegeLinkToken Privilege = "link-token"
	// PrivilegeCloudIdentity lists the workspaces where a verified cloud email is an agent.
	PrivilegeCloudIdentity Privilege = "cloud-identity"
	// PrivilegeReplyToken resolves the workspace of inbound mail from the reply token in its address.
	PrivilegeReplyToken Privilege = "reply-token"
	// PrivilegeInboundSecret resolves the workspace that owns an inbound email secret.
	PrivilegeInboundSecret Privilege = "inbound-secret"
	// PrivilegeOutboxClaim leases due outbox rows of every workspace for the email worker.
	PrivilegeOutboxClaim Privilege = "outbox-claim"
)
