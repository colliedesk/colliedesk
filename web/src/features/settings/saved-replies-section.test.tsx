import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { ChangedElsewhereError, InvalidRequestError } from '../../api/client';
import type { SavedReply, SavedReplyVersion, TrashedSavedReply } from '../../api/schemas';
import { SavedRepliesSection } from './saved-replies-section';

const reply = (id: string, title: string, body: string): SavedReply => ({
  id, title, body, created_by_agent_id: null, created_at: '2026-10-15T00:00:00Z', updated_at: '2026-10-15T00:00:00Z', last_change: null,
});
const replies = [reply('1', 'Greeting', 'Hi {{customer.name}}'), reply('2', 'Refund steps', 'We refunded you.')];
const handlers = () => ({ onCreate: vi.fn().mockResolvedValue(undefined), onUpdate: vi.fn().mockResolvedValue(undefined), onDelete: vi.fn().mockResolvedValue(undefined), onReload: vi.fn(),
  loadHistory: vi.fn().mockResolvedValue({ items: [], next_before_version: null }), onRestoreVersion: vi.fn().mockResolvedValue(undefined),
  loadTrash: vi.fn().mockResolvedValue({ items: [], next_cursor: null }), onUndelete: vi.fn().mockResolvedValue(undefined) });
const ada = { agent_id: '44444444-4444-4444-8444-444444444444', name: 'Ada Admin' };
const version = (number: number, kind: SavedReplyVersion['kind'], body: string, token: string | null): SavedReplyVersion => ({
  version: number, kind, restored_from_version: null, changed_by: ada, token: token ? { id: null, label: token } : null, agent_request_id: null,
  created_at: '2026-10-16T09:00:00Z', title: 'Greeting', body,
});

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

test('the add form stays closed until New saved reply, and Cancel closes it again', () => {
  render(<SavedRepliesSection replies={replies} {...handlers()} />);
  expect(screen.queryByLabelText('Title')).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'New saved reply' }));
  expect(screen.getByLabelText('Title')).toHaveFocus();
  expect(screen.queryByRole('button', { name: 'New saved reply' })).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
  expect(screen.queryByLabelText('Title')).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'New saved reply' })).toHaveFocus();
});

test('any agent adds one; the form closes once it is saved', async () => {
  const actions = handlers();
  render(<SavedRepliesSection replies={[]} {...actions} />);
  fireEvent.click(screen.getByRole('button', { name: 'New saved reply' }));
  const add = screen.getByRole('button', { name: 'Add saved reply' });
  expect(add).toBeDisabled();
  fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Closing' } });
  expect(add).toBeDisabled();
  fireEvent.change(screen.getByLabelText('Text'), { target: { value: 'Thanks for writing.' } });
  fireEvent.click(add);
  expect(actions.onCreate).toHaveBeenCalledWith('Closing', 'Thanks for writing.');
  await waitFor(() => expect(screen.queryByLabelText('Title')).not.toBeInTheDocument());
  // Focus returns in an effect after the form unmounts.
  await waitFor(() => expect(screen.getByRole('button', { name: 'New saved reply' })).toHaveFocus());
  fireEvent.click(screen.getByRole('button', { name: 'New saved reply' }));
  expect(screen.getByLabelText('Title')).toHaveValue('');
  expect(screen.getByLabelText('Text')).toHaveValue('');
});

test('a refused add keeps what was typed and shows the server reason', async () => {
  const actions = handlers();
  actions.onCreate.mockRejectedValue(new InvalidRequestError('A saved reply with this title already exists. Titles are compared ignoring case.'));
  render(<SavedRepliesSection replies={replies} {...actions} />);
  fireEvent.click(screen.getByRole('button', { name: 'New saved reply' }));
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
  fireEvent.click(screen.getByRole('button', { name: 'New saved reply' }));
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
  expect(actions.onUpdate).toHaveBeenCalledWith('1', { title: 'Greeting', body: 'Hello {{customer.name}}', updated_at: '2026-10-15T00:00:00Z' });
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
  expect(screen.getByText('Move "Refund steps" to the trash? It can be restored for 30 days.')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
  expect(actions.onDelete).not.toHaveBeenCalled();
  expect(screen.queryByText('Move "Refund steps" to the trash? It can be restored for 30 days.')).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Delete Refund steps' }));
  fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
  expect(actions.onDelete).toHaveBeenCalledExactlyOnceWith('2', '2026-10-15T00:00:00Z');
});

test('a failed delete says so', async () => {
  const actions = handlers();
  actions.onDelete.mockRejectedValue(new Error('500'));
  render(<SavedRepliesSection replies={replies} {...actions} />);
  fireEvent.click(screen.getByRole('button', { name: 'Delete Greeting' }));
  fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('could not be saved');
});

test('an edit made from an older version is refused, says who won, and Reload shows the current text', async () => {
  const actions = handlers();
  actions.onUpdate.mockRejectedValue(new ChangedElsewhereError('This saved reply was changed by someone else. Reload to see their version, then edit again.'));
  render(<SavedRepliesSection replies={replies} {...actions} />);
  fireEvent.click(screen.getByRole('button', { name: 'Edit Greeting' }));
  fireEvent.change(screen.getByLabelText('Text', { selector: 'form[aria-label="Edit Greeting"] textarea' }), { target: { value: 'My version' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save saved reply' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('changed by someone else');
  expect(screen.getByRole('form', { name: 'Edit Greeting' })).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Reload' }));
  expect(actions.onReload).toHaveBeenCalledOnce();
  expect(screen.queryByRole('form', { name: 'Edit Greeting' })).not.toBeInTheDocument();
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
});

test('each reply says who made its latest change and through which agent token', () => {
  const edited = { ...reply('1', 'Greeting', 'Hi'), last_change: { ...version(3, 'edit', 'Hi', 'Claude triage'), title: undefined, body: undefined } };
  const restored = { ...reply('2', 'Refund steps', 'We refunded you.'), last_change: { ...version(4, 'restore', 'x', null), restored_from_version: 2, title: undefined, body: undefined } };
  render(<SavedRepliesSection replies={[edited, restored]} timezone="UTC" {...handlers()} />);
  const items = screen.getAllByRole('listitem');
  expect(within(items[0]).getByText(/Edited by Ada Admin via Claude triage/)).toBeInTheDocument();
  expect(within(items[1]).getByText(/Version 2 restored by Ada Admin/)).toBeInTheDocument();
});

test('History lists every version, newest first, and Restore makes an old one current', async () => {
  const actions = handlers();
  actions.loadHistory.mockResolvedValue({ items: [version(2, 'edit', 'Hello {{customer.name}}', 'Claude triage'), version(1, 'create', 'Hi {{customer.name}}', null)], next_before_version: null });
  render(<SavedRepliesSection replies={replies} timezone="UTC" {...actions} />);
  fireEvent.click(screen.getByRole('button', { name: 'History of Greeting' }));
  const panel = await screen.findByRole('region', { name: 'History of Greeting' });
  expect(actions.loadHistory).toHaveBeenCalledWith('1', undefined);
  const versions = await within(panel).findAllByRole('listitem');
  expect(versions).toHaveLength(2);
  expect(within(versions[0]).getByText(/Version 2/)).toBeInTheDocument();
  expect(within(versions[0]).getByText(/via Claude triage/)).toBeInTheDocument();
  expect(within(versions[1]).getByText('Hi {{customer.name}}')).toBeInTheDocument();
  expect(within(versions[0]).queryByRole('button', { name: /Restore/ })).not.toBeInTheDocument();
  fireEvent.click(within(versions[1]).getByRole('button', { name: 'Restore version 1' }));
  await waitFor(() => expect(actions.onRestoreVersion).toHaveBeenCalledWith('1', 1, '2026-10-15T00:00:00Z'));
  fireEvent.click(within(panel).getByRole('button', { name: 'Close history' }));
  expect(screen.queryByRole('region', { name: 'History of Greeting' })).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'History of Greeting' })).toHaveFocus();
});

test('Trash lists deleted replies with the days left and restores one', async () => {
  const actions = handlers();
  const trashed: TrashedSavedReply = { ...reply('9', 'Old macro', 'Retired text.'), deleted_at: '2026-10-16T09:00:00Z', deleted_by: ada, purges_at: '2026-11-15T09:00:00Z' };
  actions.loadTrash.mockResolvedValueOnce({ items: [trashed], next_cursor: null });
  render(<SavedRepliesSection replies={replies} timezone="UTC" now={new Date('2026-10-17T09:00:00Z')} {...actions} />);
  fireEvent.click(screen.getByRole('button', { name: 'Trash' }));
  const panel = await screen.findByRole('region', { name: 'Trash' });
  const item = await within(panel).findByText('Old macro');
  expect(within(panel).getByText(/29 days left/)).toBeInTheDocument();
  expect(within(panel).getByText(/Deleted by Ada Admin/)).toBeInTheDocument();
  expect(item).toBeInTheDocument();
  fireEvent.click(within(panel).getByRole('button', { name: 'Restore Old macro' }));
  await waitFor(() => expect(actions.onUndelete).toHaveBeenCalledWith('9', '2026-10-15T00:00:00Z'));
  await waitFor(() => expect(within(panel).queryByText('Old macro')).not.toBeInTheDocument());
});

test('an empty trash says so', async () => {
  const actions = handlers();
  render(<SavedRepliesSection replies={replies} timezone="UTC" {...actions} />);
  fireEvent.click(screen.getByRole('button', { name: 'Trash' }));
  expect(await screen.findByText('The trash is empty.')).toBeInTheDocument();
});

test('History marks what each version changed, folds long texts, and loads older versions on demand', async () => {
  const actions = handlers();
  const long = 'Long text. '.repeat(60);
  actions.loadHistory
    .mockResolvedValueOnce({ items: [{ ...version(3, 'edit', long, null), title: 'Greeting' }, { ...version(2, 'edit', 'Hi', null), title: 'Hello' }], next_before_version: 2 })
    .mockResolvedValueOnce({ items: [{ ...version(1, 'baseline', 'Hi', null), title: 'Hi', changed_by: null }], next_before_version: null });
  render(<SavedRepliesSection replies={replies} timezone="UTC" {...actions} />);
  fireEvent.click(screen.getByRole('button', { name: 'History of Greeting' }));
  const panel = await screen.findByRole('region', { name: 'History of Greeting' });
  let versions = await within(panel).findAllByRole('listitem');
  expect(within(versions[0]).getByText('Title changed')).toBeInTheDocument();
  expect(within(versions[0]).getByText('Text changed')).toBeInTheDocument();
  expect(within(versions[1]).queryByText('Title changed')).not.toBeInTheDocument();
  const show = within(versions[0]).getByRole('button', { name: 'Show the whole text of version 3' });
  expect(show).toHaveAttribute('aria-expanded', 'false');
  fireEvent.click(show);
  expect(show).toHaveAttribute('aria-expanded', 'true');

  fireEvent.click(within(panel).getByRole('button', { name: 'Show older versions' }));
  await waitFor(() => expect(actions.loadHistory).toHaveBeenLastCalledWith('1', 2));
  versions = await within(panel).findAllByRole('listitem');
  await waitFor(() => expect(within(panel).getAllByRole('listitem')).toHaveLength(3));
  versions = within(panel).getAllByRole('listitem');
  expect(within(versions[1]).getByText('Title changed')).toBeInTheDocument();
  expect(within(versions[2]).getByText(/State before history/)).toBeInTheDocument();
  expect(within(panel).queryByRole('button', { name: 'Show older versions' })).not.toBeInTheDocument();
});

test('Trash loads more replies on demand', async () => {
  const actions = handlers();
  const trashed = (id: string, title: string): TrashedSavedReply => ({ ...reply(id, title, 'x'), deleted_at: '2026-10-16T09:00:00Z', deleted_by: ada, purges_at: '2026-11-15T09:00:00Z' });
  actions.loadTrash.mockResolvedValueOnce({ items: [trashed('7', 'Newer')], next_cursor: 'next' }).mockResolvedValueOnce({ items: [trashed('8', 'Older')], next_cursor: null });
  render(<SavedRepliesSection replies={replies} timezone="UTC" {...actions} />);
  fireEvent.click(screen.getByRole('button', { name: 'Trash' }));
  const panel = await screen.findByRole('region', { name: 'Trash' });
  await within(panel).findByText('Newer');
  fireEvent.click(within(panel).getByRole('button', { name: 'Show more' }));
  expect(await within(panel).findByText('Older')).toBeInTheDocument();
  expect(actions.loadTrash).toHaveBeenLastCalledWith('next');
  expect(within(panel).getByText('Newer')).toBeInTheDocument();
});
