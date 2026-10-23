import { z } from 'zod';
import { request } from '../../api/client';
import type { Generated, JsonResponse } from '../../api/schemas';

// AI reply drafts load with their part of the reply box, so their schemas and calls are not in the first bundle.

const nullableDate = z.string().datetime().nullable();
export const aiDraftSchema = z.object({
  id: z.string().uuid(), conversation_id: z.string().uuid(), status: z.enum(['queued', 'running', 'ready', 'failed', 'cancelled']),
  error_code: z.string().nullable(), prompt: z.string(), text: z.string().nullable(), missing_info: z.string().nullable(),
  sources: z.array(z.object({ kind: z.enum(['kb_article', 'conversation']), id: z.string().uuid(), locale: z.string().optional(), number: z.number().int().optional(), title: z.string() })),
  warnings: z.array(z.object({ kind: z.enum(['email', 'number', 'name', 'script', 'placeholder']), value: z.string() })),
  model: z.string().nullable(), outcome: z.enum(['sent_unchanged', 'sent_edited', 'discarded', 'superseded', 'expired']).nullable(),
  similarity: z.number().nullable(), sent_message_id: z.string().uuid().nullable(), requested_by_agent_id: z.string().uuid().nullable(),
  created_at: z.string().datetime(), ready_at: nullableDate, decided_at: nullableDate,
}) satisfies z.ZodType<Generated<'AIDraft'>>;
const aiDraftListSchema = z.object({ items: z.array(aiDraftSchema) }) satisfies z.ZodType<JsonResponse<'listAIDrafts', 200>>;

export type AIDraft = z.infer<typeof aiDraftSchema>;

const base = (conversation: string) => `/api/v1/conversations/${conversation}/ai-drafts`;

export const aiDraftApi = {
  list: (conversation: string) => request(base(conversation), aiDraftListSchema),
  // wait holds the answer up to that many seconds while the draft is written.
  get: (conversation: string, id: string, wait = 0) => request(`${base(conversation)}/${id}${wait ? `?wait=${wait}` : ''}`, aiDraftSchema),
  create: (conversation: string, prompt: string) => request(base(conversation), aiDraftSchema, { method: 'POST', body: JSON.stringify({ prompt }) }),
  discard: (conversation: string, id: string) => request(`${base(conversation)}/${id}/discard`, aiDraftSchema, { method: 'POST' }),
};

export const writing = (draft: AIDraft | undefined) => draft?.status === 'queued' || draft?.status === 'running';
