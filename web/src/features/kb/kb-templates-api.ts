import { z } from 'zod';
import { request } from '../../api/client';
import type { RequestBody } from '../../api/client';
import { savedReplyChangeSchema } from '../../api/schemas';
import type { Generated, JsonResponse } from '../../api/schemas';

// Article templates: the Markdown a new article starts from. They never reach the help center.

const personSchema = z.object({ agent_id: z.string().uuid().nullable(), name: z.string() }) satisfies z.ZodType<Generated<'SavedReplyPerson'>>;
const templateFields = {
  id: z.string().uuid(), name: z.string(), description: z.string(), body: z.string(),
  created_at: z.string().datetime(), updated_at: z.string().datetime(), last_change: savedReplyChangeSchema.nullable(),
};
export const kbTemplateSchema = z.object(templateFields) satisfies z.ZodType<Generated<'KBTemplate'>>;
const trashedTemplateSchema = z.object({
  ...templateFields, deleted_at: z.string().datetime(), deleted_by: personSchema.nullable(), purges_at: z.string().datetime(),
}) satisfies z.ZodType<Generated<'TrashedKBTemplate'>>;
const templateVersionSchema = z.object({
  version: z.number().int(), kind: z.enum(['create', 'edit', 'delete', 'restore', 'undelete']), restored_from_version: z.number().int().nullable(),
  changed_by: personSchema.nullable(), token: z.object({ id: z.string().uuid().nullable(), label: z.string() }).nullable(),
  agent_request_id: z.string().uuid().nullable(), created_at: z.string().datetime(), name: z.string(), description: z.string(), body: z.string(),
}) satisfies z.ZodType<Generated<'KBTemplateVersion'>>;
const templateListSchema = z.object({ items: z.array(kbTemplateSchema) }) satisfies z.ZodType<JsonResponse<'listKBTemplates', 200>>;
const trashPageSchema = z.object({ items: z.array(trashedTemplateSchema), next_cursor: z.string().nullable() }) satisfies z.ZodType<JsonResponse<'listTrashedKBTemplates', 200>>;
const versionPageSchema = z.object({ items: z.array(templateVersionSchema), next_before_version: z.number().int().nullable() }) satisfies z.ZodType<JsonResponse<'listKBTemplateVersions', 200>>;

export type KBTemplate = z.infer<typeof kbTemplateSchema>;
export type TrashedKBTemplate = z.infer<typeof trashedTemplateSchema>;
export type KBTemplateVersion = z.infer<typeof templateVersionSchema>;
export type KBTemplateInput = RequestBody<'createKBTemplate'>;

type Operation = 'createKBTemplate' | 'updateKBTemplate' | 'restoreKBTemplate' | 'restoreKBTemplateVersion';
const json = <O extends Operation>(value: RequestBody<O>): string => JSON.stringify(value);
const noContent = z.undefined();

// query is called with a variable, never an object literal, inside a path (web_routes_test.go reads a path up to its first brace).
function query(values: Record<string, string | undefined>): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(values)) if (value) params.set(key, value);
  const text = params.toString();
  return text ? `?${text}` : '';
}
const version = (updatedAt: string) => query({ updated_at: updatedAt });
const page = (cursor?: string) => query({ cursor });
const before = (beforeVersion?: number) => query({ before_version: beforeVersion ? String(beforeVersion) : undefined });

// templatesKey caches the live templates: the Templates page, the New article menu and Insert template share it.
export const templatesKey = ['kb-templates'];

export const kbTemplatesApi = {
  list: () => request('/api/v1/kb/templates', templateListSchema),
  trash: (cursor?: string) => request(`/api/v1/kb/templates/trash${page(cursor)}`, trashPageSchema),
  get: (id: string) => request(`/api/v1/kb/templates/${id}`, kbTemplateSchema),
  // key names this save (Idempotency-Key): a retry with the same key returns the template the first stored.
  create: (value: KBTemplateInput, key: string) => request('/api/v1/kb/templates', kbTemplateSchema,
    { method: 'POST', headers: { 'Idempotency-Key': key }, body: json<'createKBTemplate'>(value) }),
  update: (id: string, value: RequestBody<'updateKBTemplate'>) => request(`/api/v1/kb/templates/${id}`, kbTemplateSchema, { method: 'PATCH', body: json<'updateKBTemplate'>(value) }),
  // updatedAt is the version the person saw; the server refuses the delete if it was saved since.
  delete: (id: string, updatedAt: string) => request(`/api/v1/kb/templates/${id}${version(updatedAt)}`, noContent, { method: 'DELETE' }),
  restore: (id: string, updatedAt: string, key: string) => request(`/api/v1/kb/templates/${id}/restore`, kbTemplateSchema,
    { method: 'POST', headers: { 'Idempotency-Key': key }, body: json<'restoreKBTemplate'>({ updated_at: updatedAt }) }),
  versions: (id: string, beforeVersion?: number) => request(`/api/v1/kb/templates/${id}/versions${before(beforeVersion)}`, versionPageSchema),
  restoreVersion: (id: string, version: number, updatedAt: string, key: string) => request(`/api/v1/kb/templates/${id}/versions/${version}/restore`, kbTemplateSchema,
    { method: 'POST', headers: { 'Idempotency-Key': key }, body: json<'restoreKBTemplateVersion'>({ updated_at: updatedAt }) }),
};
