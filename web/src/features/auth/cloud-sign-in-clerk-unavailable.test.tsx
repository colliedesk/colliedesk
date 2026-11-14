import { act, render, screen } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { CloudSignIn } from './cloud-sign-in';

// The real Clerk package, not a mock: Clerk is the boundary, and here it fails as it does for a visitor
// whose blocker or network stops its script, or on a server with a wrong key.

afterEach(() => { vi.useRealTimers(); document.querySelectorAll('script[data-clerk-js-script]').forEach((script) => script.remove()); });

test('a publishable key Clerk refuses says sign-in is not set up on this server, instead of crashing the page', () => {
  vi.spyOn(console, 'error').mockImplementation(() => undefined);
  render(<main><h1>Sign in to Colliedesk</h1><CloudSignIn publishableKey="pk_test_e2e_colliedesk" onSessionToken={vi.fn()} /></main>);

  expect(screen.getByRole('heading', { level: 1, name: 'Sign in to Colliedesk' })).toBeInTheDocument();
  // The visitor cannot fix the server's key: the admin can, and an ad blocker is not the cause.
  expect(screen.getByRole('alert')).toHaveTextContent('Sign-in is not set up correctly on this server. Ask your workspace admin.');
  expect(screen.queryByText(/ad blocker/)).not.toBeInTheDocument();
});

test('when Clerk\'s script cannot be downloaded, sign-in says it could not load and offers a retry', async () => {
  vi.useFakeTimers();
  vi.spyOn(console, 'error').mockImplementation(() => undefined);
  // pk_test_ and base64 of "clerk.e2e.colliedesk.test$": a key of the right shape for a host that never answers.
  render(<CloudSignIn publishableKey="pk_test_Y2xlcmsuZTJlLmNvbGxpZWRlc2sudGVzdCQ=" onSessionToken={vi.fn()} />);
  expect(screen.getByText('Signing in…')).toBeInTheDocument();

  // The browser reports the download failed each time Clerk asks for the script, until Clerk gives up.
  const block = new MutationObserver(() => {
    document.querySelectorAll('script[data-clerk-js-script]').forEach((script) => script.dispatchEvent(new Event('error')));
  });
  block.observe(document.body, { childList: true });
  document.querySelectorAll('script[data-clerk-js-script]').forEach((script) => script.dispatchEvent(new Event('error')));
  await act(async () => { await vi.advanceTimersByTimeAsync(20_000); });
  block.disconnect();

  expect(screen.getByRole('alert')).toHaveTextContent('Sign-in could not load.');
  expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument();
});
