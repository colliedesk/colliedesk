import { z } from 'zod';
import { request } from '../../api/client';
import type { RequestBody } from '../../api/client';
import { noContentSchema } from '../../api/schemas';
import type { Generated, JsonResponse } from '../../api/schemas';

// The workspace's help centers: the primary one at the workspace's address and the others below it at
// /<handle> (or on a custom domain of their own). Top-level collections choose the ones they show on.
const helpCenterSummarySchema = z.object({
  id: z.string().uuid(), name: z.string(), handle: z.string().nullable(), primary: z.boolean(), enabled: z.boolean(), title: z.string(), locale: z.string(),
  require_approval: z.boolean(), show_tags: z.boolean(), content_version: z.number().int(), updated_at: z.string().datetime().nullable(), url: z.string(),
}) satisfies z.ZodType<Generated<'HelpCenterSummary'>>;
const helpCenterListSchema = z.object({ help_centers: z.array(helpCenterSummarySchema), limit: z.number().int() }) satisfies z.ZodType<JsonResponse<'listHelpCenters', 200>>;

export type HelpCenterSummary = z.infer<typeof helpCenterSummarySchema>;

export const helpCenterLanguagesSchema = z.object({
  primary: z.string(),
  languages: z.array(z.object({
    locale: z.string(), visible: z.boolean(), position: z.number().int(), published: z.number().int(), drafts: z.number().int(), outdated: z.number().int(), missing: z.number().int(),
  })),
  unlisted: z.array(z.string()),
}) satisfies z.ZodType<Generated<'HelpCenterLanguages'>>;
export type HelpCenterLanguages = z.infer<typeof helpCenterLanguagesSchema>;

export const helpCentersKey = ['help-centers'] as const;

export const helpCentersApi = {
  list: () => request('/api/v1/help-centers', helpCenterListSchema),
  create: (value: RequestBody<'createHelpCenter'>, key: string) =>
    request('/api/v1/help-centers', helpCenterSummarySchema, { method: 'POST', headers: { 'Idempotency-Key': key }, body: JSON.stringify(value) }),
  update: (id: string, value: RequestBody<'updateHelpCenter'>) => request(`/api/v1/help-centers/${id}`, helpCenterSummarySchema, { method: 'PATCH', body: JSON.stringify(value) }),
  remove: (id: string) => request(`/api/v1/help-centers/${id}`, noContentSchema, { method: 'DELETE' }),
  // The languages of one help center, which the singular route answers for when named by id.
  languages: (id: string) => request(`/api/v1/help-center/languages?help_center_id=${encodeURIComponent(id)}`, helpCenterLanguagesSchema),
  saveLanguages: (id: string, value: RequestBody<'putHelpCenterLanguages'>) => request(`/api/v1/help-center/languages?help_center_id=${encodeURIComponent(id)}`,
    helpCenterLanguagesSchema, { method: 'PUT', body: JSON.stringify(value) }),
};

// helpCenterName is what the app calls a help center: its name, else its title, else its handle.
export const helpCenterName = (center: HelpCenterSummary, fallback: string) => center.name || center.title || center.handle || fallback;
