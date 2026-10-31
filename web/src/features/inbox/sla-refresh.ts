import { useCallback, useEffect, useRef, useState } from 'react';
import type { SLA } from '../../api/schemas';
import { nextChange } from '../../lib/sla';

// setTimeout waits at most this long; a moment further away is left to a later load of the list.
const longestWait = 2 ** 31 - 1;
// The server says a clock is due soon or late once its moment has passed; a second's margin covers the clocks.
const margin = 1000;
// A browser clock ahead of the server's reads before the server turned the mark: read again this much later,
// as long as nothing the server says has changed.
const retries = [5_000, 15_000];

// What the server says of the listed clocks: when it changes, the moment has been seen.
const marks = (items: { sla?: SLA | null }[]) => items.map((item) => [item.sla?.first_response, item.sla?.resolution]
  .map((clock) => (clock ? `${clock.due_soon ? 's' : ''}${clock.overdue ? 'o' : ''}${clock.state[0]}` : '-')).join('')).join(',');

// useSLARefresh reads the list again at the soonest moment a listed (or opened) conversation's target mark
// may change, so an inbox left open shows "Due" and "Overdue" without a reload. One timer for the whole
// list, never a tick per row; it is set again after each load, and cleared when the inbox goes.
export function useSLARefresh(items: { sla?: SLA | null }[], refresh: () => void) {
  const at = nextChange(items)?.getTime();
  const seen = marks(items);
  const latest = useRef(seen);
  latest.current = seen;
  const [retry, setRetry] = useState<{ marks: string; next: number }>();
  useEffect(() => {
    if (at === undefined) return undefined;
    const wait = Math.max(at - Date.now(), 0) + margin;
    if (wait > longestWait) return undefined;
    const timer = setTimeout(() => { refresh(); setRetry({ marks: latest.current, next: 0 }); }, wait);
    return () => clearTimeout(timer);
  }, [at, refresh]);
  useEffect(() => {
    if (!retry || retry.next >= retries.length) return undefined;
    // The server has turned a mark since: nothing to wait for.
    if (seen !== retry.marks) return undefined;
    const timer = setTimeout(() => { refresh(); setRetry({ ...retry, next: retry.next + 1 }); }, retries[retry.next]);
    return () => clearTimeout(timer);
  }, [retry, seen, refresh]);
}

// The server counts a first reply or a new status into the clocks a moment after it answers the request.
const settle = 3_000;

// useSettleTargets reads the list now and once more when the server has counted what was just done (a reply
// sent, a status changed), so a met or stopped clock does not stay "Overdue" on screen. Cleared with the inbox.
export function useSettleTargets(refresh: () => void): () => void {
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);
  return useCallback(() => {
    refresh();
    clearTimeout(timer.current);
    timer.current = setTimeout(refresh, settle);
  }, [refresh]);
}
