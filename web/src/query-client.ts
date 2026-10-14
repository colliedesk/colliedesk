import { MutationCache, QueryCache, QueryClient } from '@tanstack/react-query';
import { UnauthorizedError } from './api/client';

// createQueryClient builds the app's query cache. A 401 on any request means the session is gone
// (revoked, expired): the me query is refetched so the app shell switches to the signed-out state,
// even while an older successful me response is still cached. A 401 is never retried.
export function createQueryClient(): QueryClient {
  const client: QueryClient = new QueryClient({
    queryCache: new QueryCache({ onError: (error, query) => signedOut(error, query.queryKey[0]) }),
    mutationCache: new MutationCache({ onError: (error) => signedOut(error) }),
    defaultOptions: { queries: {
      staleTime: 15_000, retry: (failures, error) => !(error instanceof UnauthorizedError) && failures < 1,
    } },
  });
  function signedOut(error: Error, key?: unknown) {
    if (error instanceof UnauthorizedError && key !== 'me') void client.invalidateQueries({ queryKey: ['me'] });
  }
  return client;
}
