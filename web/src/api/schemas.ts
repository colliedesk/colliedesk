import { z } from 'zod';
import type { components, operations } from './generated';

export type Generated<Name extends keyof components['schemas']> = components['schemas'][Name];
export type JsonResponse<Operation extends keyof operations, Status extends keyof operations[Operation]['responses']> =
  operations[Operation]['responses'][Status] extends { content: { 'application/json': infer Body } } ? Body : never;

const nullableDate = z.string().datetime().nullable();
export const roleSchema = z.enum(['admin', 'agent']);
export const statusSchema = z.enum(['open', 'pending', 'resolved', 'closed']);
export const prioritySchema = z.enum(['low', 'normal', 'high', 'urgent']);
// How a conversation started: the values known today, which the filters offer, and any other channel type the
// server's registry names (a chat type), which the inbox shows by its name instead of failing to load.
export const knownChannels = ['email', 'web_form', 'help_chat', 'zalo', 'imported', 'agent', 'problem_report'] as const;
export const channelSchema = z.string().regex(/^[a-z_]{1,40}$/);
export const conversationSortSchema = z.enum(['updated_desc', 'updated_asc', 'created_desc', 'created_asc', 'priority', 'waiting_longest', 'due']);

export const workspaceSchema = z.object({
  id: z.string().uuid(), slug: z.string(), name: z.string(), timezone: z.string(),
}) satisfies z.ZodType<Generated<'Workspace'>>;
export const renamedWorkspaceSchema = workspaceSchema.extend({ continue_url: z.string().url().optional() }) satisfies z.ZodType<Generated<'RenamedWorkspace'>>;
export const workspaceChoiceSchema = z.object({ slug: z.string(), name: z.string() }) satisfies z.ZodType<Generated<'WorkspaceChoice'>>;
export const cloudWorkspaceChoicesSchema = z.object({ workspaces: z.array(workspaceChoiceSchema).min(2) }) satisfies z.ZodType<Generated<'CloudWorkspaceChoices'>>;
export const serverFeatures = ['gmail', 'microsoft', 'autofix', 'ai_server', 'hosted_mail', 'cloud_sign_in', 'billing', 'signup'] as const;
export type ServerFeature = (typeof serverFeatures)[number];
const featureSchema = z.object({ enabled: z.boolean() });
// Why a feature is not offered, never which setting; a server from before the switches reads as not set up.
const featureOffReason = z.enum(['not_set_up', 'switched_off']).nullable().default(null);
export const publicConfigSchema = z.object({
  version: z.string().optional(),
  cloud_sign_in: z.object({ enabled: z.boolean(), publishable_key: z.string().nullable() }),
  // switched_off: set up but switched off by the operator; a server from before the switches never is.
  billing: z.object({ enabled: z.boolean(), switched_off: z.boolean().default(false) }),
  signup: z.object({ enabled: z.boolean() }),
  limits: z.object({ outgoing_attachments_bytes: z.number().int(), outgoing_attachments_files: z.number().int() }),
  product_analytics: z.object({ enabled: z.boolean(), key: z.string().nullable(), host: z.string().nullable(), realm: z.enum(['cloud', 'self-hosted']).nullable() }),
  ai: z.object({ server_provider: z.boolean() }),
  // Autofix needs the server's GitHub App; a server from before Autofix reads as without it.
  autofix: z.object({ available: z.boolean(), missing_settings: z.array(z.string()), off_reason: featureOffReason })
    .default({ available: false, missing_settings: [], off_reason: 'not_set_up' }),
  // Which of the server's features run here (operator console → Features); a server from before the
  // switches reads as running none of them, and the fields above still say what it has.
  features: z.object(Object.fromEntries(serverFeatures.map((name) => [name, featureSchema])) as Record<ServerFeature, typeof featureSchema>)
    .default(Object.fromEntries(serverFeatures.map((name) => [name, { enabled: false }])) as Record<ServerFeature, { enabled: boolean }>),
  // A server from before workspace hosts runs everything on one origin.
  app_origin: z.string().default(''),
  workspace_hosts: z.boolean().default(false),
}) satisfies z.ZodType<Generated<'PublicConfig'>>;
export const checkoutPlanSchema = z.enum(['solo_monthly', 'solo_yearly', 'small_monthly', 'small_yearly', 'team_monthly', 'team_yearly', 'team25_monthly', 'team25_yearly', 'founding']);
export const planSchema = z.enum(['trial', 'solo', 'small', 'team', 'team25', 'founding', 'none']);
export const billingSchema = z.object({
  plan: planSchema, trial_ends_at: z.string().datetime(), locked: z.boolean(),
  status: z.enum(['none', 'incomplete', 'incomplete_expired', 'trialing', 'active', 'past_due', 'canceled', 'unpaid', 'paused', 'unknown']),
  current_period_end: nullableDate, cancel_at_period_end: z.boolean(), active_subscriptions: z.number().int(),
  complimentary: z.boolean(), complimentary_until: nullableDate, checkout_plans: z.array(checkoutPlanSchema),
  checkout_refusals: z.array(z.object({ plan: checkoutPlanSchema, reason: z.string() })),
  storage_upgrade_plan: z.enum(['solo', 'small', 'team', 'team25', 'founding']).nullable(),
  limits: z.object({ agents: z.number().int(), replies_per_24h: z.number().int().nullable(), storage_bytes: z.number().int().nullable(), storage_warn_bytes: z.number().int().nullable() }),
  usage: z.object({ agents: z.number().int(), pending_invites: z.number().int(), replies_last_24h: z.number().int(), storage_bytes: z.number().int() }),
}) satisfies z.ZodType<Generated<'Billing'>>;
export const exportLinkSchema = z.object({ url: z.string().min(1), expires_in_seconds: z.number().int() }) satisfies z.ZodType<Generated<'ExportLink'>>;
export const billingLinkSchema = z.object({ url: z.string().url() }) satisfies z.ZodType<JsonResponse<'createBillingPortal', 200>>;
export const agentSchema = z.object({
  id: z.string().uuid(), email: z.string().email(), display_name: z.string(),
  role: roleSchema, timezone_override: z.string().nullable(),
}) satisfies z.ZodType<Generated<'Agent'>>;
export const teamMemberSchema = z.object({
  id: z.string().uuid(), email: z.string().email(), display_name: z.string(),
  role: roleSchema, timezone_override: z.string().nullable(),
  deactivated_at: z.string().datetime().nullable(), last_active_at: z.string().datetime().nullable(), open_conversations: z.number().int(),
}) satisfies z.ZodType<Generated<'TeamMember'>>;
const savedReplyPersonSchema = z.object({ agent_id: z.string().uuid().nullable(), name: z.string() }) satisfies z.ZodType<Generated<'SavedReplyPerson'>>;
const savedReplyChangeFields = {
  version: z.number().int(), kind: z.enum(['baseline', 'create', 'edit', 'delete', 'restore', 'undelete']), restored_from_version: z.number().int().nullable(),
  changed_by: savedReplyPersonSchema.nullable(), token: z.object({ id: z.string().uuid().nullable(), label: z.string() }).nullable(),
  agent_request_id: z.string().uuid().nullable(), created_at: z.string().datetime(),
};
export const savedReplyChangeSchema = z.object(savedReplyChangeFields) satisfies z.ZodType<Generated<'SavedReplyChange'>>;
const savedReplyFields = {
  id: z.string().uuid(), title: z.string(), body: z.string(), created_by_agent_id: z.string().uuid().nullable(),
  created_at: z.string().datetime(), updated_at: z.string().datetime(), last_change: savedReplyChangeSchema.nullable(),
};
export const savedReplySchema = z.object(savedReplyFields) satisfies z.ZodType<Generated<'SavedReply'>>;
export const trashedSavedReplySchema = z.object({
  ...savedReplyFields, deleted_at: z.string().datetime(), deleted_by: savedReplyPersonSchema.nullable(), purges_at: z.string().datetime(),
}) satisfies z.ZodType<Generated<'TrashedSavedReply'>>;
export const savedReplyVersionSchema = z.object({ ...savedReplyChangeFields, title: z.string(), body: z.string() }) satisfies z.ZodType<Generated<'SavedReplyVersion'>>;
export const tagSchema = z.object({ id: z.string().uuid(), name: z.string(), color: z.string() }) satisfies z.ZodType<Generated<'Tag'>>;
export const contactIdentitySchema = z.object({
  channel_id: z.string().uuid(), kind: z.string(), value: z.string(), display_name: z.string().nullable(),
}) satisfies z.ZodType<Generated<'ContactIdentity'>>;
// A customer known only on a channel has no email: the whole response must still parse.
export const contactSchema = z.object({
  id: z.string().uuid(), name: z.string(), email: z.string().email().nullable(), organization: z.string().nullable(),
  merged_into_id: z.string().uuid().nullable(), created_at: z.string().datetime(), updated_at: z.string().datetime(),
  identities: z.array(contactIdentitySchema).optional(),
}) satisfies z.ZodType<Generated<'Contact'>>;
const stringList = z.array(z.string());
export const conversationOriginSchema = z.object({
  system: z.string(), external_id: z.string(), type: z.string().nullable(), source: z.string().nullable(),
  due_at: nullableDate, first_response_due_at: nullableDate, cc: stringList, forwarded_to: stringList, mailboxes: stringList.optional(),
}) satisfies z.ZodType<Generated<'ConversationOrigin'>>;
export const messageOriginSchema = z.object({
  system: z.string(), external_id: z.string(),
  kind: z.enum(['description', 'reply', 'incoming', 'automated_response', 'private_note', 'public_note', 'forward', 'forwarded_email', 'other']),
  from: z.string().nullable(), to: stringList, cc: stringList, bcc: stringList,
}) satisfies z.ZodType<Generated<'MessageOrigin'>>;
export const timeEntrySchema = z.object({
  id: z.string().uuid(), conversation_id: z.string().uuid(), agent_id: z.string().uuid().nullable(), agent_name: z.string(),
  minutes: z.number().int().nonnegative(), note: z.string(), billable: z.boolean(), executed_at: z.string().datetime(),
}) satisfies z.ZodType<Generated<'TimeEntry'>>;
// A conversation's response targets (docs/user/response-targets.md): its clocks as the server counts them in
// business hours. null when targets are off, its channel is left out, or it was imported.
const slaClockSchema = z.object({
  state: z.enum(['running', 'paused', 'stopped', 'met']), due_at: z.string().datetime().nullable(), warn_at: z.string().datetime().nullable(),
  met_at: z.string().datetime().nullable(), breached_at: z.string().datetime().nullable(), due_soon: z.boolean(), overdue: z.boolean(),
  business_minutes_left: z.number().int().nullable(),
}) satisfies z.ZodType<Generated<'SLAClock'>>;
export const slaSchema = z.object({
  state: z.enum(['ok', 'due_soon', 'overdue', 'met', 'paused', 'stopped']), first_response: slaClockSchema.nullable(), resolution: slaClockSchema.nullable(),
  alerts: z.array(z.object({ clock: z.enum(['first_response', 'resolution']), threshold: z.enum(['reminder', 'overdue']), due_at: z.string().datetime(),
    status: z.enum(['sending', 'sent', 'rejected', 'nobody']), recipients: z.number().int(), at: z.string().datetime() })).optional(),
}) satisfies z.ZodType<Generated<'ConversationSLA'>>;
export type SLA = z.infer<typeof slaSchema>;
export type SLAClock = z.infer<typeof slaClockSchema>;
export const conversationSchema = z.object({
  id: z.string().uuid(), contact_id: z.string().uuid(), contact_name: z.string(), subject: z.string(),
  status: statusSchema, priority: prioritySchema, assignee_id: z.string().uuid().nullable(),
  tags: z.array(tagSchema), channel: channelSchema, origin: conversationOriginSchema.nullable(), created_at: z.string().datetime(), updated_at: z.string().datetime(),
  latest_reply_number: z.number().int().nonnegative(),
  number: z.number().int().positive().optional(), number_series: z.string().optional(), display_ref: z.string().optional(),
  // An older server sends none.
  sla: slaSchema.nullable().optional(),
  // Who set the status last when it was not a person on the team; an older server sends none.
  last_status_change: z.object({ to: statusSchema, at: z.string().datetime(), by: z.enum(['agent', 'token', 'customer', 'command', 'auto_close', 'import']), name: z.string().optional() }).nullable().optional()
    // One a newer server tells of in a way this app does not know is left out, not a failed conversation.
    .catch(null),
  // The statuses that, set now, would tell the customer in its chat (only on the conversation itself).
  status_notices: z.array(statusSchema).optional(),
  // Whether a status the team sets now may tell its customer (any status); on every conversation, lists too.
  may_tell_customer: z.boolean().optional(),
  channel_ref: z.object({ id: z.string(), type: z.string(), name: z.string(), enabled: z.boolean(), deleted: z.boolean(),
    // A server from before chat channels sends no type: its replies always left by email.
    replies_via: z.object({ id: z.string(), type: z.string().catch('email'), name: z.string(), enabled: z.boolean(),
      instead_of: z.object({ id: z.string(), name: z.string() }).nullable().optional() }).nullable() }).nullable().optional(),
}) satisfies z.ZodType<Generated<'Conversation'>>;
export const deliveryStatusSchema = z.enum(['queued', 'sent', 'failed']);
export const messageDeliverySchema = z.object({
  status: deliveryStatusSchema, attempts: z.number().int().min(0), last_error: z.string().nullable(), sent_at: nullableDate, reason_code: z.string().nullable().optional(), retryable: z.boolean().optional(),
}) satisfies z.ZodType<Generated<'MessageDelivery'>>;
export const emailAuthenticationSchema = z.enum(['pass', 'fail', 'none']);
export const incomingEmailSchema = z.object({
  from: z.string(), authentication: emailAuthenticationSchema, via: z.enum(['platform', 'workspace', 'web_form']).optional(),
  cc: z.array(z.string()).optional(),
  attachments: z.array(z.object({ filename: z.string(), content_type: z.string() })),
  original_attachment_id: z.string().uuid().nullable(),
}) satisfies z.ZodType<Generated<'IncomingEmail'>>;
export const attachmentSchema = z.object({
  id: z.string().uuid(), message_id: z.string().uuid(), filename: z.string(), content_type: z.string(),
  size: z.number().int().nonnegative(), sha256: z.string().regex(/^[0-9a-f]{64}$/), inline: z.boolean(), created_at: z.string().datetime(),
}) satisfies z.ZodType<Generated<'Attachment'>>;
// A token holds a tier (a preset) or, as custom, its own list of permissions.
export const tokenScopeSchema = z.enum(['read', 'draft', 'send', 'admin', 'custom']) satisfies z.ZodType<Generated<'TokenScope'>>;
export type TokenScope = Generated<'TokenScope'>;
export type TokenPreset = Generated<'TokenPreset'>;
export type TokenPermission = Generated<'TokenPermission'>;
export const messageAgentTokenSchema = z.object({
  id: z.string().uuid(), label: z.string(), scope: tokenScopeSchema, agent_name: z.string(),
}) satisfies z.ZodType<Generated<'MessageAgentToken'>>;
export const messageSchema = z.object({
  id: z.string().uuid(), conversation_id: z.string().uuid(), author_agent_id: z.string().uuid().nullable(),
  author_name: z.string().nullable(), agent_token: messageAgentTokenSchema.nullable(), agent_request_id: z.string().uuid().nullable(),
  // An older server sends no draft id.
  ai_draft_id: z.string().uuid().nullable().catch(null),
  ai_draft_outcome: z.enum(['sent_unchanged', 'sent_edited']).nullable().catch(null),
  kind: z.enum(['incoming', 'outgoing', 'note']), body: z.string(),
  body_html: z.string().nullable(), origin: messageOriginSchema.nullable(),
  attachments: z.array(attachmentSchema), cc: z.array(z.string()), created_at: z.string().datetime(),
  delivery: messageDeliverySchema.nullable(), email: incomingEmailSchema.nullable(), reply_number: z.number().int().positive().nullable(),
  // A customer message moved in from another contact's conversation; an older server sends nothing.
  other_contact_id: z.string().uuid().nullable().optional(),
  intake_reply_kind: z.enum(['substantive', 'acknowledgement']).nullable().optional(),
  notice_kind: z.string().nullable().optional(),
}) satisfies z.ZodType<Generated<'Message'>>;
export const emailDnsRecordSchema = z.object({
  record: z.string(), type: z.string(), name: z.string(), value: z.string(), priority: z.number().int().nullable(), status: z.string(),
}) satisfies z.ZodType<Generated<'EmailDNSRecord'>>;
export const emailChannelSchema = z.object({
  channel_id: z.string().uuid().optional(), support_address: z.string().email(), sender_name: z.string(), secret_set: z.boolean(), secret_rotated_at: nullableDate,
  domain: z.string(), domain_verified_at: nullableDate, dns_record_name: z.string(), dns_record_value: z.string(),
  dns_records: z.array(emailDnsRecordSchema), dns_records_expired_at: nullableDate, created_at: z.string().datetime(), updated_at: z.string().datetime(),
}) satisfies z.ZodType<Generated<'EmailChannel'>>;
export const mailboxConnectionSchema = z.object({
  provider: z.enum(['google', 'microsoft']), address: z.string().email(), status: z.enum(['connected', 'failed']),
  error_code: z.enum(['revoked', 'permission', 'mailbox_unavailable']).nullable(), error: z.string().nullable(),
  failed_at: nullableDate, connected_at: z.string().datetime(),
}) satisfies z.ZodType<Generated<'MailboxConnection'>>;
export const gmailSettingsSchema = z.object({
  available: z.boolean(),
  missing_settings: z.array(z.enum(['GOOGLE_OAUTH_CLIENT_ID', 'GOOGLE_OAUTH_CLIENT_SECRET', 'MAILBOX_TOKEN_KEYS'])),
  off_reason: featureOffReason,
  redirect_uri: z.string().url(), connection: mailboxConnectionSchema.nullable(), address_matches: z.boolean().optional(),
}) satisfies z.ZodType<Generated<'GmailSettings'>>;
export const microsoftSettingsSchema = z.object({
  available: z.boolean(),
  missing_settings: z.array(z.enum(['MICROSOFT_OAUTH_CLIENT_ID', 'MICROSOFT_OAUTH_CLIENT_SECRET', 'MAILBOX_TOKEN_KEYS'])),
  off_reason: featureOffReason,
  redirect_uri: z.string().url(), admin_consent_redirect_uri: z.string().url(), admin_consent_url: z.string().url().nullable(),
  large_attachment_bytes: z.number().int().nonnegative(), server_credentials_refused: z.boolean(),
  connection: mailboxConnectionSchema.nullable(), address_matches: z.boolean().optional(),
}) satisfies z.ZodType<Generated<'MicrosoftSettings'>>;
export const consentLinkSchema = z.object({ url: z.string().url() }) satisfies z.ZodType<JsonResponse<'connectGmail', 200>>;
// How many live channels each type has: the inbox says which mailbox a reply leaves from once there are two.
// The whole list and its schemas load with Settings, not with the inbox.
export const channelCountsSchema = z.object({ types: z.array(z.object({ type: z.string(), count: z.number() })) });
export const emailChannelSettingsSchema = z.object({
  channel: emailChannelSchema.nullable(), inbound_url: z.string().url(), gmail: gmailSettingsSchema, microsoft: microsoftSettingsSchema,
  from_policy: z.enum(['verified', 'any']), sending_from: z.string().nullable(), hosted_address: z.string().nullable(),
  domain_verification: z.enum(['txt', 'provider']),
  sending_pause: z.object({ paused_at: z.string().datetime(), reason: z.string() }).nullable(),
  smtp: z.object({ configured: z.boolean(), host: z.string(), port: z.number().int(), starttls: z.boolean(), authenticated: z.boolean() }),
}) satisfies z.ZodType<Generated<'EmailChannelSettings'>>;
export const webFormSettingsSchema = z.object({
  channel_id: z.string().uuid().optional(), enabled: z.boolean(), allowed_origins: z.array(z.string()), submit_path: z.string(), legacy_slug: z.boolean().optional(),
}) satisfies z.ZodType<Generated<'WebFormSettings'>>;
export const testMessageResultSchema = z.object({ conversation_id: z.string(), message_id: z.string(), sent_to: z.string() }) satisfies z.ZodType<JsonResponse<'sendEmailTestMessage', 201>>;
export const inboundSecretSchema = z.object({ inbound_secret: z.string().min(1), inbound_url: z.string().url() }) satisfies z.ZodType<JsonResponse<'rotateEmailInboundSecret', 200>>;
export const sessionSchema = z.object({
  id: z.string().uuid(), agent_id: z.string().uuid(), agent_email: z.string().email(), label: z.string(),
  scope: tokenScopeSchema.nullable(), permissions: z.array(z.string()), current: z.boolean(),
  created_at: z.string().datetime(), last_seen_at: z.string().datetime(), expires_at: z.string().datetime(), revoked_at: nullableDate,
}) satisfies z.ZodType<Generated<'Session'>>;
export const inviteSchema = z.object({
  id: z.string().uuid(), email: z.string().email(), role_ceiling: roleSchema, created_by_email: z.string().email(),
  created_at: z.string().datetime(), expires_at: z.string().datetime(), used_at: nullableDate, revoked_at: nullableDate,
}) satisfies z.ZodType<Generated<'Invite'>>;
export const errorSchema = z.object({ error: z.object({ code: z.string(), message: z.string() }) }) satisfies z.ZodType<Generated<'Error'>>;
export const redeemedSchema = z.object({ token: z.string().optional(), agent: agentSchema, workspace: workspaceSchema, continue_url: z.string().url().optional() }) satisfies z.ZodType<Generated<'Redeemed'>>;
export const onboardingGoalSchema = z.enum(['leave_helpdesk', 'shared_inbox', 'lower_cost', 'ai_agents', 'other']) satisfies z.ZodType<Generated<'OnboardingGoal'>>;
const setupStepKeys = ['email', 'import', 'invite', 'first_reply'] as const;
export type SetupStepKey = (typeof setupStepKeys)[number];
export const setupProgressSchema = z.object({
  steps: z.array(z.object({ key: z.enum(setupStepKeys), state: z.enum(['none', 'waiting', 'working', 'needs_attention', 'running', 'done', 'alone', 'invited', 'joined', 'sent']), detail: z.string().nullable() })),
  complete: z.boolean(),
  recommended: z.enum(['email', 'import', 'invite']).nullable(),
}) satisfies z.ZodType<Generated<'SetupProgress'>>;
export type SetupProgress = z.infer<typeof setupProgressSchema>;
export const setupGuideHiddenSchema = z.object({ hidden: z.boolean() }) satisfies z.ZodType<JsonResponse<'setSetupGuideHidden', 200>>;
export const meSchema = z.object({
  agent: agentSchema, workspace: workspaceSchema, timezone: z.string(), scope: tokenScopeSchema.nullable(), permissions: z.array(z.string()).catch([]),
  // An older server sends no goal; a goal this client does not know yet orders nothing.
  onboarding_goal: onboardingGoalSchema.nullable().catch(null),
  // An older server has no setup guide to hide.
  setup_guide_hidden: z.boolean().catch(false),
  // An older server offers no AI feature.
  ai_features: z.object({ triage: z.boolean(), draft: z.boolean() }).catch({ triage: false, draft: false }),
  plan: z.object({ plan: planSchema, trial_ends_at: z.string().datetime(), locked: z.boolean(), trial_extended: z.boolean(), agents_over: z.number().int(), seat_paused: z.boolean() }).nullable(),
}) satisfies z.ZodType<Generated<'Me'>>;
export const sessionListSchema = z.object({ items: z.array(sessionSchema) }) satisfies z.ZodType<JsonResponse<'listSessions', 200>>;
// The raw token is in this answer only; the client keeps it in no cache. A repeat of the same press (200)
// answers the session without it.
export const agentTokenSchema = z.object({ token: z.string().startsWith('cdk_agent_').optional(), session: sessionSchema }) satisfies z.ZodType<JsonResponse<'createAgentToken', 201> | JsonResponse<'createAgentToken', 200>>;
export const inviteListSchema = z.object({ items: z.array(inviteSchema) }) satisfies z.ZodType<JsonResponse<'listInvites', 200>>;
// A repeat of the same press (200) answers the invite without its one-time link.
export const inviteCreatedSchema = z.object({ invite: inviteSchema, url: z.string().url().optional() }) satisfies z.ZodType<JsonResponse<'createInvite', 201> | JsonResponse<'createInvite', 200>>;
export const conversationPageSchema = z.object({ items: z.array(conversationSchema), page: z.number(), page_size: z.number() }) satisfies z.ZodType<Generated<'ConversationPage'>>;
export const conversationViewerSchema = z.object({
  agent_id: z.string().uuid(), name: z.string(), writing: z.boolean(), writing_since: z.string().datetime().nullable(), edited_at: z.string().datetime().nullable(),
  agent_token: messageAgentTokenSchema.nullable(),
}) satisfies z.ZodType<Generated<'ConversationViewer'>>;
export const inboxActivitySchema = z.object({
  now: z.string().datetime(),
  new_conversations: z.array(z.object({ id: z.string().uuid(), subject: z.string(), contact_name: z.string(), created_at: z.string().datetime() })),
  assigned_to_me: z.array(z.object({ id: z.string().uuid(), subject: z.string(), contact_name: z.string(), assigned_at: z.string().datetime(), by_ai: z.boolean().catch(false) })),
  updated: z.array(z.object({ id: z.string().uuid(), subject: z.string(), contact_name: z.string(), assignee_id: z.string().uuid().nullable(), updated_at: z.string().datetime(), incoming_at: z.string().datetime().nullable() })),
  viewing: z.array(z.object({ conversation_id: z.string().uuid(), viewers: z.array(conversationViewerSchema) })),
}) satisfies z.ZodType<Generated<'InboxActivity'>>;
export const messageListSchema = z.object({ items: z.array(messageSchema) }) satisfies z.ZodType<JsonResponse<'listMessages', 200>>;
export const contactListSchema = z.object({ items: z.array(contactSchema), has_more: z.boolean().optional() }) satisfies z.ZodType<JsonResponse<'listContacts', 200>>;
export const agentListSchema = z.object({ items: z.array(teamMemberSchema) }) satisfies z.ZodType<JsonResponse<'listAgents', 200>>;
export const timeEntryListSchema = z.object({ items: z.array(timeEntrySchema) }) satisfies z.ZodType<JsonResponse<'listTimeEntries', 200>>;
export const savedReplyListSchema = z.object({ items: z.array(savedReplySchema) }) satisfies z.ZodType<JsonResponse<'listSavedReplies', 200>>;
export const trashedSavedReplyListSchema = z.object({ items: z.array(trashedSavedReplySchema), next_cursor: z.string().nullable() }) satisfies z.ZodType<JsonResponse<'listTrashedSavedReplies', 200>>;
export const savedReplyVersionListSchema = z.object({ items: z.array(savedReplyVersionSchema), next_before_version: z.number().int().nullable() }) satisfies z.ZodType<JsonResponse<'listSavedReplyVersions', 200>>;
export const bulkUpdateResultSchema = z.object({ updated: z.number().int(), told: z.number().int().optional() }) satisfies z.ZodType<JsonResponse<'bulkUpdateConversations', 200>>;
export const tagListSchema = z.object({ items: z.array(tagSchema) }) satisfies z.ZodType<JsonResponse<'listTags', 200>>;

export type Agent = Generated<'Agent'>;
export type TeamMember = Generated<'TeamMember'>;
export type Billing = Generated<'Billing'>;
export type CheckoutPlan = Generated<'CheckoutPlan'>;
export type Me = Generated<'Me'>;
export type PublicConfig = z.infer<typeof publicConfigSchema>;
export type OnboardingGoal = Generated<'OnboardingGoal'>;
export type Attachment = Generated<'Attachment'>;
export type ConversationOrigin = Generated<'ConversationOrigin'>;
export type MessageOrigin = Generated<'MessageOrigin'>;
export type TimeEntry = Generated<'TimeEntry'>;
export type ConversationViewer = Generated<'ConversationViewer'>;
export type WorkspaceChoice = Generated<'WorkspaceChoice'>;
export type Conversation = Generated<'Conversation'>;
export type Contact = Generated<'Contact'>;
export type ContactDetail = Generated<'ContactDetail'>;
export type ContactMerge = Generated<'ContactMerge'>;
export type Invite = Generated<'Invite'>;
export type Message = Generated<'Message'>;
export type EmailChannelSettings = Generated<'EmailChannelSettings'>;
export type EmailDnsRecord = Generated<'EmailDNSRecord'>;
export type GmailSettings = Generated<'GmailSettings'>;
export type MicrosoftSettings = Generated<'MicrosoftSettings'>;
export type MailboxConnection = Generated<'MailboxConnection'>;
export type WebFormSettings = Generated<'WebFormSettings'>;
export type Session = Generated<'Session'>;
export type AgentToken = Generated<'AgentToken'>;
export type Tag = Generated<'Tag'>;
export type SavedReply = Generated<'SavedReply'>;
export type SavedView = Generated<'SavedView'>;
export type ConversationFilters = Generated<'ConversationFilters'>;
export type SavedReplyChange = Generated<'SavedReplyChange'>;
export type TrashedSavedReply = Generated<'TrashedSavedReply'>;
export type SavedReplyVersion = Generated<'SavedReplyVersion'>;
export type SavedReplyVersionPage = z.infer<typeof savedReplyVersionListSchema>;
export type TrashedSavedReplyPage = z.infer<typeof trashedSavedReplyListSchema>;

export const agentRequestStatusSchema = z.enum(['pending', 'approved', 'failed', 'rejected', 'cancelled', 'expired']);
// A stored call body and a call's answer are any JSON value.
const anyJson = z.unknown() as z.ZodType<Generated<'AgentRequest'>['body']>;
export const agentRequestSchema = z.object({
  id: z.string().uuid(), kind: z.enum(['reply_draft', 'action']), status: agentRequestStatusSchema,
  method: z.enum(['GET', 'POST', 'PUT', 'PATCH', 'DELETE']), route: z.string(), path: z.string(), body: anyJson, edited_body: anyJson,
  approvable_by: z.enum(['agent', 'admin', 'owner']), summary: z.string(),
  conversation_id: z.string().uuid().nullable(),
  // What approving a status change would tell customers, one per chat channel.
  notices: z.array(z.object({ channel_id: z.string().uuid(), channel_name: z.string(), example_ref: z.string(), text: z.string(), told: z.number().int() })).optional(),
  saved_reply: z.object({
    id: z.string().uuid(),
    before: z.object({ title: z.string(), body: z.string(), updated_at: z.string().datetime() }).nullable(),
    current: z.object({ title: z.string(), body: z.string(), updated_at: z.string().datetime(), deleted_at: nullableDate }).nullable(),
    restores: z.object({ version: z.number().int(), title: z.string(), body: z.string() }).nullable(),
  }).nullable(),
  requested_by: z.object({
    token_id: z.string().uuid().nullable(), token_label: z.string(), token_scope: tokenScopeSchema, token_permissions: z.array(z.string()).catch([]), token_revoked: z.boolean(),
    agent_id: z.string().uuid().nullable(), agent_name: z.string().nullable(),
  }),
  decided_by: z.object({ agent_id: z.string().uuid().nullable(), name: z.string() }).nullable(),
  decided_at: nullableDate, reason: z.string().nullable(), result_status: z.number().int().nullable(), result: anyJson,
  result_note: z.string().nullable(), created_at: z.string().datetime(), expires_at: z.string().datetime(),
}) satisfies z.ZodType<Generated<'AgentRequest'>>;
export type AgentRequest = z.infer<typeof agentRequestSchema>;
export const agentRequestListSchema = z.object({ items: z.array(agentRequestSchema) }) satisfies z.ZodType<JsonResponse<'listAgentRequests', 200>>;
export const approvedAgentRequestSchema = z.object({ request: agentRequestSchema, result: anyJson }) satisfies z.ZodType<JsonResponse<'approveAgentRequest', 200>>;
