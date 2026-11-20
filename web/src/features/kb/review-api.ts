import { z } from 'zod';
import { request } from '../../api/client';
import type { Generated, JsonResponse } from '../../api/schemas';
import { kbArticleSchema } from './kb-api';

// Reviews of knowledge base text: where a collection requires approval, a teammate who may publish approves
// the exact text before it goes on the help center. The schemas come with the knowledge base chunk.

const personSchema = z.object({ agent_id: z.string().uuid().nullable(), name: z.string() }) satisfies z.ZodType<Generated<'SavedReplyPerson'>>;
const tokenSchema = z.object({ id: z.string().uuid().nullable(), label: z.string() }).nullable() satisfies z.ZodType<Generated<'KBReviewToken'>>;
const reviewStateSchema = z.enum(['open', 'approved', 'changes_requested', 'withdrawn', 'superseded']);
export const kbReviewSchema = z.object({
  id: z.string().uuid(), article_id: z.string().uuid(), article_title: z.string(), locale: z.string().nullable(), state: reviewStateSchema,
  note: z.string(), content_hash: z.string(), current: z.boolean(),
  reviewers: z.array(z.object({ agent_id: z.string().uuid(), name: z.string(), seen_at: z.string().datetime().nullable() })),
  requested_by: personSchema.nullable(), requested_by_token: tokenSchema, decided_by: personSchema.nullable(), decided_by_token: tokenSchema,
  decided_at: z.string().datetime().nullable(), decision_note: z.string(), supersedes_review_id: z.string().uuid().nullable(), created_at: z.string().datetime(),
}) satisfies z.ZodType<Generated<'KBReview'>>;
const reviewListSchema = z.object({ reviews: z.array(kbReviewSchema) }) satisfies z.ZodType<JsonResponse<'listKBReviews', 200>>;
const contentSchema = z.object({
  title: z.string(), slug: z.string(), body_md: z.string(), seo_title: z.string(), seo_description: z.string(), collection_ids: z.array(z.string().uuid()),
}) satisfies z.ZodType<Generated<'KBReviewContent'>>;
const reviewDetailSchema = kbReviewSchema.extend({
  live: contentSchema.nullable(), proposed: contentSchema, diff: z.array(z.object({ change: z.enum(['same', 'removed', 'added']), text: z.string() })),
}) satisfies z.ZodType<JsonResponse<'getKBReview', 200>>;
const bulkError = z.object({ code: z.string(), message: z.string() });
const approveResultsSchema = z.object({ results: z.array(z.object({ id: z.string(), review: kbReviewSchema.optional(), error: bulkError.optional() })) }) satisfies z.ZodType<JsonResponse<'approveKBReviews', 200>>;
const publishResultsSchema = z.object({ results: z.array(z.object({ id: z.string(), article: kbArticleSchema.optional(), error: bulkError.optional() })) }) satisfies z.ZodType<JsonResponse<'publishKBArticles', 200>>;

export type KBReview = z.infer<typeof kbReviewSchema>;
export type KBReviewDetail = z.infer<typeof reviewDetailSchema>;
export type KBReviewState = z.infer<typeof reviewStateSchema>;
export type BulkResult = { id: string; error?: { code: string; message: string } };

// The text a review is about: the article's own, or one translation's.
const textPath = (articleId: string, locale?: string) => `/api/v1/kb/articles/${articleId}${locale ? `/translations/${encodeURIComponent(locale)}` : ''}`;
const post = (key: string, body: unknown) => ({ method: 'POST', headers: { 'Idempotency-Key': key }, body: JSON.stringify(body) });

export const reviewApi = {
  // reviewer 'me' lists what waits for the person signed in.
  reviews: (filter: { state?: KBReviewState; reviewer?: 'me' }) => request(`/api/v1/kb/reviews?${new URLSearchParams(filter)}`, reviewListSchema),
  ofText: (articleId: string, locale?: string) => request(`${textPath(articleId, locale)}/reviews`, reviewListSchema),
  review: (id: string) => request(`/api/v1/kb/reviews/${id}`, reviewDetailSchema),
  send: (of: { articleId: string; locale?: string; reviewerIds: string[]; note: string }, key: string) =>
    request(`${textPath(of.articleId, of.locale)}/reviews`, kbReviewSchema, post(key, { reviewer_ids: of.reviewerIds, note: of.note })),
  approve: (id: string, key: string) => request(`/api/v1/kb/reviews/${id}/approve`, kbReviewSchema, post(key, {})),
  approveMany: (ids: string[], key: string) => request('/api/v1/kb/reviews/approve', approveResultsSchema, post(key, { review_ids: ids })),
  requestChanges: (id: string, note: string, key: string) => request(`/api/v1/kb/reviews/${id}/request-changes`, kbReviewSchema, post(key, { note })),
  withdraw: (id: string, key: string) => request(`/api/v1/kb/reviews/${id}/withdraw`, kbReviewSchema, post(key, {})),
  publishMany: (ids: string[], key: string) => request('/api/v1/kb/articles/publish', publishResultsSchema, post(key, { article_ids: ids })),
};

// reviewsKey is the cache of one text's reviews; openForMeKey, the badge's count of what waits for me.
export const reviewsKey = (articleId: string, locale?: string) => ['kb-reviews', articleId, locale ?? ''];
export const openForMeKey = ['kb-reviews', 'open', 'me'];
