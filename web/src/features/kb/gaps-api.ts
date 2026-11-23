import { z } from 'zod';
import { json, optionalJson, request } from '../../api/client';
import type { OptionalBody, RequestBody } from '../../api/client';
import type { Generated } from '../../api/schemas';
import { noContentSchema } from '../../api/schemas';
import { kbArticleSchema } from './kb-api';

// Knowledge gaps: what readers asked for and did not find, grouped by their words.

const kinds = ['search_no_result', 'vote_no', 'chat_handoff', 'chat_unanswered', 'recurring_topic', 'requested'] as const;
export const gapKinds = kinds;
const kindSchema = z.enum(kinds) satisfies z.ZodType<Generated<'KBGapSignalKind'>>;
const personSchema = z.object({ agent_id: z.string().uuid().nullable(), name: z.string() }) satisfies z.ZodType<Generated<'SavedReplyPerson'>>;
const gapSchema = z.object({
  id: z.string().uuid(), title: z.string(), status: z.enum(['open', 'fixed', 'dismissed']), kinds: z.array(kindSchema),
  askers: z.number().int(), recent_askers: z.number().int(), conversation_count: z.number().int(), first_seen: z.string().datetime(), last_seen: z.string().datetime(),
  fixed_by_article: z.object({ id: z.string().uuid(), title: z.string() }).nullable(), fixed_automatically: z.boolean(),
  status_changed_by: personSchema.nullable(), status_changed_at: z.string().datetime().nullable(),
}) satisfies z.ZodType<Generated<'KBGap'>>;
const signalSchema = z.object({ kind: kindSchema, text: z.string(), note: z.string().nullable(), created_at: z.string().datetime() }) satisfies z.ZodType<Generated<'KBGapSignal'>>;
const conversationSchema = z.object({ conversation_id: z.string().uuid(), display_ref: z.string(), subject: z.string() }) satisfies z.ZodType<Generated<'KBConversationRef'>>;
const detailSchema = gapSchema.extend({ signal_count: z.number().int(), signals: z.array(signalSchema), conversations: z.array(conversationSchema) }) satisfies z.ZodType<Generated<'KBGapDetail'>>;
const pageSchema = z.object({ items: z.array(gapSchema) });
const requestedSchema = z.object({ gap: gapSchema, created: z.boolean(), added_to_existing: z.boolean() }) satisfies z.ZodType<Generated<'KBArticleRequest'>>;

export type KBGap = z.infer<typeof gapSchema>;
export type KBGapKind = z.infer<typeof kindSchema>;
export type KBGapStatus = KBGap['status'];
export type KBArticleRequest = z.infer<typeof requestedSchema>;

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
  // start opens an empty draft titled as the gap, beside the conversations that asked for it; no AI writes it.
  start: (id: string, key: string) =>
    request(`/api/v1/kb/gaps/${id}/article`, kbArticleSchema, { method: 'POST', headers: { 'Idempotency-Key': key } }),
  // similar lists the open gaps close to these words, closest first: the ones a request with them would join.
  similar: (words: string, signal?: AbortSignal) =>
    request(`/api/v1/kb/gaps?${new URLSearchParams({ similar: words })}`, pageSchema, { signal }),
  // ask files the conversation's request for an article; asking again with the same words changes nothing.
  ask: (conversationId: string, value: OptionalBody<'requestKBArticle'>) =>
    request(`/api/v1/conversations/${conversationId}/article-request`, requestedSchema, { method: 'POST', body: optionalJson<'requestKBArticle'>(value) }),
  withdraw: (conversationId: string) =>
    request(`/api/v1/conversations/${conversationId}/article-request`, noContentSchema, { method: 'DELETE' }),
  // fromReply keeps a sent reply as a draft article, with the customer question beside it.
  fromReply: (conversationId: string, messageId: string, key: string) =>
    request(`/api/v1/conversations/${conversationId}/messages/${messageId}/article`, kbArticleSchema, { method: 'POST', headers: { 'Idempotency-Key': key } }),
};
