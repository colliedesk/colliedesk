import { render, screen } from '@testing-library/react';
import { Component } from 'react';
import type { ReactNode } from 'react';
import { afterEach, expect, test, vi } from 'vitest';
import { ClerkLoadGuard } from './clerk-load-guard';

// The real Clerk package, not a mock: what the guard tells apart is what Clerk itself throws.

afterEach(() => { document.querySelectorAll('script[data-clerk-js-script]').forEach((script) => script.remove()); });

const copy = { notLoaded: 'Sign-in could not load.', notSetUp: 'Sign-in is not set up correctly.', retry: 'Retry' };
// pk_test_ and base64 of "clerk.e2e.colliedesk.test$": a key Clerk accepts, for a host that never answers.
const wellFormedKey = 'pk_test_Y2xlcmsuZTJlLmNvbGxpZWRlc2sudGVzdCQ=';

// The app's own boundary, as the router's error screen is: whatever reaches it is shown here.
class AppBoundary extends Component<{ children: ReactNode }, { error?: Error }> {
  state: { error?: Error } = {};
  static getDerivedStateFromError(error: Error) { return { error }; }
  render() { return this.state.error ? <p>App error: {this.state.error.message}</p> : this.props.children; }
}

function BrokenForm(): ReactNode {
  throw new Error('optional question has no label');
}

test('a bug in the sign-in form reaches the app error screen, not the could-not-load message', () => {
  vi.spyOn(console, 'error').mockImplementation(() => undefined);
  render(<AppBoundary><ClerkLoadGuard publishableKey={wellFormedKey} copy={copy}><BrokenForm /></ClerkLoadGuard></AppBoundary>);

  expect(screen.getByText('App error: optional question has no label')).toBeInTheDocument();
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
});

test('a key Clerk refuses says sign-in is not set up, without a retry that cannot help', () => {
  vi.spyOn(console, 'error').mockImplementation(() => undefined);
  render(<AppBoundary><ClerkLoadGuard publishableKey="pk_test_e2e_colliedesk" copy={copy}><p>form</p></ClerkLoadGuard></AppBoundary>);

  expect(screen.getByRole('alert')).toHaveTextContent('Sign-in is not set up correctly.');
  expect(screen.queryByRole('button', { name: 'Retry' })).not.toBeInTheDocument();
  expect(screen.queryByText(/App error/)).not.toBeInTheDocument();
});

test('a missing key also says sign-in is not set up', () => {
  vi.spyOn(console, 'error').mockImplementation(() => undefined);
  render(<AppBoundary><ClerkLoadGuard publishableKey="" copy={copy}><p>form</p></ClerkLoadGuard></AppBoundary>);

  expect(screen.getByRole('alert')).toHaveTextContent('Sign-in is not set up correctly.');
});
