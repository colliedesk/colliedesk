import { z } from 'zod';
import { ChangedElsewhereError, request } from '../../api/client';
import type { RequestBody } from '../../api/client';
import type { Generated, JsonResponse } from '../../api/schemas';

// The media library's schemas and calls, in the knowledge base chunk. A video goes up in parts
// (uploadVideo below); an image in one request.

const usageSchema = z.object({
  articles: z.number().int(), trashed_articles: z.number().int(), older_versions: z.number().int(), help_center: z.array(z.enum(['logo', 'favicon'])),
}) satisfies z.ZodType<Generated<'KBMediaUsage'>>;
export const kbMediaSchema = z.object({
  id: z.string().uuid(), kind: z.enum(['image', 'video']),
  content_type: z.enum(['image/png', 'image/jpeg', 'image/gif', 'image/webp', 'video/mp4', 'video/webm']), size: z.number().int(), sha256: z.string(),
  url: z.string(), name: z.string(), alt: z.string(), width: z.number().int().nullable(), height: z.number().int().nullable(),
  duration_ms: z.number().int().nullable(), codecs: z.array(z.string()), thumbnail_url: z.string().nullable(), poster_url: z.string().nullable(),
  kept: z.boolean(), deleted_at: z.string().datetime().nullable(), created_at: z.string().datetime(), updated_at: z.string().datetime(),
  usage: usageSchema.optional(), purge_at: z.string().datetime().optional(),
}) satisfies z.ZodType<Generated<'KBMedia'>>;
const detailsSchema = kbMediaSchema.extend({
  articles: z.array(z.object({ id: z.string().uuid(), number: z.number().int(), title: z.string(), status: z.enum(['draft', 'published']), in_trash: z.boolean(), current: z.boolean() })),
}) satisfies z.ZodType<Generated<'KBMediaDetails'>>;
const summarySchema = z.object({ count: z.number().int(), bytes: z.number().int(), kept_count: z.number().int(), kept_bytes: z.number().int() }) satisfies z.ZodType<Generated<'KBMediaUnusedSummary'>>;
const pageSchema = z.object({ items: z.array(kbMediaSchema), next_cursor: z.string().nullable(), summary: summarySchema.optional() }) satisfies z.ZodType<JsonResponse<'listKBMedia', 200>>;
const trashPageSchema = z.object({ items: z.array(kbMediaSchema), next_cursor: z.string().nullable() }) satisfies z.ZodType<JsonResponse<'listTrashedKBMedia', 200>>;
const resultSchema = z.object({
  id: z.string().uuid(), status: z.enum(['trashed', 'already_in_trash', 'in_use', 'not_found', 'restored', 'not_in_trash', 'purged']),
}) satisfies z.ZodType<Generated<'KBMediaResult'>>;
const batchSchema = z.object({ results: z.array(resultSchema), batch_id: z.string().uuid().nullable() }) satisfies z.ZodType<Generated<'KBMediaBatchResult'>>;
const resultsSchema = z.object({ results: z.array(resultSchema) }) satisfies z.ZodType<JsonResponse<'restoreKBMediaBatch', 200>>;
const trashUnusedSchema = z.object({ trashed: z.number().int(), more: z.boolean(), batch_id: z.string().uuid().nullable() }) satisfies z.ZodType<JsonResponse<'trashUnusedKBMedia', 200>>;
const replacementSchema = z.object({
  id: z.string().uuid(), from_media_id: z.string().uuid(), to_media_id: z.string().uuid(), article_ids: z.array(z.string().uuid()),
  translations: z.array(z.object({ article_id: z.string().uuid(), locale: z.string() })), skipped_article_ids: z.array(z.string().uuid()),
  created_at: z.string().datetime(), undone_at: z.string().datetime().nullable(),
}) satisfies z.ZodType<Generated<'KBMediaReplacement'>>;
const uploadSchema = z.object({
  id: z.string().uuid(), kind: z.literal('video'), name: z.string(), size: z.number().int(), offset: z.number().int(), part_size: z.number().int(),
  expires_at: z.string().datetime(), media_id: z.string().uuid().nullable(),
  created_by: z.object({ agent_id: z.string().uuid().nullable(), name: z.string() }).nullable(), created_at: z.string().datetime(),
}) satisfies z.ZodType<Generated<'KBMediaUpload'>>;
const uploadListSchema = z.object({ items: z.array(uploadSchema) }) satisfies z.ZodType<JsonResponse<'listKBMediaUploads', 200>>;

export type KBMedia = z.infer<typeof kbMediaSchema>;
export type KBMediaDetails = z.infer<typeof detailsSchema>;
export type KBMediaPage = z.infer<typeof pageSchema>;
export type KBMediaSummary = z.infer<typeof summarySchema>;
export type KBMediaResult = z.infer<typeof resultSchema>;
export type KBMediaUpload = z.infer<typeof uploadSchema>;
export type MediaKind = KBMedia['kind'];
export type MediaUsage = 'in_use' | 'unused' | 'missing_alt';
export type MediaSort = 'newest' | 'oldest' | 'name' | 'largest';
export interface MediaQuery { kind?: MediaKind; q?: string; usage?: MediaUsage; sort?: MediaSort; cursor?: string }

type Operation = 'updateKBMedia' | 'trashKBMedia' | 'trashUnusedKBMedia' | 'restoreKBMediaBatch' | 'purgeKBMedia' | 'replaceKBMedia' | 'createKBMediaUpload';
const json = <O extends Operation>(value: RequestBody<O>): string => JSON.stringify(value);
const noContent = z.undefined();
const filterQuery = (filter: MediaQuery) => query({ ...filter });
const page = (cursor?: string) => query({ cursor });
const imageQuery = (name: string, keep: boolean) => query({ name, keep: keep ? 'true' : undefined });

// query is called with a variable, never an object literal, inside a path (see kb-api.ts).
function query(values: Record<string, string | undefined>): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(values)) if (value) params.set(key, value);
  const text = params.toString();
  return text ? `?${text}` : '';
}

export const mediaApi = {
  list: (filter: MediaQuery, signal?: AbortSignal) => request(`/api/v1/kb/media${filterQuery(filter)}`, pageSchema, { signal }),
  trash: (cursor?: string) => request(`/api/v1/kb/media/trash${page(cursor)}`, trashPageSchema),
  details: (id: string) => request(`/api/v1/kb/media/${id}/details`, detailsSchema),
  update: (id: string, value: RequestBody<'updateKBMedia'>) => request(`/api/v1/kb/media/${id}`, kbMediaSchema, { method: 'PATCH', body: json<'updateKBMedia'>(value) }),
  moveToTrash: (value: RequestBody<'trashKBMedia'>) => request('/api/v1/kb/media/trash', batchSchema, { method: 'POST', body: json<'trashKBMedia'>(value) }),
  trashUnused: (value: RequestBody<'trashUnusedKBMedia'>) => request('/api/v1/kb/media/trash-unused', trashUnusedSchema, { method: 'POST', body: json<'trashUnusedKBMedia'>(value) }),
  restore: (value: RequestBody<'restoreKBMediaBatch'>) => request('/api/v1/kb/media/restore', resultsSchema, { method: 'POST', body: json<'restoreKBMediaBatch'>(value) }),
  purge: (ids: string[]) => request('/api/v1/kb/media/purge', resultsSchema, { method: 'POST', body: json<'purgeKBMedia'>({ ids }) }),
  replace: (id: string, withId: string) => request(`/api/v1/kb/media/${id}/replace`, replacementSchema, { method: 'POST', body: json<'replaceKBMedia'>({ with_media_id: withId }) }),
  undoReplacement: (id: string) => request(`/api/v1/kb/media-replacements/${id}/undo`, replacementSchema, { method: 'POST' }),
  // The body is the image itself; keep marks an upload made in the library, which the purge spares.
  uploadImage: (file: Blob, name: string, keep: boolean) =>
    request(`/api/v1/kb/images${imageQuery(name, keep)}`, kbMediaSchema, { method: 'POST', body: file, headers: { 'Content-Type': file.type } }),
  setPoster: (id: string, image: Blob) => request(`/api/v1/kb/media/${id}/poster`, kbMediaSchema, { method: 'PUT', body: image, headers: { 'Content-Type': image.type } }),
  startUpload: (value: RequestBody<'createKBMediaUpload'>) => request('/api/v1/kb/media-uploads', uploadSchema, { method: 'POST', body: json<'createKBMediaUpload'>(value) }),
  upload: (id: string) => request(`/api/v1/kb/media-uploads/${id}`, uploadSchema),
  uploads: () => request('/api/v1/kb/media-uploads', uploadListSchema),
  cancelUpload: (id: string) => request(`/api/v1/kb/media-uploads/${id}`, noContent, { method: 'DELETE' }),
  completeUpload: (id: string) => request(`/api/v1/kb/media-uploads/${id}/complete`, kbMediaSchema, { method: 'POST' }),
};

// updateFields changes an item's name, alt text or kept mark from the version item was read at. A refusal
// because the item was saved since, by a change that left those three as they were (a poster drawn after its
// upload), is no conflict for them: the change goes again over the newer version. Any other is thrown.
export async function updateFields(item: KBMedia, change: Omit<RequestBody<'updateKBMedia'>, 'updated_at'>): Promise<KBMedia> {
  try {
    return await mediaApi.update(item.id, { updated_at: item.updated_at, ...change });
  } catch (error) {
    if (!(error instanceof ChangedElsewhereError)) throw error;
    const fresh = await mediaApi.details(item.id);
    if (fresh.name !== item.name || fresh.alt !== item.alt || fresh.kept !== item.kept) throw error;
    return mediaApi.update(item.id, { updated_at: fresh.updated_at, ...change });
  }
}

// sendPart puts one part of an upload with its Content-Range and reports the bytes sent as they go;
// fetch cannot tell upload progress, so it is an XMLHttpRequest. It resolves with the status and the
// upload as the server answered (or the error body), and rejects when aborted or offline.
export type PartSender = (id: string, part: Blob, first: number, size: number, onSent: (bytes: number) => void, signal: AbortSignal) =>
  Promise<{ status: number; body: unknown; offset: string | null }>;

export const sendPart: PartSender = (id, part, first, size, onSent, signal) => new Promise((resolve, reject) => {
  const xhr = new XMLHttpRequest();
  xhr.open('PUT', `/api/v1/kb/media-uploads/${id}`);
  xhr.withCredentials = true;
  xhr.setRequestHeader('Content-Type', 'application/octet-stream');
  xhr.setRequestHeader('Content-Range', `bytes ${first}-${first + part.size - 1}/${size}`);
  xhr.upload.onprogress = (event) => onSent(event.loaded);
  xhr.onload = () => {
    let body: unknown;
    try { body = JSON.parse(xhr.responseText); } catch { body = undefined; }
    resolve({ status: xhr.status, body, offset: xhr.getResponseHeader('Upload-Offset') });
  };
  xhr.onerror = () => reject(new Error('offline'));
  xhr.onabort = () => reject(new DOMException('Aborted', 'AbortError'));
  signal.addEventListener('abort', () => xhr.abort(), { once: true });
  xhr.send(part);
});
