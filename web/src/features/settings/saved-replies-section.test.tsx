import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { InvalidRequestError } from '../../api/client';
import type { SavedReply } from '../../api/schemas';
import { SavedRepliesSection } from './saved-replies-section';

const reply = (id: string, title: string, body: string): SavedReply => ({
  id, title, body, created_by_agent_id: null, created_at: '2026-10-15T00:00:00Z', updated_at: '2026-10-15T00:00:00Z',
});
const replies = [reply('1', 'Greeting', 'Hi {{customer.name}}'), reply('2', 'Refund steps', 'We refunded you.')];
const handlers = () => ({ onCreate: vi.fn().mockResolvedValue(undefined), onUpdate: vi.fn().mockResolvedValue(undefined), onDelete: vi.fn().mockResolvedValue(undefined) });

test('lists every saved reply with its title and the start of its text, and names the variables', () => {
  render(<SavedRepliesSection replies={replies} {...handlers()} />);
  expect(screen.getByRole('heading', { name: 'Saved replies' })).toBeInTheDocument();
  expect(screen.getByText(/\{\{customer\.name\}\} and \{\{agent\.name\}\} are filled in/)).toBeInTheDocument();
  const items = screen.getAllByRole('listitem');
  expect(within(items[0]).getByText('Greeting')).toBeInTheDocument();
  expect(within(items[0]).getByText('Hi {{customer.name}}')).toBeInTheDocument();
  expect(within(items[1]).getByText('Refund steps')).toBeInTheDocument();
});

test('says so when there are none', () => {
  render(<SavedRepliesSection replies={[]} {...handlers()} />);
  expect(screen.getByText('No saved replies yet.')).toBeInTheDocument();
});

test('any agent adds one; the form clears once it is saved', async () => {
  const actions = handlers();
  render(<SavedRepliesSection replies={[]} {...actions} />);
  const add = screen.getByRole('button', { name: 'Add saved reply' });
  expect(add).toBeDisabled();
  fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Closing' } });
  expect(add).toBeDisabled();
  fireEvent.change(screen.getByLabelText('Text'), { target: { value: 'Thanks for writing.' } });
  fireEvent.click(add);
  expect(actions.onCreate).toHaveBeenCalledWith('Closing', 'Thanks for writing.');
  await waitFor(() => expect(screen.getByLabelText('Title')).toHaveValue(''));
  expect(screen.getByLabelText('Text')).toHaveValue('');
});

test('a refused add keeps what was typed and shows the server reason', async () => {
  const actions = handlers();
  actions.onCreate.mockRejectedValue(new InvalidRequestError('A saved reply with this title already exists. Titles are compared ignoring case.'));
  render(<SavedRepliesSection replies={replies} {...actions} />);
  fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'greeting' } });
  fireEvent.change(screen.getByLabelText('Text'), { target: { value: 'Again' } });
  fireEvent.click(screen.getByRole('button', { name: 'Add saved reply' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('A saved reply with this title already exists.');
  expect(screen.getByLabelText('Title')).toHaveValue('greeting');
  expect(screen.getByLabelText('Text')).toHaveValue('Again');
});

test('a failure without a reason from the server shows a generic message', async () => {
  const actions = handlers();
  actions.onCreate.mockRejectedValue(new Error('500'));
  render(<SavedRepliesSection replies={[]} {...actions} />);
  fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'A' } });
  fireEvent.change(screen.getByLabelText('Text'), { target: { value: 'B' } });
  fireEvent.click(screen.getByRole('button', { name: 'Add saved reply' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('The saved reply could not be saved. Try again.');
});

test('editing opens the reply in a form, saves both fields, and cancel changes nothing', async () => {
  const actions = handlers();
  render(<SavedRepliesSection replies={replies} {...actions} />);
  fireEvent.click(screen.getByRole('button', { name: 'Edit Greeting' }));
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
  expect(actions.onUpdate).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Edit Greeting' }));
  const form = screen.getByRole('form', { name: 'Edit Greeting' });
  expect(within(form).getByLabelText('Title')).toHaveValue('Greeting');
  expect(within(form).getByLabelText('Text')).toHaveValue('Hi {{customer.name}}');
  fireEvent.change(within(form).getByLabelText('Text'), { target: { value: 'Hello {{customer.name}}' } });
  fireEvent.click(within(form).getByRole('button', { name: 'Save saved reply' }));
  expect(actions.onUpdate).toHaveBeenCalledWith('1', { title: 'Greeting', body: 'Hello {{customer.name}}' });
  await waitFor(() => expect(screen.queryByRole('form', { name: 'Edit Greeting' })).not.toBeInTheDocument());
});

test('a refused edit stays open with the reason', async () => {
  const actions = handlers();
  actions.onUpdate.mockRejectedValue(new InvalidRequestError('Send a new title, new text, or both.'));
  render(<SavedRepliesSection replies={replies} {...actions} />);
  fireEvent.click(screen.getByRole('button', { name: 'Edit Greeting' }));
  fireEvent.click(screen.getByRole('button', { name: 'Save saved reply' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Send a new title, new text, or both.');
  expect(screen.getByRole('form', { name: 'Edit Greeting' })).toBeInTheDocument();
});

test('delete asks first and only deletes on confirmation', async () => {
  const actions = handlers();
  render(<SavedRepliesSection replies={replies} {...actions} />);
  fireEvent.click(screen.getByRole('button', { name: 'Delete Refund steps' }));
  expect(screen.getByText('Delete "Refund steps" for everyone on the team?')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
  expect(actions.onDelete).not.toHaveBeenCalled();
  expect(screen.queryByText('Delete "Refund steps" for everyone on the team?')).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Delete Refund steps' }));
  fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
  expect(actions.onDelete).toHaveBeenCalledExactlyOnceWith('2');
});

test('a failed delete says so', async () => {
  const actions = handlers();
  actions.onDelete.mockRejectedValue(new Error('500'));
  render(<SavedRepliesSection replies={replies} {...actions} />);
  fireEvent.click(screen.getByRole('button', { name: 'Delete Greeting' }));
  fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('could not be saved');
});
