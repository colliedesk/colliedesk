import { z } from 'zod';
import { request } from './client';
import type { RequestBody } from './client';
import type { Generated } from './schemas';

// The workspace's response targets (docs/user/response-targets.md): business hours, holidays, the targets
// of each priority and how long before one a conversation is due soon. Settings → Response targets reads
// and writes them whole; it loads with that page, not in the first bundle.

const day = z.enum(['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun']);
// A target the server leaves out is none.
const target = z.object({ first_response_minutes: z.number().int().positive().nullable().default(null), resolution_minutes: z.number().int().positive().nullable().default(null) })
  .default({ first_response_minutes: null, resolution_minutes: null });
export const responseTargetsSchema = z.object({
  enabled: z.boolean(), timezone: z.string().nullable(),
  // A closed day may come without its hours.
  hours: z.array(z.object({ day, open: z.boolean(), from: z.string().default('08:00'), to: z.string().default('17:00') })),
  holidays: z.array(z.object({ date: z.string(), name: z.string() })).default([]),
  targets: z.object({ urgent: target, high: target, normal: target, low: target }),
  warn_before_minutes: z.number().int().nonnegative().default(0), channel_ids: z.array(z.string().uuid()).nullable().default(null),
  // alert_email: the workspace emails reminders and overdue alerts; my_alert_email: whether they reach the reader.
  alert_email: z.boolean().default(true), my_alert_email: z.boolean().optional(),
  enabled_at: z.string().datetime().nullable(), updated_at: z.string().datetime().nullable(),
}) satisfies z.ZodType<Generated<'ResponseTargets'>>;
export type ResponseTargets = z.infer<typeof responseTargetsSchema>;
export type ResponseTargetsChange = RequestBody<'setResponseTargets'>;
export type Weekday = z.infer<typeof day>;
export const weekdays: Weekday[] = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'];

export const responseTargetsApi = {
  get: () => request('/api/v1/response-targets', responseTargetsSchema),
  // The whole document: a card changes its part of what was read and sends the rest as it was.
  set: (value: ResponseTargetsChange) => request('/api/v1/response-targets', responseTargetsSchema, { method: 'PUT', body: JSON.stringify(value) }),
  // Whether the reader's own reminders and overdue alerts come by email.
  setMyAlerts: (email: boolean) => request('/api/v1/agents/me/sla-alerts', z.object({ email: z.boolean() }), { method: 'PATCH', body: JSON.stringify({ email } satisfies RequestBody<'setMySLAAlerts'>) }),
};
