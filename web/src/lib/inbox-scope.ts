import type { ConversationFilters } from '../api/schemas';
import { isDefaultQuery, sameQuery } from './inbox-query';
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

// onChannel: the list is that channel's conversations. Status, order, search and the Filter menu's picks narrow the list
// inside the channel; it stays the place until a second channel joins it or it is removed.
export function onChannel(query: InboxQuery, channelId: string): boolean {
  return scopedChannel(query) === channelId;
}

// placeOnly is the list cleared of every filter but the place it is in: the one channel it is narrowed to, else the inbox.
export function placeOnly(query: InboxQuery): InboxQuery {
  const channel = scopedChannel(query);
  return channel ? { channel_id: channel } : {};
}

// narrowedInPlace: a filter other than the place, the status and the order is on, so clearing the filters shows more.
export function narrowedInPlace(query: InboxQuery): boolean {
  const place = scopedChannel(query);
  return Object.keys(query).some((key) => key !== 'status' && key !== 'sort' && !(key === 'channel_id' && place));
}

// Filters counted on the Filter button; status, sort and search have their own controls, and a date range counts once.
// Source, organization and the date ranges are set only by a link and show as chips.
const menuKeys: InboxQueryKey[] = ['priority', 'assignee', 'tag', 'sla', 'contact', 'organization', 'awaiting', 'has_pending_draft', 'delivery_failed',
  'source', 'channel', 'channel_id', 'created_after', 'updated_after'];
const has = (query: InboxQuery, key: InboxQueryKey) => query[key] !== undefined && query[key] !== '';

// activeFilterCount counts the filters that are on, the channel the title names left out. The first screen shows it on
// the inert Filter control too, so nothing moves when the menu arrives.
export function activeFilterCount(query: InboxQuery, scopeChannel?: string): number {
  return menuKeys.filter((key) => has(query, key) && !(key === 'channel_id' && scopeChannel)).length
    + (!has(query, 'created_after') && has(query, 'created_before') ? 1 : 0) + (!has(query, 'updated_after') && has(query, 'updated_before') ? 1 : 0);
}

const without = (query: InboxQuery, keys: readonly string[]): InboxQuery =>
  Object.fromEntries(Object.entries(query).filter(([key]) => !keys.includes(key))) as InboxQuery;

// onView: the list shows a saved view's filters. The agent may change the order inside a view. A status or a search
// the view saved is part of it; a view that saved none shows every status and no search, and the agent may pick a status
// or type a search inside it. A view that saved nothing but a status, an order or a search is a way of looking at the
// whole inbox, not a place of its own: it matches only exactly, and never the default list, which belongs to Inbox.
export function onView(query: InboxQuery, filters: ConversationFilters): boolean {
  const view = queryOf(filters);
  if (Object.keys(view).every((key) => insidePlace.includes(key))) return !isDefaultQuery(query) && sameQuery(query, view);
  const loose = ['sort', ...(filters.status?.length ? [] : ['status']), ...(filters.search ? [] : ['search'])];
  return sameQuery(without(query, loose), without(view, loose));
}
