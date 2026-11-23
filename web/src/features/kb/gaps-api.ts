import { z } from 'zod';
import { json, request } from '../../api/client';
import type { RequestBody } from '../../api/client';
import type { Generated } from '../../api/schemas';
import { kbArticleSchema } from './kb-api';

// Knowledge gaps: what readers asked for and did not find, grouped by their words.

const kinds = ['search_no_result', 'vote_no', 'chat_handoff', 'chat_unanswered', 'recurring_topic'] as const;
export const gapKinds = kinds;
const kindSchema = z.enum(kinds) satisfies z.ZodType<Generated<'KBGapSignalKind'>>;
const personSchema = z.object({ agent_id: z.string().uuid().nullable(), name: z.string() }) satisfies z.ZodType<Generated<'SavedReplyPerson'>>;
const gapSchema = z.object({
  id: z.string().uuid(), title: z.string(), status: z.enum(['open', 'fixed', 'dismissed']), kinds: z.array(kindSchema),
  askers: z.number().int(), recent_askers: z.number().int(), first_seen: z.string().datetime(), last_seen: z.string().datetime(),
  fixed_by_article: z.object({ id: z.string().uuid(), title: z.string() }).nullable(), fixed_automatically: z.boolean(),
  status_changed_by: personSchema.nullable(), status_changed_at: z.string().datetime().nullable(),
}) satisfies z.ZodType<Generated<'KBGap'>>;
const signalSchema = z.object({ kind: kindSchema, text: z.string(), created_at: z.string().datetime() }) satisfies z.ZodType<Generated<'KBGapSignal'>>;
const detailSchema = gapSchema.extend({ signal_count: z.number().int(), signals: z.array(signalSchema) }) satisfies z.ZodType<Generated<'KBGapDetail'>>;
const pageSchema = z.object({ items: z.array(gapSchema) });

export type KBGap = z.infer<typeof gapSchema>;
export type KBGapKind = z.infer<typeof kindSchema>;
export type KBGapStatus = KBGap['status'];

export const gapsApi = {
  list: (status: KBGapStatus, kind: KBGapKind | undefined, signal?: AbortSignal) => {
    const query = new URLSearchParams({ status });
    if (kind) query.set('kind', kind);
    return request(`/api/v1/kb/gaps?${query}`, pageSchema, { signal });
  },
  get: (id: string) => request(`/api/v1/kb/gaps/${id}`, detailSchema),
  update: (id: string, value: RequestBody<'updateKBGap'>) =>
    request(`/api/v1/kb/gaps/${id}`, gapSchema, { method: 'PATCH', body: json<'updateKBGap'>(value) }),
  // draft has the AI write the missing article as a draft in the first collection; key names the press.
  draft: (id: string, key: string) =>
    request(`/api/v1/kb/gaps/${id}/draft`, kbArticleSchema, { method: 'POST', headers: { 'Idempotency-Key': key } }),
};
