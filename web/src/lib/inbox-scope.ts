import type { ConversationFilters } from '../api/schemas';
import { sameQuery } from './inbox-query';
import type { InboxQuery } from './inbox-query';
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

// onView: the list shows a saved view's filters. The agent may change the order inside a view. A status or a search
// the view saved is part of it; a view that saved none shows every status and no search, and the agent may pick a status
// or type a search inside it. A view that saved nothing but a status, an order or a search is a way of looking at the
// whole inbox, not a place of its own: it matches only exactly, or it would take over the plain inbox.
export function onView(query: InboxQuery, filters: ConversationFilters): boolean {
  const view = queryOf(filters);
  if (Object.keys(view).every((key) => insidePlace.includes(key))) return sameQuery(query, view);
  const loose = ['sort', ...(filters.status?.length ? [] : ['status']), ...(filters.search ? [] : ['search'])];
  return sameQuery(without(query, loose), without(view, loose));
}
