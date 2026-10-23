import { z } from 'zod';
import { request } from './client';
import type { JsonResponse } from './schemas';

// The results page loads as its own lazy part, so its schemas and calls are not in the first bundle.

const date = z.string().datetime();
const range = z.object({ from: date, to: date });
const count = z.number().int();
const seconds = count.nullable();
const resultsWindowSchema = z.object({
  created: range, resolved_created: range, available_on: date.nullable(),
  first_response: z.object({ median_seconds: seconds, p90_seconds: seconds, answered: count, waiting: count }),
  resolution: z.object({ median_seconds: seconds, resolved: count, unresolved: count, without_resolve_time: count }).nullable(),
  reopened: z.object({ rate: z.number().nullable(), reopened: count, resolutions: count }).nullable(),
  volume: z.object({ count, from: date, days: z.number(), per_week: z.number().nullable() }),
  handle: z.object({ median_minutes: count.nullable(), conversations: count }).nullable(),
  ai: z.object({ sent_unchanged: count, sent_edited: count, discarded: count, conversations_sent_unchanged: count }).nullable(),
});
const snapshotSchema = z.object({
  id: z.string().uuid(), computed_at: date, measured_at: date, history_since: date.optional(), source: z.string().optional(),
  import_status: z.enum(['OK', 'FAILED', 'UNVERIFIED', '']).optional(), end: date.optional(), stats_read: count.optional(),
  windows: z.object({ '30d': resultsWindowSchema, '7d': resultsWindowSchema }),
});
const hoursSavedSchema = z.object({ hours: z.number(), conversations: count, median_handle_minutes: count }).nullable();
export const resultsSchema = z.object({
  current: snapshotSchema.nullable(), baseline: snapshotSchema.nullable(), baseline_state: z.enum(['none', 'computing', 'ready']),
  hours_saved: z.object({ '30d': hoursSavedSchema.optional(), '7d': hoursSavedSchema.optional() }),
  error: z.object({ message: z.string(), at: date }).nullable(),
}) satisfies z.ZodType<JsonResponse<'getResults', 200>>;

export type Results = z.infer<typeof resultsSchema>;
export type ResultsSnapshot = z.infer<typeof snapshotSchema>;
export type ResultsWindow = z.infer<typeof resultsWindowSchema>;
export type WindowKey = '30d' | '7d';

export const resultsApi = {
  get: () => request('/api/v1/results', resultsSchema),
};
