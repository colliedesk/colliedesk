import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeAll, beforeEach, expect, test, vi } from 'vitest';
import { presenceViewPart } from './presence-part';
import { presenceClientId } from './presence-runtime';
import { useConversationPresence } from './use-conversation-presence';

const api = vi.hoisted(() => ({ announcePresence: vi.fn(), leavePresence: vi.fn() }));
const { TooManyRequestsError } = vi.hoisted(() => ({ TooManyRequestsError: class extends Error { constructor(readonly retryAfterSeconds: number) { super('429'); } } }));
vi.mock('../../api/client', () => ({ api, TooManyRequestsError }));

const one = '11111111-1111-4111-8111-111111111111';
const two = '22222222-2222-4222-8222-222222222222';
const lan = { agent_id: '44444444-4444-4444-8444-444444444444', name: 'Lan', writing: true, writing_since: '2026-10-22T09:00:00Z', edited_at: '2026-10-22T09:00:00Z', agent_token: null };

// The presence code comes in its own chunk; these tests start with it there.
beforeAll(async () => {
  presenceViewPart.preload();
  await import('./presence-view');
  await new Promise((resolve) => setTimeout(resolve, 0));
});

let visibility: DocumentVisibilityState = 'visible';
const setVisibility = (state: DocumentVisibilityState) => act(() => {
  visibility = state;
  document.dispatchEvent(new Event('visibilitychange'));
});
// Promise answers are handed over on the microtask queue, so every tick flushes it too.
const tick = (ms: number) => act(async () => { await vi.advanceTimersByTimeAsync(ms); });
const announces = () => api.announcePresence.mock.calls.map(([id, body]) => ({ id, ...body }));

beforeEach(() => {
  vi.useFakeTimers();
  visibility = 'visible';
  vi.spyOn(document, 'visibilityState', 'get').mockImplementation(() => visibility);
  api.announcePresence.mockReset().mockImplementation(async (_id: string, body: { hidden?: boolean }) => ({ viewers: [lan], heartbeat_seconds: body.hidden ? 30 : 5 }));
  api.leavePresence.mockReset().mockResolvedValue(undefined);
});
afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks(); });

test('an open conversation is announced once the page has started its own requests, then every heartbeat, and lists the others', async () => {
  const { result } = renderHook(() => useConversationPresence(one, false));
  expect(api.announcePresence).not.toHaveBeenCalled();
  await tick(0);
  expect(announces()).toEqual([{ id: one, client_id: presenceClientId, writing: false, edited: false, hidden: false }]);
  expect(result.current.viewers).toEqual([lan]);
  await tick(4_900);
  expect(api.announcePresence).toHaveBeenCalledTimes(1);
  await tick(100);
  expect(api.announcePresence).toHaveBeenCalledTimes(2);
  expect(presenceClientId).toMatch(/^[A-Za-z0-9_-]{8,64}$/);
});

test('starting and ending a reply is said at once, but never twice within a second', async () => {
  const { rerender } = renderHook(({ writing }) => useConversationPresence(one, writing), { initialProps: { writing: false } });
  await tick(0);
  rerender({ writing: true });
  await tick(0);
  // The first announce went out a moment ago: the change waits for the second to pass.
  expect(api.announcePresence).toHaveBeenCalledTimes(1);
  await tick(1_000);
  expect(announces()[1]).toMatchObject({ writing: true });
  await tick(3_000);
  rerender({ writing: false });
  await tick(0);
  expect(announces().at(-1)).toMatchObject({ writing: false });
});

test('an edit of the reply is reported once, with the next announce', async () => {
  const { result, rerender } = renderHook(({ writing }) => useConversationPresence(one, writing), { initialProps: { writing: true } });
  await tick(0);
  act(() => result.current.markEdited());
  await tick(5_000);
  expect(announces().map((item) => item.edited)).toEqual([false, true]);
  await tick(5_000);
  expect(announces().at(-1)).toMatchObject({ edited: false });
  rerender({ writing: true });
});

test('a tab in the background with an empty reply box leaves at once and comes back when shown', async () => {
  renderHook(() => useConversationPresence(one, false));
  await tick(0);
  setVisibility('hidden');
  await tick(0);
  expect(api.leavePresence).toHaveBeenCalledWith(one, presenceClientId);
  await tick(60_000);
  expect(api.announcePresence).toHaveBeenCalledTimes(1);
  setVisibility('visible');
  await tick(0);
  expect(api.announcePresence).toHaveBeenCalledTimes(2);
  expect(announces()[1]).toMatchObject({ hidden: false });
});

test('a tab in the background with a reply not sent yet stays listed as writing, at the slower pace the server asks', async () => {
  renderHook(() => useConversationPresence(one, true));
  await tick(0);
  await tick(2_000);
  const before = api.announcePresence.mock.calls.length;
  setVisibility('hidden');
  await tick(0);
  // Nothing extra goes out now (a closing tab's leave must not be overtaken); the next heartbeat says hidden.
  expect(api.leavePresence).not.toHaveBeenCalled();
  expect(api.announcePresence).toHaveBeenCalledTimes(before);
  await tick(3_000); // the heartbeat due 5 s after the first announce
  expect(announces().at(-1)).toMatchObject({ writing: true, hidden: true });
  const count = api.announcePresence.mock.calls.length;
  await tick(29_000);
  expect(api.announcePresence).toHaveBeenCalledTimes(count);
  await tick(1_000);
  expect(api.announcePresence).toHaveBeenCalledTimes(count + 1);
});

test('closing or leaving the page leaves at once', async () => {
  renderHook(() => useConversationPresence(one, true));
  await tick(0);
  act(() => { window.dispatchEvent(new Event('pagehide')); });
  expect(api.leavePresence).toHaveBeenCalledWith(one, presenceClientId);
  await tick(60_000);
  expect(api.announcePresence).toHaveBeenCalledTimes(1);
});

test('opening another conversation leaves the first and announces the second; closing the inbox leaves', async () => {
  const { result, rerender, unmount } = renderHook(({ id }) => useConversationPresence(id, false), { initialProps: { id: one as string | undefined } });
  await tick(0);
  rerender({ id: two });
  expect(result.current.viewers).toEqual([]);
  expect(api.leavePresence).toHaveBeenCalledWith(one, presenceClientId);
  await tick(0);
  expect(announces().at(-1)).toMatchObject({ id: two });
  rerender({ id: undefined });
  expect(api.leavePresence).toHaveBeenLastCalledWith(two, presenceClientId);
  unmount();
  expect(api.leavePresence).toHaveBeenCalledTimes(2);
});

test('a refused announce lists nobody and tries again later, without a burst', async () => {
  api.announcePresence.mockRejectedValue(new Error('offline'));
  const { result } = renderHook(() => useConversationPresence(one, false));
  await tick(0);
  expect(result.current.viewers).toEqual([]);
  await tick(9_000);
  expect(api.announcePresence).toHaveBeenCalledTimes(1);
  await tick(1_000);
  expect(api.announcePresence).toHaveBeenCalledTimes(2);
});

test('an announce the server takes later is sent again after Retry-After, not at the next heartbeat', async () => {
  const { rerender } = renderHook(({ writing }) => useConversationPresence(one, writing), { initialProps: { writing: false } });
  await tick(0);
  api.announcePresence.mockRejectedValueOnce(new TooManyRequestsError(1));
  await tick(1_000);
  rerender({ writing: true });
  await tick(0);
  expect(announces().at(-1)).toMatchObject({ writing: true });
  const refused = api.announcePresence.mock.calls.length;
  await tick(999);
  expect(api.announcePresence).toHaveBeenCalledTimes(refused);
  await tick(1);
  expect(api.announcePresence).toHaveBeenCalledTimes(refused + 1);
  expect(announces().at(-1)).toMatchObject({ writing: true });
});
