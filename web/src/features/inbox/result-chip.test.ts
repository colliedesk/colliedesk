import { expect, test } from 'vitest';
import { resultLabel } from './filter-panel';
import type { Results } from '../../api/results';

const window = {
  created: { from: '2026-09-23T12:00:00Z', to: '2026-10-23T12:00:00Z' }, resolved_created: { from: '2026-09-16T12:00:00Z', to: '2026-10-16T12:00:00Z' },
};
const results = {
  current: { id: 'live', windows: { '30d': window, '7d': window } },
  baseline: { id: 'before', source: 'freshdesk:account:1', windows: { '30d': { ...window, created: { from: '2026-08-02T12:00:00Z', to: '2026-09-01T12:00:00Z' } }, '7d': window } },
} as unknown as Results;

test('a results chip names the side and the days its number counts', () => {
  expect(resultLabel('live:30d.first_response', results, 'Europe/Berlin')).toBe('Counted in: First response, Colliedesk, Sep 23, 2026 – Oct 23, 2026');
  expect(resultLabel('before:30d.first_response', results, 'Europe/Berlin')).toBe('Counted in: First response, Freshdesk, Aug 2, 2026 – Sep 1, 2026');
  expect(resultLabel('live:30d.resolution', results, 'Europe/Berlin')).toBe('Counted in: Time to resolve, Colliedesk, Sep 16, 2026 – Oct 16, 2026');
  // Without the numbers (or for a refreshed snapshot) it still says what it counts.
  expect(resultLabel('gone:7d.volume', results, 'UTC')).toBe('Counted in: Conversations per week, last 7 days');
  expect(resultLabel('live:7d.volume', undefined, 'UTC')).toBe('Counted in: Conversations per week, last 7 days');
});
