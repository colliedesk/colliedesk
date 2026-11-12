import { expect, test } from 'vitest';
import type { SavedView } from '../../api/schemas';
import type { InboxQuery } from '../../lib/inbox-query';
import { channelItems, viewItems } from './sidebar-items';

const hrefOf = (query: InboxQuery) => `/?${new URLSearchParams(query as Record<string, string>).toString()}`;
const view = (name: string, filters: SavedView['filters']): SavedView => ({
  id: `11111111-1111-4111-8111-${String(name.length).padStart(12, '0')}`, name, scope: 'shared' as const, filters,
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
