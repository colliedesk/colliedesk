import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { useInboxActivity } from './use-inbox-activity';

const api = vi.hoisted(() => ({ inboxActivity: vi.fn() }));
vi.mock('../../api/client', () => ({ api, UnauthorizedError: class extends Error {}, WorkspaceLockedError: class extends Error {} }));

const notifications: Array<{ title: string; options?: NotificationOptions }> = [];
class FakeNotification {
  static permission: NotificationPermission = 'granted';
  constructor(title: string, options?: NotificationOptions) { notifications.push({ title, options }); }
}

const item = (id: string, subject: string) => ({ id, subject, contact_name: 'Ada', contact_email: 'ada@example.com' });
const created = (id: string, subject: string) => ({ ...item(id, subject), created_at: '2026-10-15T10:00:00Z' });
const assigned = (id: string, subject: string) => ({ ...item(id, subject), assigned_at: '2026-10-15T10:00:00Z' });
const idle = (now: string) => ({ now, new_conversations: [], assigned_to_me: [] });
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
  renderHook(() => useInboxActivity(), { wrapper });
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
    .mockResolvedValueOnce({ now: '2026-10-15T10:00:40Z', new_conversations: [created(id(1), 'Printer')], assigned_to_me: [] });
  renderHook(() => useInboxActivity(), { wrapper });
  await tick(0);
  await tick(20_000);
  expect(invalidate).not.toHaveBeenCalled();
  await tick(20_000);
  expect(invalidate).toHaveBeenCalledWith({ queryKey: ['conversations'] });
});

test('raises one notification per item and counts them in the tab title until focus returns', async () => {
  api.inboxActivity.mockResolvedValueOnce(idle('2026-10-15T10:00:00Z')).mockResolvedValueOnce({
    now: '2026-10-15T10:00:20Z', new_conversations: [created(id(1), 'Printer')], assigned_to_me: [assigned(id(2), 'Refund')],
  }).mockResolvedValue(idle('2026-10-15T10:00:40Z'));
  renderHook(() => useInboxActivity(), { wrapper });
  await tick(0);
  await tick(20_000);
  expect(notifications.map((entry) => entry.title)).toEqual(['New conversation', 'Assigned to you']);
  expect(notifications[0]?.options?.body).toBe('Printer · Ada');
  expect(document.title).toBe('(2) Colliedesk');
  act(() => { window.dispatchEvent(new Event('focus')); });
  expect(document.title).toBe('Colliedesk');
});

test('a burst of more than three items becomes one summary notification', async () => {
  const many = [1, 2, 3, 4].map((n) => created(id(n), `Subject ${n}`));
  api.inboxActivity.mockResolvedValueOnce(idle('2026-10-15T10:00:00Z')).mockResolvedValueOnce({ now: '2026-10-15T10:00:20Z', new_conversations: many, assigned_to_me: [] });
  renderHook(() => useInboxActivity(), { wrapper });
  await tick(0);
  await tick(20_000);
  expect(notifications).toHaveLength(1);
  expect(notifications[0]?.title).toBe('4 new updates in your inbox');
  expect(document.title).toBe('(4) Colliedesk');
});

test('a focused tab refreshes the list without a notification or title count', async () => {
  focused = true;
  api.inboxActivity.mockResolvedValueOnce(idle('2026-10-15T10:00:00Z')).mockResolvedValueOnce({
    now: '2026-10-15T10:00:20Z', new_conversations: [created(id(1), 'Printer')], assigned_to_me: [],
  });
  const invalidate = vi.spyOn(client, 'invalidateQueries');
  renderHook(() => useInboxActivity(), { wrapper });
  await tick(0);
  await tick(20_000);
  expect(invalidate).toHaveBeenCalledWith({ queryKey: ['conversations'] });
  expect(notifications).toHaveLength(0);
  expect(document.title).toBe('Colliedesk');
});

test('keeps the title count but raises no notification when notifications are off', async () => {
  localStorage.removeItem('colliedesk.desktop-notifications');
  api.inboxActivity.mockResolvedValueOnce(idle('2026-10-15T10:00:00Z')).mockResolvedValueOnce({
    now: '2026-10-15T10:00:20Z', new_conversations: [created(id(1), 'Printer')], assigned_to_me: [],
  });
  renderHook(() => useInboxActivity(), { wrapper });
  await tick(0);
  await tick(20_000);
  expect(notifications).toHaveLength(0);
  expect(document.title).toBe('(1) Colliedesk');
});

test('a failed poll keeps the cursor so nothing is skipped', async () => {
  api.inboxActivity.mockResolvedValueOnce(idle('2026-10-15T10:00:00Z')).mockRejectedValueOnce(new Error('offline')).mockResolvedValue(idle('2026-10-15T10:01:00Z'));
  renderHook(() => useInboxActivity(), { wrapper });
  await tick(0);
  await tick(20_000);
  await tick(20_000);
  expect(api.inboxActivity).toHaveBeenLastCalledWith('2026-10-15T10:00:00Z');
});
