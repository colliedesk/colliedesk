import { channelSchema, conversationSortSchema, prioritySchema, statusSchema } from '../api/schemas';

// The inbox filters live in the address, so a link opens the same list. They use the names and the
// comma-separated lists of GET /api/v1/conversations, with one difference: no status is the Open
// view (what still needs an answer), and status=all is every status.

export const listKeys = ['status', 'priority', 'assignee', 'tag', 'channel'] as const;
export const textKeys = ['tag_match', 'contact', 'organization', 'awaiting', 'source', 'created_after', 'created_before', 'updated_after', 'updated_before', 'sort', 'search'] as const;
export const flagKeys = ['has_pending_draft', 'delivery_failed'] as const;

type ListKey = (typeof listKeys)[number];
type TextKey = (typeof textKeys)[number];
type FlagKey = (typeof flagKeys)[number];
export type InboxQueryKey = ListKey | TextKey | FlagKey;
export type InboxQuery = Partial<Record<ListKey | TextKey, string> & Record<FlagKey, boolean>>;

// The order of the API query, so one set of filters is always one cache key; status goes last.
const apiOrder: InboxQueryKey[] = [...listKeys.slice(1), ...textKeys, ...flagKeys];

export const defaultStatus = 'open';
export const allStatuses = 'all';

const isList = (key: string): key is ListKey => (listKeys as readonly string[]).includes(key);
const isText = (key: string): key is TextKey => (textKeys as readonly string[]).includes(key);
const isFlag = (key: string): key is FlagKey => (flagKeys as readonly string[]).includes(key);

// The values the API knows, so an old or mistyped link drops what it cannot use instead of breaking the
// list with a 400. The server still checks everything.
const words: Partial<Record<InboxQueryKey, readonly string[]>> = {
  status: statusSchema.options, priority: prioritySchema.options, channel: channelSchema.options, sort: conversationSortSchema.options,
  awaiting: ['team', 'customer'], source: ['imported', 'native'], tag_match: ['any', 'all'],
};
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const instant = /^\d{4}-\d\d-\d\dT[\d:.]+(Z|[+-]\d\d:\d\d)$/;

function known(key: InboxQueryKey, item: string): boolean {
  if (key === 'tag' || key === 'contact') return uuid.test(item);
  if (key === 'assignee') return uuid.test(item) || ['me', 'unassigned', 'anyone'].includes(item);
  if (/_(after|before)$/.test(key)) return instant.test(item) && !Number.isNaN(Date.parse(item));
  const allowed = words[key];
  return !allowed || allowed.includes(item);
}

// inboxQueryFromSearch reads the router's search object. The router turns true into a boolean and a
// number into a number; anything that is not a known filter or value is dropped.
export function inboxQueryFromSearch(search: Record<string, unknown>): InboxQuery {
  const query: InboxQuery = {};
  for (const [key, value] of Object.entries(search)) {
    if (isFlag(key)) {
      if (value === true || value === 'true') query[key] = true;
      else if (value === false || value === 'false') query[key] = false;
    } else if ((isList(key) || isText(key)) && (typeof value === 'string' || typeof value === 'number')) {
      const text = String(value);
      const kept = key === 'status' && text === allStatuses ? text
        : isList(key) ? text.split(',').filter((item) => known(key, item)).join(',') : known(key, text) ? text : '';
      if (kept) query[key] = kept;
    }
  }
  return query;
}

// apiQuery is the query string of GET /api/v1/conversations for a page of the list.
export function apiQuery(query: InboxQuery, page: number, pageSize = 50): string {
  const values = new URLSearchParams();
  for (const key of apiOrder) {
    const value = query[key];
    if (value !== undefined && value !== '') values.set(key, String(value));
  }
  const status = query.status ?? defaultStatus;
  if (status !== allStatuses) values.set('status', status);
  values.set('page', String(page));
  values.set('page_size', String(pageSize));
  return values.toString();
}

// sameQuery compares two queries as the API sees them.
export function sameQuery(left: InboxQuery, right: InboxQuery): boolean {
  return apiQuery(left, 1) === apiQuery(right, 1);
}

export function isDefaultQuery(query: InboxQuery): boolean {
  return sameQuery(query, {}) || sameQuery(query, { sort: 'updated_desc' });
}
