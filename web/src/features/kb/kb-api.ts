import { z } from 'zod';
import { request } from '../../api/client';
import type { RequestBody } from '../../api/client';
import type { Generated, JsonResponse } from '../../api/schemas';

// The knowledge base pages load in their own chunk, so their schemas and calls are not in the first bundle.

const nullableDate = z.string().datetime().nullable();
const personSchema = z.object({ agent_id: z.string().uuid().nullable(), name: z.string() }) satisfies z.ZodType<Generated<'SavedReplyPerson'>>;
const statusSchema = z.enum(['draft', 'published']) satisfies z.ZodType<Generated<'KBArticleStatus'>>;
const changeFields = {
  version: z.number().int(), kind: z.enum(['create', 'edit', 'delete', 'restore', 'undelete', 'import', 'media_replace', 'ai_exclude', 'ai_include']), restored_from_version: z.number().int().nullable(),
  changed_by: personSchema.nullable(), token: z.object({ id: z.string().uuid().nullable(), label: z.string() }).nullable(),
  agent_request_id: z.string().uuid().nullable(), created_at: z.string().datetime(),
};
const changeSchema = z.object(changeFields) satisfies z.ZodType<Generated<'KBArticleChange'>>;
const collectionFields = {
  id: z.string().uuid(), parent_id: z.string().uuid().nullable(), name: z.string(), slug: z.string(), description: z.string(),
  position: z.number().int(), article_count: z.number().int(), created_at: z.string().datetime(), updated_at: z.string().datetime(),
};
export const kbCollectionSchema = z.object(collectionFields) satisfies z.ZodType<Generated<'KBCollection'>>;
const trashedCollectionSchema = z.object({
  ...collectionFields, deleted_at: z.string().datetime(), deleted_by: personSchema.nullable(), purges_at: z.string().datetime(),
}) satisfies z.ZodType<Generated<'TrashedKBCollection'>>;
const summaryFields = {
  id: z.string().uuid(), number: z.number().int(), collection_id: z.string().uuid(), title: z.string(), slug: z.string(), status: statusSchema,
  published_at: nullableDate, author_agent_id: z.string().uuid().nullable(), created_at: z.string().datetime(), updated_at: z.string().datetime(),
  last_change: changeSchema.nullable(), url: z.string(),
};
const summarySchema = z.object(summaryFields) satisfies z.ZodType<Generated<'KBArticleSummary'>>;
export const kbArticleSchema = z.object({
  ...summaryFields, body_md: z.string(), has_source_html: z.boolean(), deleted_at: nullableDate,
  ai_exclusion: z.object({ at: z.string().datetime(), by: personSchema.nullable() }).nullable(),
  translations: z.array(z.object({ locale: z.string(), title: z.string(), body_md: z.string(), updated_at: z.string().datetime() })),
}) satisfies z.ZodType<Generated<'KBArticle'>>;
const trashedArticleSchema = z.object({
  ...summaryFields, deleted_at: z.string().datetime(), deleted_by: personSchema.nullable(), purges_at: z.string().datetime(),
}) satisfies z.ZodType<Generated<'TrashedKBArticle'>>;
const versionSchema = z.object({
  ...changeFields, title: z.string(), slug: z.string(), body_md: z.string(), status: statusSchema, collection_id: z.string().uuid().nullable(),
}) satisfies z.ZodType<Generated<'KBArticleVersion'>>;
const imageSchema = z.object({
  id: z.string().uuid(), content_type: z.enum(['image/png', 'image/jpeg', 'image/gif', 'image/webp']), size: z.number().int(), sha256: z.string(),
  url: z.string(), created_at: z.string().datetime(),
}) satisfies z.ZodType<Pick<Generated<'KBMedia'>, 'id' | 'content_type' | 'size' | 'sha256' | 'url' | 'created_at'>>;
const helpCenterSchema = z.object({
  enabled: z.boolean(), title: z.string(), locale: z.string(), content_version: z.number().int(), updated_at: nullableDate, url: z.string(),
}) satisfies z.ZodType<Generated<'HelpCenter'>>;

const collectionListSchema = z.object({ items: z.array(kbCollectionSchema) }) satisfies z.ZodType<JsonResponse<'listKBCollections', 200>>;
const trashedCollectionListSchema = z.object({ items: z.array(trashedCollectionSchema) }) satisfies z.ZodType<JsonResponse<'listTrashedKBCollections', 200>>;
const articlePageSchema = z.object({ items: z.array(summarySchema), next_cursor: z.string().nullable() }) satisfies z.ZodType<JsonResponse<'listKBArticles', 200>>;
const trashedArticlePageSchema = z.object({ items: z.array(trashedArticleSchema), next_cursor: z.string().nullable() }) satisfies z.ZodType<JsonResponse<'listTrashedKBArticles', 200>>;
const versionPageSchema = z.object({ items: z.array(versionSchema), next_before_version: z.number().int().nullable() }) satisfies z.ZodType<JsonResponse<'listKBArticleVersions', 200>>;

export type KBCollection = z.infer<typeof kbCollectionSchema>;
export type TrashedKBCollection = z.infer<typeof trashedCollectionSchema>;
export type KBArticleSummary = z.infer<typeof summarySchema>;
export type KBArticle = z.infer<typeof kbArticleSchema>;
export type TrashedKBArticle = z.infer<typeof trashedArticleSchema>;
export type KBArticleVersion = z.infer<typeof versionSchema>;
export type KBArticlePage = z.infer<typeof articlePageSchema>;
export type HelpCenter = z.infer<typeof helpCenterSchema>;
export type KBArticleStatus = z.infer<typeof statusSchema>;
export type KBCollectionChange = Omit<RequestBody<'updateKBCollection'>, 'updated_at'>;
export type KBArticleChange = Omit<RequestBody<'updateKBArticle'>, 'updated_at'>;

type Operation = 'createKBCollection' | 'updateKBCollection' | 'restoreKBCollection' | 'createKBArticle' | 'updateKBArticle' | 'restoreKBArticle'
  | 'restoreKBArticleVersion' | 'putHelpCenter';
const json = <O extends Operation>(value: RequestBody<O>): string => JSON.stringify(value);

// The calls that answer 204 carry no body.
const noContent = z.undefined();

export type ArticleQuery = { q?: string; status?: KBArticleStatus; collection_id?: string; cursor?: string };

// query is called with a variable, never an object literal, inside a path: the gate that checks every path
// the web app calls is a route (web_routes_test.go) reads a path up to its first closing brace.
function query(values: Record<string, string | undefined>): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(values)) if (value) params.set(key, value);
  const text = params.toString();
  return text ? `?${text}` : '';
}

const version = (updatedAt: string) => query({ updated_at: updatedAt });
const page = (cursor?: string) => query({ cursor });
const before = (beforeVersion?: number) => query({ before_version: beforeVersion ? String(beforeVersion) : undefined });

export const kbApi = {
  collections: () => request('/api/v1/kb/collections', collectionListSchema),
  trashedCollections: () => request('/api/v1/kb/collections/trash', trashedCollectionListSchema),
  createCollection: (value: RequestBody<'createKBCollection'>) => request('/api/v1/kb/collections', kbCollectionSchema, { method: 'POST', body: json<'createKBCollection'>(value) }),
  updateCollection: (id: string, value: RequestBody<'updateKBCollection'>) => request(`/api/v1/kb/collections/${id}`, kbCollectionSchema, { method: 'PATCH', body: json<'updateKBCollection'>(value) }),
  // updatedAt is the version the person saw; the server refuses the delete if it was saved since.
  deleteCollection: (id: string, updatedAt: string) => request(`/api/v1/kb/collections/${id}${version(updatedAt)}`, noContent, { method: 'DELETE' }),
  restoreCollection: (id: string, updatedAt: string) => request(`/api/v1/kb/collections/${id}/restore`, kbCollectionSchema, { method: 'POST', body: json<'restoreKBCollection'>({ updated_at: updatedAt }) }),
  // signal lets a newer search cancel the request of the one it replaced.
  articles: (filter: ArticleQuery, signal?: AbortSignal) => request(`/api/v1/kb/articles${query(filter)}`, articlePageSchema, { signal }),
  trashedArticles: (cursor?: string) => request(`/api/v1/kb/articles/trash${page(cursor)}`, trashedArticlePageSchema),
  article: (id: string) => request(`/api/v1/kb/articles/${id}`, kbArticleSchema),
  createArticle: (value: RequestBody<'createKBArticle'>) => request('/api/v1/kb/articles', kbArticleSchema, { method: 'POST', body: json<'createKBArticle'>(value) }),
  updateArticle: (id: string, value: RequestBody<'updateKBArticle'>) => request(`/api/v1/kb/articles/${id}`, kbArticleSchema, { method: 'PATCH', body: json<'updateKBArticle'>(value) }),
  deleteArticle: (id: string, updatedAt: string) => request(`/api/v1/kb/articles/${id}${version(updatedAt)}`, noContent, { method: 'DELETE' }),
  restoreArticle: (id: string, updatedAt: string) => request(`/api/v1/kb/articles/${id}/restore`, kbArticleSchema, { method: 'POST', body: json<'restoreKBArticle'>({ updated_at: updatedAt }) }),
  // AI may not read an excluded article, in any language; including it again undoes that.
  excludeFromAI: (id: string) => request(`/api/v1/kb/articles/${id}/ai-exclusion`, kbArticleSchema, { method: 'POST' }),
  includeInAI: (id: string) => request(`/api/v1/kb/articles/${id}/ai-exclusion`, kbArticleSchema, { method: 'DELETE' }),
  versions: (id: string, beforeVersion?: number) => request(`/api/v1/kb/articles/${id}/versions${before(beforeVersion)}`, versionPageSchema),
  restoreVersion: (id: string, version: number, updatedAt: string) => request(`/api/v1/kb/articles/${id}/versions/${version}/restore`, kbArticleSchema,
    { method: 'POST', body: json<'restoreKBArticleVersion'>({ updated_at: updatedAt }) }),
  // The body is the image itself; its own type replaces the JSON one request() sets.
  uploadImage: (file: Blob) => request('/api/v1/kb/images', imageSchema, { method: 'POST', body: file, headers: { 'Content-Type': file.type } }),
  helpCenter: () => request('/api/v1/help-center', helpCenterSchema),
  saveHelpCenter: (value: RequestBody<'putHelpCenter'>) => request('/api/v1/help-center', helpCenterSchema, { method: 'PUT', body: json<'putHelpCenter'>(value) }),
};
