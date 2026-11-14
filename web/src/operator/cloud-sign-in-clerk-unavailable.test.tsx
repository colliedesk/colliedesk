import { render, screen } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import CloudSignIn from './cloud-sign-in';

// The real Clerk package, not a mock: a server whose identity provider key is malformed.
test('a key the identity provider refuses says sign-in is not set up on this server', () => {
  vi.spyOn(console, 'error').mockImplementation(() => undefined);
  render(<CloudSignIn publishableKey="pk_test_e2e_colliedesk" endSession={false} onStep={vi.fn()} onError={vi.fn()} />);

  expect(screen.getByRole('alert')).toHaveTextContent('Sign-in is not set up correctly on this server. Ask the person who runs it.');
  expect(screen.queryByText(/ad blocker/)).not.toBeInTheDocument();
});
