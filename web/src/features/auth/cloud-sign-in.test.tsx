import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, expect, test, vi } from 'vitest';
import { CloudSignIn } from './cloud-sign-in';

const clerk = vi.hoisted(() => ({ getToken: vi.fn(), signedIn: true, signInProps: undefined as Record<string, unknown> | undefined }));

vi.mock('@clerk/clerk-react', () => ({
  ClerkProvider: ({ children }: { children: React.ReactNode }) => children,
  SignIn: (props: Record<string, unknown>) => { clerk.signInProps = props; return <div>Clerk widget</div>; },
  useAuth: () => ({ getToken: clerk.getToken, isLoaded: true, isSignedIn: clerk.signedIn }),
}));

beforeEach(() => {
  clerk.signedIn = true;
  clerk.signInProps = undefined;
  clerk.getToken.mockReset();
});

test('offers a retry when Clerk cannot provide a session token', async () => {
  clerk.getToken.mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce('session-token');
  const onSessionToken = vi.fn().mockResolvedValue(undefined);
  render(<CloudSignIn publishableKey="pk_test_example" onSessionToken={onSessionToken} />);

  const retry = await screen.findByRole('button', { name: /try cloud sign-in again/i });
  fireEvent.click(retry);

  await waitFor(() => expect(onSessionToken).toHaveBeenCalledWith('session-token'));
});

test('keeps Clerk on the current page after sign-in so the exchange still runs', async () => {
  window.history.pushState({}, '', '/redeem?from=cloud');
  clerk.signedIn = false;
  clerk.getToken.mockResolvedValue('session-token');
  const onSessionToken = vi.fn().mockResolvedValue(undefined);
  const view = render(<CloudSignIn publishableKey="pk_test_example" onSessionToken={onSessionToken} />);

  expect(clerk.signInProps).toMatchObject({
    forceRedirectUrl: '/redeem?from=cloud',
    signUpForceRedirectUrl: '/redeem?from=cloud',
  });
  expect(onSessionToken).not.toHaveBeenCalled();

  clerk.signedIn = true;
  view.rerender(<CloudSignIn publishableKey="pk_test_example" onSessionToken={onSessionToken} />);
  await waitFor(() => expect(onSessionToken).toHaveBeenCalledWith('session-token'));
});

test('renders the combined sign-in-or-up flow so an agent without a Clerk identity can sign in', () => {
  clerk.signedIn = false;
  render(<CloudSignIn publishableKey="pk_test_example" onSessionToken={vi.fn()} />);

  expect(clerk.signInProps).toMatchObject({ withSignUp: true });
});
