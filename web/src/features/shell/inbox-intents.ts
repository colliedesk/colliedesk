import { useEffect, useRef } from 'react';

// What the sidebar asks of the inbox: focus its search box, or open the New conversation window. The sidebar is on
// every page, so the request may be made before the inbox is on screen: it waits here until the inbox takes it.
export type InboxIntent = 'search' | 'new-conversation';

let pending: InboxIntent | undefined;
const listeners = new Set<() => void>();

export function askInbox(intent: InboxIntent) {
  pending = intent;
  for (const listener of listeners) listener();
}

// takeInboxIntent hands the waiting request to its one taker.
function takeInboxIntent(): InboxIntent | undefined {
  const intent = pending;
  pending = undefined;
  return intent;
}

// useInboxIntent runs handle for a request made before the inbox mounted and for every one made while it is open.
export function useInboxIntent(handle: (intent: InboxIntent) => void) {
  const latest = useRef(handle);
  latest.current = handle;
  useEffect(() => {
    const run = () => {
      const intent = takeInboxIntent();
      if (intent) latest.current(intent);
    };
    run();
    listeners.add(run);
    return () => { listeners.delete(run); };
  }, []);
}
