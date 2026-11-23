import { z } from 'zod';
import { request } from '../../api/client';
import type { Generated } from '../../api/schemas';

// The sent replies the workspace's small model found to contradict an article, and the clear that ends its
// "May be out of date" flag while the replies stay listed with who cleared them.

const personSchema = z.object({ agent_id: z.string().uuid().nullable(), name: z.string() });
const conflictSchema = z.object({
  id: z.string().uuid(), reason: z.string(), created_at: z.string().datetime(),
  reply: z.object({
    message_id: z.string().uuid(), excerpt: z.string(), author: personSchema.nullable(), sent_at: z.string().datetime(),
    // null for a member who may not read conversations.
    conversation: z.object({ id: z.string().uuid(), display_ref: z.string(), subject: z.string() }).nullable(),
  }),
  cleared_at: z.string().datetime().nullable(), cleared_by: personSchema.nullable(),
  cleared_with_token: z.object({ id: z.string().uuid().nullable(), label: z.string() }).nullable(),
}) satisfies z.ZodType<Generated<'KBArticleConflict'>>;
const conflictsSchema = z.object({
  article_id: z.string().uuid(), open: z.number().int(), conflicts: z.array(conflictSchema),
}) satisfies z.ZodType<Generated<'KBArticleConflicts'>>;
export type KBArticleConflicts = z.infer<typeof conflictsSchema>;

const conflictsPath = (id: string) => `/api/v1/kb/articles/${id}/conflicts`;

export const conflictsApi = {
  conflicts: (id: string) => request(conflictsPath(id), conflictsSchema),
  // key names this press (Idempotency-Key): a repeat answers the first clear.
  clear: (id: string, key: string) => request(`${conflictsPath(id)}/clear`, conflictsSchema, { method: 'POST', headers: { 'Idempotency-Key': key } }),
};
