import { expect, test } from 'vitest';
import type { SavedView } from '../../api/schemas';
import type { InboxQuery } from '../../lib/inbox-query';
import { channelItems, inboxIsCurrent, viewItems } from './sidebar-items';

const hrefOf = (query: InboxQuery) => `/?${new URLSearchParams(query as Record<string, string>).toString()}`;
const view = (name: string, filters: SavedView['filters']): SavedView => ({
  id: `11111111-1111-4111-8111-${String(name.length).padStart(12, '0')}`, name, area: 'inbox' as const, scope: 'shared' as const, filters,
  owner_agent_id: null, created_at: '2026-11-01T00:00:00Z', updated_at: '2026-11-01T00:00:00Z', deleted_at: null, purges_at: null,
});

test('a saved view opens the inbox on its own filters, with a mark that says what it holds', () => {
  const items = viewItems([
    view('Mine, open', { status: ['open'], assignee: ['me'] }),
    view('Waiting on customer', { status: ['pending'] }),
    view('Urgent', { priority: ['urgent'] }),
    view('Unassigned', { assignee: ['none'] }),
  ], hrefOf, undefined);
  expect(items.map(({ label, href, mark }) => ({ label, href, mark }))).toEqual([
    { label: 'Mine, open', href: '/?status=open&assignee=me', mark: 'open' },
    { label: 'Waiting on customer', href: '/?status=pending', mark: 'pending' },
    // A view that names no status shows every status, as the Views menu opens it.
    { label: 'Urgent', href: '/?priority=urgent&status=all', mark: 'urgent' },
    { label: 'Unassigned', href: '/?assignee=none&status=all', mark: 'none' },
  ]);
  expect(items.every((item) => !item.current)).toBe(true);
});

test('the view whose filters are on screen in the inbox is the current one', () => {
  const items = viewItems([view('Mine, open', { status: ['open'], assignee: ['me'] }), view('Pending', { status: ['pending'] })], hrefOf, { status: 'pending' });
  expect(items.map((item) => item.current)).toEqual([false, true]);
});

const channel = (id: string, name: string, type: string, state: string) => ({ id, name, type, status: { state } });

test('a channel opens the inbox filtered to it (its open conversations, as the inbox opens), and only a channel that needs attention carries the warning', () => {
  const items = channelItems([
    channel('aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', 'support@northwind.help', 'email', 'working'),
    channel('bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb', 'Chat widget', 'help_chat', 'needs_attention'),
  ], hrefOf, { channel_id: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb' });
  expect(items.map(({ label, href, type, warn, current }) => ({ label, href, type, warn, current }))).toEqual([
    { label: 'support@northwind.help', href: '/?channel_id=aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', type: 'email', warn: false, current: false },
    { label: 'Chat widget', href: '/?channel_id=bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb', type: 'help_chat', warn: true, current: true },
  ]);
});

test('a removed channel is not offered', () => {
  expect(channelItems([channel('cccccccc-cccc-4ccc-8ccc-cccccccccccc', 'Old form', 'web_form', 'removed')], hrefOf, undefined)).toEqual([]);
});

const zalo = channel('dddddddd-dddd-4ddd-8ddd-dddddddddddd', 'Zalo OA', 'zalo_oa', 'working');

test('a channel stays current while the agent switches status, sort or search inside it', () => {
  for (const current of [{ channel_id: zalo.id, status: 'all' }, { channel_id: zalo.id, status: 'pending', sort: 'priority' }, { channel_id: zalo.id, search: 'refund', status: 'all' }]) {
    expect(channelItems([zalo], hrefOf, current)[0].current).toBe(true);
  }
});

test('a channel stays current while Filter menu picks narrow it: the channel is still the place', () => {
  for (const current of [{ channel_id: zalo.id, priority: 'urgent' }, { channel_id: zalo.id, assignee: 'me', status: 'pending', tag: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa' }]) {
    expect(channelItems([zalo], hrefOf, current)[0].current).toBe(true);
  }
});

test('a channel is not current once a second channel joins it, or on a list of every channel', () => {
  for (const current of [{ channel_id: `${zalo.id},aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa` }, { status: 'all' }, { priority: 'urgent' }]) {
    expect(channelItems([zalo], hrefOf, current)[0].current).toBe(false);
  }
});

test('a view stays current while the agent changes its sort or search; a status it stores still counts', () => {
  const [mine] = viewItems([view('Mine, open', { status: ['open'], assignee: ['me'] })], hrefOf, { status: 'open', assignee: 'me', sort: 'priority', search: 'refund' });
  expect(mine.current).toBe(true);
  const [moved] = viewItems([view('Mine, open', { status: ['open'], assignee: ['me'] })], hrefOf, { status: 'pending', assignee: 'me' });
  expect(moved.current).toBe(false);
});

test('a view that stores no status stays current on any status the agent picks', () => {
  for (const current of [{ priority: 'urgent', status: 'all' }, { priority: 'urgent' }, { priority: 'urgent', status: 'closed', sort: 'updated_desc' }]) {
    expect(viewItems([view('Urgent', { priority: ['urgent'] })], hrefOf, current)[0].current).toBe(true);
  }
  expect(viewItems([view('Urgent', { priority: ['urgent'] })], hrefOf, { priority: 'urgent', assignee: 'me' })[0].current).toBe(false);
});

test('a view that stores only an order is current only on that order, not on every list', () => {
  const newest = view('Newest', { sort: 'updated_desc' });
  expect(viewItems([newest], hrefOf, {})[0].current).toBe(false);
  expect(viewItems([newest], hrefOf, { sort: 'updated_desc', status: 'all' })[0].current).toBe(true);
});

test('a view that stores a status with an order or a search is not the plain inbox', () => {
  expect(viewItems([view('Open by priority', { status: ['open'], sort: 'priority' })], hrefOf, {})[0].current).toBe(false);
  expect(viewItems([view('Open refunds', { status: ['open'], search: 'refund' })], hrefOf, {})[0].current).toBe(false);
  expect(viewItems([view('Open by priority', { status: ['open'], sort: 'priority' })], hrefOf, { sort: 'priority' })[0].current).toBe(true);
});

test('a search the view saved is part of it; a search typed inside a view that saved none is not', () => {
  expect(viewItems([view('Urgent refunds', { priority: ['urgent'], search: 'refund' })], hrefOf, { priority: 'urgent' })[0].current).toBe(false);
  expect(viewItems([view('Urgent refunds', { priority: ['urgent'], search: 'refund' })], hrefOf, { priority: 'urgent', search: 'refund', sort: 'priority' })[0].current).toBe(true);
  expect(viewItems([view('Urgent', { priority: ['urgent'] })], hrefOf, { priority: 'urgent', search: 'refund' })[0].current).toBe(true);
});

test('a view that saved only a status is never the plain inbox: the bare address belongs to Inbox', () => {
  const open = view('Open', { status: ['open'] });
  for (const current of [{}, { status: 'open' }, { sort: 'due' }]) expect(viewItems([open], hrefOf, current)[0].current).toBe(false);
  expect(inboxIsCurrent(viewItems([open], hrefOf, {}), channelItems([zalo], hrefOf, {}))).toBe(true);
  // Away from the default list it still marks its own status.
  expect(viewItems([view('Pending', { status: ['pending'] })], hrefOf, { status: 'pending' })[0].current).toBe(true);
  expect(viewItems([view('Open, newest', { status: ['open'], sort: 'updated_desc' })], hrefOf, { sort: 'updated_desc' })[0].current).toBe(true);
});

test('Inbox is the current place only while no channel or view in the sidebar is', () => {
  const channels = channelItems([zalo], hrefOf, { channel_id: zalo.id, status: 'all' });
  const views = viewItems([view('Urgent', { priority: ['urgent'] })], hrefOf, { channel_id: zalo.id, status: 'all' });
  expect(inboxIsCurrent(views, channels)).toBe(false);
  expect(inboxIsCurrent(viewItems([view('Urgent', { priority: ['urgent'] })], hrefOf, {}), channelItems([zalo], hrefOf, {}))).toBe(true);
  expect(inboxIsCurrent([], [])).toBe(true);
});
