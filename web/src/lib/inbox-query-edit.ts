import type { ConversationFilters } from '../api/schemas';
import { allStatuses, defaultStatus, flagKeys, listKeys, textKeys } from './inbox-query';
import type { InboxQuery, InboxQueryKey } from './inbox-query';

// Changing inbox filters and turning them into a saved view and back. Only the lazy Filter and Views
// menus need these, so they are kept apart from inbox-query.ts, which the first screen loads.

type ListKey = (typeof listKeys)[number];
const isList = (key: string): key is ListKey => (listKeys as readonly string[]).includes(key);
const split = (value: string) => value.split(',').filter(Boolean);

export function listOf(query: InboxQuery, key: ListKey): string[] {
  return split(query[key] ?? '');
}

// filtersOf is the ConversationFilters object a saved view stores. The Open view stores its status,
// so the view opens on Open; All stores none, which the API reads as every status.
export function filtersOf(query: InboxQuery): ConversationFilters {
  const filters: Record<string, unknown> = {};
  const status = query.status ?? defaultStatus;
  if (status !== allStatuses) filters.status = split(status);
  for (const key of listKeys) if (key !== 'status' && query[key]) filters[key] = split(query[key]);
  // A result's snapshot and a conversation's code name rows, not a view: neither is saved.
  for (const key of textKeys) if (query[key] && key !== 'result' && key !== 'ref') filters[key] = query[key];
  for (const key of flagKeys) if (query[key] !== undefined) filters[key] = query[key];
  return filters as ConversationFilters;
}

// queryOf is the address of a saved view's filters.
export function queryOf(filters: ConversationFilters): InboxQuery {
  const query: InboxQuery = {};
  for (const key of listKeys) {
    const values = filters[key];
    if (values && values.length > 0) query[key] = values.join(',');
  }
  if (!query.status) query.status = allStatuses;
  for (const key of textKeys) {
    const value = key === 'result' || key === 'ref' ? undefined : filters[key];
    if (value) query[key] = value;
  }
  for (const key of flagKeys) {
    const value = filters[key];
    if (value !== undefined) query[key] = value;
  }
  return query;
}

// withoutValue removes one value of a list filter, or the whole filter for the others.
export function withoutValue(query: InboxQuery, key: InboxQueryKey, value?: string): InboxQuery {
  const next: InboxQuery = { ...query };
  if (isList(key) && value !== undefined) {
    const rest = split(query[key] ?? '').filter((item) => item !== value);
    if (rest.length > 0) next[key] = rest.join(',');
    else delete next[key];
  } else {
    delete next[key];
  }
  if (!next.tag) delete next.tag_match;
  return next;
}
