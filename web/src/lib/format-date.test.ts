import { formatDate, formatShortDate } from './format-date';
import { expect, test } from 'vitest';

test('formats UTC timestamps in the agent effective IANA timezone', () => {
  expect(formatDate('2026-10-14T00:30:00Z', 'America/New_York')).toContain('Oct 13');
  expect(formatDate('2026-10-14T00:30:00Z', 'UTC')).toContain('Oct 14');
});

test('falls back to UTC when Intl rejects a timezone', () => {
  expect(formatDate('2026-10-14T00:30:00Z', 'Invalid/Timezone')).toBe(formatDate('2026-10-14T00:30:00Z', 'UTC'));
});

test('short list date shows the time today and the day otherwise, in the agent timezone', () => {
  const now = new Date('2026-10-14T15:00:00Z');
  expect(formatShortDate('2026-10-14T13:05:00Z', 'UTC', now)).toBe('1:05 PM');
  expect(formatShortDate('2026-10-13T23:00:00Z', 'UTC', now)).toBe('Oct 13');
  // 00:30 UTC on the 14th is still the 13th in New York, while "now" is the 14th there too.
  expect(formatShortDate('2026-10-14T00:30:00Z', 'America/New_York', now)).toBe('Oct 13');
  expect(formatShortDate('2025-12-30T12:00:00Z', 'UTC', now)).toBe('Dec 30, 2025');
});
