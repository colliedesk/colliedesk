import { QueryObserver } from '@tanstack/react-query';
import { expect, test, vi } from 'vitest';
import { UnauthorizedError, PlanLimitError, SeatPausedError, WorkspaceLockedError } from './api/client';
import { createQueryClient } from './query-client';

test('a 401 on any query marks the session signed out, not only the me query', async () => {
  const client = createQueryClient();
  const me = vi.fn().mockResolvedValueOnce({ agent: {} }).mockRejectedValue(new UnauthorizedError());
  const meObserver = new QueryObserver(client, { queryKey: ['me'], queryFn: me, staleTime: 15_000 });
  const unsubscribe = meObserver.subscribe(() => {});
  await vi.waitFor(() => expect(meObserver.getCurrentResult().isSuccess).toBe(true));

  // The session is revoked while me is still cached as a success; the next list request gets 401.
  await client.fetchQuery({ queryKey: ['conversations'], queryFn: () => Promise.reject(new UnauthorizedError()) }).catch(() => {});

  await vi.waitFor(() => expect(meObserver.getCurrentResult().error).toBeInstanceOf(UnauthorizedError));
  unsubscribe();
});

test('other failures are retried once and leave the session alone', async () => {
  const client = createQueryClient();
  const failing = vi.fn().mockRejectedValue(new Error('API 500'));
  await client.fetchQuery({ queryKey: ['tags'], queryFn: failing, retryDelay: 0 }).catch(() => {});
  expect(failing).toHaveBeenCalledTimes(2);
  expect(client.getQueryState(['me'])).toBeUndefined();
});

test('a 402 workspace_locked on any request locks the cached session at once and is not retried', async () => {
  const client = createQueryClient();
  const me = { agent: {}, workspace: {}, timezone: 'UTC', plan: { plan: 'trial', trial_ends_at: '2026-10-15T09:00:00Z', locked: false, trial_extended: true, agents_over: 0, seat_paused: false } };
  client.setQueryData(['me'], me);
  const refused = vi.fn().mockRejectedValue(new WorkspaceLockedError('This workspace is locked.'));
  await client.fetchQuery({ queryKey: ['conversations'], queryFn: refused, retryDelay: 0 }).catch(() => {});
  expect(refused).toHaveBeenCalledTimes(1);
  expect(client.getQueryData<typeof me>(['me'])?.plan).toEqual({ plan: 'none', trial_ends_at: '2026-10-15T09:00:00Z', locked: true, trial_extended: true, agents_over: 0, seat_paused: false });
});

test('a 402 that is not the lock leaves the session alone', async () => {
  const client = createQueryClient();
  const me = { plan: { plan: 'trial', trial_ends_at: '2026-10-15T09:00:00Z', locked: false, trial_extended: false, agents_over: 0, seat_paused: false } };
  client.setQueryData(['me'], me);
  await client.fetchQuery({ queryKey: ['x'], queryFn: () => Promise.reject(new PlanLimitError('cap')), retryDelay: 0 }).catch(() => {});
  expect(client.getQueryData(['me'])).toEqual(me);
});

test('a lock on an instance without plans (me.plan is null) never invents a trial end', async () => {
  const client = createQueryClient();
  const me = { agent: {}, workspace: {}, timezone: 'UTC', plan: null };
  client.setQueryData(['me'], me);
  await client.fetchQuery({ queryKey: ['x'], queryFn: () => Promise.reject(new WorkspaceLockedError('locked')), retryDelay: 0 }).catch(() => {});
  expect(client.getQueryData(['me'])).toEqual(me);
});

test('once me is refused, every cached workspace query is dropped so the next person never sees it', async () => {
  const client = createQueryClient();
  client.setQueryData(['conversations', { status: 'open' }], { items: [{ id: 'from-workspace-a' }] });
  client.setQueryData(['saved-replies'], { items: [] });
  client.setQueryData(['public-config'], { billing: { enabled: true } });
  await client.fetchQuery({ queryKey: ['me'], queryFn: () => Promise.reject(new UnauthorizedError()) }).catch(() => {});
  expect(client.getQueryData(['conversations', { status: 'open' }])).toBeUndefined();
  expect(client.getQueryData(['saved-replies'])).toBeUndefined();
  expect(client.getQueryData(['public-config'])).toEqual({ billing: { enabled: true } });
});

test('a 402 seat_paused on any request pauses the cached session at once', async () => {
  const client = createQueryClient();
  const me = { plan: { plan: 'small', trial_ends_at: '2026-10-15T09:00:00Z', locked: false, trial_extended: false, agents_over: 1, seat_paused: false } };
  client.setQueryData(['me'], me);
  await client.fetchQuery({ queryKey: ['x'], queryFn: () => Promise.reject(new SeatPausedError('paused')), retryDelay: 0 }).catch(() => {});
  expect(client.getQueryData<typeof me>(['me'])?.plan.seat_paused).toBe(true);
});
