import { z } from 'zod';
import { request } from './client';
import type { RequestBody } from './client';
import { channelSchema, conversationSortSchema, prioritySchema, statusSchema } from './schemas';
import type { Generated, JsonResponse } from './schemas';

// Saved views load with the Views menu, a lazy part of the inbox, so their schemas and calls are not in
// the first bundle.

const nullableDate = z.string().datetime().nullable();
export const conversationFiltersSchema = z.object({
  status: z.array(statusSchema).optional(), priority: z.array(prioritySchema).optional(), assignee: z.array(z.string()).optional(),
  tag: z.array(z.string().uuid()).optional(), tag_match: z.enum(['any', 'all']).optional(), contact: z.string().uuid().optional(),
  organization: z.string().optional(), awaiting: z.enum(['team', 'customer']).optional(), has_pending_draft: z.boolean().optional(),
  delivery_failed: z.boolean().optional(), source: z.enum(['imported', 'native']).optional(), channel: z.array(channelSchema).optional(),
  channel_id: z.array(z.string().uuid()).optional(), sla: z.array(z.enum(['overdue', 'due_soon'])).optional(),
  created_after: z.string().datetime({ offset: true }).optional(), created_before: z.string().datetime({ offset: true }).optional(),
  updated_after: z.string().datetime({ offset: true }).optional(), updated_before: z.string().datetime({ offset: true }).optional(),
  sort: conversationSortSchema.optional(), search: z.string().optional(),
}) satisfies z.ZodType<Generated<'ConversationFilters'>>;
export const savedViewSchema = z.object({
  id: z.string().uuid(), name: z.string(), scope: z.enum(['personal', 'shared']), filters: conversationFiltersSchema,
  owner_agent_id: z.string().uuid().nullable(), created_at: z.string().datetime(), updated_at: z.string().datetime(),
  deleted_at: nullableDate, purges_at: nullableDate,
}) satisfies z.ZodType<Generated<'SavedView'>>;
export const savedViewListSchema = z.object({ items: z.array(savedViewSchema) }) satisfies z.ZodType<JsonResponse<'listSavedViews', 200>>;

const json = <Operation extends 'createSavedView' | 'updateSavedView'>(value: RequestBody<Operation>): string => JSON.stringify(value);

export const savedViewsApi = {
  list: () => request('/api/v1/saved-views', savedViewListSchema),
  trash: () => request('/api/v1/saved-views/trash', savedViewListSchema),
  create: (value: RequestBody<'createSavedView'>, key: string) => request('/api/v1/saved-views', savedViewSchema, { method: 'POST', headers: { 'Idempotency-Key': key }, body: json<'createSavedView'>(value) }),
  update: (id: string, value: RequestBody<'updateSavedView'>) => request(`/api/v1/saved-views/${id}`, savedViewSchema, { method: 'PATCH', body: json<'updateSavedView'>(value) }),
  remove: (id: string) => request(`/api/v1/saved-views/${id}`, savedViewSchema, { method: 'DELETE' }),
  restore: (id: string) => request(`/api/v1/saved-views/${id}/restore`, savedViewSchema, { method: 'POST' }),
};
