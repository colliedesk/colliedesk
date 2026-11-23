import { useEffect, useSyncExternalStore } from 'react';
import type { ReactNode } from 'react';
import { recordClientError } from '../lib/diagnostics';
import { reloadForNewBuild } from '../lib/stale-build';
import { messages } from '../messages';

export type PartState<T> = { status: 'loading' } | { status: 'ready'; module: T } | { status: 'error' };

export interface ReadyOptions { target?: string; ahead?: boolean }

export interface LazyPart<T> {
  // preload starts the import now (once); a failure is kept for use() to report, never thrown.
  preload: () => void;
  // ready starts the import and settles once the part can render, loaded or failed; it never rejects. A route
  // loader returns it, so the router keeps the page on screen until the next one can show. target is the address
  // being opened, given by route loaders only: a chunk missing after a deploy loads the app again there
  // (lib/stale-build.ts); without it the part shows PartUnavailable. ahead marks a
  // load ahead of a click (a pointer over a link): its failure reloads nothing until the page is really opened.
  ready: (options?: ReadyOptions) => Promise<void>;
  // use returns the part's state and starts the import if nothing did, unless load is false (it starts once
  // load turns true). A hook: call it unconditionally.
  use: (load?: boolean) => PartState<T>;
}

// lazyPart loads a part of a page from its own chunk without React.lazy and Suspense. React 19 holds a
// Suspense fallback on screen for at least 300 ms, so a chunk that arrives in 60 ms would still show 300 ms
// late; here the part renders on the first render after the module is there, or at once when it already is.
// A chunk that fails (the network dropped, or a tab opened before a deploy asks for a chunk that no longer
// exists) is a state of this part only; it never reaches the router, which would replace the whole page. Browsers
// keep a failed dynamic import failed, so the way back is a reload: for a page being opened the app makes it once by
// itself; a part that still fails, or a part inside a page, is shown with PartUnavailable.
export function lazyPart<T>(load: () => Promise<T>): LazyPart<T> {
  let state: PartState<T> | undefined;
  const listeners = new Set<() => void>();
  const set = (next: PartState<T>) => {
    state = next;
    for (const listener of listeners) listener();
  };
  let settled: Promise<void> | undefined;
  // target: the address of the page this part draws, from route loaders. opening: a loader is opening it now (the
  // address is not on screen yet). wanted: someone needs the part now, not only ahead of a click.
  let target: string | undefined;
  let opening = false;
  let wanted = false;
  // Most often a failed chunk means the tab runs the build before a deploy and the chunk is gone: the page being
  // opened, or the page on screen when it is this part's own, loads the app again once and keeps its placeholder
  // until it is replaced. A part drawn inside another page (a picker, a dialog, a block in a conversation) never
  // reloads by itself: the person may be typing. A reload the person cancels on the leave prompt shows the error.
  const recover = () => {
    if (!target || (!opening && target !== location.pathname + location.search) || !reloadForNewBuild(target)) return false;
    set({ status: 'loading' });
    setTimeout(() => { if (state?.status === 'loading') set({ status: 'error' }); }, cancelledReloadMs);
    return true;
  };
  const ready = ({ target: address, ahead = false }: ReadyOptions = {}) => {
    if (address) target = address;
    if (!ahead) {
      wanted = true;
      if (address) opening = true;
    }
    if (settled) {
      if (!ahead && state?.status === 'error') {
        recover();
        opening = false;
      }
      return settled;
    }
    set({ status: 'loading' });
    settled = load().then((module) => set({ status: 'ready', module }), (error: unknown) => {
      // Not swallowed: the console shows it, and "Report a problem" can attach it.
      console.warn('A part of the page could not be loaded', error);
      recordClientError('lazy-part', error);
      const reloading = wanted && recover();
      // The opening is over: a later use of this part reloads only while its own page is on screen.
      opening = false;
      if (!reloading) set({ status: 'error' });
    });
    return settled;
  };
  const preload = () => { void ready(); };
  const subscribe = (listener: () => void) => {
    listeners.add(listener);
    return () => { listeners.delete(listener); };
  };
  const snapshot = (): PartState<T> => state ?? loading;
  const use = (load = true) => {
    useEffect(() => { if (load) preload(); }, [load]);
    return useSyncExternalStore(subscribe, snapshot);
  };
  return { preload, ready, use };
}

const loading = { status: 'loading' } as const;
// A page still here this long after it asked to load again stayed (the person chose Stay on the leave prompt).
const cancelledReloadMs = 5000;

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
      <button type="button" className="link-action part-reload" onClick={() => window.location.reload()}>{messages.reloadPage}</button>
    </p>
  );
}
