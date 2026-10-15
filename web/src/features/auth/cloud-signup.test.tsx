import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { SignupError } from '../../api/cloud-signup';
import { CloudSignup } from './cloud-signup';

function fill(name: string) {
  fireEvent.change(screen.getByLabelText(/workspace name/i), { target: { value: name } });
  fireEvent.click(screen.getByRole('button', { name: 'Start free trial' }));
}

test('submits the trimmed name', async () => {
  const onCreate = vi.fn().mockResolvedValue(undefined);
  render(<CloudSignup onCreate={onCreate} onSignIn={vi.fn()} onUseAnotherAccount={vi.fn()} />);
  fill('  Acme Support ');
  await waitFor(() => expect(onCreate).toHaveBeenCalledWith('Acme Support'));
});

test('a name outside 2 to 60 characters is stopped before any request', () => {
  const onCreate = vi.fn();
  render(<CloudSignup onCreate={onCreate} onSignIn={vi.fn()} onUseAnotherAccount={vi.fn()} />);
  fill('A');
  expect(screen.getByRole('alert')).toHaveTextContent('The workspace name must be 2 to 60 characters.');
  expect(onCreate).not.toHaveBeenCalled();
});

test('a mailbox that already had a trial is sent to sign in', async () => {
  const onSignIn = vi.fn();
  render(<CloudSignup onCreate={vi.fn().mockRejectedValue(new SignupError('trial_already_used'))} onSignIn={onSignIn} onUseAnotherAccount={vi.fn()} />);
  fill('Acme');
  expect(await screen.findByRole('alert')).toHaveTextContent(/already started a colliedesk trial/i);
  fireEvent.click(screen.getByRole('button', { name: 'Sign in to your workspace' }));
  expect(onSignIn).toHaveBeenCalled();
});

test.each([
  ['rate_limited', /too many workspaces were created from this network/i],
  ['name_not_allowed', /name is not available/i],
  ['slug_unavailable', /no address was free/i],
  ['invalid', /2 to 60 characters/i],
  ['unavailable', /could not be created/i],
] as const)('%s is explained in words', async (reason, words) => {
  render(<CloudSignup onCreate={vi.fn().mockRejectedValue(new SignupError(reason))} onSignIn={vi.fn()} onUseAnotherAccount={vi.fn()} />);
  fill('Acme');
  expect(await screen.findByRole('alert')).toHaveTextContent(words);
  expect(screen.queryByRole('button', { name: 'Sign in to your workspace' })).not.toBeInTheDocument();
});

test('the person can leave with another account', () => {
  const other = vi.fn();
  render(<CloudSignup onCreate={vi.fn()} onSignIn={vi.fn()} onUseAnotherAccount={other} />);
  fireEvent.click(screen.getByRole('button', { name: 'Use another account' }));
  expect(other).toHaveBeenCalled();
});
