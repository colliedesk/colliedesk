import { expect, test } from 'vitest';
import { zonedDay, zonedDayStart } from './zoned-day';

test('a calendar day in the agent timezone starts at its own UTC instant, daylight saving included', () => {
  expect(zonedDayStart('2026-10-14', 'UTC')).toBe('2026-10-14T00:00:00.000Z');
  expect(zonedDayStart('2026-10-14', 'Asia/Ho_Chi_Minh')).toBe('2026-10-13T17:00:00.000Z');
  // New York: winter time (UTC-5) on March 7, summer time (UTC-4) on March 9; the switch is March 8, 2026 at 2:00.
  expect(zonedDayStart('2026-03-07', 'America/New_York')).toBe('2026-03-07T05:00:00.000Z');
  expect(zonedDayStart('2026-03-09', 'America/New_York')).toBe('2026-03-09T04:00:00.000Z');
  expect(zonedDayStart('2026-03-08', 'America/New_York')).toBe('2026-03-08T05:00:00.000Z');
  // Santiago skips midnight on September 6, 2026: the day starts at 01:00 (04:00 UTC), not on the evening before.
  expect(zonedDayStart('2026-09-06', 'America/Santiago')).toBe('2026-09-06T04:00:00.000Z');
  expect(zonedDay(zonedDayStart('2026-09-06', 'America/Santiago')!, 'America/Santiago')).toBe('2026-09-06');
  expect(zonedDayStart('2026-09-07', 'America/Santiago')).toBe('2026-09-07T03:00:00.000Z');
  expect(zonedDayStart('not a day', 'UTC')).toBeUndefined();
});

test('an instant reads back as the calendar day it falls on in the agent timezone', () => {
  expect(zonedDay('2026-10-13T17:00:00.000Z', 'Asia/Ho_Chi_Minh')).toBe('2026-10-14');
  expect(zonedDay('2026-10-13T17:00:00.000Z', 'UTC')).toBe('2026-10-13');
  expect(zonedDay('2026-03-09T04:00:00.000Z', 'America/New_York')).toBe('2026-03-09');
  expect(zonedDay('garbage', 'UTC')).toBe('');
});
