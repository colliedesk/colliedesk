import { QueryClient, QueryObserver } from '@tanstack/react-query';
import { afterEach, expect, test } from 'vitest';
import { restartQuery } from './restart-query';

let client: QueryClient;
afterEach(() => client.clear());

// A first request that never answers: a plain refetch would wait on that same stuck request.
test('restarting a query whose first request is stuck aborts it and sends a new one', async () => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const signals: AbortSignal[] = [];
  const queryFn = ({ signal }: { signal: AbortSignal }) => {
    signals.push(signal);
    if (signals.length === 1) return new Promise<string>(() => undefined);
    return Promise.resolve('fresh');
  };
  const observer = new QueryObserver(client, { queryKey: ['conversations', 'open'], queryFn });
  const unsubscribe = observer.subscribe(() => undefined);
  await Promise.resolve();
  expect(signals).toHaveLength(1);

  await restartQuery(client, ['conversations', 'open']);

  expect(signals).toHaveLength(2);
  expect(signals[0].aborted).toBe(true);
  expect(observer.getCurrentResult().data).toBe('fresh');
  unsubscribe();
});

test('restarting a query that failed fetches it again', async () => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  let calls = 0;
  const queryFn = () => {
    calls += 1;
    return calls === 1 ? Promise.reject(new Error('API 502')) : Promise.resolve('fresh');
  };
  const observer = new QueryObserver(client, { queryKey: ['messages', 'a'], queryFn });
  const unsubscribe = observer.subscribe(() => undefined);
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(observer.getCurrentResult().isError).toBe(true);

  await restartQuery(client, ['messages', 'a']);

  expect(calls).toBe(2);
  expect(observer.getCurrentResult().data).toBe('fresh');
  unsubscribe();
});
