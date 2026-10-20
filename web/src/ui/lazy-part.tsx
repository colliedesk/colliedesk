import { useEffect, useSyncExternalStore } from 'react';
import type { ReactNode } from 'react';
import { recordClientError } from '../lib/diagnostics';
import { messages } from '../messages';

export type PartState<T> = { status: 'loading' } | { status: 'ready'; module: T } | { status: 'error' };

export interface LazyPart<T> {
  // preload starts the import now (once); a failure is kept for use() to report, never thrown.
  preload: () => void;
  // use returns the part's state and starts the import if nothing did. A hook: call it unconditionally.
  use: () => PartState<T>;
}

// lazyPart loads a part of a page from its own chunk without React.lazy and Suspense. React 19 holds a
// Suspense fallback on screen for at least 300 ms, so a chunk that arrives in 60 ms would still show 300 ms
// late; here the part renders on the first render after the module is there, or at once when it already is.
// A chunk that fails (the network dropped, or a tab opened before a deploy asks for a chunk that no longer
// exists) is a state of this part only, shown with PartUnavailable; it never reaches the router, which would
// replace the whole page. Browsers keep a failed dynamic import failed, so the way back is a reload.
export function lazyPart<T>(load: () => Promise<T>): LazyPart<T> {
  let state: PartState<T> | undefined;
  const listeners = new Set<() => void>();
  const set = (next: PartState<T>) => {
    state = next;
    for (const listener of listeners) listener();
  };
  const preload = () => {
    if (state) return;
    set({ status: 'loading' });
    load().then((module) => set({ status: 'ready', module }), (error: unknown) => {
      // Not swallowed: the console shows it, and "Report a problem" can attach it.
      console.warn('A part of the page could not be loaded', error);
      recordClientError('lazy-part', error);
      set({ status: 'error' });
    });
  };
  const subscribe = (listener: () => void) => {
    listeners.add(listener);
    return () => { listeners.delete(listener); };
  };
  const snapshot = (): PartState<T> => state ?? loading;
  const use = () => {
    useEffect(preload, []);
    return useSyncExternalStore(subscribe, snapshot);
  };
  return { preload, use };
}

const loading = { status: 'loading' } as const;

// Part renders a piece of a lazy part once its chunk is there: nothing until then, and failed (nothing by
// default) when it cannot load.
export function Part<T>({ part, render, failed = null }: { part: LazyPart<T>; render: (module: T) => ReactNode; failed?: ReactNode }) {
  const state = part.use();
  return state.status === 'ready' ? render(state.module) : state.status === 'error' ? failed : null;
}

// PartUnavailable stands in for a part whose chunk could not be loaded.
export function PartUnavailable() {
  return (
    <p role="alert" className="error-copy">
      {messages.partNotLoaded}{' '}
      <button type="button" className="link-action" onClick={() => window.location.reload()}>{messages.reloadPage}</button>
    </p>
  );
}
