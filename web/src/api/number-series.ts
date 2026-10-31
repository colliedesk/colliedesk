import { z } from 'zod';
import { request } from './client';
import type { RequestBody } from './client';
import type { Generated } from './schemas';

// The series conversations are numbered in (docs/user/inbox.md#conversation-numbers). Settings → Conversation
// numbers and a channel's References read them; they load with those pages, not in the first bundle.

export const numberSeriesSchema = z.object({
  series: z.string(), format: z.string(), next: z.number().int().positive(), start: z.number().int().positive(),
  start_year: z.number().int().nullable(), updated_at: z.string().datetime(),
}) satisfies z.ZodType<Generated<'ConversationNumberSeries'>>;
const numberSeriesListSchema = z.object({ items: z.array(numberSeriesSchema) });

export type NumberSeries = z.infer<typeof numberSeriesSchema>;
export type NumberSeriesChange = RequestBody<'setConversationNumberSeries'>;

export const numberSeriesApi = {
  list: () => request('/api/v1/conversation-number-series', numberSeriesListSchema),
  set: (series: string, change: NumberSeriesChange) => request(`/api/v1/conversation-number-series/${encodeURIComponent(series)}`, numberSeriesSchema,
    { method: 'PUT', body: JSON.stringify(change) }),
};

// A series' name: what a channel's settings.number_series names.
export const seriesName = /^[a-z][a-z0-9_-]{0,19}$/;

// codeOf is a code the format gives number n in a year: {n} or {n:0K} (padded to K digits), {yyyy}.
export function codeOf(format: string, n: number, year: number): string {
  return format.replace(/\{n(?::0(\d))?\}/, (_all, width?: string) => String(n).padStart(Number(width ?? 0), '0')).replace('{yyyy}', String(year));
}

// yearIn is the current year where the workspace is: a code's {yyyy} is the year it was opened there.
export function yearIn(timezone: string, now = new Date()): number {
  return Number(new Intl.DateTimeFormat('en', { timeZone: timezone, year: 'numeric' }).format(now));
}
