import { useEffect } from 'react';
import type { SLA } from '../../api/schemas';
import { nextChange } from '../../lib/sla';

// setTimeout waits at most this long; a moment further away is left to a later load of the list.
const longestWait = 2 ** 31 - 1;
// The server says a clock is due soon or late once its moment has passed; a second's margin covers the clocks.
const margin = 1000;

// useSLARefresh reads the list again at the soonest moment a listed (or opened) conversation's target mark
// may change, so an inbox left open shows "Due" and "Overdue" without a reload. One timer for the whole
// list, never a tick per row; it is set again after each load, and cleared when the inbox goes.
export function useSLARefresh(items: { sla?: SLA | null }[], refresh: () => void) {
  const at = nextChange(items)?.getTime();
  useEffect(() => {
    if (at === undefined) return undefined;
    const wait = Math.max(at - Date.now(), 0) + margin;
    if (wait > longestWait) return undefined;
    const timer = setTimeout(refresh, wait);
    return () => clearTimeout(timer);
  }, [at, refresh]);
}
