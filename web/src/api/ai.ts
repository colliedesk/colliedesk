import { z } from 'zod';
import { request, requestNoContent } from './client';
import type { RequestBody } from './client';
import type { Generated, JsonResponse } from './schemas';

// AI settings load with their settings page, a lazy part, so their schemas and calls are not in the first bundle.

const nullableDate = z.string().datetime().nullable();
export const aiSlotNameSchema = z.enum(['triage', 'draft', 'chat']) satisfies z.ZodType<Generated<'AISlotName'>>;
const kindSchema = z.enum(['anthropic', 'openai_compatible']) satisfies z.ZodType<Generated<'AIProviderKind'>>;
export const aiProviderSchema = z.object({
  id: z.string().uuid(), kind: kindSchema, label: z.string(), base_url: z.string().nullable(), auth_kind: z.enum(['api_key', 'none']),
  key_hint: z.string().nullable(), models: z.array(z.string()), check_status: z.enum(['ok', 'failing']), check_error_code: z.string().nullable(),
  checked_at: z.string().datetime(), created_by: z.string().uuid().nullable(), created_at: z.string().datetime(), updated_at: z.string().datetime(),
  used_by: z.array(aiSlotNameSchema),
}) satisfies z.ZodType<Generated<'AIProvider'>>;
export const aiSlotSchema = z.object({
  slot: aiSlotNameSchema, source: z.enum(['server', 'provider', 'none']), provider_id: z.string().uuid().nullable(), model: z.string().nullable(),
  effort: z.enum(['low', 'medium', 'high']), enabled: z.boolean(), automatic: z.boolean(), auto_parts: z.array(z.enum(['tags', 'priority', 'assignee'])),
  active: z.boolean(),
  off_reason: z.enum(['server_provider_missing', 'model_not_offered']).nullable(),
  consented_by: z.string().uuid().nullable(), consented_at: nullableDate, updated_by: z.string().uuid().nullable(), updated_at: nullableDate,
  // An older server has no chat slot and no limit.
  monthly_cap_micro_usd: z.number().int().nullable().catch(null),
}) satisfies z.ZodType<Generated<'AISlot'>>;
export const aiAutomaticSchema = z.object({
  per_day: z.number().int(), per_minute: z.number().int(), used_today: z.number().int(), paused_until: nullableDate,
  draft_per_day: z.number().int(), drafts_today: z.number().int(), drafts_paused_until: nullableDate, drafts_reserved_until: nullableDate,
}) satisfies z.ZodType<Generated<'AIAutomaticLimits'>>;
const aiAllowanceSchema = z.object({
  limit_micro_usd: z.number().int(), used_micro_usd: z.number().int(), resets_at: z.string().datetime(),
  drafts_left: z.number().int().nullable(),
}) satisfies z.ZodType<Generated<'AIAllowance'>>;
export const aiUsageSchema = z.object({
  month: z.string(), allowance: aiAllowanceSchema.nullable(),
  rows: z.array(z.object({
    feature: z.enum(['check', 'triage', 'draft', 'chat']), billed_to: z.enum(['server', 'workspace_key']), provider_label: z.string(),
    calls: z.number().int(), failed: z.number().int(), automatic: z.number().int(), input_tokens: z.number().int(), output_tokens: z.number().int(),
    cost_micro_usd: z.number().int().nullable(),
  })),
}) satisfies z.ZodType<Generated<'AIUsage'>>;
const aiEventSchema = z.object({
  id: z.string().uuid(), kind: z.enum(['provider_added', 'provider_changed', 'provider_key_changed', 'provider_removed', 'slot_changed', 'automatic_limits_changed', 'chat_deleted']),
  actor_agent_id: z.string().uuid().nullable(), actor_name: z.string().nullable(), via_token: z.boolean(),
  provider_id: z.string().uuid().nullable(), provider_label: z.string().nullable(), slot: aiSlotNameSchema.nullable(),
  detail: z.record(z.string(), z.unknown()), created_at: z.string().datetime(),
}) satisfies z.ZodType<Generated<'AIEvent'>>;
export const aiSettingsSchema = z.object({
  server: z.object({ kind: kindSchema, label: z.string(), models: z.record(z.string(), z.array(z.string())) }).nullable(),
  providers: z.array(aiProviderSchema), slots: z.array(aiSlotSchema), events: z.array(aiEventSchema),
  provider_limit: z.number().int(), can_store_keys: z.boolean(), allow_private_base_urls: z.boolean(),
  automatic: aiAutomaticSchema, allowance: aiAllowanceSchema.nullable(), auto_triage: z.object({ applied: z.number().int(), undone: z.number().int() }),
  auto_draft_estimate: z.object({ customer_messages: z.number().int(), draft_cost_micro_usd: z.number().int().nullable() }),
}) satisfies z.ZodType<JsonResponse<'getAISettings', 200>>;
const removedSchema = z.object({ turned_off: z.array(aiSlotNameSchema) }) satisfies z.ZodType<JsonResponse<'removeAIProvider', 200>>;
const aiModelListSchema = z.object({ models: z.array(z.string()) }) satisfies z.ZodType<JsonResponse<'listAIModels', 200>>;

const chatStatusSchema = z.enum(['open', 'ended', 'handed_off']) satisfies z.ZodType<Generated<'AIChatStatus'>>;
export const aiChatUsageSchema = z.object({
  month: z.string(), answers: z.number().int(), answers_today: z.number().int(), answer_limit: z.number().int().nullable(),
  day_answer_limit: z.number().int().nullable(), spent_micro_usd: z.number().int().nullable(), limit_micro_usd: z.number().int().nullable(),
  spent_percent: z.number().int().nullable(), chats: z.number().int(), handed_off: z.number().int(), rated_helpful: z.number().int(), rated: z.number().int(),
  articles: z.number().int(), resets_at: z.string().datetime(),
}) satisfies z.ZodType<Generated<'AIChatUsage'>>;
const chatSummaryShape = {
  id: z.string().uuid(), channel_id: z.string().uuid(), channel_name: z.string(), surface: z.enum(['help_center', 'article', 'widget']),
  status: chatStatusSchema, turns: z.number().int(), first_question: z.string().nullable(), not_helpful: z.boolean(),
  conversation_id: z.string().uuid().nullable(), created_at: z.string().datetime(), last_turn_at: z.string().datetime(), ended_at: nullableDate,
  kept_until: z.string().datetime(),
};
const aiChatSummarySchema = z.object(chatSummaryShape) satisfies z.ZodType<Generated<'AIChatSessionSummary'>>;
const aiChatPageSchema = z.object({ items: z.array(aiChatSummarySchema), next_cursor: z.string().nullable() }) satisfies z.ZodType<Generated<'AIChatSessionPage'>>;
const aiChatTurnSchema = z.object({
  id: z.string().uuid(), seq: z.number().int(), question: z.string().nullable(), answer: z.string().nullable(),
  status: z.enum(['streaming', 'answered', 'no_source', 'over_limit', 'failed']), error_code: z.string().nullable(), viewing_article_id: z.string().uuid().nullable(),
  sources: z.array(z.object({ ordinal: z.number().int(), article_id: z.string().uuid().nullable(), number: z.number().int(), version: z.number().int(),
    title: z.string().nullable(), folder: z.string().nullable() })),
  feedback: z.enum(['helpful', 'not_helpful']).nullable(), created_at: z.string().datetime(), finished_at: nullableDate,
}) satisfies z.ZodType<Generated<'AIChatTurn'>>;
export const aiChatSessionSchema = z.object({ ...chatSummaryShape, history_cut_at: nullableDate, transcript: z.array(aiChatTurnSchema) }) satisfies z.ZodType<Generated<'AIChatSession'>>;

export const aiChatPreviewSchema = z.object({
  answer: z.string(), status: z.enum(['answered', 'no_source', 'over_limit']),
  sources: z.array(z.object({ number: z.number().int(), title: z.string(), folder: z.string() })),
  redacted: z.boolean(), whole: z.boolean(), articles: z.number().int(), took_ms: z.number().int(),
}) satisfies z.ZodType<Generated<'AIChatPreview'>>;
export type AIChatPreview = z.infer<typeof aiChatPreviewSchema>;
export type AIChatUsage = z.infer<typeof aiChatUsageSchema>;
export type AIChatStatus = z.infer<typeof chatStatusSchema>;
export type AIChatSummary = z.infer<typeof aiChatSummarySchema>;
export type AIChatPage = z.infer<typeof aiChatPageSchema>;
export type AIChatSession = z.infer<typeof aiChatSessionSchema>;
export type AIChatTurn = z.infer<typeof aiChatTurnSchema>;
export interface AIChatFilter { status?: AIChatStatus; notHelpful?: boolean; cursor?: string }

export type AISettings = z.infer<typeof aiSettingsSchema>;
export type AIProvider = z.infer<typeof aiProviderSchema>;
export type AISlot = z.infer<typeof aiSlotSchema>;
export type AIEvent = z.infer<typeof aiEventSchema>;
export type AISlotName = z.infer<typeof aiSlotNameSchema>;
export type AIUsage = z.infer<typeof aiUsageSchema>;
export type AIAutomatic = z.infer<typeof aiAutomaticSchema>;
export type AIProviderInput = RequestBody<'addAIProvider'>;
export type AISlotInput = RequestBody<'setAISlot'>;
export type AIModelsInput = RequestBody<'listAIModels'>;

const json = <Operation extends 'addAIProvider' | 'listAIModels' | 'updateAIProvider' | 'setAIProviderKey' | 'setAISlot' | 'setAIAutomaticLimits'>(value: RequestBody<Operation>): string => JSON.stringify(value);

export const aiApi = {
  settings: () => request('/api/v1/ai', aiSettingsSchema),
  // The models a key may use, asked before the provider is saved; nothing is stored.
  models: (input: AIModelsInput) => request('/api/v1/ai/models', aiModelListSchema, { method: 'POST', body: json<'listAIModels'>(input) }),
  add: (input: AIProviderInput, key: string) => request('/api/v1/ai/providers', aiProviderSchema, { method: 'POST', headers: { 'Idempotency-Key': key }, body: json<'addAIProvider'>(input) }),
  update: (id: string, change: RequestBody<'updateAIProvider'>) =>
    request(`/api/v1/ai/providers/${id}`, aiProviderSchema, { method: 'PATCH', body: json<'updateAIProvider'>(change) }),
  setKey: (id: string, apiKey: string) => request(`/api/v1/ai/providers/${id}/key`, aiProviderSchema, { method: 'PUT', body: json<'setAIProviderKey'>({ api_key: apiKey }) }),
  check: (id: string) => request(`/api/v1/ai/providers/${id}/check`, aiProviderSchema, { method: 'POST' }),
  remove: (id: string) => request(`/api/v1/ai/providers/${id}`, removedSchema, { method: 'DELETE' }),
  setSlot: (slot: AISlotName, input: AISlotInput) => request(`/api/v1/ai/slots/${slot}`, aiSlotSchema, { method: 'PUT', body: json<'setAISlot'>(input) }),
  usage: (month: string) => request(`/api/v1/ai/usage?month=${encodeURIComponent(month)}`, aiUsageSchema),
  chatUsage: () => request('/api/v1/ai/chat/usage', aiChatUsageSchema),
  chatSessions: (filter: AIChatFilter) => {
    const query = new URLSearchParams();
    if (filter.status) query.set('status', filter.status);
    if (filter.notHelpful) query.set('feedback', 'not_helpful');
    if (filter.cursor) query.set('cursor', filter.cursor);
    const search = query.toString();
    return request(`/api/v1/ai/chat/sessions${search ? `?${search}` : ''}`, aiChatPageSchema);
  },
  chatSession: (id: string) => request(`/api/v1/ai/chat/sessions/${encodeURIComponent(id)}`, aiChatSessionSchema),
  // One key per press: a press sent again after a lost answer gets the first answer, not a second paid one.
  chatPreview: (question: string, key: string) =>
    request('/api/v1/ai/chat/preview', aiChatPreviewSchema, { method: 'POST', headers: { 'Idempotency-Key': key }, body: JSON.stringify({ question }) }),
  deleteChatSession: (id: string) => requestNoContent(`/api/v1/ai/chat/sessions/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  setAutomatic: (perDay: number, perMinute: number, draftPerDay?: number) =>
    request('/api/v1/ai/automatic-limits', aiAutomaticSchema, { method: 'PUT',
      body: json<'setAIAutomaticLimits'>({ per_day: perDay, per_minute: perMinute, ...(draftPerDay === undefined ? {} : { draft_per_day: draftPerDay }) }) }),
};
