import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { clearCheckoutIntent, readCheckoutIntent, rememberCheckoutIntent } from './checkout-intent';

const minute = 60_000;
beforeEach(() => window.sessionStorage.clear());
afterEach(() => vi.restoreAllMocks());

test('an intent is readable within 30 minutes and gone after that', () => {
  const start = Date.parse('2026-10-16T10:00:00Z');
  const now = vi.spyOn(Date, 'now').mockReturnValue(start);
  rememberCheckoutIntent('founding');
  now.mockReturnValue(start + 29 * minute);
  expect(readCheckoutIntent()).toBe('founding');
  now.mockReturnValue(start + 31 * minute);
  expect(readCheckoutIntent()).toBeUndefined();
});

test('clearing removes the intent, and unreadable stored values count as none', () => {
  rememberCheckoutIntent('founding');
  clearCheckoutIntent();
  expect(readCheckoutIntent()).toBeUndefined();
  window.sessionStorage.setItem('colliedesk.checkout-intent', 'founding');
  expect(readCheckoutIntent()).toBeUndefined();
});
