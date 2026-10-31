import { expect, test } from 'vitest';
import { apiQuery, inboxQueryFromSearch, isDefaultQuery } from './inbox-query';
import { filtersOf, queryOf, withoutValue } from './inbox-query-edit';

test('the address keeps only the filters the inbox knows, in the types it uses', () => {
  expect(inboxQueryFromSearch({ status: 'pending', priority: 'high,urgent', has_pending_draft: true, delivery_failed: 'false', page: 3, junk: 'x', sort: 'priority', search: 42 }))
    .toEqual({ status: 'pending', priority: 'high,urgent', has_pending_draft: true, delivery_failed: false, sort: 'priority', search: '42' });
  expect(inboxQueryFromSearch({ has_pending_draft: 'maybe', status: '', tag: { x: 1 } })).toEqual({});
});

test('a value the inbox does not know is dropped from the address, so an old or mistyped link still opens a list', () => {
  const tag = '33333333-3333-4333-8333-333333333333';
  expect(inboxQueryFromSearch({
    priority: 'bogus,urgent', status: 'open,nope', sort: 'nope', awaiting: 'zzz', source: 'native', channel: 'fax',
    created_after: 'junk', updated_before: '2026-10-01T00:00:00.000Z', tag: `${tag},x`, tag_match: 'some', assignee: 'me,bob', contact: '42',
  })).toEqual({ priority: 'urgent', status: 'open', source: 'native', updated_before: '2026-10-01T00:00:00.000Z', tag, assignee: 'me' });
  expect(inboxQueryFromSearch({ status: 'all' })).toEqual({ status: 'all' });
  expect(inboxQueryFromSearch({ status: 'all,open' })).toEqual({ status: 'open' });
});

test('no status in the address is the Open view; All is said out loud', () => {
  expect(apiQuery({}, 1)).toBe('status=open&page=1&page_size=50');
  expect(apiQuery({ status: 'all', search: 'refund' }, 2)).toBe('search=refund&page=2&page_size=50');
  expect(apiQuery({ priority: 'high,urgent', has_pending_draft: true, sort: 'waiting_longest', tag: 'a,b', tag_match: 'all' }, 1))
    .toBe('priority=high%2Curgent&tag=a%2Cb&tag_match=all&sort=waiting_longest&has_pending_draft=true&status=open&page=1&page_size=50');
});

test('a saved view stores the filters as lists and reads back as the same address', () => {
  const query = { status: 'open,pending', priority: 'urgent', assignee: 'me,unassigned', delivery_failed: true, created_after: '2026-10-01T00:00:00.000Z', sort: 'created_asc' };
  const filters = filtersOf(query);
  expect(filters).toEqual({ status: ['open', 'pending'], priority: ['urgent'], assignee: ['me', 'unassigned'], delivery_failed: true, created_after: '2026-10-01T00:00:00.000Z', sort: 'created_asc' });
  expect(queryOf(filters)).toEqual(query);
  // The Open view saves its status, so the view opens on Open; All saves none, which the API reads as every status.
  expect(filtersOf({})).toEqual({ status: ['open'] });
  expect(filtersOf({ status: 'all' })).toEqual({});
  expect(queryOf({})).toEqual({ status: 'all' });
});

test('a chip removes one value of a list and the whole filter when it was the last', () => {
  expect(withoutValue({ priority: 'high,urgent', tag: 'a' }, 'priority', 'high')).toEqual({ priority: 'urgent', tag: 'a' });
  expect(withoutValue({ priority: 'urgent', tag: 'a' }, 'priority', 'urgent')).toEqual({ tag: 'a' });
  expect(withoutValue({ tag: 'a,b', tag_match: 'all' }, 'tag', 'b')).toEqual({ tag: 'a', tag_match: 'all' });
  // Without tags, matching all of them means nothing.
  expect(withoutValue({ tag: 'a', tag_match: 'all' }, 'tag', 'a')).toEqual({});
  expect(withoutValue({ has_pending_draft: true }, 'has_pending_draft')).toEqual({});
});

test('the default view is Open, newest activity first, nothing else', () => {
  expect(isDefaultQuery({})).toBe(true);
  expect(isDefaultQuery({ status: 'open' })).toBe(true);
  expect(isDefaultQuery({ sort: 'updated_desc' })).toBe(true);
  expect(isDefaultQuery({ status: 'all' })).toBe(false);
  expect(isDefaultQuery({ awaiting: 'team' })).toBe(false);
});

test('a results number opens its conversations in any status, and is never saved in a view', () => {
  const result = '11111111-1111-4111-8111-111111111111:30d.first_response';
  const query = inboxQueryFromSearch({ status: 'all', result });
  expect(query).toEqual({ status: 'all', result });
  expect(apiQuery(query, 1)).toBe(`result=${encodeURIComponent(result)}&page=1&page_size=50`);
  expect(inboxQueryFromSearch({ result: 'not-a-number' })).toEqual({});
  expect(filtersOf(query)).toEqual({});
  expect(queryOf({ result } as never)).toEqual({ status: 'all' });
});

test('a channel filter keeps channel ids only, goes to the API, and is saved in a view as a list', () => {
  const id = '55555555-5555-4555-8555-555555555555';
  expect(inboxQueryFromSearch({ channel_id: `${id},nope` })).toEqual({ channel_id: id });
  expect(apiQuery({ channel_id: id }, 1)).toBe(`channel_id=${id}&status=open&page=1&page_size=50`);
  expect(filtersOf({ status: 'all', channel_id: id })).toEqual({ channel_id: [id] });
  expect(queryOf({ channel_id: [id] })).toEqual({ status: 'all', channel_id: id });
});

test('a code in the address finds its conversation in every status', () => {
  expect(inboxQueryFromSearch({ ref: ' CHD-2026-0142 ' })).toEqual({ ref: 'CHD-2026-0142', status: 'all' });
  expect(apiQuery(inboxQueryFromSearch({ ref: 'CHD-2026-0142' }), 1)).toBe('ref=CHD-2026-0142&page=1&page_size=50');
  expect(inboxQueryFromSearch({ ref: 'CHD-2026-0142', status: 'pending' })).toEqual({ ref: 'CHD-2026-0142', status: 'pending' });
});

test('a view saves the Targets filter and the Due first sort, and opens on them again', async () => {
  const { conversationFiltersSchema } = await import('../api/saved-views');
  const filters = filtersOf(inboxQueryFromSearch({ sla: 'overdue,due_soon', sort: 'due' }));
  expect(filters).toMatchObject({ sla: ['overdue', 'due_soon'], sort: 'due' });
  // As the server sends it back: the filter is kept, not dropped by the schema.
  const read = conversationFiltersSchema.parse(JSON.parse(JSON.stringify(filters)));
  expect(queryOf(read)).toMatchObject({ sla: 'overdue,due_soon', sort: 'due' });
});
