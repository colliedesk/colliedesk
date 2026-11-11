import { z } from 'zod';
import { aiSourceSchema } from './ai-sources';
import { request } from '../../api/client';
import type { Generated, JsonResponse } from '../../api/schemas';

// AI reply drafts load with their part of the reply box, so their schemas and calls are not in the first bundle.

const nullableDate = z.string().datetime().nullable();
export const aiDraftSchema = z.object({
  id: z.string().uuid(), conversation_id: z.string().uuid(), status: z.enum(['queued', 'running', 'ready', 'failed', 'cancelled']),
  // written: words someone (usually an agent token) left for a person to send, shown even while AI drafts are off.
  source: z.enum(['ai', 'written']), cc: z.array(z.string()).nullable(),
  written_by: z.object({ token_label: z.string().nullable(), agent_name: z.string() }).nullable(),
  // Files left with a written draft: only the team sees them until a person sends the draft with them.
  files: z.array(z.object({ id: z.string().uuid(), filename: z.string(), content_type: z.string(), size: z.number().int().nonnegative(), created_at: z.string().datetime() })),
  error_code: z.string().nullable(), prompt: z.string(), text: z.string().nullable(), missing_info: z.string().nullable(),
  sources: z.array(aiSourceSchema),
  warnings: z.array(z.object({ kind: z.enum(['email', 'number', 'name', 'script', 'placeholder']), value: z.string() })),
  model: z.string().nullable(), outcome: z.enum(['sent_unchanged', 'sent_edited', 'discarded', 'superseded', 'expired']).nullable(),
  similarity: z.number().nullable(), sent_message_id: z.string().uuid().nullable(), requested_by_agent_id: z.string().uuid().nullable(),
  automatic: z.boolean(), answers_message_id: z.string().uuid().nullable(), reply_needed: z.boolean().nullable(),
  created_at: z.string().datetime(), ready_at: nullableDate, decided_at: nullableDate,
  decided_by_name: z.string().nullable(), restored_at: nullableDate,
}) satisfies z.ZodType<Generated<'AIDraft'>>;
const aiDraftListSchema = z.object({ items: z.array(aiDraftSchema), next_before: z.string().uuid().nullable() }) satisfies z.ZodType<JsonResponse<'listAIDrafts', 200>>;

export type AIDraft = z.infer<typeof aiDraftSchema>;

const base = (conversation: string) => `/api/v1/conversations/${conversation}/ai-drafts`;

export const aiDraftApi = {
  // A page of drafts, newest first; before (next_before of the page read) lists the older ones.
  list: (conversation: string, before?: string) => request(`${base(conversation)}${before ? `?${new URLSearchParams({ before })}` : ''}`, aiDraftListSchema),
  // wait holds the answer up to that many seconds while the draft is written.
  get: (conversation: string, id: string, wait = 0) => request(`${base(conversation)}/${id}${wait ? `?wait=${wait}` : ''}`, aiDraftSchema),
  create: (conversation: string, prompt: string) => request(base(conversation), aiDraftSchema, { method: 'POST', body: JSON.stringify({ prompt }) }),
  discard: (conversation: string, id: string) => request(`${base(conversation)}/${id}/discard`, aiDraftSchema, { method: 'POST' }),
  // Brings an earlier written draft back above the reply box, with its files.
  restore: (conversation: string, id: string) => request(`${base(conversation)}/${id}/restore`, aiDraftSchema, { method: 'POST' }),
};

// Where a file of a draft downloads from.
export const draftFileHref = (draft: AIDraft, file: string) => `${base(draft.conversation_id)}/${draft.id}/files/${file}`;

// The written drafts that wait on a conversation for a person to send.
export const waitingWritten = (drafts: AIDraft[] | undefined) => (drafts ?? []).filter((draft) => draft.source === 'written' && draft.status === 'ready' && draft.outcome === null);

// The earlier written drafts a person can bring back: replaced, expired or discarded, text still kept.
export const earlierWritten = (drafts: AIDraft[] | undefined) => (drafts ?? []).filter((draft) => draft.source === 'written' && draft.status === 'ready'
  && (draft.outcome === 'superseded' || draft.outcome === 'expired' || draft.outcome === 'discarded') && draft.text !== null);

export const writing = (draft: AIDraft | undefined) => draft?.status === 'queued' || draft?.status === 'running';
