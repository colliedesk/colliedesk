import { z } from 'zod';
import { request } from './client';
import type { Generated } from './schemas';

// The intake queue's light summary, polled for the rail badge on every page; the queue itself
// (api/intake.ts) loads with the Review page, a lazy part.

export const intakeSummarySchema = z.object({
  available: z.boolean(),
  waiting: z.number().int(),
  deciding: z.number().int(),
  scopes: z.array(z.object({ channel_id: z.string().uuid(), channel_name: z.string(), scope: z.string() })),
}) satisfies z.ZodType<Generated<'IntakeSummary'>>;

export type IntakeSummary = z.infer<typeof intakeSummarySchema>;

export const intakeSummary = () => request('/api/v1/intake/summary', intakeSummarySchema);
