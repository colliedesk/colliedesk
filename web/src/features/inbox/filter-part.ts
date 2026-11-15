import { useContext, useEffect, useState } from 'react';
import { QueryClientContext } from '@tanstack/react-query';
import { channelsApi } from '../../api/channels';
import type { ChannelList } from '../../api/channels';
import type { Conversation, Tag, TeamMember } from '../../api/schemas';
import type { InboxQuery, InboxQueryKey } from '../../lib/inbox-query';
import { useHolds } from '../../lib/permissions-context';
import { trackFilterChanges } from './filter-analytics';

// What the Filter menu, the Sort menu and the chips share. They load on their own chunk, right after the first screen
// (inbox-filters.tsx); only types of this module may be imported from the first bundle.

export interface FilterPartProps {
  query: InboxQuery;
  onChange?: (query: InboxQuery) => void;
  tags: Tag[];
  agents: TeamMember[];
  timezone: string;
  conversations?: Conversation[];
  // The signed-in agent: listed as Me, never a second time under their name.
  currentAgentId?: string;
  // The order Due first gives this workspace's list, as the server last answered it: waiting_longest while
  // reply targets are off. The menu then names that order and does not offer Due first beside it.
  dueSort?: string;
  // The channel the list's title names: it has no chip and is not counted on Filter.
  scopeChannel?: string;
}

export const dateFilters = [['created', 'created_after', 'created_before'], ['updated', 'updated_after', 'updated_before']] as const;

// useChannelList reads the workspace's channels, removed ones too, once it is wanted: to offer their types
// and choose one channel in the menu, and to name the chip of one. Through the app's query cache (Settings reloads it);
// a part rendered without one reads them itself.
export function useChannelList(asked: boolean): ChannelList | undefined {
  const client = useContext(QueryClientContext);
  // The list of channels is read only by a role that may read channels (channels:read).
  const reads = useHolds()('channels:read');
  const wanted = asked && reads;
  const [list, setList] = useState<ChannelList>();
  useEffect(() => {
    if (!wanted) return undefined;
    let live = true;
    const load = client ? client.fetchQuery({ queryKey: ['channels', 'all'], queryFn: channelsApi.listAll, staleTime: 30_000 }) : channelsApi.listAll();
    load.then((value) => { if (live) setList(value); }, () => {});
    return () => { live = false; };
  }, [client, wanted]);
  return list;
}

// changeOf replaces the filters (reporting what kind changed) and sets one of them.
export function changeOf({ query, onChange }: Pick<FilterPartProps, 'query' | 'onChange'>) {
  const change = (next: InboxQuery) => { trackFilterChanges(query, next); onChange?.(next); };
  const set = (key: InboxQueryKey, value: string | boolean | undefined) => {
    const next: InboxQuery = { ...query };
    if (value === undefined || value === '') delete next[key];
    else (next as Record<string, string | boolean>)[key] = value;
    if (!next.tag) delete next.tag_match;
    change(next);
  };
  return { change, set };
}
