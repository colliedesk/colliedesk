import type { ConversationFilters } from '../api/schemas';
import { sameQuery } from './inbox-query';
import type { InboxQuery, InboxQueryKey } from './inbox-query';
import { queryOf } from './inbox-query-edit';

// Where the inbox list is: one channel, a saved view, or every channel. Status, sort and search are what an agent
// changes inside that place (the list header has a control for each), so they never move the list out of it.
const insidePlace: readonly string[] = ['status', 'sort', 'search'];

// scopedChannel is the one channel the list is narrowed to (channel_id with a single id), if any.
export function scopedChannel(query: InboxQuery): string | undefined {
  const id = query.channel_id;
  return id && !id.includes(',') ? id : undefined;
}

// onChannel: the list is that channel's conversations, under any status, order or search, and nothing else narrows it.
export function onChannel(query: InboxQuery, channelId: string): boolean {
  return scopedChannel(query) === channelId && Object.keys(query).every((key) => key === 'channel_id' || insidePlace.includes(key));
}

const without = (query: InboxQuery, keys: readonly string[]): InboxQuery =>
  Object.fromEntries(Object.entries(query).filter(([key]) => !keys.includes(key))) as InboxQuery;

// onView: the list shows a saved view's filters. Its sort and search are the agent's to change inside the view. A status
// the view stores is part of it; a view that stores none shows every status, and the agent may pick one inside it. A view
// that stores nothing besides those keys would otherwise match every list, so it then matches only exactly.
export function onView(query: InboxQuery, filters: ConversationFilters): boolean {
  const view = queryOf(filters);
  const loose: string[] = filters.status?.length ? ['sort', 'search'] : [...insidePlace];
  const holds = (Object.keys(view) as InboxQueryKey[]).some((key) => !loose.includes(key));
  if (!holds) return sameQuery(query, view);
  return sameQuery(without(query, loose), without(view, loose));
}
