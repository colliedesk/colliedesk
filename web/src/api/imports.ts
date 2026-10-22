import { z } from 'zod';
import { request } from './client';
import type { RequestBody } from './client';
import type { Generated, JsonResponse } from './schemas';

// Imports load with their settings page, a lazy part, so their schemas and calls are not in the first bundle.

const nullableDate = z.string().datetime().nullable();
const accessSchema = z.enum(['readable', 'none', 'refused', 'unknown']) satisfies z.ZodType<Generated<'ImportAccess'>>;
export const importOptionsSchema = z.object({
  recent_days: z.number().int().optional(),
  include_personal_canned_responses: z.boolean().optional(),
  help_center_portal: z.number().int().optional(),
  old_help_center_domain: z.string().optional(),
}) satisfies z.ZodType<Generated<'ImportOptions'>>;
export const importPreviewSchema = z.object({
  domain: z.string(), account_id: z.number().int(), source: z.string(),
  agents: z.array(z.object({ id: z.number().int(), name: z.string(), email: z.string(), active: z.boolean() })),
  companies: z.number().int(), contacts: z.number().int().nullable(), tickets_outside_archive: z.number().int().nullable(),
  highest_listed_ticket_id: z.number().int(), archive: accessSchema, canned_responses: accessSchema, knowledge_base: accessSchema,
  rate_limit_per_minute: z.number().int(), api_calls: z.number().int(), checked_at: z.string().datetime(),
}) satisfies z.ZodType<Generated<'ImportPreview'>>;
export const importProgressSchema = z.object({
  step: z.string(), done: z.number().int(), total: z.number().int(), rate_limit_until: z.string().datetime().optional(),
  api_calls: z.number().int(), eta_seconds: z.number().int().optional(), updated_at: z.string().datetime(),
}) satisfies z.ZodType<Generated<'ImportProgress'>>;
export const importJobSchema = z.object({
  id: z.string().uuid(), kind: z.enum(['import', 'undo']), source: z.enum(['freshdesk']), domain: z.string(),
  account_source: z.string().nullable(), undo_of: z.string().uuid().nullable(),
  state: z.enum(['previewing', 'previewed', 'queued', 'running', 'retry_wait', 'waiting_key', 'finished', 'failed', 'cancelled', 'discarded']),
  phase: z.enum(['preview', 'recent', 'full', 'undo']), options: importOptionsSchema,
  preview: importPreviewSchema.nullable(), progress: importProgressSchema.nullable(),
  status: z.enum(['OK', 'FAILED', 'UNVERIFIED']).nullable(), error_code: z.string().nullable(), error_message: z.string().nullable(),
  imported_conversations: z.number().int().nullable(),
  has_key: z.boolean(), has_interim_report: z.boolean(), has_report: z.boolean(), cancel_requested: z.boolean(),
  next_run_at: nullableDate, created_by: z.string().uuid().nullable(), created_at: z.string().datetime(),
  started_at: nullableDate, finished_at: nullableDate, updated_at: z.string().datetime(),
}) satisfies z.ZodType<Generated<'ImportJob'>>;
export const importPersonSchema = z.object({
  external_id: z.string(), name: z.string(), email: z.string(), active: z.boolean(), linked_agent_id: z.string().uuid().nullable(),
  linked_by_email: z.boolean(), pending: z.boolean(), updated_at: z.string().datetime(),
}) satisfies z.ZodType<Generated<'ImportPerson'>>;
export const importPeopleSchema = z.object({ items: z.array(importPersonSchema) }) satisfies z.ZodType<JsonResponse<'listImportPeople', 200>>;
export type ImportPerson = z.infer<typeof importPersonSchema>;

export const importListSchema = z.object({ items: z.array(importJobSchema) }) satisfies z.ZodType<JsonResponse<'listImports', 200>>;

// The report is the importer's own JSON; the page reads the counts it shows and keeps the rest opaque.
const countSchema = z.object({ source: z.number(), imported: z.number(), failed: z.number() }).partial();
// links is the section on the old Freshdesk help center: where its addresses and the links inside
// imported content lead now (server/internal/freshdesk/report-links.go).
const linksSchema = z.object({
  status: z.string(), detail: z.array(z.string()).nullable().optional(), help_center: z.string(),
  old_addresses: z.object({ found: z.number(), redirected: z.number() }).passthrough(),
  rewritten: z.object({ articles: z.number(), saved_replies: z.number(), links: z.number() }),
  left: z.array(z.object({ entity: z.string(), freshdesk_id: z.string(), url: z.string(), reason: z.string() })).nullable().optional(),
}).passthrough();
export const importReportSchema = z.object({
  status: z.string(),
  interim: z.boolean().optional(),
  contacts: countSchema.optional(), tickets: countSchema.extend({ unresolved_ids: z.array(z.string()).nullable().optional() }).optional(),
  messages: countSchema.optional(), attachments: countSchema.optional(), time_entries: countSchema.optional(), saved_replies: countSchema.optional(),
  knowledge_base: z.object({ articles: countSchema.optional() }).partial().optional(),
  missing: z.array(z.string()).nullable().optional(),
  failures: z.array(z.object({ entity: z.string(), freshdesk_id: z.string(), error: z.string() })).nullable().optional(),
  links: linksSchema.optional(),
}).passthrough();
// An undo's report says what it removed, with counts, not the import's report.
export const undoReportSchema = z.object({ conversations: z.number(), contacts: z.number() }).passthrough();
// The report route answers either; the page reads it as the kind of its job.
const anyReportSchema = z.record(z.string(), z.unknown());

export type ImportJob = z.infer<typeof importJobSchema>;
export type ImportPreview = z.infer<typeof importPreviewSchema>;
export type ImportReport = z.infer<typeof importReportSchema>;
export type UndoReport = z.infer<typeof undoReportSchema>;
export type ImportLinks = z.infer<typeof linksSchema>;

const json = <Operation extends 'previewImport' | 'setImportKey' | 'linkImportPerson'>(value: RequestBody<Operation>): string => JSON.stringify(value);

export const importsApi = {
  list: () => request('/api/v1/imports', importListSchema),
  get: (id: string) => request(`/api/v1/imports/${id}`, importJobSchema),
  // oldHelpCenterDomain is the old Freshdesk help center's own domain, when it had one.
  preview: (domain: string, apiKey: string, oldHelpCenterDomain?: string) =>
    request('/api/v1/imports', importJobSchema, { method: 'POST', body: json<'previewImport'>({ source: 'freshdesk', domain, api_key: apiKey,
      ...(oldHelpCenterDomain ? { options: { old_help_center_domain: oldHelpCenterDomain } } : {}) }) }),
  start: (id: string) => request(`/api/v1/imports/${id}/start`, importJobSchema, { method: 'POST' }),
  cancel: (id: string) => request(`/api/v1/imports/${id}/cancel`, importJobSchema, { method: 'POST' }),
  setKey: (id: string, apiKey: string) => request(`/api/v1/imports/${id}/key`, importJobSchema, { method: 'PUT', body: json<'setImportKey'>({ api_key: apiKey }) }),
  report: (id: string) => request(`/api/v1/imports/${id}/report`, anyReportSchema),
  undo: (id: string) => request(`/api/v1/imports/${id}/undo`, importJobSchema, { method: 'POST' }),
  people: (id: string) => request(`/api/v1/imports/${id}/people`, importPeopleSchema),
  link: (id: string, externalId: string, agentId: string) =>
    request(`/api/v1/imports/${id}/people/${externalId}/link`, importPersonSchema, { method: 'PUT', body: json<'linkImportPerson'>({ agent_id: agentId }) }),
  unlink: (id: string, externalId: string) => request(`/api/v1/imports/${id}/people/${externalId}/link`, importPersonSchema, { method: 'DELETE' }),
};

// reportDownloadPath is the Markdown report as a file to save.
export const reportDownloadPath = (id: string) => `/api/v1/imports/${id}/report?format=markdown`;
