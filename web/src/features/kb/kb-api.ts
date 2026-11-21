import { z } from 'zod';
import { request } from '../../api/client';
import type { RequestBody } from '../../api/client';
import type { Generated, JsonResponse } from '../../api/schemas';

// The knowledge base pages load in their own chunk, so their schemas and calls are not in the first bundle.

const nullableDate = z.string().datetime().nullable();
const personSchema = z.object({ agent_id: z.string().uuid().nullable(), name: z.string() }) satisfies z.ZodType<Generated<'SavedReplyPerson'>>;
const statusSchema = z.enum(['draft', 'published']) satisfies z.ZodType<Generated<'KBArticleStatus'>>;
// The steps of a review the history lists beside the edits.
const reviewKinds = ['review_request', 'review_approve', 'review_changes', 'review_withdraw'] as const;
// A help center or a collection that requires approval publishes only text a teammate approved; inherit follows the help center.
export const approvalModeSchema = z.enum(['inherit', 'on', 'off']) satisfies z.ZodType<Generated<'KBApprovalMode'>>;
// Who can see an article or a collection: the help center and help chat, help chat only, or only the team.
export const visibilitySchema = z.enum(['everyone', 'help_chat', 'agents']) satisfies z.ZodType<Generated<'KBVisibility'>>;
const visibilityFields = { visibility: visibilitySchema, effective_visibility: visibilitySchema };
const changeFields = {
  version: z.number().int(), kind: z.enum(['create', 'edit', 'delete', 'restore', 'undelete', 'import', 'media_replace', 'ai_exclude', 'ai_include', 'draft', ...reviewKinds, 'author_change']), restored_from_version: z.number().int().nullable(),
  changed_by: personSchema.nullable(), token: z.object({ id: z.string().uuid().nullable(), label: z.string() }).nullable(),
  agent_request_id: z.string().uuid().nullable(), created_at: z.string().datetime(),
};
// The team member an article is credited to; left once they left the team, and the article still names them.
export const kbAuthorSchema = z.object({ agent_id: z.string().uuid(), name: z.string(), left: z.boolean() }) satisfies z.ZodType<Generated<'KBArticleAuthor'>>;
const changeSchema = z.object(changeFields) satisfies z.ZodType<Generated<'KBArticleChange'>>;
const collectionNameSchema = z.object({
  locale: z.string(), name: z.string(), description: z.string(), imported: z.boolean(), updated_at: z.string().datetime(),
}) satisfies z.ZodType<Generated<'KBCollectionTranslation'>>;
const collectionFields = {
  id: z.string().uuid(), parent_id: z.string().uuid().nullable(), name: z.string(), slug: z.string(), description: z.string(),
  position: z.number().int(), article_count: z.number().int(), created_at: z.string().datetime(), updated_at: z.string().datetime(),
};
export const kbCollectionSchema = z.object({ ...collectionFields, ...visibilityFields, default_template_id: z.string().uuid().nullable(), require_approval: approvalModeSchema, translations: z.array(collectionNameSchema) }) satisfies z.ZodType<Generated<'KBCollection'>>;
const trashedCollectionSchema = z.object({
  ...collectionFields, visibility: visibilitySchema.optional(), effective_visibility: visibilitySchema.optional(), default_template_id: z.string().uuid().nullable().optional(), require_approval: approvalModeSchema.optional(), translations: z.array(collectionNameSchema).optional(), deleted_at: z.string().datetime(), deleted_by: personSchema.nullable(), purges_at: z.string().datetime(),
}) satisfies z.ZodType<Generated<'TrashedKBCollection'>>;
const translationChangeSchema = z.object({
  ...changeFields, kind: z.enum(['create', 'edit', 'delete', 'restore', 'undelete', 'import', 'media_replace', 'mark_current', 'mark_outdated', ...reviewKinds]),
}) satisfies z.ZodType<Generated<'KBTranslationChange'>>;
const translationSummaryFields = {
  article_id: z.string().uuid(), locale: z.string(), title: z.string(), slug: z.string(), status: statusSchema, published_at: nullableDate,
  outdated: z.boolean(), outdated_at: nullableDate, created_at: z.string().datetime(), updated_at: z.string().datetime(), in_trash: z.boolean(),
  last_change: translationChangeSchema.nullable(), url: z.string(),
};
const translationSummarySchema = z.object(translationSummaryFields) satisfies z.ZodType<Generated<'KBTranslationSummary'>>;
const summaryFields = {
  id: z.string().uuid(), number: z.number().int(), collection_id: z.string().uuid(), collection_ids: z.array(z.string().uuid()).min(1), position: z.number().int(), title: z.string(), slug: z.string(), status: statusSchema,
  published_at: nullableDate, author_agent_id: z.string().uuid().nullable(), author: kbAuthorSchema.nullable(), created_at: z.string().datetime(), updated_at: z.string().datetime(),
  last_change: changeSchema.nullable(), url: z.string(),
  // has_draft: a published article with unpublished changes; the help center still shows the published text.
  has_draft: z.boolean(),
  // The published tags, shared by its translations; a published article's tag change waits in draft.tags.
  tags: z.array(z.string()),
  ...visibilityFields,
};
const summarySchema = z.object(summaryFields) satisfies z.ZodType<Generated<'KBArticleSummary'>>;
// A file listed under an article or translation, in the order the reader sees.
export const attachmentSchema = z.object({ id: z.string().uuid(), name: z.string(), content_type: z.string(), size: z.number().int(), url: z.string() }) satisfies z.ZodType<Generated<'KBAttachment'>>;
// The unpublished changes of a published article: what publishing them puts live. Its updated_at is the
// version every save, publish, discard, restore or delete sends while it exists.
const draftSchema = z.object({
  title: z.string(), slug: z.string(), body_md: z.string(), seo_title: z.string(), seo_description: z.string(), collection_id: z.string().uuid(),
  collection_ids: z.array(z.string().uuid()).min(1), show_contents: z.boolean(), attachments: z.array(attachmentSchema), created_at: z.string().datetime(),
  updated_at: z.string().datetime(), updated_by: personSchema.nullable(), tags: z.array(z.string()),
}) satisfies z.ZodType<Generated<'KBArticleDraft'>>;
export const kbArticleSchema = z.object({
  ...summaryFields, body_md: z.string(), seo_title: z.string(), seo_description: z.string(), show_contents: z.boolean(), has_source_html: z.boolean(), deleted_at: nullableDate,
  ai_exclusion: z.object({ at: z.string().datetime(), by: personSchema.nullable() }).nullable(),
  // Only when one article is read: the AI reply drafts this month that used it.
  ai_drafts_this_month: z.number().int().optional(),
  translations: z.array(translationSummarySchema), attachments: z.array(attachmentSchema),
  draft: draftSchema.nullable(),
  // The collection that keeps the article more restricted than its own visibility; null when its own decides.
  visibility_from: z.object({ id: z.string().uuid(), name: z.string(), visibility: visibilitySchema }).nullable(),
}) satisfies z.ZodType<Generated<'KBArticle'>>;
export const kbTranslationSchema = z.object({
  ...translationSummaryFields, body_md: z.string(), seo_title: z.string(), seo_description: z.string(), has_source_html: z.boolean(), deleted_at: nullableDate,
  language_visible: z.boolean(), missing_media: z.array(z.string().uuid()).optional(), attachments: z.array(attachmentSchema),
  original: z.object({
    id: z.string().uuid(), number: z.number().int(), locale: z.string(), title: z.string(), body_md: z.string(), seo_title: z.string(), seo_description: z.string(),
    status: statusSchema, collection_id: z.string().uuid(), collection_name: z.string(), version: z.number().int(), updated_at: z.string().datetime(),
  }),
}) satisfies z.ZodType<Generated<'KBArticleTranslation'>>;
const trashedTranslationSchema = z.object({
  ...translationSummaryFields, article_title: z.string(), article_number: z.number().int(), deleted_at: z.string().datetime(), deleted_by: personSchema.nullable(),
  purges_at: z.string().datetime(),
}) satisfies z.ZodType<Generated<'TrashedKBTranslation'>>;
const translationVersionSchema = z.object({
  ...changeFields, kind: translationChangeSchema.shape.kind, title: z.string(), slug: z.string(), body_md: z.string(), seo_title: z.string(), seo_description: z.string(), status: statusSchema,
}) satisfies z.ZodType<Generated<'KBTranslationVersion'>>;
const languageItemSchema = z.object({
  article_id: z.string().uuid(), number: z.number().int(), title: z.string(), status: statusSchema, collection_id: z.string().uuid(), collection_name: z.string(),
  updated_at: z.string().datetime(), translation: translationSummarySchema.nullable(),
}) satisfies z.ZodType<Generated<'KBLanguageItem'>>;
export const helpCenterLanguagesSchema = z.object({
  primary: z.string(),
  languages: z.array(z.object({
    locale: z.string(), visible: z.boolean(), position: z.number().int(), published: z.number().int(), drafts: z.number().int(), outdated: z.number().int(), missing: z.number().int(),
  })),
  unlisted: z.array(z.string()),
}) satisfies z.ZodType<Generated<'HelpCenterLanguages'>>;
const languagePageSchema = z.object({ items: z.array(languageItemSchema), next_cursor: z.string().nullable() }) satisfies z.ZodType<JsonResponse<'listKBLanguage', 200>>;
const trashedTranslationPageSchema = z.object({ items: z.array(trashedTranslationSchema), next_cursor: z.string().nullable() }) satisfies z.ZodType<JsonResponse<'listTrashedKBTranslations', 200>>;
const translationVersionPageSchema = z.object({ items: z.array(translationVersionSchema), next_before_version: z.number().int().nullable() }) satisfies z.ZodType<JsonResponse<'listKBTranslationVersions', 200>>;
const trashedArticleSchema = z.object({
  ...summaryFields, deleted_at: z.string().datetime(), deleted_by: personSchema.nullable(), purges_at: z.string().datetime(),
}) satisfies z.ZodType<Generated<'TrashedKBArticle'>>;
const versionSchema = z.object({
  ...changeFields, title: z.string(), slug: z.string(), body_md: z.string(), seo_title: z.string(), seo_description: z.string(), show_contents: z.boolean(), status: statusSchema, collection_id: z.string().uuid().nullable(),
  author_change: z.object({ from: kbAuthorSchema.nullable(), to: kbAuthorSchema.nullable() }).optional(),
}) satisfies z.ZodType<Generated<'KBArticleVersion'>>;
const imageSchema = z.object({
  id: z.string().uuid(), content_type: z.enum(['image/png', 'image/jpeg', 'image/gif', 'image/webp']), size: z.number().int(), sha256: z.string(),
  url: z.string(), created_at: z.string().datetime(),
}) satisfies z.ZodType<Pick<Generated<'KBMedia'>, 'id' | 'content_type' | 'size' | 'sha256' | 'url' | 'created_at'>>;
const helpCenterSchema = z.object({
  enabled: z.boolean(), title: z.string(), locale: z.string(), require_approval: z.boolean(), content_version: z.number().int(), updated_at: nullableDate, url: z.string(),
}) satisfies z.ZodType<Generated<'HelpCenter'>>;

const orderSchema = z.object({
  parent_id: z.string().uuid().nullable(), collection_ids: z.array(z.string().uuid()), article_ids: z.array(z.string().uuid()),
}) satisfies z.ZodType<Generated<'KBOrder'>>;
const collectionListSchema = z.object({ items: z.array(kbCollectionSchema) }) satisfies z.ZodType<JsonResponse<'listKBCollections', 200>>;
const trashedCollectionListSchema = z.object({ items: z.array(trashedCollectionSchema) }) satisfies z.ZodType<JsonResponse<'listTrashedKBCollections', 200>>;
const articlePageSchema = z.object({ items: z.array(summarySchema), next_cursor: z.string().nullable() }) satisfies z.ZodType<JsonResponse<'listKBArticles', 200>>;
const trashedArticlePageSchema = z.object({ items: z.array(trashedArticleSchema), next_cursor: z.string().nullable() }) satisfies z.ZodType<JsonResponse<'listTrashedKBArticles', 200>>;
const versionPageSchema = z.object({ items: z.array(versionSchema), next_before_version: z.number().int().nullable() }) satisfies z.ZodType<JsonResponse<'listKBArticleVersions', 200>>;

export type KBCollection = z.infer<typeof kbCollectionSchema>;
export type TrashedKBCollection = z.infer<typeof trashedCollectionSchema>;
export type KBArticleSummary = z.infer<typeof summarySchema>;
export type KBArticle = z.infer<typeof kbArticleSchema>;
export type KBArticleDraft = z.infer<typeof draftSchema>;
export type KBAttachment = z.infer<typeof attachmentSchema>;
export type TrashedKBArticle = z.infer<typeof trashedArticleSchema>;
export type KBArticleVersion = z.infer<typeof versionSchema>;
export type KBArticlePage = z.infer<typeof articlePageSchema>;
export type HelpCenter = z.infer<typeof helpCenterSchema>;
export type KBArticleStatus = z.infer<typeof statusSchema>;
export type KBCollectionChange = Omit<RequestBody<'updateKBCollection'>, 'updated_at'>;
export type KBArticleChange = Omit<RequestBody<'updateKBArticle'>, 'updated_at'>;
export type KBOrder = z.infer<typeof orderSchema>;
export type KBTranslationSummary = z.infer<typeof translationSummarySchema>;
export type KBTranslation = z.infer<typeof kbTranslationSchema>;
export type KBTranslationVersion = z.infer<typeof translationVersionSchema>;
export type KBLanguageItem = z.infer<typeof languageItemSchema>;
export type HelpCenterLanguages = z.infer<typeof helpCenterLanguagesSchema>;
export type KBApprovalMode = z.infer<typeof approvalModeSchema>;
export type KBVisibility = z.infer<typeof visibilitySchema>;
export type KBLanguageState = 'missing' | 'outdated' | 'translated';

type Operation = 'createKBCollection' | 'updateKBCollection' | 'restoreKBCollection' | 'createKBArticle' | 'updateKBArticle' | 'restoreKBArticle'
  | 'restoreKBArticleVersion' | 'putHelpCenter' | 'setKBOrder' | 'putHelpCenterLanguages' | 'createKBTranslation' | 'updateKBTranslation' | 'restoreKBTranslation'
  | 'restoreKBTranslationVersion' | 'putKBCollectionTranslation' | 'publishKBArticleDraft'
  | 'setKBArticleAttachments' | 'setKBTranslationAttachments';
const json = <O extends Operation>(value: RequestBody<O>): string => JSON.stringify(value);

// The calls that answer 204 carry no body.
const noContent = z.undefined();

// sort is the order without q: recent (the default without collection_id) or manual, the help center's.
// limit is the page size, 1 to 200 (50 when left out). has_draft 'true' keeps the articles with unpublished changes.
// state keeps the articles whose unpublished text waits for a reviewer (in_review) or was approved as it is (approved).
export type ArticleQuery = { q?: string; status?: KBArticleStatus; collection_id?: string; sort?: 'recent' | 'manual'; limit?: number; cursor?: string; has_draft?: 'true'; state?: 'in_review' | 'approved'; visibility?: KBVisibility };

// query is called with a variable, never an object literal, inside a path: the gate that checks every path
// the web app calls is a route (web_routes_test.go) reads a path up to its first closing brace.
function query(values: Record<string, string | number | undefined>): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(values)) if (value) params.set(key, String(value));
  const text = params.toString();
  return text ? `?${text}` : '';
}

const version = (updatedAt: string) => query({ updated_at: updatedAt });
const page = (cursor?: string) => query({ cursor });
const before = (beforeVersion?: number) => query({ before_version: beforeVersion ? String(beforeVersion) : undefined });

export const kbApi = {
  collections: () => request('/api/v1/kb/collections', collectionListSchema),
  trashedCollections: () => request('/api/v1/kb/collections/trash', trashedCollectionListSchema),
  createCollection: (value: RequestBody<'createKBCollection'>, key: string) => request('/api/v1/kb/collections', kbCollectionSchema, { method: 'POST', headers: { 'Idempotency-Key': key }, body: json<'createKBCollection'>(value) }),
  updateCollection: (id: string, value: RequestBody<'updateKBCollection'>) => request(`/api/v1/kb/collections/${id}`, kbCollectionSchema, { method: 'PATCH', body: json<'updateKBCollection'>(value) }),
  // updatedAt is the version the person saw; the server refuses the delete if it was saved since.
  deleteCollection: (id: string, updatedAt: string) => request(`/api/v1/kb/collections/${id}${version(updatedAt)}`, noContent, { method: 'DELETE' }),
  restoreCollection: (id: string, updatedAt: string) => request(`/api/v1/kb/collections/${id}/restore`, kbCollectionSchema, { method: 'POST', body: json<'restoreKBCollection'>({ updated_at: updatedAt }) }),
  // signal lets a newer search cancel the request of the one it replaced.
  articles: (filter: ArticleQuery, signal?: AbortSignal) => request(`/api/v1/kb/articles${query(filter)}`, articlePageSchema, { signal }),
  trashedArticles: (cursor?: string) => request(`/api/v1/kb/articles/trash${page(cursor)}`, trashedArticlePageSchema),
  article: (id: string) => request(`/api/v1/kb/articles/${id}`, kbArticleSchema),
  // key names this save (Idempotency-Key): a retry with the same key returns the article the first stored.
  createArticle: (value: RequestBody<'createKBArticle'>, key: string) => request('/api/v1/kb/articles', kbArticleSchema,
    { method: 'POST', headers: { 'Idempotency-Key': key }, body: json<'createKBArticle'>(value) }),
  // init carries keepalive for an autosave sent as the page closes.
  updateArticle: (id: string, value: RequestBody<'updateKBArticle'>, init?: RequestInit) => request(`/api/v1/kb/articles/${id}`, kbArticleSchema, { ...init, method: 'PATCH', body: json<'updateKBArticle'>(value) }),
  deleteArticle: (id: string, updatedAt: string) => request(`/api/v1/kb/articles/${id}${version(updatedAt)}`, noContent, { method: 'DELETE' }),
  // Publishing the unpublished changes puts them on the help center; key names the press (Idempotency-Key).
  publishDraft: (id: string, updatedAt: string, key: string) => request(`/api/v1/kb/articles/${id}/draft/publish`, kbArticleSchema,
    { method: 'POST', headers: { 'Idempotency-Key': key }, body: json<'publishKBArticleDraft'>({ updated_at: updatedAt }) }),
  // Discarding them leaves the published article and its history as they are.
  discardDraft: (id: string, updatedAt: string) => request(`/api/v1/kb/articles/${id}/draft${version(updatedAt)}`, kbArticleSchema, { method: 'DELETE' }),
  restoreArticle: (id: string, updatedAt: string) => request(`/api/v1/kb/articles/${id}/restore`, kbArticleSchema, { method: 'POST', body: json<'restoreKBArticle'>({ updated_at: updatedAt }) }),
  // AI may not read an excluded article, in any language; including it again undoes that.
  excludeFromAI: (id: string) => request(`/api/v1/kb/articles/${id}/ai-exclusion`, kbArticleSchema, { method: 'POST' }),
  includeInAI: (id: string) => request(`/api/v1/kb/articles/${id}/ai-exclusion`, kbArticleSchema, { method: 'DELETE' }),
  versions: (id: string, beforeVersion?: number) => request(`/api/v1/kb/articles/${id}/versions${before(beforeVersion)}`, versionPageSchema),
  restoreVersion: (id: string, version: number, updatedAt: string) => request(`/api/v1/kb/articles/${id}/versions/${version}/restore`, kbArticleSchema,
    { method: 'POST', body: json<'restoreKBArticleVersion'>({ updated_at: updatedAt }) }),
  // The files listed under the article, in order; on a published article they go into its draft.
  setAttachments: (id: string, mediaIds: string[], updatedAt: string) => request(`/api/v1/kb/articles/${id}/attachments`, kbArticleSchema,
    { method: 'PUT', body: json<'setKBArticleAttachments'>({ media_ids: mediaIds, updated_at: updatedAt }) }),
  // The body is the image itself; its own type replaces the JSON one request() sets.
  uploadImage: (file: Blob) => request('/api/v1/kb/images', imageSchema, { method: 'POST', body: file, headers: { 'Content-Type': file.type } }),
  // The lists name everything directly under parent_id, first to last; a stale pair is a ConflictError
  // whose detail holds the lists as they are (order_stale).
  setOrder: (value: KBOrder) => request('/api/v1/kb/order', orderSchema, { method: 'PUT', body: json<'setKBOrder'>(value) }),
  helpCenter: () => request('/api/v1/help-center', helpCenterSchema),
  // The help center's languages besides the primary one, in menu order, each shown or hidden.
  languages: () => request('/api/v1/help-center/languages', helpCenterLanguagesSchema),
  saveLanguages: (value: RequestBody<'putHelpCenterLanguages'>) => request('/api/v1/help-center/languages', helpCenterLanguagesSchema,
    { method: 'PUT', body: json<'putHelpCenterLanguages'>(value) }),
  // The live articles as one language sees them: missing, outdated or translated in it.
  languageArticles: (locale: string, state: KBLanguageState, cursor?: string) => {
    const filter = query({ locale, state, cursor });
    return request(`/api/v1/kb/translations${filter}`, languagePageSchema);
  },
  trashedTranslations: (cursor?: string) => request(`/api/v1/kb/translations/trash${page(cursor)}`, trashedTranslationPageSchema),
  translation: (id: string, locale: string) => request(`/api/v1/kb/articles/${id}/translations/${encodeURIComponent(locale)}`, kbTranslationSchema),
  createTranslation: (id: string, value: RequestBody<'createKBTranslation'>, key: string) => request(`/api/v1/kb/articles/${id}/translations`, kbTranslationSchema,
    { method: 'POST', headers: { 'Idempotency-Key': key }, body: json<'createKBTranslation'>(value) }),
  updateTranslation: (id: string, locale: string, value: RequestBody<'updateKBTranslation'>) => request(`/api/v1/kb/articles/${id}/translations/${encodeURIComponent(locale)}`, kbTranslationSchema,
    { method: 'PATCH', body: json<'updateKBTranslation'>(value) }),
  deleteTranslation: (id: string, locale: string, updatedAt: string) => request(`/api/v1/kb/articles/${id}/translations/${encodeURIComponent(locale)}${version(updatedAt)}`, noContent, { method: 'DELETE' }),
  // key names this press (Idempotency-Key): a repeat answers the first restore.
  restoreTranslation: (id: string, locale: string, updatedAt: string, key: string) => request(`/api/v1/kb/articles/${id}/translations/${encodeURIComponent(locale)}/restore`, kbTranslationSchema,
    { method: 'POST', headers: { 'Idempotency-Key': key }, body: json<'restoreKBTranslation'>({ updated_at: updatedAt }) }),
  translationVersions: (id: string, locale: string, beforeVersion?: number) => request(`/api/v1/kb/articles/${id}/translations/${encodeURIComponent(locale)}/versions${before(beforeVersion)}`,
    translationVersionPageSchema),
  restoreTranslationVersion: (id: string, locale: string, version: number, updatedAt: string, key: string) => request(`/api/v1/kb/articles/${id}/translations/${encodeURIComponent(locale)}/versions/${version}/restore`,
    kbTranslationSchema, { method: 'POST', headers: { 'Idempotency-Key': key }, body: json<'restoreKBTranslationVersion'>({ updated_at: updatedAt }) }),
  // A translation's own files apply at once; with none, the help center lists the article's.
  setTranslationAttachments: (id: string, locale: string, mediaIds: string[], updatedAt: string) => request(`/api/v1/kb/articles/${id}/translations/${encodeURIComponent(locale)}/attachments`,
    kbTranslationSchema, { method: 'PUT', body: json<'setKBTranslationAttachments'>({ media_ids: mediaIds, updated_at: updatedAt }) }),
  nameCollection: (id: string, locale: string, value: RequestBody<'putKBCollectionTranslation'>) => request(`/api/v1/kb/collections/${id}/translations/${encodeURIComponent(locale)}`,
    kbCollectionSchema, { method: 'PUT', body: json<'putKBCollectionTranslation'>(value) }),
  unnameCollection: (id: string, locale: string) => request(`/api/v1/kb/collections/${id}/translations/${encodeURIComponent(locale)}`, kbCollectionSchema, { method: 'DELETE' }),
  saveHelpCenter: (value: RequestBody<'putHelpCenter'>) => request('/api/v1/help-center', helpCenterSchema, { method: 'PUT', body: json<'putHelpCenter'>(value) }),
};
