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
  import_status: z.enum(['OK', 'FAILED', 'UNVERIFIED', '']).optional(), import_cause: z.literal('archive_refused').optional(), end: date.optional(), stats_read: count.optional(),
  windows: z.object({ '30d': resultsWindowSchema, '7d': resultsWindowSchema }),
});
const hoursSavedSchema = z.object({ hours: z.number(), conversations: count, median_handle_minutes: count }).nullable();
export const resultsSchema = z.object({
  current: snapshotSchema.nullable(), baseline: snapshotSchema.nullable(), baseline_state: z.enum(['none', 'computing', 'ready']),
  hours_saved: z.object({ '30d': hoursSavedSchema.optional(), '7d': hoursSavedSchema.optional() }),
  error: z.object({ message: z.string(), at: date }).nullable(),
  weekly_email: z.object({ subscribed: z.boolean(), can_send: z.boolean(), next_at: date, timezone: z.string() }).nullable(),
}) satisfies z.ZodType<JsonResponse<'getResults', 200>>;
const shareLinkSchema = z.object({ created_at: date, created_by_name: z.string().nullable(), last_viewed_at: date.nullable() });
export const shareLinkStateSchema = z.object({ link: shareLinkSchema.nullable() }) satisfies z.ZodType<JsonResponse<'getResultsShareLink', 200>>;
const shareLinkCreatedSchema = z.object({ link: shareLinkSchema, url: z.string() }) satisfies z.ZodType<JsonResponse<'createResultsShareLink', 201>>;
const sharedSideSchema = z.object({ measured_at: date, windows: z.object({ '30d': resultsWindowSchema, '7d': resultsWindowSchema }) });
export const sharedResultsSchema = z.object({
  workspace_name: z.string(), computed_at: date.nullable(), baseline_from: z.enum(['freshdesk', 'helpscout']).nullable(),
  baseline_import_status: z.enum(['OK', 'UNVERIFIED']).nullable(),
  current: sharedSideSchema.nullable(), baseline: sharedSideSchema.nullable(),
  hours_saved: z.object({ '30d': hoursSavedSchema.optional(), '7d': hoursSavedSchema.optional() }),
}) satisfies z.ZodType<JsonResponse<'getSharedResults', 200>>;
const weeklySchema = z.object({ weekly: z.boolean() }) satisfies z.ZodType<JsonResponse<'setResultsEmail', 200>>;

export type Results = z.infer<typeof resultsSchema>;
export type ResultsSnapshot = z.infer<typeof snapshotSchema>;
export type ResultsWindow = z.infer<typeof resultsWindowSchema>;
export type WindowKey = '30d' | '7d';
export type ShareLink = z.infer<typeof shareLinkSchema>;
export type SharedResults = z.infer<typeof sharedResultsSchema>;

export const resultsApi = {
  get: () => request('/api/v1/results', resultsSchema),
  shareLink: () => request('/api/v1/results/share-link', shareLinkStateSchema),
  createShareLink: () => request('/api/v1/results/share-link', shareLinkCreatedSchema, { method: 'POST' }),
  revokeShareLink: () => request('/api/v1/results/share-link', shareLinkStateSchema, { method: 'DELETE' }),
  setWeekly: (weekly: boolean) => request('/api/v1/agents/me/results-email', weeklySchema, { method: 'PATCH', body: JSON.stringify({ weekly }) }),
};

// sharedResults reads what a share link shows. A link turned off or replaced answers 404: null.
export async function sharedResults(token: string): Promise<SharedResults | null> {
  const response = await fetch('/api/v1/results/shared', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ token }) });
  if (response.status === 404) return null;
  if (!response.ok) throw new Error(`API ${response.status}`);
  return sharedResultsSchema.parse(await response.json());
}
