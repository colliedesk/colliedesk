import { z } from 'zod';
import { json, request } from '../../api/client';
import { conversationSchema, messageSchema } from '../../api/schemas';
import type { components } from '../../api/generated';

type Generated<Name extends keyof components['schemas']> = components['schemas'][Name];
const nullableDate = z.string().datetime().nullable();

// AI drafts live in their own chunk, loaded when a conversation opens (docs/user/ai-drafts.md).
const aiDraftSourceSchema = z.object({
  kind: z.enum(['kb_article', 'conversation']), id: z.string().uuid(), number: z.number().int().optional(), title: z.string(), locale: z.string().optional(),
}) satisfies z.ZodType<Generated<'AIDraftSource'>>;
const aiTriageSchema = z.object({
  tags: z.array(z.string()), priority: z.enum(['low', 'normal', 'high', 'urgent']).optional(), reason: z.string().optional(),
}) satisfies z.ZodType<Generated<'AITriage'>>;
export const aiDraftSchema = z.object({
  id: z.string().uuid(), conversation_id: z.string().uuid(), message_id: z.string().uuid().nullable(),
  status: z.enum(['queued', 'ready', 'failed', 'superseded', 'used', 'discarded', 'scheduled', 'auto_sent', 'cancelled']),
  action: z.enum(['answer', 'clarify', 'handoff']).nullable(), body: z.string().nullable(), confidence: z.enum(['high', 'medium', 'low']).nullable(),
  confidence_reasons: z.array(z.string()), handoff_reason: z.string().nullable(), topic_article_id: z.string().uuid().nullable(),
  sources: z.array(aiDraftSourceSchema), triage: aiTriageSchema.nullable(), triage_applied_at: nullableDate,
  outcome: z.enum(['sent_unchanged', 'sent_edited', 'discarded', 'auto_sent', 'auto_cancelled']).nullable(), similarity: z.number().nullable(),
  auto_send_at: nullableDate, error_code: z.string().nullable(), model: z.string().nullable(), created_at: z.string().datetime(), ready_at: nullableDate,
}) satisfies z.ZodType<Generated<'AIDraft'>>;
export type AIDraft = z.infer<typeof aiDraftSchema>;

const aiDraftStateSchema = z.object({ draft: aiDraftSchema.nullable(), available: z.boolean(), drafts_enabled: z.boolean() }) satisfies z.ZodType<Generated<'AIDraftState'>>;
const envelopeSchema = z.object({ draft: aiDraftSchema }) satisfies z.ZodType<Generated<'AIDraftEnvelope'>>;
const appliedSchema = z.object({ draft: aiDraftSchema, conversation: conversationSchema });

const base = (conversationId: string) => `/api/v1/conversations/${encodeURIComponent(conversationId)}/ai-draft`;
const action = (conversationId: string, draftId: string, name: string) => `${base(conversationId)}/${encodeURIComponent(draftId)}/${name}`;

export const aiDraftApi = {
  get: (conversationId: string, signal?: AbortSignal) => request(base(conversationId), aiDraftStateSchema, { signal }),
  requestNew: (conversationId: string) => request(base(conversationId), envelopeSchema, { method: 'POST' }),
  discard: (conversationId: string, draftId: string) => request(action(conversationId, draftId, 'discard'), envelopeSchema, { method: 'POST' }),
  applyTriage: (conversationId: string, draftId: string) => request(action(conversationId, draftId, 'apply-triage'), appliedSchema, { method: 'POST' }),
  cancelAutoAnswer: (conversationId: string, draftId: string) => request(action(conversationId, draftId, 'cancel-auto-answer'), envelopeSchema, { method: 'POST' }),
  // A reply sent from the draft names it, so its outcome (sent unchanged or edited) is recorded.
  send: (conversationId: string, draftId: string, body: string) => request(`/api/v1/conversations/${encodeURIComponent(conversationId)}/messages`, messageSchema,
    { method: 'POST', body: json<'createMessage'>({ kind: 'outgoing', body, ai_draft_id: draftId }) }),
};
