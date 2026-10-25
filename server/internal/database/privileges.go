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
	// PrivilegeKBPurge deletes, in every workspace, the knowledge base articles and collections that
	// have been in the trash for more than 30 days, and the library items the purge rule names; and it
	// finds the images the backfill has not read yet. Only workspace ids, item ids and storage keys
	// leave the transaction; bytes go through the scoped DiscardObject, and the backfill reads and
	// writes each image in its own workspace.
	PrivilegeKBPurge Privilege = "kb-purge"
	// PrivilegeKBUploadDisk sums, over every workspace, the uploads of videos in progress and the bytes
	// they still need, so a new upload's disk check counts what the other workspaces reserved. Only a
	// count and a byte total leave the transaction.
	PrivilegeKBUploadDisk Privilege = "kb-upload-disk"
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
	// PrivilegeChannelRoute resolves the live channel that is an account outside (a chat account), from
	// an event a channel's webhook received, or that a public address names by its random id (a web
	// form's own address). Accounts are unique on the server; only the workspace and channel ids leave
	// the transaction.
	PrivilegeChannelRoute Privilege = "channel-route"
	// PrivilegeChannelInboundClaim leases due events of the channel journal of every workspace (received,
	// failed, or collected until their flush time) for the inbound worker; each is then processed in its
	// own workspace.
	PrivilegeChannelInboundClaim Privilege = "channel-inbound-claim"
	// PrivilegeImportJobClaim leases due in-app import jobs of every workspace for the import worker,
	// and discards previews nobody started within the hour.
	PrivilegeImportJobClaim Privilege = "import-job-claim"
	// PrivilegeVerifiedSender resolves the workspace whose support address domain the email provider
	// verified, from the From domain of a delivery event. At most one workspace holds a given name.
	PrivilegeVerifiedSender Privilege = "verified-sender"
	// PrivilegeProviderDomainSweep lists and settles the provider domains waiting to be deleted at the
	// email provider, for workspaces that no longer use them or no longer exist, and releases provider domains that stayed unverified for a week.
	PrivilegeProviderDomainSweep Privilege = "provider-domain-sweep"
	// PrivilegeTelemetry claims the install's daily usage report in the platform-level instance
	// table and reads instance-wide counts (workspaces, active agents, replies, channels in use) and,
	// on Colliedesk Cloud, the same counts per workspace id. Only aggregates leave the transaction,
	// never row content.
	PrivilegeTelemetry Privilege = "telemetry"
	// PrivilegeServerSecret reads and first creates the secrets every server process shares (the image
	// proxy's signing key) in server_secrets, which has no workspace and no grant to the app role.
	PrivilegeServerSecret Privilege = "server-secret"
	// PrivilegeMailImageSweep evicts cached email images in every workspace: those past their age, then
	// the least recently served while the cache is over its byte cap. Only keys and sizes leave the
	// transaction; the stored bytes go through the scoped DiscardObject.
	PrivilegeMailImageSweep Privilege = "mail-image-sweep"
	// PrivilegeBillingReconcile reads every workspace id and every stored Polar subscription, so the
	// operator command can compare them with Polar's list. Writes go through the scoped path.
	PrivilegeBillingReconcile Privilege = "billing-reconcile"
	// PrivilegeMailboxGrantShared answers whether any other workspace still holds a Gmail connection for an
	// address, after a workspace dropped its own. Google revokes an account's whole grant to the project, so
	// the grant is revoked only when no workspace holds it. Only a yes or no leaves the transaction.
	PrivilegeMailboxGrantShared Privilege = "mailbox-grant-shared"
	// PrivilegeAIJobClaim leases due AI tasks of every workspace for the AI worker; closes the usage rows
	// a dead process left started; expires AI drafts and triage suggestions nobody decided on for a week and erases the text of
	// drafts that ended 90 days ago. Only task ids, workspace ids and claim tokens leave the claim; each
	// task is then read, run and recorded in its own workspace, and the sweeps return nothing.
	PrivilegeAIJobClaim Privilege = "ai-job-claim"
	// PrivilegeAIRetrievalQueue lists the workspaces that have knowledge base articles or conversations
	// queued for AI retrieval to build. Only workspace ids leave the transaction; each is built in its
	// own scope.
	PrivilegeAIRetrievalQueue Privilege = "ai-retrieval-queue"
	// PrivilegeResultsClaim adds a results_state row for every workspace without one and leases the
	// workspaces whose results page numbers are due. Only workspace ids leave the transaction; each is
	// computed and stored in its own scope.
	PrivilegeResultsClaim Privilege = "results-claim"
	// PrivilegeResultsShareToken resolves the workspace of a results share link from its token hash.
	PrivilegeResultsShareToken Privilege = "results-share-token"
)
