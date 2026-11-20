import { z } from 'zod';
import { request } from '../../api/client';
import type { Generated } from '../../api/schemas';

// An article's "Was this article helpful?" votes as the team reads them, and the reset that starts them
// again from 0 while the earlier counts stay in its history.

const tallySchema = z.object({ helpful: z.number().int(), not_helpful: z.number().int() }) satisfies z.ZodType<Generated<'KBVoteTally'>>;
const voteSchema = z.object({
  helpful: z.boolean(), missing: z.string().nullable(),
  created_at: z.string().datetime(), updated_at: z.string().datetime(),
}) satisfies z.ZodType<Generated<'KBArticleVote'>>;
const resetSchema = z.object({
  id: z.string().uuid(), helpful: z.number().int(), not_helpful: z.number().int(),
  reset_by: z.object({ agent_id: z.string().uuid().nullable(), name: z.string() }).nullable(),
  token: z.object({ id: z.string().uuid().nullable(), label: z.string() }).nullable(), created_at: z.string().datetime(),
}) satisfies z.ZodType<Generated<'KBArticleVoteReset'>>;
const votesSchema = z.object({
  article_id: z.string().uuid(), helpful: z.number().int(), not_helpful: z.number().int(), imported: tallySchema,
  votes: z.array(voteSchema), resets: z.array(resetSchema),
}) satisfies z.ZodType<Generated<'KBArticleVotes'>>;
export type KBArticleVotes = z.infer<typeof votesSchema>;

const votesPath = (id: string) => `/api/v1/kb/articles/${id}/votes`;

export const votesApi = {
  votes: (id: string) => request(votesPath(id), votesSchema),
  // key names this press (Idempotency-Key): a repeat answers the first reset and stores no second one.
  reset: (id: string, key: string) => request(`${votesPath(id)}/reset`, votesSchema, { method: 'POST', headers: { 'Idempotency-Key': key } }),
};
