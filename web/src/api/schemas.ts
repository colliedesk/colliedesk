import { z } from 'zod';
import type { components, operations } from './generated';

type Generated<Name extends keyof components['schemas']> = components['schemas'][Name];
type JsonResponse<Operation extends keyof operations, Status extends keyof operations[Operation]['responses']> =
  operations[Operation]['responses'][Status] extends { content: { 'application/json': infer Body } } ? Body : never;

const nullableDate = z.string().datetime().nullable();
export const roleSchema = z.enum(['admin', 'agent']);
export const statusSchema = z.enum(['open', 'pending', 'resolved', 'closed']);
export const prioritySchema = z.enum(['low', 'normal', 'high', 'urgent']);

export const workspaceSchema = z.object({
  id: z.string().uuid(), slug: z.string(), name: z.string(), timezone: z.string(),
}) satisfies z.ZodType<Generated<'Workspace'>>;
export const workspaceChoiceSchema = z.object({ slug: z.string(), name: z.string() }) satisfies z.ZodType<Generated<'WorkspaceChoice'>>;
export const cloudWorkspaceChoicesSchema = z.object({ workspaces: z.array(workspaceChoiceSchema).min(2) }) satisfies z.ZodType<Generated<'CloudWorkspaceChoices'>>;
export const publicConfigSchema = z.object({
  cloud_sign_in: z.object({ enabled: z.boolean(), publishable_key: z.string().nullable() }),
  billing: z.object({ enabled: z.boolean() }),
  signup: z.object({ enabled: z.boolean() }),
  limits: z.object({ outgoing_attachments_bytes: z.number().int(), outgoing_attachments_files: z.number().int() }),
}) satisfies z.ZodType<Generated<'PublicConfig'>>;
export const checkoutPlanSchema = z.enum(['team_monthly', 'team_yearly', 'founding']);
export const planSchema = z.enum(['trial', 'team', 'founding', 'none']);
export const billingSchema = z.object({
  plan: planSchema, trial_ends_at: z.string().datetime(), locked: z.boolean(),
  status: z.enum(['none', 'incomplete', 'incomplete_expired', 'trialing', 'active', 'past_due', 'canceled', 'unpaid', 'paused', 'unknown']),
  current_period_end: nullableDate, cancel_at_period_end: z.boolean(), active_subscriptions: z.number().int(),
  complimentary: z.boolean(), complimentary_until: nullableDate,
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
export const savedReplySchema = z.object({
  id: z.string().uuid(), title: z.string(), body: z.string(), created_by_agent_id: z.string().uuid().nullable(),
  created_at: z.string().datetime(), updated_at: z.string().datetime(),
}) satisfies z.ZodType<Generated<'SavedReply'>>;
export const tagSchema = z.object({ id: z.string().uuid(), name: z.string(), color: z.string() }) satisfies z.ZodType<Generated<'Tag'>>;
export const contactSchema = z.object({
  id: z.string().uuid(), name: z.string(), email: z.string().email(), organization: z.string().nullable(),
  created_at: z.string().datetime(), updated_at: z.string().datetime(),
}) satisfies z.ZodType<Generated<'Contact'>>;
const stringList = z.array(z.string());
export const conversationOriginSchema = z.object({
  system: z.string(), external_id: z.string(), type: z.string().nullable(), source: z.string().nullable(),
  due_at: nullableDate, first_response_due_at: nullableDate, cc: stringList, forwarded_to: stringList,
}) satisfies z.ZodType<Generated<'ConversationOrigin'>>;
export const messageOriginSchema = z.object({
  system: z.string(), external_id: z.string(),
  kind: z.enum(['description', 'reply', 'incoming', 'private_note', 'public_note', 'forward', 'forwarded_email', 'other']),
  from: z.string().nullable(), to: stringList, cc: stringList, bcc: stringList,
}) satisfies z.ZodType<Generated<'MessageOrigin'>>;
export const timeEntrySchema = z.object({
  id: z.string().uuid(), conversation_id: z.string().uuid(), agent_id: z.string().uuid().nullable(), agent_name: z.string(),
  minutes: z.number().int().nonnegative(), note: z.string(), billable: z.boolean(), executed_at: z.string().datetime(),
}) satisfies z.ZodType<Generated<'TimeEntry'>>;
export const conversationSchema = z.object({
  id: z.string().uuid(), contact_id: z.string().uuid(), contact_name: z.string(), subject: z.string(),
  status: statusSchema, priority: prioritySchema, assignee_id: z.string().uuid().nullable(),
  tags: z.array(tagSchema), origin: conversationOriginSchema.nullable(), created_at: z.string().datetime(), updated_at: z.string().datetime(),
}) satisfies z.ZodType<Generated<'Conversation'>>;
export const contactDetailSchema = contactSchema.extend({ conversations: z.array(conversationSchema) }) satisfies z.ZodType<Generated<'ContactDetail'>>;
export const deliveryStatusSchema = z.enum(['queued', 'sent', 'failed']);
export const messageDeliverySchema = z.object({
  status: deliveryStatusSchema, attempts: z.number().int().min(0), last_error: z.string().nullable(), sent_at: nullableDate,
}) satisfies z.ZodType<Generated<'MessageDelivery'>>;
export const emailAuthenticationSchema = z.enum(['pass', 'fail', 'none']);
export const incomingEmailSchema = z.object({
  from: z.string(), authentication: emailAuthenticationSchema, via: z.enum(['platform', 'workspace', 'web_form']).optional(),
  attachments: z.array(z.object({ filename: z.string(), content_type: z.string() })),
  original_attachment_id: z.string().uuid().nullable(),
}) satisfies z.ZodType<Generated<'IncomingEmail'>>;
export const attachmentSchema = z.object({
  id: z.string().uuid(), message_id: z.string().uuid(), filename: z.string(), content_type: z.string(),
  size: z.number().int().nonnegative(), sha256: z.string().regex(/^[0-9a-f]{64}$/), created_at: z.string().datetime(),
}) satisfies z.ZodType<Generated<'Attachment'>>;
export const messageSchema = z.object({
  id: z.string().uuid(), conversation_id: z.string().uuid(), author_agent_id: z.string().uuid().nullable(),
  author_name: z.string().nullable(), kind: z.enum(['incoming', 'outgoing', 'note']), body: z.string(),
  body_html: z.string().nullable(), origin: messageOriginSchema.nullable(),
  attachments: z.array(attachmentSchema), created_at: z.string().datetime(),
  delivery: messageDeliverySchema.nullable(), email: incomingEmailSchema.nullable(),
}) satisfies z.ZodType<Generated<'Message'>>;
export const emailChannelSchema = z.object({
  support_address: z.string().email(), secret_set: z.boolean(), secret_rotated_at: nullableDate,
  domain: z.string(), domain_verified_at: nullableDate, dns_record_name: z.string(), dns_record_value: z.string(),
  created_at: z.string().datetime(), updated_at: z.string().datetime(),
}) satisfies z.ZodType<Generated<'EmailChannel'>>;
export const emailChannelSettingsSchema = z.object({
  channel: emailChannelSchema.nullable(), inbound_url: z.string().url(),
  from_policy: z.enum(['verified', 'any']), sending_from: z.string().nullable(), hosted_address: z.string().nullable(),
  smtp: z.object({ configured: z.boolean(), host: z.string(), port: z.number().int(), starttls: z.boolean(), authenticated: z.boolean() }),
}) satisfies z.ZodType<Generated<'EmailChannelSettings'>>;
export const webFormSettingsSchema = z.object({
  enabled: z.boolean(), allowed_origins: z.array(z.string()), submit_path: z.string(),
}) satisfies z.ZodType<Generated<'WebFormSettings'>>;
export const inboundSecretSchema = z.object({ inbound_secret: z.string().min(1), inbound_url: z.string().url() }) satisfies z.ZodType<JsonResponse<'rotateEmailInboundSecret', 200>>;
export const sessionSchema = z.object({
  id: z.string().uuid(), agent_id: z.string().uuid(), agent_email: z.string().email(), label: z.string(),
  created_at: z.string().datetime(), last_seen_at: z.string().datetime(), expires_at: z.string().datetime(), revoked_at: nullableDate,
}) satisfies z.ZodType<Generated<'Session'>>;
export const inviteSchema = z.object({
  id: z.string().uuid(), email: z.string().email(), role_ceiling: roleSchema, created_by_email: z.string().email(),
  created_at: z.string().datetime(), expires_at: z.string().datetime(), used_at: nullableDate, revoked_at: nullableDate,
}) satisfies z.ZodType<Generated<'Invite'>>;
export const errorSchema = z.object({ error: z.object({ code: z.string(), message: z.string() }) }) satisfies z.ZodType<Generated<'Error'>>;
export const redeemedSchema = z.object({ token: z.string().optional(), agent: agentSchema, workspace: workspaceSchema }) satisfies z.ZodType<Generated<'Redeemed'>>;
export const meSchema = z.object({
  agent: agentSchema, workspace: workspaceSchema, timezone: z.string(),
  plan: z.object({ plan: planSchema, trial_ends_at: z.string().datetime(), locked: z.boolean(), trial_extended: z.boolean() }).nullable(),
}) satisfies z.ZodType<Generated<'Me'>>;
export const sessionListSchema = z.object({ items: z.array(sessionSchema) }) satisfies z.ZodType<JsonResponse<'listSessions', 200>>;
export const inviteListSchema = z.object({ items: z.array(inviteSchema) }) satisfies z.ZodType<JsonResponse<'listInvites', 200>>;
export const inviteCreatedSchema = z.object({ invite: inviteSchema, url: z.string().url() }) satisfies z.ZodType<JsonResponse<'createInvite', 201>>;
export const conversationPageSchema = z.object({ items: z.array(conversationSchema), page: z.number(), page_size: z.number() }) satisfies z.ZodType<Generated<'ConversationPage'>>;
export const inboxActivitySchema = z.object({
  now: z.string().datetime(),
  new_conversations: z.array(z.object({ id: z.string().uuid(), subject: z.string(), contact_name: z.string(), created_at: z.string().datetime() })),
  assigned_to_me: z.array(z.object({ id: z.string().uuid(), subject: z.string(), contact_name: z.string(), assigned_at: z.string().datetime() })),
  updated: z.array(z.object({ id: z.string().uuid(), subject: z.string(), contact_name: z.string(), assignee_id: z.string().uuid().nullable(), updated_at: z.string().datetime(), incoming_at: z.string().datetime().nullable() })),
}) satisfies z.ZodType<Generated<'InboxActivity'>>;
export const messageListSchema = z.object({ items: z.array(messageSchema) }) satisfies z.ZodType<JsonResponse<'listMessages', 200>>;
export const contactListSchema = z.object({ items: z.array(contactSchema) }) satisfies z.ZodType<JsonResponse<'listContacts', 200>>;
export const agentListSchema = z.object({ items: z.array(teamMemberSchema) }) satisfies z.ZodType<JsonResponse<'listAgents', 200>>;
export const timeEntryListSchema = z.object({ items: z.array(timeEntrySchema) }) satisfies z.ZodType<JsonResponse<'listTimeEntries', 200>>;
export const savedReplyListSchema = z.object({ items: z.array(savedReplySchema) }) satisfies z.ZodType<JsonResponse<'listSavedReplies', 200>>;
export const bulkUpdateResultSchema = z.object({ updated: z.number().int() }) satisfies z.ZodType<JsonResponse<'bulkUpdateConversations', 200>>;
export const tagListSchema = z.object({ items: z.array(tagSchema) }) satisfies z.ZodType<JsonResponse<'listTags', 200>>;

export type Agent = Generated<'Agent'>;
export type TeamMember = Generated<'TeamMember'>;
export type Billing = Generated<'Billing'>;
export type CheckoutPlan = Generated<'CheckoutPlan'>;
export type Me = Generated<'Me'>;
export type Attachment = Generated<'Attachment'>;
export type ConversationOrigin = Generated<'ConversationOrigin'>;
export type MessageOrigin = Generated<'MessageOrigin'>;
export type TimeEntry = Generated<'TimeEntry'>;
export type WorkspaceChoice = Generated<'WorkspaceChoice'>;
export type Conversation = Generated<'Conversation'>;
export type Invite = Generated<'Invite'>;
export type Message = Generated<'Message'>;
export type EmailChannelSettings = Generated<'EmailChannelSettings'>;
export type WebFormSettings = Generated<'WebFormSettings'>;
export type Session = Generated<'Session'>;
export type Tag = Generated<'Tag'>;
export type SavedReply = Generated<'SavedReply'>;
