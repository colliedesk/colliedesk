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
}) satisfies z.ZodType<Generated<'PublicConfig'>>;
export const agentSchema = z.object({
  id: z.string().uuid(), email: z.string().email(), display_name: z.string(),
  role: roleSchema, timezone_override: z.string().nullable(),
}) satisfies z.ZodType<Generated<'Agent'>>;
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
  id: z.string().uuid(), contact_id: z.string().uuid(), subject: z.string(),
  status: statusSchema, priority: prioritySchema, assignee_id: z.string().uuid().nullable(),
  tags: z.array(tagSchema), origin: conversationOriginSchema.nullable(), created_at: z.string().datetime(), updated_at: z.string().datetime(),
}) satisfies z.ZodType<Generated<'Conversation'>>;
export const contactDetailSchema = contactSchema.extend({ conversations: z.array(conversationSchema) }) satisfies z.ZodType<Generated<'ContactDetail'>>;
export const attachmentSchema = z.object({
  id: z.string().uuid(), message_id: z.string().uuid(), filename: z.string(), content_type: z.string(),
  size: z.number().int().nonnegative(), sha256: z.string().regex(/^[0-9a-f]{64}$/), created_at: z.string().datetime(),
}) satisfies z.ZodType<Generated<'Attachment'>>;
export const messageSchema = z.object({
  id: z.string().uuid(), conversation_id: z.string().uuid(), author_agent_id: z.string().uuid().nullable(),
  author_name: z.string().nullable(), kind: z.enum(['incoming', 'outgoing', 'note']), body: z.string(),
  body_html: z.string().nullable(), origin: messageOriginSchema.nullable(),
  attachments: z.array(attachmentSchema), created_at: z.string().datetime(),
}) satisfies z.ZodType<Generated<'Message'>>;
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
export const meSchema = z.object({ agent: agentSchema, workspace: workspaceSchema, timezone: z.string() }) satisfies z.ZodType<Generated<'Me'>>;
export const sessionListSchema = z.object({ items: z.array(sessionSchema) }) satisfies z.ZodType<JsonResponse<'listSessions', 200>>;
export const inviteListSchema = z.object({ items: z.array(inviteSchema) }) satisfies z.ZodType<JsonResponse<'listInvites', 200>>;
export const inviteCreatedSchema = z.object({ invite: inviteSchema, url: z.string().url() }) satisfies z.ZodType<JsonResponse<'createInvite', 201>>;
export const conversationPageSchema = z.object({ items: z.array(conversationSchema), page: z.number(), page_size: z.number() }) satisfies z.ZodType<Generated<'ConversationPage'>>;
export const messageListSchema = z.object({ items: z.array(messageSchema) }) satisfies z.ZodType<JsonResponse<'listMessages', 200>>;
export const contactListSchema = z.object({ items: z.array(contactSchema) }) satisfies z.ZodType<JsonResponse<'listContacts', 200>>;
export const agentListSchema = z.object({ items: z.array(agentSchema) }) satisfies z.ZodType<JsonResponse<'listAgents', 200>>;
export const timeEntryListSchema = z.object({ items: z.array(timeEntrySchema) }) satisfies z.ZodType<JsonResponse<'listTimeEntries', 200>>;
export const tagListSchema = z.object({ items: z.array(tagSchema) }) satisfies z.ZodType<JsonResponse<'listTags', 200>>;

export type Agent = Generated<'Agent'>;
export type Attachment = Generated<'Attachment'>;
export type ConversationOrigin = Generated<'ConversationOrigin'>;
export type MessageOrigin = Generated<'MessageOrigin'>;
export type TimeEntry = Generated<'TimeEntry'>;
export type WorkspaceChoice = Generated<'WorkspaceChoice'>;
export type Conversation = Generated<'Conversation'>;
export type Invite = Generated<'Invite'>;
export type Message = Generated<'Message'>;
export type Session = Generated<'Session'>;
export type Tag = Generated<'Tag'>;
