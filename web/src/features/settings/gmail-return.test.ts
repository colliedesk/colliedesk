import { afterEach, expect, test, vi } from 'vitest';

// The router keeps its own keys in each history entry's state (@tanstack/history).
const entry = (key: string, index: number) => ({ key, __TSR_key: key, __TSR_index: index });

afterEach(() => { window.history.replaceState(null, '', '/'); vi.resetModules(); });

test('every part of the page the provider returned to reads the outcome, the address is cleaned and the router keeps its entry', async () => {
  window.history.replaceState(entry('a', 3), '', '/setup/email?microsoft_error=denied&tab=replies#top');
  const { mailboxReturn } = await import('./gmail-return');
  expect(mailboxReturn()).toEqual({ kind: 'error', reason: 'denied', channel: undefined, provider: 'microsoft' });
  expect(window.location.pathname + window.location.search + window.location.hash).toBe('/setup/email?tab=replies#top');
  expect(window.history.state).toMatchObject(entry('a', 3));
  // Another part mounted after the first one cleaned the address (or the first one again, mounted anew).
  expect(mailboxReturn()).toEqual({ kind: 'error', reason: 'denied', channel: undefined, provider: 'microsoft' });
});

test('the outcome belongs to the visit it came back to: another page, a later visit and a reload do not show it', async () => {
  window.history.replaceState(entry('a', 3), '', '/settings/channels/c1?gmail=connected');
  const { mailboxReturn } = await import('./gmail-return');
  expect(mailboxReturn()).toEqual({ kind: 'connected' });
  window.history.pushState(entry('b', 4), '', '/settings/channels/c1');
  expect(mailboxReturn()).toBeUndefined();

  // A reload starts the app again on the cleaned address.
  window.history.replaceState(entry('a', 3), '', '/settings/channels/c1');
  vi.resetModules();
  const reloaded = await import('./gmail-return');
  expect(reloaded.mailboxReturn()).toBeUndefined();
});
