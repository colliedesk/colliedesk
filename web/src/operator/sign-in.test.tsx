import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { SignIn } from './sign-in';

afterEach(() => { vi.restoreAllMocks(); window.history.replaceState(null, '', '/'); });

function answer(status: number, body: unknown) {
  return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }));
}

test('the link token leaves the address at once and is spent only by Continue', async () => {
  window.history.replaceState(null, '', '/operator/sign-in?token=secret-link');
  const fetch = vi.spyOn(globalThis, 'fetch').mockImplementation(() => answer(200, { step: 'code', email: 'ops@example.com', ticket: 't1' }));
  render(<SignIn onSignedIn={() => {}} />);
  await waitFor(() => expect(window.location.search).toBe(''));
  expect(fetch).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
  await screen.findByLabelText('Authenticator code');
  expect(fetch).toHaveBeenCalledTimes(1);
  expect(JSON.parse(String(fetch.mock.calls[0][1]?.body))).toEqual({ token: 'secret-link' });
});

test('a wrong code says how many tries are left and keeps the step', async () => {
  window.history.replaceState(null, '', '/operator/sign-in?token=t');
  vi.spyOn(globalThis, 'fetch')
    .mockImplementationOnce(() => answer(200, { step: 'code', email: 'ops@example.com', ticket: 't1' }))
    .mockImplementationOnce(() => answer(400, { error: { code: 'operator_code_wrong', message: 'no', tries_left: 7 } }));
  render(<SignIn onSignedIn={() => {}} />);
  fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
  fireEvent.change(await screen.findByLabelText('Authenticator code'), { target: { value: '123456' } });
  fireEvent.click(screen.getByRole('button', { name: 'Sign in' }));
  expect(await screen.findByRole('alert')).toHaveTextContent("That code didn't match. 7 tries left");
});

test('an expired link explains itself and shows the command for a new one', async () => {
  window.history.replaceState(null, '', '/operator/sign-in?token=t');
  vi.spyOn(globalThis, 'fetch').mockImplementation(() => answer(410, { error: { code: 'operator_link_expired', message: 'expired' } }));
  render(<SignIn onSignedIn={() => {}} />);
  fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
  expect(await screen.findByRole('heading', { name: 'This link expired' })).toBeInTheDocument();
  expect(screen.getByText(/collie operator pair --email/)).toBeInTheDocument();
});
