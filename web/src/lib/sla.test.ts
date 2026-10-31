import { expect, test } from 'vitest';
import type { SLA, SLAClock } from '../api/schemas';
import { clockLine, duration, slaMark } from './sla';

// The design's sample (spec §4b): Monday 2 November 2026, 12:30 in Ho Chi Minh City.
const now = new Date('2026-11-02T05:30:00Z');
const zone = 'Asia/Ho_Chi_Minh';
const clock = (change: Partial<SLAClock>): SLAClock => ({ state: 'running', due_at: null, warn_at: null, met_at: null, breached_at: null, due_soon: false, overdue: false, business_minutes_left: null, ...change });
const sla = (first: Partial<SLAClock>, resolution: Partial<SLAClock> | null = null, state: SLA['state'] = 'ok'): SLA => ({ state, first_response: clock(first), resolution: resolution && clock(resolution) });

test('a late first reply reads Overdue on the row, Reply overdue in the header, and says since when in business hours', () => {
  const mark = slaMark(sla({ due_at: '2026-11-02T04:10:00Z', breached_at: '2026-11-02T04:10:00Z', overdue: true, business_minutes_left: -80 }, { due_at: '2026-11-02T09:10:00Z' }), zone, now);
  expect(mark).toMatchObject({ tone: 'late', clock: 'first_response', short: 'Overdue', header: 'Reply overdue', full: 'First reply overdue since 11:10 AM, counted in business hours' });
});

test('a clock due soon reads its time today, or its weekday when the deadline falls on the next working day', () => {
  expect(slaMark(sla({ due_at: '2026-11-02T06:15:00Z', due_soon: true }), zone, now)).toMatchObject({ tone: 'soon', short: 'Due 1:15 PM', header: 'Reply by 1:15 PM',
    full: 'First reply due 1:15 PM today, counted in business hours' });
  const friday = new Date('2026-10-30T09:45:00Z');
  expect(slaMark(sla({ due_at: '2026-11-02T01:30:00Z', due_soon: true }), zone, friday)?.short).toBe('Due Mon 8:30 AM');
});

test('a clock far from its deadline, met or paused draws nothing on the row; the late one wins over the one due soon', () => {
  expect(slaMark(sla({ due_at: '2026-11-02T09:00:00Z' }), zone, now)).toBeUndefined();
  expect(slaMark(sla({ state: 'met', met_at: '2026-11-02T02:12:00Z' }), zone, now)).toBeUndefined();
  expect(slaMark(null, zone, now)).toBeUndefined();
  expect(slaMark(sla({ due_at: '2026-11-02T06:15:00Z', due_soon: true }, { due_at: '2026-11-02T05:00:00Z', overdue: true }), zone, now)).toMatchObject({ clock: 'resolution', header: 'Resolve overdue' });
});

test('the details say each clock in full, with the business time left or late', () => {
  expect(clockLine(clock({ due_at: '2026-11-02T04:10:00Z', breached_at: '2026-11-02T04:10:00Z', overdue: true, business_minutes_left: -80 }), zone, now))
    .toEqual({ text: 'Overdue since 11:10 AM', detail: '1 h 20 min of business time late', tone: 'late' });
  expect(clockLine(clock({ due_at: '2026-11-02T09:10:00Z', business_minutes_left: 220 }), zone, now)).toEqual({ text: 'Due 4:10 PM today', detail: '3 h 40 min of business time left', tone: undefined });
  expect(clockLine(clock({ state: 'met', met_at: '2026-11-02T02:12:00Z' }), zone, now)).toEqual({ text: 'Met 9:12 AM', tone: 'met' });
  expect(clockLine(clock({ state: 'paused' }), zone, now).text).toBe('Paused while pending');
  expect(clockLine(null, zone, now).text).toBe('No target');
  expect(duration(45)).toBe('45 min');
  expect(duration(180)).toBe('3 h');
});
