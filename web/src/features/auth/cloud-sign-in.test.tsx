import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, expect, test, vi } from 'vitest';
import { NotAnAgentError } from '../../api/cloud-signup';
import { CloudSignIn } from './cloud-sign-in';

const clerk = vi.hoisted(() => ({ getToken: vi.fn(), signOut: vi.fn(), signedIn: true, signInProps: vi.fn() }));

vi.mock('@clerk/clerk-react', () => ({
  ClerkProvider: ({ children }: { children: React.ReactNode }) => children,
  SignIn: (props: Record<string, unknown>) => { clerk.signInProps(props); return <div>Clerk widget</div>; },
  useAuth: () => ({ getToken: clerk.getToken, isLoaded: true, isSignedIn: clerk.signedIn }),
  useClerk: () => ({ signOut: clerk.signOut }),
}));

beforeEach(() => { vi.clearAllMocks(); clerk.signedIn = true; });

test('offers a retry when Clerk cannot provide a session token', async () => {
  clerk.getToken.mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce('session-token');
  const onSessionToken = vi.fn().mockResolvedValue(undefined);
  render(<CloudSignIn publishableKey="pk_test_example" onSessionToken={onSessionToken} />);

  const retry = await screen.findByRole('button', { name: /try cloud sign-in again/i });
  fireEvent.click(retry);

  await waitFor(() => expect(onSessionToken).toHaveBeenCalledWith('session-token'));
});

test('a verified identity with no workspace can create one, with a token fetched at that moment', async () => {
  clerk.getToken.mockResolvedValueOnce('first-token').mockResolvedValueOnce('fresh-token');
  const onSessionToken = vi.fn().mockRejectedValue(new NotAnAgentError());
  const onSignUp = vi.fn().mockResolvedValue(undefined);
  render(<CloudSignIn publishableKey="pk_test_example" onSessionToken={onSessionToken} onSignUp={onSignUp} />);

  fireEvent.change(await screen.findByLabelText(/workspace name/i), { target: { value: 'Acme Support' } });
  fireEvent.click(screen.getByRole('button', { name: 'Start free trial' }));

  await waitFor(() => expect(onSignUp).toHaveBeenCalledWith('fresh-token', 'Acme Support'));
  expect(onSessionToken).toHaveBeenCalledTimes(1);
});

test('without sign-up, an identity with no workspace is told to ask for an invite and can switch account', async () => {
  clerk.getToken.mockResolvedValue('token');
  render(<CloudSignIn publishableKey="pk_test_example" onSessionToken={vi.fn().mockRejectedValue(new NotAnAgentError())} />);

  expect(await screen.findByText(/not part of a workspace yet/i)).toBeInTheDocument();
  expect(screen.queryByLabelText(/workspace name/i)).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Use another account' }));
  expect(clerk.signOut).toHaveBeenCalled();
});

test('"Sign in to your workspace" after a used trial signs the identity out of the sign-up form', async () => {
  clerk.getToken.mockResolvedValue('token');
  const { SignupError } = await import('../../api/cloud-signup');
  render(<CloudSignIn publishableKey="pk_test_example" onSessionToken={vi.fn().mockRejectedValue(new NotAnAgentError())}
    onSignUp={vi.fn().mockRejectedValue(new SignupError('trial_already_used'))} />);
  fireEvent.change(await screen.findByLabelText(/workspace name/i), { target: { value: 'Acme' } });
  fireEvent.click(screen.getByRole('button', { name: 'Start free trial' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Sign in to your workspace' }));
  expect(clerk.signOut).toHaveBeenCalled();
});

test('Clerk offers its own sign-up only where Colliedesk sign-up exists, so no orphan accounts pile up', () => {
  clerk.signedIn = false;
  const { unmount } = render(<CloudSignIn publishableKey="pk_test_example" onSessionToken={vi.fn()} onSignUp={vi.fn()} />);
  expect(clerk.signInProps).toHaveBeenLastCalledWith(expect.objectContaining({ withSignUp: true }));
  unmount();
  render(<CloudSignIn publishableKey="pk_test_example" onSessionToken={vi.fn()} />);
  expect(clerk.signInProps).toHaveBeenLastCalledWith(expect.objectContaining({ withSignUp: false }));
});

test('a failed sign-out is said aloud, not swallowed', async () => {
  clerk.getToken.mockResolvedValue('token');
  clerk.signOut.mockRejectedValue(new Error('offline'));
  render(<CloudSignIn publishableKey="pk_test_example" onSessionToken={vi.fn().mockRejectedValue(new NotAnAgentError())} />);
  fireEvent.click(await screen.findByRole('button', { name: 'Use another account' }));
  expect(await screen.findByText(/sign out failed/i)).toBeInTheDocument();
});
