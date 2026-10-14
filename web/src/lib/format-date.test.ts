import { formatDate } from './format-date';
import { expect, test } from 'vitest';

test('formats UTC timestamps in the agent effective IANA timezone', () => {
  expect(formatDate('2026-10-14T00:30:00Z', 'America/New_York')).toContain('Oct 13');
  expect(formatDate('2026-10-14T00:30:00Z', 'UTC')).toContain('Oct 14');
});

test('falls back to UTC when Intl rejects a timezone', () => {
  expect(formatDate('2026-10-14T00:30:00Z', 'Invalid/Timezone')).toBe(formatDate('2026-10-14T00:30:00Z', 'UTC'));
});
