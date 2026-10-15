import { MutationCache, QueryCache, QueryClient } from '@tanstack/react-query';
import { UnauthorizedError, WorkspaceLockedError } from './api/client';
import type { Me } from './api/schemas';

// createQueryClient builds the app's query cache. A 401 on any request means the session is gone
// (revoked, expired): the me query is refetched so the app shell switches to the signed-out state,
// even while an older successful me response is still cached. A 402 workspace_locked on any request
// means the trial or subscription ended: the cached me is marked locked so the app shows the locked
// screen at once. Neither a 401 nor a lock is retried.
export function createQueryClient(): QueryClient {
  const client: QueryClient = new QueryClient({
    queryCache: new QueryCache({ onError: (error, query) => { signedOut(error, query.queryKey[0]); locked(error); } }),
    mutationCache: new MutationCache({ onError: (error) => { signedOut(error); locked(error); } }),
    defaultOptions: { queries: {
      staleTime: 15_000, retry: (failures, error) => !(error instanceof UnauthorizedError) && !(error instanceof WorkspaceLockedError) && failures < 1,
    } },
  });
  function signedOut(error: Error, key?: unknown) {
    if (error instanceof UnauthorizedError && key !== 'me') void client.invalidateQueries({ queryKey: ['me'] });
  }
  function locked(error: Error) {
    if (!(error instanceof WorkspaceLockedError)) return;
    client.setQueryData<Me>(['me'], (me) => {
      if (!me || me.plan?.locked) return me;
      return { ...me, plan: { plan: 'none', trial_ends_at: me.plan?.trial_ends_at ?? new Date(0).toISOString(), locked: true } };
    });
  }
  return client;
}
