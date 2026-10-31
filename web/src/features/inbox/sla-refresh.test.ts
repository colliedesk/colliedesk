import { renderHook } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import type { SLA } from '../../api/schemas';
import { useSLARefresh } from './sla-refresh';

const clock = (due_at: string, warn_at: string) => ({ state: 'running' as const, due_at, warn_at, met_at: null, breached_at: null, due_soon: false, overdue: false, business_minutes_left: 60 });
const sla = (due: string, warn: string): SLA => ({ state: 'ok', first_response: clock(due, warn), resolution: null });

beforeEach(() => { vi.useFakeTimers(); vi.setSystemTime(new Date('2026-11-02T05:30:00Z')); });
afterEach(() => vi.useRealTimers());

test('the list is read again once, at the soonest warning or deadline of what is shown, and the timer goes with the inbox', () => {
  const refresh = vi.fn();
  const items = [{ sla: sla('2026-11-02T07:00:00Z', '2026-11-02T06:00:00Z') }, { sla: sla('2026-11-02T08:00:00Z', '2026-11-02T06:00:00Z') }, { sla: null }];
  const { rerender, unmount } = renderHook(({ list }) => useSLARefresh(list, refresh), { initialProps: { list: items } });
  vi.advanceTimersByTime(31 * 60_000);
  expect(refresh).toHaveBeenCalledTimes(1);
  // After the reload the next moment is the first deadline.
  rerender({ list: [{ sla: sla('2026-11-02T07:00:00Z', '2026-11-02T06:00:00Z') }] });
  vi.advanceTimersByTime(60 * 60_000);
  expect(refresh).toHaveBeenCalledTimes(2);
  rerender({ list: [{ sla: sla('2026-11-02T09:00:00Z', '2026-11-02T08:30:00Z') }] });
  unmount();
  vi.advanceTimersByTime(4 * 60 * 60_000);
  expect(refresh).toHaveBeenCalledTimes(2);
});

test('a moment beyond the longest timer is not read early', () => {
  const refresh = vi.fn();
  renderHook(() => useSLARefresh([{ sla: sla('2027-06-01T00:00:00Z', '2027-05-31T00:00:00Z') }], refresh));
  vi.advanceTimersByTime(2 ** 31 - 1);
  expect(refresh).not.toHaveBeenCalled();
});
