import { z } from 'zod';
import { request } from './client';
import type { RequestBody } from './client';
import type { Generated, JsonResponse } from './schemas';

// AI settings load with their settings page, a lazy part, so their schemas and calls are not in the first bundle.

const nullableDate = z.string().datetime().nullable();
export const aiSlotNameSchema = z.enum(['triage', 'draft']) satisfies z.ZodType<Generated<'AISlotName'>>;
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
}) satisfies z.ZodType<Generated<'AISlot'>>;
export const aiAutomaticSchema = z.object({
  per_day: z.number().int(), per_minute: z.number().int(), used_today: z.number().int(), paused_until: nullableDate,
}) satisfies z.ZodType<Generated<'AIAutomaticLimits'>>;
const aiAllowanceSchema = z.object({
  limit_micro_usd: z.number().int(), used_micro_usd: z.number().int(), resets_at: z.string().datetime(),
  drafts_left: z.number().int().nullable(),
}) satisfies z.ZodType<Generated<'AIAllowance'>>;
export const aiUsageSchema = z.object({
  month: z.string(), allowance: aiAllowanceSchema.nullable(),
  rows: z.array(z.object({
    feature: z.enum(['check', 'triage', 'draft']), billed_to: z.enum(['server', 'workspace_key']), provider_label: z.string(),
    calls: z.number().int(), failed: z.number().int(), automatic: z.number().int(), input_tokens: z.number().int(), output_tokens: z.number().int(),
    cost_micro_usd: z.number().int().nullable(),
  })),
}) satisfies z.ZodType<Generated<'AIUsage'>>;
const aiEventSchema = z.object({
  id: z.string().uuid(), kind: z.enum(['provider_added', 'provider_changed', 'provider_key_changed', 'provider_removed', 'slot_changed', 'automatic_limits_changed']),
  actor_agent_id: z.string().uuid().nullable(), actor_name: z.string().nullable(), via_token: z.boolean(),
  provider_id: z.string().uuid().nullable(), provider_label: z.string().nullable(), slot: aiSlotNameSchema.nullable(),
  detail: z.record(z.string(), z.unknown()), created_at: z.string().datetime(),
}) satisfies z.ZodType<Generated<'AIEvent'>>;
export const aiSettingsSchema = z.object({
  server: z.object({ kind: kindSchema, label: z.string(), models: z.record(z.string(), z.array(z.string())) }).nullable(),
  providers: z.array(aiProviderSchema), slots: z.array(aiSlotSchema), events: z.array(aiEventSchema),
  provider_limit: z.number().int(), can_store_keys: z.boolean(), allow_private_base_urls: z.boolean(),
  automatic: aiAutomaticSchema, allowance: aiAllowanceSchema.nullable(), auto_triage: z.object({ applied: z.number().int(), undone: z.number().int() }),
}) satisfies z.ZodType<JsonResponse<'getAISettings', 200>>;
const removedSchema = z.object({ turned_off: z.array(aiSlotNameSchema) }) satisfies z.ZodType<JsonResponse<'removeAIProvider', 200>>;

export type AISettings = z.infer<typeof aiSettingsSchema>;
export type AIProvider = z.infer<typeof aiProviderSchema>;
export type AISlot = z.infer<typeof aiSlotSchema>;
export type AIEvent = z.infer<typeof aiEventSchema>;
export type AISlotName = z.infer<typeof aiSlotNameSchema>;
export type AIUsage = z.infer<typeof aiUsageSchema>;
export type AIAutomatic = z.infer<typeof aiAutomaticSchema>;
export type AIProviderInput = RequestBody<'addAIProvider'>;
export type AISlotInput = RequestBody<'setAISlot'>;

const json = <Operation extends 'addAIProvider' | 'updateAIProvider' | 'setAIProviderKey' | 'setAISlot' | 'setAIAutomaticLimits'>(value: RequestBody<Operation>): string => JSON.stringify(value);

export const aiApi = {
  settings: () => request('/api/v1/ai', aiSettingsSchema),
  add: (input: AIProviderInput) => request('/api/v1/ai/providers', aiProviderSchema, { method: 'POST', body: json<'addAIProvider'>(input) }),
  update: (id: string, change: RequestBody<'updateAIProvider'>) =>
    request(`/api/v1/ai/providers/${id}`, aiProviderSchema, { method: 'PATCH', body: json<'updateAIProvider'>(change) }),
  setKey: (id: string, apiKey: string) => request(`/api/v1/ai/providers/${id}/key`, aiProviderSchema, { method: 'PUT', body: json<'setAIProviderKey'>({ api_key: apiKey }) }),
  check: (id: string) => request(`/api/v1/ai/providers/${id}/check`, aiProviderSchema, { method: 'POST' }),
  remove: (id: string) => request(`/api/v1/ai/providers/${id}`, removedSchema, { method: 'DELETE' }),
  setSlot: (slot: AISlotName, input: AISlotInput) => request(`/api/v1/ai/slots/${slot}`, aiSlotSchema, { method: 'PUT', body: json<'setAISlot'>(input) }),
  usage: (month: string) => request(`/api/v1/ai/usage?month=${encodeURIComponent(month)}`, aiUsageSchema),
  setAutomatic: (perDay: number, perMinute: number) =>
    request('/api/v1/ai/automatic-limits', aiAutomaticSchema, { method: 'PUT', body: json<'setAIAutomaticLimits'>({ per_day: perDay, per_minute: perMinute }) }),
};
