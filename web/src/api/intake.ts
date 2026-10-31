import { z } from 'zod';
import { json, request } from './client';
import type { RequestBody } from './client';
import type { Generated } from './schemas';
import { statusSchema } from './schemas';

// The intake queue of chat channels (docs/user/review.md). It loads with the Review page, a lazy part,
// so its schemas and calls are not in the first bundle.

const uuid = z.string().uuid();
const date = z.string().datetime();

const chatSenderSchema = z.object({ kind: z.string().nullable(), value: z.string().nullable(), name: z.string().nullable() }) satisfies z.ZodType<Generated<'ChatSender'>>;

export const chatItemSchema = z.object({
  id: uuid, thread_id: uuid, direction: z.enum(['in', 'out']), role: z.enum(['customer', 'staff', 'bot']),
  sender: chatSenderSchema, contact_id: uuid.nullable(), agent_id: uuid.nullable(), is_other_customer: z.boolean(), body: z.string(),
  external_id: z.string().nullable(), quote_external_id: z.string().nullable(), message_id: uuid.nullable(),
  disposition: z.enum(['unresolved', 'gathering', 'conversation', 'pending_review', 'not_request', 'staff', 'bot', 'archived']),
  disposition_reason: z.string().nullable(), occurred_at: date,
  files: z.array(z.object({ name: z.string(), content_type: z.string(), status: z.string(), size: z.number().int().nullable(), error: z.string().nullable() })),
  linked: z.boolean(),
  link_reason: z.enum(['opened', 'gathered', 'command', 'ai', 'agent', 'reopen', 'reply', 'import']).nullable(),
}) satisfies z.ZodType<Generated<'ChatItem'>>;

const deciderSchema = z.enum(['rules', 'ai', 'agent']);
const ignoreReasonSchema = z.enum(['chat', 'handled_in_chat', 'duplicate', 'other']);

export const intakeGatheringSchema = z.object({
  id: uuid, channel_id: uuid, scope: z.string(), thread_id: uuid.nullable(), lane_key: z.string(), contact_id: uuid.nullable(),
  state: z.enum(['deciding', 'review', 'created', 'attached', 'ignored', 'applied', 'cancelled']),
  review_reason: z.enum(['no_command', 'ai_unsure', 'ai_unavailable', 'too_old', 'credential', 'target_changed', 'lane_blocked', 'requeued', 'gate_closed']).nullable(),
  rules: z.object({ action: z.enum(['create', 'attach', 'review', 'archive']), rule: z.string(), basis: z.enum(['match', 'default']), target_conversation_id: uuid.nullable() }),
  conversation_id: uuid.nullable(), decided_by: deciderSchema.nullable(), decided_at: date.nullable(), decided_by_agent_id: uuid.nullable(),
  ignore_reason: ignoreReasonSchema.nullable(), fallback: z.boolean(), opened_at: date, closed_at: date, decide_by: date.nullable(),
  requeued_at: date.nullable(), requeued_by_agent_id: uuid.nullable(), item_count: z.number().int(), sender: chatSenderSchema,
  place: z.object({ kind: z.enum(['direct', 'group']), name: z.string().nullable() }).nullable(), preview: z.string(),
}) satisfies z.ZodType<Generated<'IntakeGathering'>>;

export const intakeDecisionSchema = z.object({
  id: uuid, decider: z.enum(['ai', 'agent']), basis: z.enum(['model', 'person']), action: z.enum(['create', 'attach', 'ignore', 'review']),
  target_conversation_id: uuid.nullable(), runner_up_conversation_id: uuid.nullable(), runner_up_confidence: z.number().nullable(),
  command: z.string().nullable(), subject: z.string().nullable(), reply_kind: z.enum(['substantive', 'acknowledgement']).nullable(),
  confidence: z.number().nullable(), reason: z.string().nullable(), applied: z.boolean(), not_applied_reason: z.string().nullable(),
  ignore_reason: ignoreReasonSchema.nullable(), fallback: z.boolean(), corrects_decision_id: uuid.nullable(), model: z.string().nullable(), agent_id: uuid.nullable(), via_token: z.boolean(), created_at: date,
}) satisfies z.ZodType<Generated<'IntakeDecision'>>;

export const intakeCandidateSchema = z.object({
  conversation_id: uuid, subject: z.string(), status: statusSchema, contact_id: uuid, contact_name: z.string(), channel_id: uuid.nullable(),
  updated_at: date, resolved: z.boolean(),
}) satisfies z.ZodType<Generated<'IntakeCandidate'>>;

export const intakeGatheringDetailSchema = intakeGatheringSchema.extend({
  items: z.array(chatItemSchema), decisions: z.array(intakeDecisionSchema), candidates: z.array(intakeCandidateSchema), commands: z.array(z.string()),
}) satisfies z.ZodType<Generated<'IntakeGatheringDetail'>>;

const intakeGatheringPageSchema = z.object({ items: z.array(intakeGatheringSchema), next_cursor: z.string().nullable() }) satisfies z.ZodType<Generated<'IntakeGatheringPage'>>;

export const intakeStatsSchema = z.object({
  days: z.number().int(), gatherings: z.number().int(), by_state: z.record(z.string(), z.number().int()), decided_by: z.record(z.string(), z.number().int()),
  fallback: z.number().int(), ai_decisions: z.number().int(), ai_corrected: z.number().int(),
}) satisfies z.ZodType<Generated<'IntakeStats'>>;

export type ChatItem = z.infer<typeof chatItemSchema>;
export type IntakeGathering = z.infer<typeof intakeGatheringSchema>;
export type IntakeGatheringDetail = z.infer<typeof intakeGatheringDetailSchema>;
export type IntakeDecision = z.infer<typeof intakeDecisionSchema>;
export type IntakeCandidate = z.infer<typeof intakeCandidateSchema>;
export type IntakeStats = z.infer<typeof intakeStatsSchema>;
export type IntakeDecideInput = RequestBody<'decideIntakeGathering'>;
export type IntakeIgnoreReason = z.infer<typeof ignoreReasonSchema>;
export type IntakeListState = 'review' | 'ignored' | 'decided';
export interface IntakeListQuery { state: IntakeListState; decided_by?: 'rules' | 'ai' | 'agent'; channel_id?: string; scope?: string; cursor?: string }

const query = (values: object) => {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(values)) if (typeof value === 'string' && value !== '') search.set(key, value);
  return search.toString();
};

export const intakeApi = {
  list: (filter: IntakeListQuery) => request(`/api/v1/intake/gatherings?${query(filter)}`, intakeGatheringPageSchema),
  get: (id: string) => request(`/api/v1/intake/gatherings/${id}`, intakeGatheringDetailSchema),
  // A decision takes the key of its press (Idempotency-Key, from api/use-create): a repeat answers the first.
  decide: (id: string, input: IntakeDecideInput, key: string) => request(`/api/v1/intake/gatherings/${id}/decide`, intakeGatheringDetailSchema,
    { method: 'POST', headers: { 'Idempotency-Key': key }, body: json<'decideIntakeGathering'>(input) }),
  requeue: (id: string, key: string) => request(`/api/v1/intake/gatherings/${id}/requeue`, intakeGatheringDetailSchema, { method: 'POST', headers: { 'Idempotency-Key': key } }),
  stats: (channelID?: string) => request(`/api/v1/intake/stats?${query({ channel_id: channelID, days: '7' })}`, intakeStatsSchema),
};
