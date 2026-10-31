import { z } from 'zod';
import { request, requestNoContent } from './client';
import type { RequestBody } from './client';
import { consentLinkSchema, emailChannelSchema, emailChannelSettingsSchema, inboundSecretSchema, testMessageResultSchema, webFormSettingsSchema } from './schemas';
import type { Generated } from './schemas';
import { chatItemSchema } from './intake';

// Channels load with their settings pages, a lazy part, so their schemas and calls are not in the first bundle.

const nullableDate = z.string().datetime().nullable();
// A type the server's registry names; a type this app has no screens for is listed by its name (channels/registry.ts).
export const channelTypeSchema = z.string().regex(/^[a-z_]{1,40}$/) satisfies z.ZodType<Generated<'ChannelType'>>;
const channelStateSchema = z.object({
  state: z.enum(['working', 'waiting', 'off', 'needs_attention', 'not_connected', 'removed']), reason_code: z.string().nullable(), message: z.string().nullable(),
}) satisfies z.ZodType<Generated<'ChannelState'>>;
export const channelSchema = z.object({
  id: z.string().uuid(), type: channelTypeSchema, name: z.string(), enabled: z.boolean(), status: channelStateSchema, summary: z.string(),
  capabilities: z.array(z.string()), held_count: z.number().int().nonnegative(), unsent_count: z.number().int().nonnegative(),
  last_inbound_at: nullableDate, is_default: z.boolean().optional(), created_at: z.string().datetime(), updated_at: z.string().datetime(),
  deleted_at: z.string().datetime().optional(),
}) satisfies z.ZodType<Generated<'Channel'>>;
// The configuration is the type's own schema; the type's panels read it, the shared pages never do. A type
// with no screens here answers its own object, which nothing reads.
export const helpChatConfigSchema = z.object({
  channel_id: z.string().uuid(), enabled: z.boolean(), greeting: z.string(), suggestions: z.array(z.string()), instructions: z.string(),
  widget_origins: z.array(z.string()),
}) satisfies z.ZodType<Generated<'HelpChatChannelConfig'>>;
export type HelpChatConfig = z.infer<typeof helpChatConfigSchema>;
export const zaloConfigSchema = z.object({
  oa_id: z.string(), app_id: z.string(), staff_user_ids: z.array(z.string()), groups: z.record(z.string(), z.string()), webhook_path: z.string(),
  // The four secrets, in order: whether each is set, its last four characters, when it was last written.
  credentials: z.array(z.object({ name: z.enum(['app_secret', 'oa_secret_key', 'access_token', 'refresh_token']), hint: z.string().nullable(), set_at: nullableDate })),
}) satisfies z.ZodType<Generated<'ZaloChannelConfig'>>;
export type ZaloConfig = z.infer<typeof zaloConfigSchema>;
const channelConfigSchema = z.union([emailChannelSchema, webFormSettingsSchema, helpChatConfigSchema, zaloConfigSchema, z.record(z.string(), z.unknown())]) satisfies z.ZodType<Generated<'ChannelConfig'>>;
// settings is what the channel stores over its type's defaults (a change replaces it whole); effective_settings the rules in force.
const settingsSchema = z.record(z.string(), z.unknown());
export const channelDetailSchema = channelSchema.extend({ config: channelConfigSchema, settings: settingsSchema.optional(), effective_settings: settingsSchema.optional() }) satisfies z.ZodType<Generated<'ChannelDetail'>>;
const channelCredentialSchema = z.object({ name: z.string(), hint: z.string().nullable() }) satisfies z.ZodType<Generated<'ChannelCredential'>>;
const chatThreadSchema = z.object({
  id: z.string().uuid(), channel_id: z.string().uuid(), kind: z.enum(['direct', 'group']), external_id: z.string(), name: z.string().nullable(),
  contact_id: z.string().uuid().nullable(), opens_conversations: z.boolean(), last_item_at: nullableDate, last_item: chatItemSchema.nullable(),
}) satisfies z.ZodType<Generated<'ChatThread'>>;
const chatThreadPageSchema = z.object({ items: z.array(chatThreadSchema), next_cursor: z.string().nullable() }) satisfies z.ZodType<Generated<'ChatThreadPage'>>;
export type ChatThread = z.infer<typeof chatThreadSchema>;
const channelTypeInfoSchema = z.object({
  type: channelTypeSchema, available: z.boolean(), count: z.number().int().nonnegative(), default_channel_id: z.string().uuid().nullable(),
}) satisfies z.ZodType<Generated<'ChannelTypeInfo'>>;
export const channelListSchema = z.object({ items: z.array(channelSchema), types: z.array(channelTypeInfoSchema) }) satisfies z.ZodType<Generated<'ChannelList'>>;
export const channelEventSchema = z.object({
  id: z.string().uuid(), at: z.string().datetime(),
  kind: z.enum(['added', 'renamed', 'enabled', 'disabled', 'config_changed', 'credential_changed', 'removed', 'held_replayed', 'held_discarded', 'default_changed', 'ai_changed']),
  channel_id: z.string().uuid(), channel_name: z.string(), channel_type: z.string(),
  actor: z.object({ agent_id: z.string().uuid().nullable(), name: z.string().nullable(), token_label: z.string().nullable() }),
  fields: z.array(z.string()).optional(), count: z.number().int().nullable().optional(),
}) satisfies z.ZodType<Generated<'ChannelEvent'>>;
const channelEventPageSchema = z.object({ items: z.array(channelEventSchema), next_cursor: z.string().nullable() }) satisfies z.ZodType<Generated<'ChannelEventPage'>>;
const heldReplaySchema = z.object({
  results: z.array(z.object({ status: z.string(), conversation_id: z.string().uuid().nullable(), message_id: z.string().uuid().nullable() })),
}) satisfies z.ZodType<Generated<'HeldReplayResult'>>;
const heldDiscardSchema = z.object({ discarded: z.number().int().nonnegative() }) satisfies z.ZodType<Generated<'HeldDiscardResult'>>;
const forwardingConfirmationSchema = z.object({
  code: z.string(), confirm_url: z.string().url().nullable(), received_at: z.string().datetime(), conversation_id: z.string().uuid(),
}) satisfies z.ZodType<Generated<'ForwardingConfirmation'>>;
// 204: no body.
const removedSchema = z.undefined();

export type ChannelType = z.infer<typeof channelTypeSchema>;
export type Channel = z.infer<typeof channelSchema>;
export type ChannelDetail = z.infer<typeof channelDetailSchema>;
export type ChannelList = z.infer<typeof channelListSchema>;
export type ChannelTypeInfo = ChannelList['types'][number];
export type ChannelEvent = z.infer<typeof channelEventSchema>;
export type ForwardingConfirmation = z.infer<typeof forwardingConfirmationSchema>;
export type ChannelCreate = RequestBody<'createChannel'>;
export type ChannelChange = RequestBody<'updateChannel'>;

const json = <Operation extends 'createChannel' | 'updateChannel' | 'replayHeldChannelMessages' | 'discardHeldChannelMessages'>(value: RequestBody<Operation>): string => JSON.stringify(value);

export const channelsApi = {
  list: () => request('/api/v1/channels', channelListSchema),
  // Removed channels too, last: they still name the conversations they carried.
  listAll: () => request('/api/v1/channels?include_removed=true', channelListSchema),
  get: (id: string) => request(`/api/v1/channels/${id}`, channelDetailSchema),
  create: (input: ChannelCreate, key: string) => request('/api/v1/channels', channelDetailSchema, { method: 'POST', headers: { 'Idempotency-Key': key }, body: json<'createChannel'>(input) }),
  update: (id: string, change: ChannelChange) => request(`/api/v1/channels/${id}`, channelDetailSchema, { method: 'PATCH', body: json<'updateChannel'>(change) }),
  remove: (id: string) => request(`/api/v1/channels/${id}`, removedSchema, { method: 'DELETE' }),
  events: (channelID?: string, cursor?: string) => {
    const query = new URLSearchParams({ ...(channelID ? { channel_id: channelID } : {}), ...(cursor ? { cursor } : {}) }).toString();
    return request(`/api/v1/channels/events${query ? `?${query}` : ''}`, channelEventPageSchema);
  },
  replayHeld: (id: string, conversationID?: string) => request(`/api/v1/channels/${id}/held/replay`, heldReplaySchema,
    { method: 'POST', body: json<'replayHeldChannelMessages'>(conversationID ? { conversation_id: conversationID } : {}) }),
  discardHeld: (id: string) => request(`/api/v1/channels/${id}/held/discard`, heldDiscardSchema, { method: 'POST', body: json<'discardHeldChannelMessages'>({}) }),
  // A secret the type declares, one at a time; only its last four characters come back.
  setCredential: (id: string, name: string, value: string) => request(`/api/v1/channels/${id}/credentials`, channelCredentialSchema,
    { method: 'PUT', body: JSON.stringify({ name, value } satisfies RequestBody<'setChannelCredential'>) }),
  // A chat channel's places (one-to-one chats, groups), most recently active first.
  threads: (id: string, kind?: 'direct' | 'group', cursor?: string) => {
    const query = new URLSearchParams({ ...(kind ? { kind } : {}), ...(cursor ? { cursor } : {}) }).toString();
    return request(`/api/v1/channels/${id}/threads${query ? `?${query}` : ''}`, chatThreadPageSchema);
  },
  // One email channel of several: its settings and its own actions.
  email: (id: string) => request(`/api/v1/channels/${id}/email`, emailChannelSettingsSchema),
  emailSecret: (id: string) => request(`/api/v1/channels/${id}/email/secret`, inboundSecretSchema, { method: 'POST' }),
  emailTestMessage: (id: string, key: string) => request(`/api/v1/channels/${id}/email/test-message`, testMessageResultSchema,
    { method: 'POST', headers: { 'Idempotency-Key': key } }),
  // What Gmail sent to confirm forwarding to this channel in the last 24 hours; 404 while nothing came.
  emailForwardingConfirmation: (id: string) => request(`/api/v1/channels/${id}/email/forwarding-confirmation`, forwardingConfirmationSchema),
  emailVerifyDomain: (id: string) => request(`/api/v1/channels/${id}/email/domain-verification`, emailChannelSettingsSchema, { method: 'POST' }),
  emailConnectGmail: (id: string) => request(`/api/v1/channels/${id}/email/gmail`, consentLinkSchema, { method: 'POST' }),
  emailDisconnectGmail: (id: string) => requestNoContent(`/api/v1/channels/${id}/email/gmail`, { method: 'DELETE' }),
  emailConnectMicrosoft: (id: string) => request(`/api/v1/channels/${id}/email/microsoft`, consentLinkSchema, { method: 'POST' }),
  emailDisconnectMicrosoft: (id: string) => requestNoContent(`/api/v1/channels/${id}/email/microsoft`, { method: 'DELETE' }),
};
