import { z } from 'zod';
import { json, request } from '../../api/client';
import type { RequestBody } from '../../api/client';
import type { Generated } from '../../api/schemas';

// One action on many articles, each answered on its own, and the Undo of the whole batch.

export type KBBulkInput = RequestBody<'bulkKBArticles'>;
// What the action does, without the articles it is applied to.
export type KBBulkChange = Omit<KBBulkInput, 'ids' | 'filter'>;
export type KBBulkResult = Generated<'KBBulkResult'>;
export type KBBulkUndoResult = Generated<'KBBulkUndoResult'>;

const itemErrorSchema = z.object({ code: z.string(), message: z.string() }) satisfies z.ZodType<Generated<'BulkItemError'>>;
const itemFields = { id: z.string(), number: z.number().int().optional(), title: z.string().optional(), error: itemErrorSchema.optional() };

const bulkResultSchema = z.object({
  operation: z.object({ id: z.string().uuid(), action: z.string(), undo_until: z.string().datetime() }).nullable(),
  counts: z.object({ done: z.number().int(), unchanged: z.number().int(), refused: z.number().int() }),
  results: z.array(z.object({ ...itemFields, outcome: z.enum(['done', 'unchanged', 'refused']), undoable: z.boolean() })),
}) satisfies z.ZodType<KBBulkResult>;

const bulkUndoResultSchema = z.object({
  operation: z.object({ id: z.string().uuid(), action: z.string(), undone_at: z.string().datetime() }),
  counts: z.object({ undone: z.number().int(), not_undone: z.number().int() }),
  results: z.array(z.object({ ...itemFields, outcome: z.enum(['undone', 'not_undone']) })),
}) satisfies z.ZodType<KBBulkUndoResult>;

export const kbBulkApi = {
  apply: (input: KBBulkInput, key: string) => request('/api/v1/kb/articles/bulk', bulkResultSchema,
    { method: 'POST', headers: { 'Idempotency-Key': key }, body: json<'bulkKBArticles'>(input) }),
  undo: (id: string, key: string) => request(`/api/v1/kb/bulk-operations/${encodeURIComponent(id)}/undo`, bulkUndoResultSchema,
    { method: 'POST', headers: { 'Idempotency-Key': key } }),
};
