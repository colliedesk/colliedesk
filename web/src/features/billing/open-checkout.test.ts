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

test('an overlay that does not answer within 3 seconds (a frame the provider refuses, a blocker) opens the hosted checkout in this tab and says so', async () => {
  vi.useFakeTimers();
  try {
    sdk.create.mockReturnValue(new Promise(() => {}));
    const opened = openCheckout('https://polar.example/checkout/1');
    await vi.advanceTimersByTimeAsync(2_999);
    expect(assign).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1);
    await opened;
    expect(assign).toHaveBeenCalledTimes(1);
    expect(assign).toHaveBeenCalledWith('https://polar.example/checkout/1');
    expect(document.querySelector('[role="status"]')).toHaveTextContent('The checkout could not open inside Colliedesk, so it opens in this tab instead.');
  } finally {
    vi.useRealTimers();
    document.querySelectorAll('[role="status"]').forEach((node) => node.remove());
  }
});

test('the timeout removes the frame the embed left on the page', async () => {
  vi.useFakeTimers();
  try {
    const frame = document.createElement('iframe');
    frame.src = 'https://polar.example/checkout/1?embed=true';
    document.body.append(frame);
    const spinner = document.createElement('div');
    spinner.append(Object.assign(document.createElement('div'), { className: 'polar-loader-spinner' }));
    document.body.append(spinner);
    document.body.classList.add('polar-no-scroll');
    sdk.create.mockReturnValue(new Promise(() => {}));
    const opened = openCheckout('https://polar.example/checkout/1');
    await vi.advanceTimersByTimeAsync(3_000);
    await opened;
    expect(document.body.contains(frame)).toBe(false);
    expect(document.body.contains(spinner)).toBe(false);
    expect(document.body.classList.contains('polar-no-scroll')).toBe(false);
  } finally {
    vi.useRealTimers();
  }
});

test('an embed script that cannot be loaded falls back to the hosted checkout', async () => {
  sdk.create.mockImplementation(() => { throw new Error('boom'); });
  await openCheckout('https://polar.example/checkout/2');
  expect(assign).toHaveBeenCalledWith('https://polar.example/checkout/2');
});
