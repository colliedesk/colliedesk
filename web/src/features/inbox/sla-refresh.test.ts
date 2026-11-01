import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import type { SLA } from '../../api/schemas';
import { useSettleTargets, useSLARefresh } from './sla-refresh';

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
  // After the reload the server says it is due soon; the next moment is the first deadline.
  rerender({ list: [{ sla: { ...sla('2026-11-02T07:00:00Z', '2026-11-02T06:00:00Z'), state: 'due_soon', first_response: { ...clock('2026-11-02T07:00:00Z', '2026-11-02T06:00:00Z'), due_soon: true } } }] });
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

test('a browser clock ahead of the server reads again a little later, a few times, until the moment shows', () => {
  const refresh = vi.fn();
  const items = [{ sla: sla('2026-11-02T07:00:00Z', '2026-11-02T06:00:00Z') }];
  const { rerender } = renderHook(({ list }) => useSLARefresh(list, refresh), { initialProps: { list: items } });
  act(() => vi.advanceTimersByTime(31 * 60_000));
  expect(refresh).toHaveBeenCalledTimes(1);
  // The server had not turned the flag yet: the list reads the same, and the warning is behind the browser's clock.
  rerender({ list: [{ sla: sla('2026-11-02T07:00:00Z', '2026-11-02T06:00:00Z') }] });
  act(() => vi.advanceTimersByTime(5_000));
  expect(refresh).toHaveBeenCalledTimes(2);
  act(() => vi.advanceTimersByTime(15_000));
  expect(refresh).toHaveBeenCalledTimes(3);
  act(() => vi.advanceTimersByTime(60_000));
  expect(refresh).toHaveBeenCalledTimes(3);
});

test('after a reply or a status change, the list is read at once and again once the server has counted it', () => {
  const refresh = vi.fn();
  const { result, unmount } = renderHook(() => useSettleTargets(refresh));
  act(() => result.current());
  expect(refresh).toHaveBeenCalledTimes(1);
  act(() => vi.advanceTimersByTime(3_000));
  expect(refresh).toHaveBeenCalledTimes(2);
  act(() => result.current());
  unmount();
  act(() => vi.advanceTimersByTime(10_000));
  expect(refresh).toHaveBeenCalledTimes(3);
});
