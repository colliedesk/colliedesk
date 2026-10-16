import { beforeEach, expect, test, vi } from 'vitest';
import { checkoutSuccessPath, openCheckout } from './open-checkout';

// The Polar embed SDK is the only mocked boundary.
const sdk = vi.hoisted(() => ({ create: vi.fn() }));
vi.mock('@polar-sh/checkout/embed', () => ({ PolarEmbedCheckout: { create: sdk.create } }));

const assign = vi.fn();
type Listener = (event: { preventDefault: () => void }) => void;

function fakeCheckout() {
  const listeners = new Map<string, Listener>();
  sdk.create.mockResolvedValue({ addEventListener: (name: string, listener: Listener) => listeners.set(name, listener) });
  return listeners;
}

beforeEach(() => {
  vi.clearAllMocks();
  Object.defineProperty(window, 'location', { configurable: true, value: { assign } });
});

test('the checkout URL opens in the overlay and the page stays put', async () => {
  fakeCheckout();
  await openCheckout('https://polar.example/checkout/1');
  expect(sdk.create).toHaveBeenCalledWith('https://polar.example/checkout/1', { theme: 'light' });
  expect(assign).not.toHaveBeenCalled();
});

test('a purchase takes the user to the success state and stops the provider redirect', async () => {
  const listeners = fakeCheckout();
  await openCheckout('https://polar.example/checkout/1');
  const preventDefault = vi.fn();
  listeners.get('success')?.({ preventDefault });
  expect(preventDefault).toHaveBeenCalled();
  expect(assign).toHaveBeenCalledWith(checkoutSuccessPath);
  expect(checkoutSuccessPath).toBe('/settings/billing?checkout=success');
});

test('closing the overlay leaves the page alone', async () => {
  const listeners = fakeCheckout();
  await openCheckout('https://polar.example/checkout/1');
  expect(listeners.has('close')).toBe(false);
  expect(assign).not.toHaveBeenCalled();
});

test('an embed that cannot start falls back to the hosted checkout', async () => {
  sdk.create.mockRejectedValue(new Error('blocked'));
  await openCheckout('https://polar.example/checkout/1');
  expect(assign).toHaveBeenCalledTimes(1);
  expect(assign).toHaveBeenCalledWith('https://polar.example/checkout/1');
});

test('an embed that throws synchronously falls back to the hosted checkout', async () => {
  sdk.create.mockImplementation(() => { throw new Error('boom'); });
  await openCheckout('https://polar.example/checkout/2');
  expect(assign).toHaveBeenCalledWith('https://polar.example/checkout/2');
});
