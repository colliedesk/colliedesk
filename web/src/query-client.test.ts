import { QueryObserver } from '@tanstack/react-query';
import { expect, test, vi } from 'vitest';
import { UnauthorizedError } from './api/client';
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
