import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, expect, test, vi } from 'vitest';
import { NotAnAgentError } from '../../api/cloud-signup';
import { CloudSignIn } from './cloud-sign-in';
import { answerSignupQuestions } from './test-signup-answers';

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
  answerSignupQuestions({ goal: 'Pay less per agent', role: 'Support agent', source: 'Search engine' });
  fireEvent.click(screen.getByRole('button', { name: 'Start free trial' }));

  await waitFor(() => expect(onSignUp).toHaveBeenCalledWith('fresh-token', 'Acme Support', { goal: 'lower_cost', role: 'support_agent', source: 'search' }));
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
  answerSignupQuestions();
  fireEvent.click(screen.getByRole('button', { name: 'Start free trial' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Sign in to your workspace' }));
  expect(clerk.signOut).toHaveBeenCalled();
});

test('a failed sign-out is said aloud, not swallowed', async () => {
  clerk.getToken.mockResolvedValue('token');
  clerk.signOut.mockRejectedValue(new Error('offline'));
  render(<CloudSignIn publishableKey="pk_test_example" onSessionToken={vi.fn().mockRejectedValue(new NotAnAgentError())} />);
  fireEvent.click(await screen.findByRole('button', { name: 'Use another account' }));
  expect(await screen.findByText(/sign out failed/i)).toBeInTheDocument();
});

test('keeps Clerk on the current page after sign-in so the exchange still runs', async () => {
  window.history.pushState({}, '', '/sign-in?from=cloud');
  clerk.signedIn = false;
  clerk.getToken.mockResolvedValue('session-token');
  const onSessionToken = vi.fn().mockResolvedValue(undefined);
  const view = render(<CloudSignIn publishableKey="pk_test_example" onSessionToken={onSessionToken} />);

  expect(clerk.signInProps).toHaveBeenLastCalledWith(expect.objectContaining({
    forceRedirectUrl: '/sign-in?from=cloud',
    signUpForceRedirectUrl: '/sign-in?from=cloud',
  }));
  expect(onSessionToken).not.toHaveBeenCalled();

  clerk.signedIn = true;
  view.rerender(<CloudSignIn publishableKey="pk_test_example" onSessionToken={onSessionToken} />);
  await waitFor(() => expect(onSessionToken).toHaveBeenCalledWith('session-token'));
});

// An invited agent has no Clerk identity yet, so Clerk's combined sign-in-or-up flow must be on even
// where Colliedesk sign-up is off; the server still admits only emails that belong to an agent.
test('renders the combined sign-in-or-up flow with and without Colliedesk sign-up', () => {
  clerk.signedIn = false;
  const { unmount } = render(<CloudSignIn publishableKey="pk_test_example" onSessionToken={vi.fn()} />);
  expect(clerk.signInProps).toHaveBeenLastCalledWith(expect.objectContaining({ withSignUp: true }));
  unmount();
  render(<CloudSignIn publishableKey="pk_test_example" onSessionToken={vi.fn()} onSignUp={vi.fn()} />);
  expect(clerk.signInProps).toHaveBeenLastCalledWith(expect.objectContaining({ withSignUp: true }));
});
