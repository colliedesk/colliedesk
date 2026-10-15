import { dayGroup, formatDate, formatShortDate } from './format-date';
import { expect, test } from 'vitest';

test('formats UTC timestamps in the agent effective IANA timezone', () => {
  expect(formatDate('2026-10-14T00:30:00Z', 'America/New_York')).toContain('Oct 13');
  expect(formatDate('2026-10-14T00:30:00Z', 'UTC')).toContain('Oct 14');
});

test('falls back to UTC when Intl rejects a timezone', () => {
  expect(formatDate('2026-10-14T00:30:00Z', 'Invalid/Timezone')).toBe(formatDate('2026-10-14T00:30:00Z', 'UTC'));
});

test('shows the year for dates outside the current year, such as imported history', () => {
  const now = new Date('2026-10-14T12:00:00Z');
  expect(formatDate('2024-01-02T18:30:00Z', 'UTC', now)).toBe('Jan 2, 2024, 6:30 PM');
  expect(formatDate('2026-10-14T00:30:00Z', 'UTC', now)).toBe('Oct 14, 12:30 AM');
  // The year is judged in the agent's timezone: this instant is still 2025 in New York.
  expect(formatDate('2026-01-01T02:00:00Z', 'America/New_York', now)).toBe('Dec 31, 2025, 9:00 PM');
});

test('short list date shows the time today and the day otherwise, in the agent timezone', () => {
  const now = new Date('2026-10-14T15:00:00Z');
  expect(formatShortDate('2026-10-14T13:05:00Z', 'UTC', now)).toBe('1:05 PM');
  expect(formatShortDate('2026-10-13T23:00:00Z', 'UTC', now)).toBe('Oct 13');
  // 00:30 UTC on the 14th is still the 13th in New York, while "now" is the 14th there too.
  expect(formatShortDate('2026-10-14T00:30:00Z', 'America/New_York', now)).toBe('Oct 13');
  expect(formatShortDate('2025-12-30T12:00:00Z', 'UTC', now)).toBe('Dec 30, 2025');
});

test('groups list dates as Today or Earlier by the calendar day in the agent timezone', () => {
  const now = new Date('2026-10-14T15:00:00Z');
  expect(dayGroup('2026-10-14T01:00:00Z', 'UTC', now)).toBe('today');
  expect(dayGroup('2026-10-13T23:59:00Z', 'UTC', now)).toBe('earlier');
  // 02:00 UTC on the 14th is still the 13th in New York, where "now" is the 14th.
  expect(dayGroup('2026-10-14T02:00:00Z', 'America/New_York', now)).toBe('earlier');
  expect(dayGroup('2026-10-14T13:00:00Z', 'America/New_York', now)).toBe('today');
});
