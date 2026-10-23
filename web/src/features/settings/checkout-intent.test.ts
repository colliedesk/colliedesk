import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { clearCheckoutIntent, readCheckoutIntent, rememberCheckoutIntent, signInCheckoutIntent } from './checkout-intent';

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

// The plan stays in the sign-in address through the identity provider's round trip, so the address
// decides: a sign-in that names a plan keeps it, a plain cloud sign-in is an unrelated visit.
test('a sign-in that names a plan remembers it', () => {
  expect(signInCheckoutIntent({ buy: 'founding' })).toBe('founding');
  expect(readCheckoutIntent()).toBe('founding');
});

test('a plain sign-in after a Founding visit in the same tab forgets the plan', () => {
  rememberCheckoutIntent('founding');
  expect(signInCheckoutIntent({})).toBeUndefined();
  expect(readCheckoutIntent()).toBeUndefined();
});

test('a one-time link keeps the remembered plan, so a self-hosted buyer still reaches the checkout', () => {
  rememberCheckoutIntent('founding');
  expect(signInCheckoutIntent({ token: 'pairing-token' })).toBe('founding');
  expect(readCheckoutIntent()).toBe('founding');
});
