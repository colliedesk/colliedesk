import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { useState } from 'react';
import type { ReactNode } from 'react';
import { expect, test, vi } from 'vitest';
import { ConflictError } from '../../api/client';
import { OutboundRefusedError } from '../../api/outbound-error';
import { NewConversationDialog } from './new-conversation-dialog';
import type { NewConversationBlock } from './new-conversation-dialog';

const api = vi.hoisted(() => ({ contacts: vi.fn() }));
vi.mock('../contacts/contact-api', async (importOriginal) => ({ ...(await importOriginal<typeof import('../contacts/contact-api')>()), contactApi: { searchContacts: api.contacts } }));

const wrap = (node: ReactNode) => render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{node}</QueryClientProvider>);

const pat = {
  id: '11111111-1111-4111-8111-111111111111', name: 'Pat Prospect', email: 'pat@prospect.example', organization: null, merged_into_id: null,
  created_at: '2026-01-02T00:00:00Z', updated_at: '2026-01-02T00:00:00Z',
};

function write(subject: string, body: string) {
  fireEvent.change(screen.getByLabelText('Subject'), { target: { value: subject } });
  fireEvent.change(screen.getByLabelText('Message'), { target: { value: body } });
}

// The page around the dialog: the button that opens it gets focus back when it closes.
function Opener({ onStart, blocked }: { onStart: (...args: unknown[]) => Promise<unknown>; blocked?: NewConversationBlock }) {
  const [open, setOpen] = useState(false);
  return <>
    <button type="button" onClick={() => setOpen(true)}>New conversation</button>
    {open && <NewConversationDialog blocked={blocked} onStart={onStart} onClose={() => setOpen(false)} />}
  </>;
}

test('a customer found by search, then the subject, the message and the copies start the conversation', async () => {
  api.contacts.mockResolvedValue({ items: [pat], has_more: false });
  const onStart = vi.fn().mockResolvedValue(undefined);
  wrap(<Opener onStart={onStart} />);
  const opener = screen.getByRole('button', { name: 'New conversation' });
  opener.focus();
  fireEvent.click(opener);
  const send = screen.getByRole('button', { name: 'Send' });
  expect(send).toBeDisabled();
  // A disabled Send says what is missing.
  expect(send).toHaveAccessibleDescription('To send, add a customer, a subject, a message.');
  fireEvent.change(screen.getByLabelText('Find a customer by name or email'), { target: { value: 'pat' } });
  fireEvent.click(await screen.findByRole('option', { name: /Pat Prospect/ }));
  expect(screen.getByText('Pat Prospect <pat@prospect.example>')).toBeInTheDocument();
  await waitFor(() => expect(screen.getByLabelText('Subject')).toHaveFocus());
  write(' Your renewal ', ' It renews next week. ');
  const cc = screen.getByRole('textbox', { name: /cc/i });
  fireEvent.change(cc, { target: { value: 'boss@prospect.example,' } });
  fireEvent.keyDown(cc, { key: ',' });
  expect(send).not.toHaveAccessibleDescription();
  fireEvent.click(send);
  await waitFor(() => expect(onStart).toHaveBeenCalledWith(pat.id, 'Your renewal', { body: 'It renews next week.', files: [], cc: ['boss@prospect.example'] }));
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  await waitFor(() => expect(opener).toHaveFocus());
});

test('a new customer goes with the conversation; a refusal keeps every field, and the address of a customer offers that customer', async () => {
  const onStart = vi.fn()
    .mockRejectedValueOnce(new OutboundRefusedError('trial_outbound_restricted', 'server text'))
    .mockRejectedValueOnce(new ConflictError('Pat Prospect (pat@prospect.example) is already a customer.', { code: 'contact_exists', contact: pat }))
    .mockResolvedValueOnce(undefined);
  wrap(<Opener onStart={onStart} />);
  fireEvent.click(screen.getByRole('button', { name: 'New conversation' }));
  fireEvent.click(screen.getByRole('button', { name: 'Add a new customer' }));
  fireEvent.change(screen.getByLabelText('Customer name'), { target: { value: 'Pat Prospect' } });
  const address = screen.getByLabelText('Customer email');
  fireEvent.change(address, { target: { value: 'not-an-address' } });
  expect(address).toHaveAttribute('aria-invalid', 'true');
  expect(address).toHaveAccessibleDescription(/valid email address/);
  fireEvent.change(address, { target: { value: 'pat@prospect.example' } });
  expect(address).not.toHaveAttribute('aria-invalid');
  write('Hello', 'First words');
  fireEvent.click(screen.getByRole('button', { name: 'Send' }));
  expect(await screen.findByRole('alert')).toHaveTextContent(/wrote to you first.*Your text is still here/);
  expect(onStart).toHaveBeenLastCalledWith({ name: 'Pat Prospect', email: 'pat@prospect.example' }, 'Hello', { body: 'First words', files: [], cc: [] });
  expect(screen.getByLabelText('Message')).toHaveValue('First words');

  fireEvent.click(screen.getByRole('button', { name: 'Send' }));
  const existing = await screen.findByRole('button', { name: 'Use Pat Prospect' });
  expect(address).toHaveAccessibleDescription(/already a customer/);
  fireEvent.click(existing);
  expect(screen.getByText('Pat Prospect <pat@prospect.example>')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Send' }));
  await waitFor(() => expect(onStart).toHaveBeenLastCalledWith(pat.id, 'Hello', { body: 'First words', files: [], cc: [] }));
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
});

test('on the trial without a verified domain the window says why, links to the way out, and takes no input', () => {
  const onOpenSettings = vi.fn();
  const onChoosePlan = vi.fn();
  const onClose = vi.fn();
  wrap(<NewConversationDialog blocked="trial" onStart={vi.fn()} onClose={onClose} onOpenSettings={onOpenSettings} onChoosePlan={onChoosePlan} />);
  const dialog = screen.getByRole('dialog', { name: 'Start a conversation' });
  expect(within(dialog).getByRole('status')).toHaveTextContent(/trial/);
  expect(within(dialog).getByRole('button', { name: 'Send' })).toBeDisabled();
  expect(within(dialog).getByLabelText('Subject')).toBeDisabled();
  fireEvent.click(within(dialog).getByRole('link', { name: 'Choose a plan' }));
  expect(onClose).toHaveBeenCalled();
  expect(onChoosePlan).toHaveBeenCalled();
  fireEvent.click(within(dialog).getByRole('link', { name: 'Verify your sending domain in Settings' }));
  expect(onOpenSettings).toHaveBeenCalled();
});

test('without an email channel the window links to Settings to connect one', () => {
  wrap(<NewConversationDialog blocked="no_channel" onStart={vi.fn()} onClose={vi.fn()} />);
  expect(screen.getByRole('link', { name: 'Connect the email channel in Settings' })).toHaveAttribute('href', '/settings/channels/type/email?tab=replies');
});

test('the picker of a new conversation offers to add the customer when nobody matches', async () => {
  api.contacts.mockResolvedValue({ items: [], has_more: false });
  wrap(<NewConversationDialog onStart={vi.fn()} onClose={vi.fn()} />);
  fireEvent.change(screen.getByLabelText('Find a customer by name or email'), { target: { value: 'nobody' } });
  expect(await screen.findByText('No customer matches. Add a new customer.')).toBeInTheDocument();
});

test('Escape closes an empty window but never one with text in it', () => {
  const onClose = vi.fn();
  wrap(<NewConversationDialog onStart={vi.fn()} onClose={onClose} />);
  fireEvent.change(screen.getByLabelText('Subject'), { target: { value: 'Draft' } });
  fireEvent.keyDown(screen.getByLabelText('Subject'), { key: 'Escape' });
  expect(onClose).not.toHaveBeenCalled();
  fireEvent.change(screen.getByLabelText('Subject'), { target: { value: '' } });
  fireEvent.keyDown(screen.getByLabelText('Subject'), { key: 'Escape' });
  expect(onClose).toHaveBeenCalled();
});

test('a click on the backdrop keeps the focus in the window, so Escape still closes it and the focus returns', async () => {
  wrap(<Opener onStart={vi.fn()} />);
  const opener = screen.getByRole('button', { name: 'New conversation' });
  opener.focus();
  fireEvent.click(opener);
  const dialog = screen.getByRole('dialog', { name: 'Start a conversation' });
  expect(fireEvent.mouseDown(dialog.parentElement!)).toBe(false);
  // Inside the window a press still places the caret where it lands.
  expect(fireEvent.mouseDown(screen.getByLabelText('Subject'))).toBe(true);
  fireEvent.keyDown(dialog, { key: 'Escape' });
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  expect(opener).toHaveFocus();
});

test('the customers found open as a list under the search box that the arrows and Enter pick from, and Escape closes', async () => {
  const sam = { ...pat, id: '22222222-2222-4222-8222-222222222222', name: 'Sam Prospect', email: 'sam@prospect.example', organization: 'Prospect Co' };
  api.contacts.mockResolvedValue({ items: [pat, sam], has_more: false });
  wrap(<NewConversationDialog onStart={vi.fn()} onClose={vi.fn()} />);
  const search = screen.getByRole('combobox', { name: 'Find a customer by name or email' });
  fireEvent.change(search, { target: { value: 'prospect' } });
  const list = await screen.findByRole('listbox');
  expect(search).toHaveAttribute('aria-expanded', 'true');
  expect(within(list).getAllByRole('option').map((option) => option.textContent)).toEqual(['Pat Prospectpat@prospect.example', 'Sam Prospectsam@prospect.example · Prospect Co']);
  expect(within(list).getByRole('option', { name: /Pat/ })).toHaveAttribute('aria-selected', 'true');
  fireEvent.keyDown(search, { key: 'Escape' });
  expect(screen.queryByRole('listbox')).not.toBeInTheDocument();
  fireEvent.keyDown(search, { key: 'ArrowDown' });
  fireEvent.keyDown(search, { key: 'ArrowDown' });
  expect(search).toHaveAttribute('aria-activedescendant', within(screen.getByRole('listbox')).getByRole('option', { name: /Sam/ }).id);
  fireEvent.keyDown(search, { key: 'Enter' });
  expect(screen.getByText('Sam Prospect <sam@prospect.example>')).toBeInTheDocument();
});

test('Enter picks from the answer to what is typed now, not from the list of the search before', async () => {
  const sam = { ...pat, id: '33333333-3333-4333-8333-333333333333', name: 'Sam Other', email: 'sam@other.example' };
  let answer: (value: unknown) => void = () => {};
  api.contacts.mockImplementation((term: string) => (term === 'pat' ? Promise.resolve({ items: [pat], has_more: false }) : new Promise((resolve) => { answer = resolve; })));
  wrap(<NewConversationDialog onStart={vi.fn()} onClose={vi.fn()} />);
  const search = screen.getByRole('combobox', { name: 'Find a customer by name or email' });
  // Arrows before any result do nothing, so the first result is still the one Enter picks.
  fireEvent.keyDown(search, { key: 'ArrowDown' });
  fireEvent.change(search, { target: { value: 'pat' } });
  await screen.findByRole('option', { name: /Pat Prospect/ });
  fireEvent.change(search, { target: { value: 'sam' } });
  fireEvent.keyDown(search, { key: 'Enter' });
  expect(screen.queryByText('Pat Prospect <pat@prospect.example>')).not.toBeInTheDocument();
  await waitFor(() => expect(api.contacts).toHaveBeenCalledWith('sam', expect.anything(), expect.anything()));
  answer({ items: [sam], has_more: false });
  await screen.findByRole('option', { name: /Sam Other/ });
  fireEvent.keyDown(search, { key: 'Enter' });
  expect(screen.getByText('Sam Other <sam@other.example>')).toBeInTheDocument();
});

// The window with two email channels: the person chooses which one sends, an off channel is listed but
// cannot be chosen, and a channel the server refuses is said at the choice with the text kept.
function TwoChannels({ onStart }: { onStart: (...args: unknown[]) => Promise<unknown> }) {
  const [channelID, setChannelID] = useState('c1');
  return <NewConversationDialog onStart={onStart} onClose={vi.fn()} channelID={channelID} onChooseChannel={setChannelID}
    senders={[{ id: 'c1', name: 'Support', off: false }, { id: 'c2', name: 'Billing', off: false }, { id: 'c3', name: 'Old', off: true }]} />;
}

test('with two email channels the window asks which one sends, and a channel turned off is said at that choice', async () => {
  api.contacts.mockResolvedValue({ items: [pat], has_more: false });
  const onStart = vi.fn().mockRejectedValueOnce(new ConflictError('This channel is off: turn it on, or start from another channel. Nothing was stored.', { code: 'channel_off' }));
  wrap(<TwoChannels onStart={onStart} />);
  const from = screen.getByLabelText('Send from');
  expect(from).toHaveValue('c1');
  expect(screen.getByRole('option', { name: 'Old (off)' })).toBeDisabled();
  fireEvent.change(from, { target: { value: 'c2' } });
  expect(from).toHaveValue('c2');
  fireEvent.change(screen.getByLabelText('Find a customer by name or email'), { target: { value: 'pat' } });
  fireEvent.click(await screen.findByRole('option', { name: /Pat Prospect/ }));
  write('Invoice', 'Here it is.');
  fireEvent.click(screen.getByRole('button', { name: 'Send' }));
  await waitFor(() => expect(from).toHaveAccessibleDescription(/This channel is off/));
  expect(from).toHaveAttribute('aria-invalid', 'true');
  expect(screen.getByLabelText('Message')).toHaveValue('Here it is.');
});

test('with one email channel the window asks nothing about channels', () => {
  wrap(<NewConversationDialog onStart={vi.fn()} onClose={vi.fn()} channelID="c1" senders={[{ id: 'c1', name: 'Support', off: false }]} />);
  expect(screen.queryByLabelText('Send from')).not.toBeInTheDocument();
});
