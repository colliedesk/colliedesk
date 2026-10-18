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
	// PrivilegeSignupSweep clears the old client address hashes of cloud_signups.
	PrivilegeSignupSweep Privilege = "signup-sweep"
	// PrivilegeAgentRequestPrune clears the call and answer of agent requests decided more than 90
	// days ago, in every workspace; the audit rows stay.
	PrivilegeAgentRequestPrune Privilege = "agent-request-prune"
	// PrivilegeSavedReplyPurge deletes, in every workspace, the saved replies that have been in the
	// trash for more than 30 days, with their history.
	PrivilegeSavedReplyPurge Privilege = "saved-reply-purge"
	// PrivilegeRetention finds locked workspaces, records and sends their deletion notice, and deletes
	// the ones locked for a year with their attachment files.
	PrivilegeRetention Privilege = "retention"
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
	// PrivilegeRequestLogin creates the request login role and lets it switch to the app role.
	PrivilegeRequestLogin Privilege = "request-login"
	// PrivilegeOutboxClaim leases due outbox rows of every workspace for the email worker.
	PrivilegeOutboxClaim Privilege = "outbox-claim"
	// PrivilegeVerifiedSender resolves the workspace whose support address domain the email provider
	// verified, from the From domain of a delivery event. At most one workspace holds a given name.
	PrivilegeVerifiedSender Privilege = "verified-sender"
	// PrivilegeProviderDomainSweep lists and settles the provider domains waiting to be deleted at the
	// email provider, for workspaces that no longer use them or no longer exist.
	PrivilegeProviderDomainSweep Privilege = "provider-domain-sweep"
	// PrivilegeTelemetry claims the install's daily usage report in the platform-level instance
	// table and reads instance-wide counts (workspaces, active agents, replies, channels in use).
	// Only aggregates leave the transaction, never row content.
	PrivilegeTelemetry Privilege = "telemetry"
	// PrivilegeBillingReconcile reads every workspace id and every stored Polar subscription, so the
	// operator command can compare them with Polar's list. Writes go through the scoped path.
	PrivilegeBillingReconcile Privilege = "billing-reconcile"
)
