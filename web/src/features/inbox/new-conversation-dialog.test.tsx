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
  fireEvent.click(await screen.findByRole('button', { name: /Pat Prospect/ }));
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
  expect(screen.getByRole('link', { name: 'Connect the email channel in Settings' })).toHaveAttribute('href', '/settings');
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
