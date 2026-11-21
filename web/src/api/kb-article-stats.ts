import { z } from 'zod';
import { request } from './client';
import type { Generated } from './schemas';

// How much one article was read and used, per calendar day in the workspace's timezone: help center views
// and the sent replies that used it. The article editor and the results page both read it.

const count = z.number().int();
const statsSchema = z.object({
  article_id: z.string().uuid(), timezone: z.string(), from: z.string().datetime(), to: z.string().datetime(),
  first_day: z.string().date(), last_day: z.string().date(), views: count, uses: count,
  days: z.array(z.object({ day: z.string().date(), views: count, uses: count })),
  replies: z.array(z.object({
    conversation_id: z.string().uuid(), display_ref: z.string(), subject: z.string(), message_id: z.string().uuid(),
    kinds: z.array(z.enum(['ai_source', 'link'])), at: z.string().datetime(),
  })),
  replies_total: count,
}) satisfies z.ZodType<Generated<'KBArticleStats'>>;
export type KBArticleStats = z.infer<typeof statsSchema>;

// range is calendar days [from, to) as YYYY-MM-DD; without it the last 30 days.
export function articleStats(id: string, range?: { from: string; to: string }): Promise<KBArticleStats> {
  const query = range ? `?${new URLSearchParams(range).toString()}` : '';
  return request(`/api/v1/kb/articles/${id}/stats${query}`, statsSchema);
}
