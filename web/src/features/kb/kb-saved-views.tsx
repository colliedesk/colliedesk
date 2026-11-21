import { useState } from 'react';
import { z } from 'zod';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { request } from '../../api/client';
import type { RequestBody } from '../../api/client';
import { viewFields } from '../../api/saved-views';
import type { Generated, JsonResponse } from '../../api/schemas';
import { useCreateMutation } from '../../api/use-create';
import { SavedViewsPanel } from '../saved-views/saved-views-panel';
import type { SavedViewChange } from '../saved-views/saved-views-panel';
import { articleFiltersOf, knowledgeSearchOf } from './knowledge-search';
import type { KBArticleFilters, KnowledgeSearch } from './knowledge-search';

// Saved views of the knowledge base article list (area kb): the same menu as the inbox's, over the list's filters.

const filtersSchema = z.object({
  q: z.string().optional(), status: z.enum(['draft', 'published']).optional(), collection_id: z.string().uuid().optional(), include_subcollections: z.boolean().optional(),
  author: z.array(z.string().uuid()).optional(), tag: z.array(z.string()).optional(), tag_match: z.enum(['any', 'all']).optional(),
  created_after: z.string().optional(), created_before: z.string().optional(), updated_after: z.string().optional(), updated_before: z.string().optional(),
  translation: z.enum(['missing', 'outdated']).optional(), locale: z.string().optional(), has_draft: z.boolean().optional(),
  visibility: z.enum(['everyone', 'help_chat', 'agents']).optional(), state: z.enum(['in_review', 'approved']).optional(), sort: z.enum(['recent', 'manual']).optional(),
}) satisfies z.ZodType<KBArticleFilters>;
const viewSchema = z.object({ ...viewFields, area: z.literal('kb'), filters: filtersSchema }) satisfies z.ZodType<Generated<'SavedView'>>;
const listSchema = z.object({ items: z.array(viewSchema) }) satisfies z.ZodType<JsonResponse<'listSavedViews', 200>>;
type KBView = z.infer<typeof viewSchema>;
type Change = SavedViewChange<KBArticleFilters>;

const json = <Operation extends 'createSavedView' | 'updateSavedView'>(value: RequestBody<Operation>): string => JSON.stringify(value);
const area = 'kb';
const kbViewsApi = {
  list: () => request(`/api/v1/saved-views?area=${area}`, listSchema),
  trash: () => request(`/api/v1/saved-views/trash?area=${area}`, listSchema),
  create: (value: Omit<RequestBody<'createSavedView'>, 'area'>, key: string) => request('/api/v1/saved-views', viewSchema,
    { method: 'POST', headers: { 'Idempotency-Key': key }, body: json<'createSavedView'>({ ...value, area }) }),
  update: (id: string, value: RequestBody<'updateSavedView'>) => request(`/api/v1/saved-views/${id}`, viewSchema, { method: 'PATCH', body: json<'updateSavedView'>(value) }),
  remove: (id: string) => request(`/api/v1/saved-views/${id}`, viewSchema, { method: 'DELETE' }),
  restore: (id: string) => request(`/api/v1/saved-views/${id}/restore`, viewSchema, { method: 'POST' }),
};

// sameFilters: a view is current when opening it would give the address on screen.
const sameFilters = (search: KnowledgeSearch, view: KBView) => JSON.stringify(articleFiltersOf(search)) === JSON.stringify(articleFiltersOf(knowledgeSearchOf(view.filters)));

export function KBSavedViews({ search, timezone, onApply }: { search: KnowledgeSearch; timezone: string; onApply: (search: KnowledgeSearch) => void }) {
  const client = useQueryClient();
  const [opened, setOpened] = useState(false);
  const [trashOpened, setTrashOpened] = useState(false);
  const views = useQuery({ queryKey: ['saved-views', area], queryFn: kbViewsApi.list, enabled: opened });
  const trash = useQuery({ queryKey: ['saved-views', area, 'trash'], queryFn: kbViewsApi.trash, enabled: trashOpened });
  const changed = () => client.invalidateQueries({ queryKey: ['saved-views', area] });
  const create = useCreateMutation({ create: (view: Parameters<typeof kbViewsApi.create>[0], key: string) => kbViewsApi.create(view, key), onSettled: changed });
  // A change carries the version it was made on; someone else's save since is refused (409), not overwritten.
  const update = useMutation({ mutationFn: ({ view, change }: { view: KBView; change: Change }) => kbViewsApi.update(view.id, { ...change, updated_at: view.updated_at }),
    onSettled: changed });
  const remove = useMutation({ mutationFn: (view: KBView) => kbViewsApi.remove(view.id), onSettled: changed });
  const restore = useMutation({ mutationFn: (view: KBView) => kbViewsApi.restore(view.id), onSettled: changed });
  return <SavedViewsPanel<KBArticleFilters, KBView> views={views.data?.items ?? []} loading={views.isPending} trash={trash.data?.items} timezone={timezone}
    filters={articleFiltersOf(search)} isCurrent={(view) => sameFilters(search, view)} onApply={(view) => onApply(knowledgeSearchOf(view.filters))}
    onOpen={() => setOpened(true)} onOpenTrash={() => setTrashOpened(true)}
    onCreate={(view) => create.mutateAsync(view)} onUpdate={(view, change) => update.mutateAsync({ view, change })}
    onDelete={(view) => remove.mutateAsync(view)} onRestore={(view) => restore.mutateAsync(view)} />;
}
