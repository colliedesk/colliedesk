import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { SignupError } from '../../api/cloud-signup';
import { CloudSignup } from './cloud-signup';
import { answerSignupQuestions, chooseSignupAnswer } from './test-signup-answers';

function fill(name: string) {
  fireEvent.change(screen.getByLabelText(/workspace name/i), { target: { value: name } });
  answerSignupQuestions();
  fireEvent.click(screen.getByRole('button', { name: 'Start free trial' }));
}

test('submits the trimmed name and the three answers', async () => {
  const onCreate = vi.fn().mockResolvedValue(undefined);
  render(<CloudSignup onCreate={onCreate} onSignIn={vi.fn()} onUseAnotherAccount={vi.fn()} />);
  fill('  Acme Support ');
  await waitFor(() => expect(onCreate).toHaveBeenCalledWith('Acme Support', { goal: 'leave_helpdesk', role: 'founder', source: 'x' }));
});

test('the form is not sent until each of the three questions has an answer', async () => {
  const onCreate = vi.fn().mockResolvedValue(undefined);
  render(<CloudSignup onCreate={onCreate} onSignIn={vi.fn()} onUseAnotherAccount={vi.fn()} />);
  fireEvent.change(screen.getByLabelText(/workspace name/i), { target: { value: 'Acme Support' } });
  const submit = screen.getByRole('button', { name: 'Start free trial' });
  fireEvent.click(submit);
  expect(screen.getByRole('alert')).toHaveTextContent('Answer each question to continue.');
  fireEvent.click(within(screen.getByRole('group', { name: 'What do you want Colliedesk to do for you first?' })).getByRole('radio', { name: 'Let AI agents draft or send replies' }));
  chooseSignupAnswer('What is your role?', 'Developer or IT');
  fireEvent.click(submit);
  expect(screen.getByRole('alert')).toHaveTextContent('Answer each question to continue.');
  expect(onCreate).not.toHaveBeenCalled();
  chooseSignupAnswer('Where did you hear about Colliedesk?', 'A friend or colleague');
  fireEvent.click(submit);
  await waitFor(() => expect(onCreate).toHaveBeenCalledWith('Acme Support', { goal: 'ai_agents', role: 'developer', source: 'referral' }));
});

test('each question offers exactly its listed answers', () => {
  render(<CloudSignup onCreate={vi.fn()} onSignIn={vi.fn()} onUseAnotherAccount={vi.fn()} />);
  const radios = (legend: string) => within(screen.getByRole('group', { name: legend })).getAllByRole('radio').map((radio) => (radio as HTMLInputElement).labels?.[0]?.textContent);
  // A drop-down starts on an empty prompt, so nobody is counted as an answer they did not pick.
  const options = (label: string) => within(screen.getByRole('combobox', { name: label })).getAllByRole('option').map((option) => option.textContent);
  expect(radios('What do you want Colliedesk to do for you first?')).toEqual(['Move off Freshdesk or another helpdesk', 'Answer support email together as a team', 'Pay less per agent', 'Let AI agents draft or send replies', 'Something else']);
  expect(options('What is your role?')).toEqual(['Choose one', 'Founder or owner', 'Support lead or manager', 'Support agent', 'Developer or IT', 'Other']);
  expect(options('Where did you hear about Colliedesk?')).toEqual(['Choose one', 'X (Twitter)', 'An email from us', 'Search engine', 'Reddit', 'GitHub', 'A friend or colleague', 'Other']);
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
  ['signup_unavailable', /temporarily unavailable/i],
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

test('a visitor who came to buy is told payment comes next, not a free trial', async () => {
  const onCreate = vi.fn().mockResolvedValue(undefined);
  render(<CloudSignup buying onCreate={onCreate} onSignIn={vi.fn()} onUseAnotherAccount={vi.fn()} />);
  expect(screen.queryByText(/free trial/i)).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Start free trial' })).not.toBeInTheDocument();
  fireEvent.change(screen.getByLabelText(/workspace name/i), { target: { value: 'Acme Support' } });
  answerSignupQuestions();
  fireEvent.click(screen.getByRole('button', { name: 'Create workspace and continue to payment' }));
  await waitFor(() => expect(onCreate).toHaveBeenCalledWith('Acme Support', { goal: 'leave_helpdesk', role: 'founder', source: 'x' }));
});
