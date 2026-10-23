import { QueryClient, QueryClientProvider, QueryObserver } from '@tanstack/react-query';
import { act, renderHook } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { useInboxActivity, useInboxViewing } from './use-inbox-activity';

const me = '66666666-6666-4666-8666-666666666666';

const api = vi.hoisted(() => ({ inboxActivity: vi.fn() }));
vi.mock('../../api/client', () => ({ api, UnauthorizedError: class extends Error {}, WorkspaceLockedError: class extends Error {} }));

const notifications: Array<{ title: string; options?: NotificationOptions }> = [];
class FakeNotification {
  static permission: NotificationPermission = 'granted';
  constructor(title: string, options?: NotificationOptions) { notifications.push({ title, options }); }
}

const item = (id: string, subject: string) => ({ id, subject, contact_name: 'Ada' });
const created = (id: string, subject: string) => ({ ...item(id, subject), created_at: '2026-10-15T10:00:10Z' });
const assigned = (id: string, subject: string, by_ai = false) => ({ ...item(id, subject), assigned_at: '2026-10-15T10:00:10Z', by_ai });
const idle = (now: string) => ({ now, new_conversations: [], assigned_to_me: [], updated: [] });
const changed = (n: number, extra: { assignee_id?: string | null; incoming_at?: string | null; updated_at?: string } = {}) =>
  ({ id: id(n), subject: `Subject ${n}`, contact_name: 'Ada', assignee_id: null, updated_at: '2026-10-15T10:00:10Z', incoming_at: null, ...extra });
const id = (n: number) => `00000000-0000-4000-8000-00000000000${n}`;

let client: QueryClient;
let focused = false;
const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>;
// React Query hands results to observers on a zero-delay timer, so every tick also flushes that one.
const tick = (ms: number) => act(async () => { await vi.advanceTimersByTimeAsync(ms); await vi.advanceTimersByTimeAsync(1); });

beforeEach(() => {
  vi.useFakeTimers();
  notifications.length = 0;
  focused = false;
  document.title = 'Colliedesk';
  localStorage.setItem('colliedesk.desktop-notifications', 'on');
  FakeNotification.permission = 'granted';
  vi.stubGlobal('Notification', FakeNotification);
  vi.spyOn(document, 'hasFocus').mockImplementation(() => focused);
  api.inboxActivity.mockReset();
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
});
afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals(); vi.restoreAllMocks(); localStorage.clear(); });

test('polls every 20 seconds with the cursor the server returned', async () => {
  api.inboxActivity.mockResolvedValueOnce(idle('2026-10-15T10:00:00Z')).mockResolvedValue(idle('2026-10-15T10:00:20Z'));
  renderHook(() => useInboxActivity(true, me), { wrapper });
  await tick(0);
  expect(api.inboxActivity).toHaveBeenCalledTimes(1);
  expect(api.inboxActivity).toHaveBeenLastCalledWith(undefined);
  await tick(19_000);
  expect(api.inboxActivity).toHaveBeenCalledTimes(1);
  await tick(1_000);
  expect(api.inboxActivity).toHaveBeenCalledTimes(2);
  expect(api.inboxActivity).toHaveBeenLastCalledWith('2026-10-15T10:00:00Z');
  await tick(20_000);
  expect(api.inboxActivity).toHaveBeenLastCalledWith('2026-10-15T10:00:20Z');
});

test('activity refreshes the conversation list, an empty poll does not', async () => {
  const invalidate = vi.spyOn(client, 'invalidateQueries');
  api.inboxActivity.mockResolvedValueOnce(idle('2026-10-15T10:00:00Z'))
    .mockResolvedValueOnce(idle('2026-10-15T10:00:20Z'))
    .mockResolvedValueOnce({ now: '2026-10-15T10:00:40Z', new_conversations: [created(id(1), 'Printer')], assigned_to_me: [], updated: [] });
  renderHook(() => useInboxActivity(true, me), { wrapper });
  await tick(0);
  await tick(20_000);
  expect(invalidate).not.toHaveBeenCalled();
  await tick(20_000);
  expect(invalidate).toHaveBeenCalledWith({ queryKey: ['conversations'] });
});

test('raises one notification per item and counts them in the tab title until focus returns', async () => {
  api.inboxActivity.mockResolvedValueOnce(idle('2026-10-15T10:00:00Z')).mockResolvedValueOnce({
    now: '2026-10-15T10:00:20Z', new_conversations: [created(id(1), 'Printer')], assigned_to_me: [assigned(id(2), 'Refund'), assigned(id(3), 'Printer jam', true)], updated: [],
  }).mockResolvedValue(idle('2026-10-15T10:00:40Z'));
  renderHook(() => useInboxActivity(true, me), { wrapper });
  await tick(0);
  await tick(20_000);
  expect(notifications.map((entry) => entry.title)).toEqual(['New conversation', 'Assigned to you', 'Assigned to you by AI']);
  expect(notifications[0]?.options?.body).toBe('Printer · Ada');
  expect(document.title).toBe('(3) Colliedesk');
  act(() => { window.dispatchEvent(new Event('focus')); });
  expect(document.title).toBe('Colliedesk');
});

test('a burst of more than three items becomes one summary notification', async () => {
  const many = [1, 2, 3, 4].map((n) => created(id(n), `Subject ${n}`));
  api.inboxActivity.mockResolvedValueOnce(idle('2026-10-15T10:00:00Z')).mockResolvedValueOnce({ now: '2026-10-15T10:00:20Z', new_conversations: many, assigned_to_me: [], updated: [] });
  renderHook(() => useInboxActivity(true, me), { wrapper });
  await tick(0);
  await tick(20_000);
  expect(notifications).toHaveLength(1);
  expect(notifications[0]?.title).toBe('4 new updates in your inbox');
  expect(document.title).toBe('(4) Colliedesk');
});

test('a focused tab refreshes the list without a notification or title count', async () => {
  focused = true;
  api.inboxActivity.mockResolvedValueOnce(idle('2026-10-15T10:00:00Z')).mockResolvedValueOnce({
    now: '2026-10-15T10:00:20Z', new_conversations: [created(id(1), 'Printer')], assigned_to_me: [], updated: [],
  });
  const invalidate = vi.spyOn(client, 'invalidateQueries');
  renderHook(() => useInboxActivity(true, me), { wrapper });
  await tick(0);
  await tick(20_000);
  expect(invalidate).toHaveBeenCalledWith({ queryKey: ['conversations'] });
  expect(notifications).toHaveLength(0);
  expect(document.title).toBe('Colliedesk');
});

test('keeps the title count but raises no notification when notifications are off', async () => {
  localStorage.removeItem('colliedesk.desktop-notifications');
  api.inboxActivity.mockResolvedValueOnce(idle('2026-10-15T10:00:00Z')).mockResolvedValueOnce({
    now: '2026-10-15T10:00:20Z', new_conversations: [created(id(1), 'Printer')], assigned_to_me: [], updated: [],
  });
  renderHook(() => useInboxActivity(true, me), { wrapper });
  await tick(0);
  await tick(20_000);
  expect(notifications).toHaveLength(0);
  expect(document.title).toBe('(1) Colliedesk');
});

test('a failed poll keeps the cursor so nothing is skipped', async () => {
  api.inboxActivity.mockResolvedValueOnce(idle('2026-10-15T10:00:00Z')).mockRejectedValueOnce(new Error('offline')).mockResolvedValue(idle('2026-10-15T10:01:00Z'));
  renderHook(() => useInboxActivity(true, me), { wrapper });
  await tick(0);
  await tick(20_000);
  await tick(20_000);
  expect(api.inboxActivity).toHaveBeenLastCalledWith('2026-10-15T10:00:00Z');
});

test('a conversation returned by two polls is announced and counted once', async () => {
  const again = { new_conversations: [created(id(1), 'Printer')], assigned_to_me: [], updated: [] };
  api.inboxActivity.mockResolvedValueOnce(idle('2026-10-15T10:00:00Z'))
    .mockResolvedValueOnce({ now: '2026-10-15T10:00:20Z', ...again })
    .mockResolvedValueOnce({ now: '2026-10-15T10:00:40Z', ...again });
  const invalidate = vi.spyOn(client, 'invalidateQueries');
  renderHook(() => useInboxActivity(true, me), { wrapper });
  await tick(0);
  await tick(20_000);
  await tick(20_000);
  expect(notifications).toHaveLength(1);
  expect(document.title).toBe('(1) Colliedesk');
  expect(invalidate).toHaveBeenCalledTimes(1);
});

test('the same conversation is announced again when it is later assigned to me', async () => {
  api.inboxActivity.mockResolvedValueOnce(idle('2026-10-15T10:00:00Z'))
    .mockResolvedValueOnce({ now: '2026-10-15T10:00:20Z', new_conversations: [created(id(1), 'Printer')], assigned_to_me: [], updated: [] })
    .mockResolvedValueOnce({ now: '2026-10-15T10:00:40Z', new_conversations: [], assigned_to_me: [assigned(id(1), 'Printer')], updated: [] });
  renderHook(() => useInboxActivity(true, me), { wrapper });
  await tick(0);
  await tick(20_000);
  await tick(20_000);
  expect(notifications.map((entry) => entry.title)).toEqual(['New conversation', 'Assigned to you']);
});

test('conversations that already existed when the inbox opened are not announced', async () => {
  api.inboxActivity.mockResolvedValueOnce(idle('2026-10-15T10:00:00Z')).mockResolvedValueOnce({
    now: '2026-10-15T10:00:20Z', new_conversations: [{ ...created(id(1), 'Seen already'), created_at: '2026-10-15T09:59:00Z' }], assigned_to_me: [], updated: [],
  });
  const invalidate = vi.spyOn(client, 'invalidateQueries');
  renderHook(() => useInboxActivity(true, me), { wrapper });
  await tick(0);
  await tick(20_000);
  expect(notifications).toHaveLength(0);
  expect(invalidate).not.toHaveBeenCalled();
  expect(document.title).toBe('Colliedesk');
});

test('does not poll while disabled', async () => {
  renderHook(() => useInboxActivity(false), { wrapper });
  await tick(60_000);
  expect(api.inboxActivity).not.toHaveBeenCalled();
});

const poll = (now: string, updated: ReturnType<typeof changed>[]) => ({ now, new_conversations: [], assigned_to_me: [], updated });

test('a new customer message refetches the open conversation timeline and the list within one poll', async () => {
  api.inboxActivity.mockResolvedValueOnce(idle('2026-10-15T10:00:00Z'))
    .mockResolvedValueOnce(poll('2026-10-15T10:00:20Z', [changed(1, { incoming_at: '2026-10-15T10:00:10Z' })]));
  const invalidate = vi.spyOn(client, 'invalidateQueries');
  renderHook(() => useInboxActivity(true, me), { wrapper });
  await tick(0);
  await tick(20_000);
  expect(invalidate).toHaveBeenCalledWith({ queryKey: ['conversations'] });
  expect(invalidate).toHaveBeenCalledWith({ queryKey: ['messages', id(1)] });
});

test('another agent changing a conversation refreshes the list and that timeline without any alert', async () => {
  focused = false;
  api.inboxActivity.mockResolvedValueOnce(idle('2026-10-15T10:00:00Z'))
    .mockResolvedValueOnce(poll('2026-10-15T10:00:20Z', [changed(1, { assignee_id: me })]));
  const invalidate = vi.spyOn(client, 'invalidateQueries');
  renderHook(() => useInboxActivity(true, me), { wrapper });
  await tick(0);
  await tick(20_000);
  expect(invalidate).toHaveBeenCalledWith({ queryKey: ['conversations'] });
  expect(invalidate).toHaveBeenCalledWith({ queryKey: ['messages', id(1)] });
  expect(notifications).toHaveLength(0);
  expect(document.title).toBe('Colliedesk');
});

test('a customer message on my conversation or an unassigned one raises one notification, on a teammate\'s it does not', async () => {
  const teammate = '99999999-9999-4999-8999-999999999999';
  api.inboxActivity.mockResolvedValueOnce(idle('2026-10-15T10:00:00Z')).mockResolvedValueOnce(poll('2026-10-15T10:00:20Z', [
    changed(1, { assignee_id: me, incoming_at: '2026-10-15T10:00:10Z' }),
    changed(2, { assignee_id: null, incoming_at: '2026-10-15T10:00:11Z' }),
    changed(3, { assignee_id: teammate, incoming_at: '2026-10-15T10:00:12Z' }),
  ]));
  const invalidate = vi.spyOn(client, 'invalidateQueries');
  renderHook(() => useInboxActivity(true, me), { wrapper });
  await tick(0);
  await tick(20_000);
  expect(notifications.map((entry) => [entry.title, entry.options?.body])).toEqual([
    ['New customer message', 'Subject 1 · Ada'], ['New customer message', 'Subject 2 · Ada'],
  ]);
  expect(document.title).toBe('(2) Colliedesk');
  expect(invalidate).toHaveBeenCalledWith({ queryKey: ['messages', id(3)] });
});

test('a customer message on a teammate\'s conversation that I have open notifies me', async () => {
  const teammate = '99999999-9999-4999-8999-999999999999';
  api.inboxActivity.mockResolvedValueOnce(idle('2026-10-15T10:00:00Z')).mockResolvedValueOnce(poll('2026-10-15T10:00:20Z', [
    changed(3, { assignee_id: teammate, incoming_at: '2026-10-15T10:00:12Z' }),
    changed(4, { assignee_id: teammate, incoming_at: '2026-10-15T10:00:13Z' }),
  ]));
  const open = new QueryObserver(client, { queryKey: ['messages', id(3)], queryFn: () => ({ items: [] }) }).subscribe(() => {});
  renderHook(() => useInboxActivity(true, me), { wrapper });
  await tick(0);
  await tick(20_000);
  expect(notifications.map((entry) => [entry.title, entry.options?.body])).toEqual([['New customer message', 'Subject 3 · Ada']]);
  expect(document.title).toBe('(1) Colliedesk');
  open();
});

test('my own change or reply (no customer message) never notifies or counts', async () => {
  api.inboxActivity.mockResolvedValueOnce(idle('2026-10-15T10:00:00Z'))
    .mockResolvedValueOnce(poll('2026-10-15T10:00:20Z', [changed(1, { assignee_id: me, incoming_at: null })]));
  renderHook(() => useInboxActivity(true, me), { wrapper });
  await tick(0);
  await tick(20_000);
  expect(notifications).toHaveLength(0);
  expect(document.title).toBe('Colliedesk');
});

test('a message repeated by the lookback is announced once, a second message on the same conversation again', async () => {
  const first = changed(1, { incoming_at: '2026-10-15T10:00:10Z', updated_at: '2026-10-15T10:00:10Z' });
  const second = changed(1, { incoming_at: '2026-10-15T10:00:30Z', updated_at: '2026-10-15T10:00:30Z' });
  const laterEdit = changed(1, { incoming_at: '2026-10-15T10:00:30Z', updated_at: '2026-10-15T10:00:45Z' });
  api.inboxActivity.mockResolvedValueOnce(idle('2026-10-15T10:00:00Z'))
    .mockResolvedValueOnce(poll('2026-10-15T10:00:20Z', [first]))
    .mockResolvedValueOnce(poll('2026-10-15T10:00:40Z', [second, first]))
    .mockResolvedValueOnce(poll('2026-10-15T10:01:00Z', [laterEdit]));
  renderHook(() => useInboxActivity(true, me), { wrapper });
  await tick(0);
  await tick(20_000);
  await tick(20_000);
  await tick(20_000);
  expect(notifications).toHaveLength(2);
  expect(document.title).toBe('(2) Colliedesk');
});

test('a reply on a conversation announced as new in the same poll is not announced twice', async () => {
  api.inboxActivity.mockResolvedValueOnce(idle('2026-10-15T10:00:00Z')).mockResolvedValueOnce({
    now: '2026-10-15T10:00:20Z', new_conversations: [created(id(1), 'Printer')], assigned_to_me: [],
    updated: [changed(1, { incoming_at: '2026-10-15T10:00:10Z' })],
  }).mockResolvedValueOnce(poll('2026-10-15T10:00:40Z', [changed(1, { incoming_at: '2026-10-15T10:00:10Z' })]));
  renderHook(() => useInboxActivity(true, me), { wrapper });
  await tick(0);
  await tick(20_000);
  await tick(20_000);
  expect(notifications.map((entry) => entry.title)).toEqual(['New conversation']);
});

test('changes stamped before the inbox opened are ignored', async () => {
  api.inboxActivity.mockResolvedValueOnce(idle('2026-10-15T10:00:00Z')).mockResolvedValueOnce(
    poll('2026-10-15T10:00:20Z', [changed(1, { updated_at: '2026-10-15T09:59:30Z', incoming_at: '2026-10-15T09:59:30Z' })]));
  const invalidate = vi.spyOn(client, 'invalidateQueries');
  renderHook(() => useInboxActivity(true, me), { wrapper });
  await tick(0);
  await tick(20_000);
  expect(invalidate).not.toHaveBeenCalled();
  expect(notifications).toHaveLength(0);
});

test('a mounted timeline query fetches again when its conversation received a customer message, others do not', async () => {
  const timeline = vi.fn(async () => ({ items: [] }));
  const other = vi.fn(async () => ({ items: [] }));
  const watching = new QueryObserver(client, { queryKey: ['messages', id(1)], queryFn: timeline }).subscribe(() => {});
  const unrelated = new QueryObserver(client, { queryKey: ['messages', id(2)], queryFn: other }).subscribe(() => {});
  api.inboxActivity.mockResolvedValueOnce(idle('2026-10-15T10:00:00Z'))
    .mockResolvedValueOnce(poll('2026-10-15T10:00:20Z', [changed(1, { incoming_at: '2026-10-15T10:00:10Z' })]));
  renderHook(() => useInboxActivity(true, me), { wrapper });
  await tick(0);
  expect(timeline).toHaveBeenCalledTimes(1);
  await tick(20_000);
  expect(timeline).toHaveBeenCalledTimes(2);
  expect(other).toHaveBeenCalledTimes(1);
  watching(); unrelated();
});

test('the inbox reads who has which conversation open from the same poll, from its first answer, and never polls itself', async () => {
  const lan = { agent_id: me, name: 'Lan', writing: true, writing_since: '2026-10-15T10:00:00Z', edited_at: '2026-10-15T10:00:00Z', agent_token: null };
  api.inboxActivity.mockResolvedValueOnce({ ...idle('2026-10-15T10:00:00Z'), viewing: [{ conversation_id: id(1), viewers: [lan] }] })
    .mockResolvedValue({ ...idle('2026-10-15T10:00:20Z'), viewing: [] });
  const { result, unmount } = renderHook(() => { useInboxActivity(true, me); return useInboxViewing(); }, { wrapper });
  await tick(0);
  expect(result.current.get(id(1))).toEqual([lan]);
  await tick(20_000);
  expect(api.inboxActivity).toHaveBeenCalledTimes(2);
  expect(result.current.size).toBe(0);
  // Without the app's poll the inbox shows nothing and asks for nothing.
  unmount();
  api.inboxActivity.mockClear();
  client = new QueryClient();
  const alone = renderHook(() => useInboxViewing(), { wrapper });
  await tick(30_000);
  expect(alone.result.current.size).toBe(0);
  expect(api.inboxActivity).not.toHaveBeenCalled();
});
