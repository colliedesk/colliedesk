import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { savedViewsApi } from '../../api/saved-views';
import { trackFilterChanges } from './filter-analytics';
import type { ConversationFilters, SavedView } from '../../api/schemas';
import { filtersOf, queryOf } from '../../lib/inbox-query-edit';
import type { InboxQuery } from '../../lib/inbox-query';
import { onView } from '../../lib/inbox-scope';
import { SavedViewsPanel } from '../saved-views/saved-views-panel';
import type { SavedViewChange as AnyViewChange } from '../saved-views/saved-views-panel';
import { useCreateMutation } from '../../api/use-create';

export type SavedViewChange = AnyViewChange<ConversationFilters>;

interface SavedViewsMenuProps {
  views: SavedView[];
  // loading: the views are on their way, so the menu does not say there are none yet.
  loading?: boolean;
  trash?: SavedView[];
  // The filters on screen: the view they match is current, and saving stores them.
  query: InboxQuery;
  timezone: string;
  onApply?: (query: InboxQuery) => void;
  onCreate?: (view: { name: string; scope: SavedView['scope']; filters: ConversationFilters }) => Promise<unknown>;
  onUpdate?: (view: SavedView, change: SavedViewChange) => Promise<unknown>;
  // A delete moves the view to the trash; onRestore takes it out again.
  onDelete?: (view: SavedView) => Promise<unknown>;
  onRestore?: (view: SavedView) => Promise<unknown>;
  // The views are asked for when the menu opens, the trash when its section opens; until then trash is
  // undefined, and the section shows no count.
  onOpen?: () => void;
  onOpenTrash?: () => void;
}


// SavedViewsMenu loads the views and their trash and keeps them current after each change. It is a lazy
// part of the inbox: the views are only needed once the person looks at them.
export function SavedViewsMenu({ query, timezone, onApply }: Pick<SavedViewsMenuProps, 'query' | 'timezone' | 'onApply'>) {
  const client = useQueryClient();
  const [opened, setOpened] = useState(false);
  const [trashOpened, setTrashOpened] = useState(false);
  const views = useQuery({ queryKey: ['saved-views'], queryFn: savedViewsApi.list, enabled: opened });
  const trash = useQuery({ queryKey: ['saved-views', 'trash'], queryFn: savedViewsApi.trash, enabled: trashOpened });
  const changed = () => client.invalidateQueries({ queryKey: ['saved-views'] });
  const create = useCreateMutation({ create: (view: Parameters<typeof savedViewsApi.create>[0], key: string) => savedViewsApi.create(view, key), onSettled: changed });
  // A change carries the version it was made on; someone else's save since is refused (409), not overwritten.
  const update = useMutation({ mutationFn: ({ view, change }: { view: SavedView; change: SavedViewChange }) => savedViewsApi.update(view.id, { ...change, updated_at: view.updated_at }),
    onSettled: changed });
  const remove = useMutation({ mutationFn: (view: SavedView) => savedViewsApi.remove(view.id), onSettled: changed });
  const restore = useMutation({ mutationFn: (view: SavedView) => savedViewsApi.restore(view.id), onSettled: changed });
  return <SavedViewsMenuView views={views.data?.items ?? []} loading={views.isPending} trash={trash.data?.items} query={query} timezone={timezone}
    onApply={(next) => { trackFilterChanges(query, next); onApply?.(next); }}
    onOpen={() => setOpened(true)} onOpenTrash={() => setTrashOpened(true)}
    onCreate={(view) => create.mutateAsync(view)} onUpdate={(view, change) => update.mutateAsync({ view, change })}
    onDelete={(view) => remove.mutateAsync(view)} onRestore={(view) => restore.mutateAsync(view)} />;
}

// SavedViewsMenuView keeps named sets of inbox filters; the view the filters on screen match is current,
// the same one the sidebar marks and the list's title names (onView).
export function SavedViewsMenuView({ query, onApply, ...rest }: SavedViewsMenuProps) {
  return <SavedViewsPanel<ConversationFilters, SavedView> {...rest} filters={filtersOf(query)} isCurrent={(view) => onView(query, view.filters)}
    onApply={(view) => onApply?.(queryOf(view.filters))} />;
}
