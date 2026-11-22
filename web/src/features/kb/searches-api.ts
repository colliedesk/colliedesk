import { z } from 'zod';
import { request } from '../../api/client';
import type { Generated } from '../../api/schemas';

// What readers searched for on the help center in the last 7 days, as the team reads it.

const termSchema = z.object({
  query: z.string().min(1), searches: z.number().int(), opened: z.number().int(), results: z.number().int(),
  last_searched_at: z.string().datetime(),
}) satisfies z.ZodType<Generated<'KBSearchTerm'>>;
const reportSchema = z.object({
  from: z.string().datetime(), to: z.string().datetime(), searches: z.number().int(),
  top: z.array(termSchema), no_results: z.array(termSchema), nothing_opened: z.array(termSchema),
}) satisfies z.ZodType<Generated<'KBSearchReport'>>;
export type KBSearchTerm = z.infer<typeof termSchema>;

export const searchesApi = {
  report: () => request('/api/v1/kb/searches/report', reportSchema),
};
