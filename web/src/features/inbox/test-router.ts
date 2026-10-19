import { useSyncExternalStore } from 'react';

// testRouter stands in for TanStack Router in the inbox route tests: navigating changes the search
// the route reads, like the address does, and nothing else. Tests set params themselves.
const listeners = new Set<() => void>();

export const testRouter = {
  params: {} as { conversationId?: string },
  search: {} as Record<string, unknown>,
  navigate: (options: { search?: Record<string, unknown> }) => {
    if (options.search) {
      testRouter.search = options.search;
      for (const listener of listeners) listener();
    }
  },
  reset: () => {
    testRouter.params = {};
    testRouter.search = {};
  },
};

export function useTestSearch(): Record<string, unknown> {
  return useSyncExternalStore((listener) => { listeners.add(listener); return () => listeners.delete(listener); }, () => testRouter.search);
}

export const routerModule = { useNavigate: () => testRouter.navigate, useParams: () => testRouter.params, useSearch: useTestSearch };
