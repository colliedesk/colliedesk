import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { SignIn } from './sign-in';

const clerk = vi.hoisted(() => ({ getToken: vi.fn(), signOut: vi.fn(), signedIn: true, provider: vi.fn() }));

vi.mock('@clerk/clerk-react', () => ({
  ClerkProvider: ({ children, publishableKey }: { children: React.ReactNode; publishableKey: string }) => { clerk.provider(publishableKey); return children; },
  SignIn: () => <div>Clerk widget</div>,
  useAuth: () => ({ getToken: clerk.getToken, isLoaded: true, isSignedIn: clerk.signedIn }),
  useClerk: () => ({ signOut: clerk.signOut }),
}));

const key = 'pk_test_Y2xlcmsuZXhhbXBsZS5jb20k';

function setKey(value: string) {
  let meta = document.querySelector('meta[name="colliedesk-operator-clerk"]');
  if (!meta) {
    meta = document.createElement('meta');
    meta.setAttribute('name', 'colliedesk-operator-clerk');
    document.head.append(meta);
  }
  meta.setAttribute('content', value);
}

function answer(status: number, body: unknown) {
  return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }));
}

beforeEach(() => {
  vi.clearAllMocks();
  clerk.signedIn = true;
  clerk.getToken.mockResolvedValue('identity-token');
  setKey(key);
  window.history.replaceState(null, '', '/operator/sign-in');
});
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); setKey(''); window.history.replaceState(null, '', '/'); });

test('an operator signs in with their email, then the authenticator code, and the console loads in full', async () => {
  const fetch = vi.spyOn(globalThis, 'fetch')
    .mockImplementationOnce(() => answer(200, { step: 'code', email: 'ops@example.com', ticket: 't1' }))
    .mockImplementationOnce(() => answer(200, { step: 'done', email: 'ops@example.com', totp_required: true }));
  render(<SignIn onSignedIn={() => {}} />);
  fireEvent.change(await screen.findByLabelText('Authenticator code'), { target: { value: '123456' } });
  const [path, init] = fetch.mock.calls[0];
  expect(path).toBe('/api/v1/operator/auth/cloud');
  expect((init?.headers as Record<string, string>).Authorization).toBe('Bearer identity-token');
  expect(clerk.provider).toHaveBeenCalledWith(key);
  const assign = vi.fn();
  vi.stubGlobal('location', { ...window.location, assign });
  fireEvent.click(screen.getByRole('button', { name: 'Sign in' }));
  await waitFor(() => expect(assign).toHaveBeenCalledWith('/operator/'));
  expect(JSON.parse(String(fetch.mock.calls[1][1]?.body))).toEqual({ ticket: 't1', code: '123456' });
});

test('an email that is no operator is refused without saying more, and can switch accounts', async () => {
  vi.spyOn(globalThis, 'fetch').mockImplementation(() => answer(401, { error: { code: 'operator_sign_in_refused', message: 'no' } }));
  clerk.signOut.mockResolvedValue(undefined);
  render(<SignIn onSignedIn={() => {}} />);
  expect(await screen.findByRole('alert')).toHaveTextContent('This account cannot sign in to the operator console.');
  expect(screen.getByText(/collie operator add --email/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Use another account' }));
  await waitFor(() => expect(clerk.signOut).toHaveBeenCalled());
});

test('a visitor signed out of the console is signed out of the identity provider before anything is exchanged', async () => {
  window.history.replaceState(null, '', '/operator/sign-in?signed_out=1');
  const fetch = vi.spyOn(globalThis, 'fetch');
  clerk.signOut.mockImplementation(async () => { clerk.signedIn = false; });
  render(<SignIn onSignedIn={() => {}} />);
  await waitFor(() => expect(clerk.signOut).toHaveBeenCalled());
  expect(fetch).not.toHaveBeenCalled();
});

test('a link still works on a server with cloud sign-in, and without cloud sign-in no identity form loads', async () => {
  window.history.replaceState(null, '', '/operator/sign-in?token=secret-link');
  render(<SignIn onSignedIn={() => {}} />);
  expect(await screen.findByRole('button', { name: 'Continue' })).toBeInTheDocument();
  expect(screen.queryByText('Clerk widget')).toBeNull();
});

test('without cloud sign-in the page asks for a link from the server', async () => {
  setKey('');
  render(<SignIn onSignedIn={() => {}} />);
  expect(await screen.findByRole('heading', { name: 'Open the console with a sign-in link' })).toBeInTheDocument();
  expect(clerk.provider).not.toHaveBeenCalled();
});
