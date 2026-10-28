import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, beforeAll, expect, test, vi } from 'vitest';
import { AttachmentsTooLargeError, AttachmentStorageError } from '../../api/attachments-error';
import { OutboundRefusedError } from '../../api/outbound-error';
import { json, serve, summary } from '../kb/kb.test.fixture';
import { ReplyConflictError } from '../../api/client';
import { presenceViewPart } from './presence-part';
import { ReplyComposer, replyPickersPart } from './reply-composer';

// The lists under the box come from their own chunk, which the composer loads when it mounts. These tests
// start with it there, as a person finds it a moment after opening a conversation; the moment before is in
// reply-composer-pickers.test.tsx.
beforeAll(async () => {
  replyPickersPart.preload();
  presenceViewPart.preload();
  await import('./reply-pickers');
  await import('./presence-view');
  // The part takes the module on the next turn of the event loop.
  await new Promise((resolve) => setTimeout(resolve, 0));
});

test('N starts a private note and R goes back to a reply, both focusing the message body', () => {
  render(<ReplyComposer />);
  const body = screen.getByLabelText('Message body');
  fireEvent.keyDown(document.body, { key: 'n' });
  expect(screen.getByRole('button', { name: 'Private note', pressed: true })).toBeInTheDocument();
  expect(body).toHaveFocus();
  body.blur();
  fireEvent.keyDown(document.body, { key: 'r' });
  expect(screen.getByRole('button', { name: 'Reply', pressed: true })).toBeInTheDocument();
  expect(body).toHaveFocus();
});

test('letters typed into a field are text, not shortcuts', () => {
  render(<><input aria-label="Search" /><ReplyComposer /></>);
  fireEvent.keyDown(screen.getByLabelText('Search'), { key: 'n' });
  fireEvent.keyDown(screen.getByLabelText('Message body'), { key: 'n' });
  expect(screen.getByRole('button', { name: 'Reply', pressed: true })).toBeInTheDocument();
});

test('shortcuts do nothing without a conversation', () => {
  render(<ReplyComposer disabled />);
  fireEvent.keyDown(document.body, { key: 'n' });
  expect(screen.getByRole('button', { name: 'Reply', pressed: true })).toBeInTheDocument();
});

test('Ctrl or Cmd + Enter sends', () => {
  const onSubmit = vi.fn();
  render(<ReplyComposer onSubmit={onSubmit} />);
  const body = screen.getByLabelText('Message body');
  fireEvent.change(body, { target: { value: 'On it.' } });
  fireEvent.keyDown(body, { key: 'Enter', metaKey: true });
  expect(onSubmit).toHaveBeenCalledWith('outgoing', 'On it.');
  fireEvent.change(body, { target: { value: 'Second' } });
  fireEvent.keyDown(body, { key: 'Enter', ctrlKey: true });
  expect(onSubmit).toHaveBeenLastCalledWith('outgoing', 'Second');
  fireEvent.keyDown(body, { key: 'Enter' });
  expect(onSubmit).toHaveBeenCalledTimes(2);
});

test('Ctrl/Cmd + Enter does not send while an input method is composing text', () => {
  const onSubmit = vi.fn();
  render(<ReplyComposer onSubmit={onSubmit} />);
  const body = screen.getByLabelText('Message body');
  fireEvent.change(body, { target: { value: 'Cảm ơn' } });
  fireEvent.keyDown(body, { key: 'Enter', ctrlKey: true, isComposing: true });
  fireEvent.keyDown(body, { key: 'Enter', metaKey: true, keyCode: 229 });
  expect(onSubmit).not.toHaveBeenCalled();
});

test('R and N still work when a radio tab or checkbox has focus', () => {
  render(<><input type="radio" aria-label="Open" /><ReplyComposer /></>);
  fireEvent.keyDown(screen.getByRole('radio', { name: 'Open' }), { key: 'n' });
  expect(screen.getByRole('button', { name: 'Private note', pressed: true })).toBeInTheDocument();
  expect(screen.getByLabelText('Message body')).toHaveFocus();
});

test('a key that ends an input method composition is not a shortcut', () => {
  render(<ReplyComposer />);
  fireEvent.keyDown(document.body, { key: 'n', isComposing: true });
  expect(screen.getByRole('button', { name: 'Reply', pressed: true })).toBeInTheDocument();
});

test('a refused reply keeps the typed text and says how to continue', async () => {
  const onSubmit = vi.fn().mockRejectedValue(new OutboundRefusedError('trial_outbound_restricted', 'server text'));
  render(<ReplyComposer onSubmit={onSubmit} />);
  const body = screen.getByLabelText('Message body');
  fireEvent.change(body, { target: { value: 'Hello there' } });
  fireEvent.keyDown(body, { key: 'Enter', ctrlKey: true });
  expect(await screen.findByRole('alert')).toHaveTextContent(/wrote to you first.*verify your sending domain.*choose a plan/i);
  expect(body).toHaveValue('Hello there');
});

test('a reply refused because the customer wrote from a merged address says where it would go, not "new conversations"', async () => {
  const onSubmit = vi.fn().mockRejectedValue(new OutboundRefusedError('trial_outbound_restricted',
    'During the trial, a reply goes only to an address that wrote to you first. This customer wrote from another address, merged into this contact, and the reply would go to dana@work.example. Verify your sending domain in Settings or upgrade to reply.',
    undefined, [], 'dana@work.example'));
  render(<ReplyComposer onSubmit={onSubmit} />);
  const body = screen.getByLabelText('Message body');
  fireEvent.change(body, { target: { value: 'Hello there' } });
  fireEvent.keyDown(body, { key: 'Enter', ctrlKey: true });
  const alert = await screen.findByRole('alert');
  expect(alert).toHaveTextContent(/wrote from another address.*would go to dana@work\.example/);
  expect(alert).not.toHaveTextContent(/new conversations/);
  expect(body).toHaveValue('Hello there');
});

test('a refused Cc names exactly the addresses the trial would not copy', async () => {
  const onSubmit = vi.fn().mockRejectedValue(new OutboundRefusedError('trial_outbound_restricted', 'server text', undefined, ['one@elsewhere.example', 'two@elsewhere.example']));
  render(<ReplyComposer onSubmit={onSubmit} />);
  const body = screen.getByLabelText('Message body');
  fireEvent.change(body, { target: { value: 'Hello all' } });
  fireEvent.keyDown(body, { key: 'Enter', ctrlKey: true });
  const alert = await screen.findByRole('alert');
  expect(alert).toHaveTextContent(/not allowed: one@elsewhere\.example, two@elsewhere\.example/i);
  expect(alert).not.toHaveTextContent(/wrote to you first/i);
  expect(body).toHaveValue('Hello all');
});

test('a reply over the daily cap says when to retry and keeps the text', async () => {
  const onSubmit = vi.fn().mockRejectedValue(new OutboundRefusedError('trial_outbound_limit', 'cap', 5400));
  render(<ReplyComposer onSubmit={onSubmit} />);
  const body = screen.getByLabelText('Message body');
  fireEvent.change(body, { target: { value: 'Hello again' } });
  fireEvent.keyDown(body, { key: 'Enter', ctrlKey: true });
  expect(await screen.findByRole('alert')).toHaveTextContent(/200 people in 24 hours, counting everyone copied.*try again in about 2 hours/i);
  expect(body).toHaveValue('Hello again');
});

test('an unexpected failure also keeps the text', async () => {
  render(<ReplyComposer onSubmit={vi.fn().mockRejectedValue(new Error('API 500'))} />);
  const body = screen.getByLabelText('Message body');
  fireEvent.change(body, { target: { value: 'Keep me' } });
  fireEvent.keyDown(body, { key: 'Enter', ctrlKey: true });
  expect(await screen.findByRole('alert')).toHaveTextContent(/could not be sent/i);
  expect(body).toHaveValue('Keep me');
});

test('the text clears and the reason goes away once a send succeeds', async () => {
  const onSubmit = vi.fn().mockRejectedValueOnce(new Error('API 500')).mockResolvedValueOnce(undefined);
  render(<ReplyComposer onSubmit={onSubmit} />);
  const body = screen.getByLabelText('Message body');
  fireEvent.change(body, { target: { value: 'Second try' } });
  fireEvent.keyDown(body, { key: 'Enter', ctrlKey: true });
  await screen.findByRole('alert');
  fireEvent.keyDown(body, { key: 'Enter', ctrlKey: true });
  await waitFor(() => expect(body).toHaveValue(''));
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
});

test('text typed while the previous message is still sending is kept when it lands', async () => {
  let accept: () => void = () => {};
  const onSubmit = vi.fn(() => new Promise<void>((resolve) => { accept = resolve; }));
  render(<ReplyComposer onSubmit={onSubmit} />);
  const body = screen.getByLabelText('Message body');
  fireEvent.change(body, { target: { value: 'Public reply' } });
  fireEvent.click(screen.getByRole('button', { name: 'Reply', pressed: true }).closest('.composer')!.querySelector('.primary-action')!);
  fireEvent.click(screen.getByRole('button', { name: 'Private note' }));
  fireEvent.change(body, { target: { value: 'Private note typed meanwhile' } });
  accept();
  await waitFor(() => expect(onSubmit).toHaveBeenCalledWith('outgoing', 'Public reply'));
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(body).toHaveValue('Private note typed meanwhile');
});

const MiB = 1024 * 1024;
const limits = { bytes: 10 * MiB, files: 10 };
const chosen = (name: string, size: number) => new File([new Uint8Array(size)], name);
const attach = (...files: File[]) => fireEvent.change(screen.getByLabelText('Attach files'), { target: { files } });

test('a reply is sent with the chosen files and they leave the composer once it is accepted', async () => {
  const onSubmit = vi.fn().mockResolvedValue(undefined);
  render(<ReplyComposer onSubmit={onSubmit} attachmentLimits={limits} />);
  const pdf = chosen('invoice.pdf', 100);
  attach(pdf);
  expect(screen.getByRole('list', { name: 'Files to send' })).toHaveTextContent('invoice.pdf');
  fireEvent.change(screen.getByLabelText('Message body'), { target: { value: 'Invoice attached' } });
  fireEvent.click(screen.getByRole('button', { name: 'Send reply' }));
  await waitFor(() => expect(onSubmit).toHaveBeenCalledWith('outgoing', 'Invoice attached', [pdf]));
  await waitFor(() => expect(screen.queryByRole('list', { name: 'Files to send' })).not.toBeInTheDocument());
});

test('a private note can carry files too', async () => {
  const onSubmit = vi.fn().mockResolvedValue(undefined);
  render(<ReplyComposer onSubmit={onSubmit} attachmentLimits={limits} />);
  fireEvent.click(screen.getByRole('button', { name: 'Private note' }));
  const log = chosen('trace.log', 5);
  attach(log);
  fireEvent.change(screen.getByLabelText('Message body'), { target: { value: 'Stack trace' } });
  fireEvent.click(screen.getByRole('button', { name: 'Add private note' }));
  await waitFor(() => expect(onSubmit).toHaveBeenCalledWith('note', 'Stack trace', [log]));
});

test('files stay chosen when the send is refused so nothing has to be picked again', async () => {
  const onSubmit = vi.fn().mockRejectedValue(new Error('API 500'));
  render(<ReplyComposer onSubmit={onSubmit} attachmentLimits={limits} />);
  attach(chosen('keep.pdf', 100));
  fireEvent.change(screen.getByLabelText('Message body'), { target: { value: 'Try' } });
  fireEvent.click(screen.getByRole('button', { name: 'Send reply' }));
  expect(await screen.findByRole('alert')).toHaveTextContent(/could not be sent/i);
  expect(screen.getByRole('list', { name: 'Files to send' })).toHaveTextContent('keep.pdf');
});

test('the send button is off and the limit is stated while the files are too big, and removing one fixes it', () => {
  const onSubmit = vi.fn();
  render(<ReplyComposer onSubmit={onSubmit} attachmentLimits={limits} />);
  attach(chosen('big.bin', 8 * MiB), chosen('bigger.bin', 3 * MiB));
  fireEvent.change(screen.getByLabelText('Message body'), { target: { value: 'Too much' } });
  expect(screen.getByRole('alert')).toHaveTextContent('10.0 MiB');
  expect(screen.getByRole('button', { name: 'Send reply' })).toBeDisabled();
  fireEvent.keyDown(screen.getByLabelText('Message body'), { key: 'Enter', ctrlKey: true });
  expect(onSubmit).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Remove bigger.bin' }));
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Send reply' })).toBeEnabled();
});

test('the server refusing the files says why in the server words and keeps the text', async () => {
  const onSubmit = vi.fn().mockRejectedValue(new AttachmentsTooLargeError('The files of one message can be at most 10 MiB in total. Nothing was sent or stored.'));
  render(<ReplyComposer onSubmit={onSubmit} attachmentLimits={limits} />);
  attach(chosen('a.bin', 1));
  fireEvent.change(screen.getByLabelText('Message body'), { target: { value: 'Hi' } });
  fireEvent.click(screen.getByRole('button', { name: 'Send reply' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('at most 10 MiB in total');
  expect(screen.getByLabelText('Message body')).toHaveValue('Hi');
});

test('a storage refusal shows the server reason and keeps the text and files', async () => {
  const onSubmit = vi.fn().mockRejectedValue(new AttachmentStorageError('This workspace has reached its 2 GiB attachment storage limit.'));
  render(<ReplyComposer onSubmit={onSubmit} attachmentLimits={limits} />);
  attach(chosen('a.bin', 1));
  fireEvent.change(screen.getByLabelText('Message body'), { target: { value: 'Hi' } });
  fireEvent.click(screen.getByRole('button', { name: 'Send reply' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('reached its 2 GiB attachment storage limit');
  expect(screen.getByLabelText('Message body')).toHaveValue('Hi');
  expect(screen.getByRole('list', { name: 'Files to send' })).toHaveTextContent('a.bin');
});

const savedReply = (id: string, title: string, body: string) => ({
  id, title, body, created_by_agent_id: null, created_at: '2026-10-15T00:00:00Z', updated_at: '2026-10-15T00:00:00Z', last_change: null,
});
const greeting = savedReply('11111111-1111-4111-8111-111111111111', 'Greeting', 'Hi {{customer.name}}, I am {{agent.name}}.');
const refund = savedReply('22222222-2222-4222-8222-222222222222', 'Refund steps', 'We refunded you.');

function pickFromButton(title: string) {
  fireEvent.click(screen.getByRole('button', { name: 'Insert saved reply' }));
  fireEvent.click(screen.getByRole('option', { name: title }));
}

test('a saved reply goes in at the cursor with both variables filled, and the rest of the draft stays', () => {
  render(<ReplyComposer savedReplies={[greeting, refund]} customerName="Mai Linh" agentName="Chinh" />);
  const body = screen.getByLabelText('Message body') as HTMLTextAreaElement;
  fireEvent.change(body, { target: { value: 'Hello.\n\nBye' } });
  body.setSelectionRange(6, 6);
  pickFromButton('Greeting');
  expect(body.value).toBe('Hello.Hi Mai Linh, I am Chinh.\n\nBye');
  expect(screen.queryByRole('dialog', { name: 'Saved replies' })).not.toBeInTheDocument();
  expect(body).toHaveFocus();
  // The cursor sits right after the inserted text, so typing continues from there.
  expect(body.selectionStart).toBe('Hello.Hi Mai Linh, I am Chinh.'.length);
  expect(body.selectionEnd).toBe(body.selectionStart);
});

test('inserting replaces nothing: selected text stays and the reply goes in before it', () => {
  render(<ReplyComposer savedReplies={[refund]} />);
  const body = screen.getByLabelText('Message body') as HTMLTextAreaElement;
  fireEvent.change(body, { target: { value: 'Keep this' } });
  body.setSelectionRange(0, 4);
  pickFromButton('Refund steps');
  expect(body.value).toBe('We refunded you.Keep this');
});

test('it works into an empty box, and the edited text is what is sent', () => {
  const onSubmit = vi.fn();
  render(<ReplyComposer savedReplies={[greeting]} customerName="Mai Linh" agentName="Chinh" onSubmit={onSubmit} />);
  pickFromButton('Greeting');
  const body = screen.getByLabelText('Message body') as HTMLTextAreaElement;
  expect(body.value).toBe('Hi Mai Linh, I am Chinh.');
  fireEvent.change(body, { target: { value: `${body.value} Please wait.` } });
  fireEvent.click(screen.getByRole('button', { name: 'Send reply' }));
  expect(onSubmit).toHaveBeenCalledWith('outgoing', 'Hi Mai Linh, I am Chinh. Please wait.');
});

test('an unknown customer never leaves a raw variable in the box', () => {
  render(<ReplyComposer savedReplies={[greeting]} agentName="Chinh" />);
  pickFromButton('Greeting');
  expect((screen.getByLabelText('Message body') as HTMLTextAreaElement).value).toBe('Hi, I am Chinh.');
});

test('inserting keeps the chosen files and works in a private note', () => {
  render(<ReplyComposer savedReplies={[refund]} />);
  const file = new File(['x'], 'invoice.pdf', { type: 'application/pdf' });
  fireEvent.change(screen.getByLabelText('Attach files'), { target: { files: [file] } });
  fireEvent.keyDown(document.body, { key: 'n' });
  pickFromButton('Refund steps');
  expect(screen.getByText('invoice.pdf')).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Private note', pressed: true })).toBeInTheDocument();
  expect((screen.getByLabelText('Message body') as HTMLTextAreaElement).value).toBe('We refunded you.');
});

test('/ opens the list from anywhere outside a text field; Enter inserts the first match', () => {
  render(<ReplyComposer savedReplies={[greeting, refund]} />);
  fireEvent.keyDown(document.body, { key: '/' });
  const search = screen.getByRole('combobox', { name: 'Search saved replies' });
  expect(search).toHaveFocus();
  fireEvent.change(search, { target: { value: 'refund' } });
  fireEvent.keyDown(search, { key: 'Enter' });
  expect((screen.getByLabelText('Message body') as HTMLTextAreaElement).value).toBe('We refunded you.');
  expect(screen.getByLabelText('Message body')).toHaveFocus();
});

test('a / typed in the message is text; Ctrl+/ opens the list from inside it', () => {
  render(<ReplyComposer savedReplies={[refund]} />);
  const body = screen.getByLabelText('Message body');
  fireEvent.keyDown(body, { key: '/' });
  expect(screen.queryByRole('dialog', { name: 'Saved replies' })).not.toBeInTheDocument();
  fireEvent.keyDown(body, { key: '/', ctrlKey: true });
  expect(screen.getByRole('dialog', { name: 'Saved replies' })).toBeInTheDocument();
});

test('Escape closes the list, leaves the draft alone and returns to the box', () => {
  render(<ReplyComposer savedReplies={[refund]} />);
  const body = screen.getByLabelText('Message body') as HTMLTextAreaElement;
  fireEvent.change(body, { target: { value: 'My draft' } });
  fireEvent.click(screen.getByRole('button', { name: 'Insert saved reply' }));
  fireEvent.keyDown(screen.getByRole('combobox', { name: 'Search saved replies' }), { key: 'Escape' });
  expect(screen.queryByRole('dialog', { name: 'Saved replies' })).not.toBeInTheDocument();
  expect(body.value).toBe('My draft');
  expect(body).toHaveFocus();
});

test('without a conversation the button is off and / does nothing', () => {
  render(<ReplyComposer disabled savedReplies={[refund]} />);
  expect(screen.getByRole('button', { name: 'Insert saved reply' })).toBeDisabled();
  fireEvent.keyDown(document.body, { key: '/' });
  expect(screen.queryByRole('dialog', { name: 'Saved replies' })).not.toBeInTheDocument();
});

test('the empty list leads to Manage saved replies', () => {
  const onManage = vi.fn();
  render(<ReplyComposer savedReplies={[]} onManageSavedReplies={onManage} />);
  fireEvent.click(screen.getByRole('button', { name: 'Insert saved reply' }));
  fireEvent.click(screen.getByRole('button', { name: 'Manage saved replies' }));
  expect(onManage).toHaveBeenCalledOnce();
});

afterEach(() => {
  delete (document as { execCommand?: unknown }).execCommand;
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

test('the browser inserts the text, so Ctrl+Z can undo it like typing', () => {
  const seen: { start: number; end: number; focused: boolean }[] = [];
  const body = () => screen.getByLabelText('Message body') as HTMLTextAreaElement;
  (document as { execCommand?: unknown }).execCommand = vi.fn(() => {
    seen.push({ start: body().selectionStart, end: body().selectionEnd, focused: document.activeElement === body() });
    return true;
  });
  render(<ReplyComposer savedReplies={[greeting]} customerName="Mai Linh" agentName="Chinh" />);
  fireEvent.change(body(), { target: { value: 'Hello.\n\nBye' } });
  body().setSelectionRange(6, 6);
  pickFromButton('Greeting');
  expect(document.execCommand).toHaveBeenCalledExactlyOnceWith('insertText', false, 'Hi Mai Linh, I am Chinh.');
  // It runs in the focused box with the cursor where the reply belongs and nothing selected.
  expect(seen).toEqual([{ start: 6, end: 6, focused: true }]);
  // The browser's own input event updates the draft; the composer does not write it a second time.
  expect(body().value).toBe('Hello.\n\nBye');
});

test('when the browser refuses to insert, the composer puts the text in itself', () => {
  (document as { execCommand?: unknown }).execCommand = vi.fn(() => false);
  render(<ReplyComposer savedReplies={[refund]} />);
  const box = screen.getByLabelText('Message body') as HTMLTextAreaElement;
  fireEvent.change(box, { target: { value: 'Draft' } });
  box.setSelectionRange(5, 5);
  pickFromButton('Refund steps');
  expect(document.execCommand).toHaveBeenCalledOnce();
  expect(box.value).toBe('DraftWe refunded you.');
});

test('a reply that still holds {{…}} asks before it is sent; a note does not', () => {
  const onSubmit = vi.fn();
  const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false);
  render(<ReplyComposer onSubmit={onSubmit} />);
  const box = screen.getByLabelText('Message body');
  fireEvent.change(box, { target: { value: 'Track it at {{ticket.url}}' } });
  fireEvent.click(screen.getByRole('button', { name: 'Send reply' }));
  expect(confirm).toHaveBeenCalledOnce();
  expect(confirm.mock.calls[0][0]).toContain('{{ticket.url}}');
  expect(onSubmit).not.toHaveBeenCalled();
  expect(box).toHaveValue('Track it at {{ticket.url}}');
  confirm.mockReturnValue(true);
  fireEvent.click(screen.getByRole('button', { name: 'Send reply' }));
  expect(onSubmit).toHaveBeenCalledExactlyOnceWith('outgoing', 'Track it at {{ticket.url}}');
  confirm.mockClear();
  fireEvent.keyDown(document.body, { key: 'n' });
  fireEvent.change(box, { target: { value: 'Remember {{ticket.url}}' } });
  fireEvent.click(screen.getByRole('button', { name: 'Add private note' }));
  expect(confirm).not.toHaveBeenCalled();
  expect(onSubmit).toHaveBeenLastCalledWith('note', 'Remember {{ticket.url}}');
});

test('a reply without {{…}} is sent without a question', () => {
  const confirm = vi.spyOn(window, 'confirm');
  const onSubmit = vi.fn();
  render(<ReplyComposer onSubmit={onSubmit} />);
  fireEvent.change(screen.getByLabelText('Message body'), { target: { value: 'Plain text, { braces } alone.' } });
  fireEvent.click(screen.getByRole('button', { name: 'Send reply' }));
  expect(confirm).not.toHaveBeenCalled();
  expect(onSubmit).toHaveBeenCalledOnce();
});

test('while the list is loading the picker says so and does not claim there are none', () => {
  render(<ReplyComposer savedReplies={[]} savedRepliesState="loading" />);
  fireEvent.click(screen.getByRole('button', { name: 'Insert saved reply' }));
  expect(screen.getByText('Loading saved replies…')).toBeInTheDocument();
  expect(screen.queryByText('No saved replies yet.')).not.toBeInTheDocument();
});

test('a list that could not be loaded says so', () => {
  render(<ReplyComposer savedReplies={[]} savedRepliesState="failed" />);
  fireEvent.click(screen.getByRole('button', { name: 'Insert saved reply' }));
  expect(screen.getByText('Saved replies could not be loaded. Close this and try again.')).toBeInTheDocument();
  expect(screen.queryByText('No saved replies yet.')).not.toBeInTheDocument();
});

const ccField = () => screen.queryByLabelText('Add Cc address');
const typeCc = (address: string) => {
  fireEvent.change(screen.getByLabelText('Add Cc address'), { target: { value: address } });
  fireEvent.keyDown(screen.getByLabelText('Add Cc address'), { key: 'Enter' });
};

test('the Cc field is collapsed until asked for, and opening it moves focus into it', () => {
  render(<ReplyComposer />);
  expect(ccField()).not.toBeInTheDocument();
  const toggle = screen.getByRole('button', { name: 'Cc', expanded: false });
  fireEvent.click(toggle);
  expect(ccField()).toHaveFocus();
  expect(screen.queryByRole('button', { name: 'Cc', expanded: false })).not.toBeInTheDocument();
});

test('addresses typed into Cc go out with the reply, after the files', () => {
  const onSubmit = vi.fn();
  render(<ReplyComposer onSubmit={onSubmit} />);
  fireEvent.click(screen.getByRole('button', { name: 'Cc' }));
  typeCc('boss@customer.example');
  typeCc('ops@customer.example');
  fireEvent.change(screen.getByLabelText('Message body'), { target: { value: 'Looping in the team.' } });
  fireEvent.click(screen.getByRole('button', { name: 'Send reply' }));
  expect(onSubmit).toHaveBeenCalledWith('outgoing', 'Looping in the team.', [], ['boss@customer.example', 'ops@customer.example']);
});

test('the addresses the customer copied are offered, shown without a click, and sent unless removed', () => {
  const onSubmit = vi.fn();
  render(<ReplyComposer onSubmit={onSubmit} suggestedCc={['boss@customer.example', 'ops@customer.example']} />);
  const list = screen.getByRole('list', { name: 'Cc' });
  expect(within(list).getAllByRole('listitem')).toHaveLength(2);
  fireEvent.click(screen.getByRole('button', { name: 'Remove ops@customer.example' }));
  fireEvent.change(screen.getByLabelText('Message body'), { target: { value: 'Thanks.' } });
  fireEvent.keyDown(screen.getByLabelText('Message body'), { key: 'Enter', ctrlKey: true });
  expect(onSubmit).toHaveBeenCalledWith('outgoing', 'Thanks.', [], ['boss@customer.example']);
});

test('removing every suggested address sends the reply to the customer alone', () => {
  const onSubmit = vi.fn();
  render(<ReplyComposer onSubmit={onSubmit} suggestedCc={['boss@customer.example']} />);
  fireEvent.click(screen.getByRole('button', { name: 'Remove boss@customer.example' }));
  fireEvent.change(screen.getByLabelText('Message body'), { target: { value: 'Only you.' } });
  fireEvent.click(screen.getByRole('button', { name: 'Send reply' }));
  expect(onSubmit).toHaveBeenCalledWith('outgoing', 'Only you.');
});

test('suggestions that arrive after the box opened fill an untouched field but never undo the agent’s edit', () => {
  const { rerender } = render(<ReplyComposer />);
  expect(ccField()).not.toBeInTheDocument();
  rerender(<ReplyComposer suggestedCc={['boss@customer.example']} />);
  expect(screen.getByRole('button', { name: 'Remove boss@customer.example' })).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Remove boss@customer.example' }));
  rerender(<ReplyComposer suggestedCc={['boss@customer.example', 'new@customer.example']} />);
  expect(screen.queryByRole('listitem')).not.toBeInTheDocument();
});

test('a private note has no Cc field and never carries addresses', () => {
  const onSubmit = vi.fn();
  render(<ReplyComposer onSubmit={onSubmit} suggestedCc={['boss@customer.example']} />);
  fireEvent.click(screen.getByRole('button', { name: 'Private note' }));
  expect(ccField()).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Cc' })).not.toBeInTheDocument();
  fireEvent.change(screen.getByLabelText('Message body'), { target: { value: 'Internal.' } });
  fireEvent.click(screen.getByRole('button', { name: 'Add private note' }));
  expect(onSubmit).toHaveBeenCalledWith('note', 'Internal.');
  fireEvent.click(screen.getByRole('button', { name: 'Reply' }));
  expect(screen.getByRole('button', { name: 'Remove boss@customer.example' })).toBeInTheDocument();
});

test('an address typed but not yet confirmed is not lost when the reply is sent with the button', () => {
  const onSubmit = vi.fn();
  render(<ReplyComposer onSubmit={onSubmit} />);
  fireEvent.click(screen.getByRole('button', { name: 'Cc' }));
  fireEvent.change(screen.getByLabelText('Add Cc address'), { target: { value: 'late@customer.example' } });
  fireEvent.change(screen.getByLabelText('Message body'), { target: { value: 'Hello.' } });
  fireEvent.blur(screen.getByLabelText('Add Cc address'));
  fireEvent.click(screen.getByRole('button', { name: 'Send reply' }));
  expect(onSubmit).toHaveBeenCalledWith('outgoing', 'Hello.', [], ['late@customer.example']);
});

test('Ctrl+Enter from the Cc box sends the reply', () => {
  const onSubmit = vi.fn();
  render(<ReplyComposer onSubmit={onSubmit} suggestedCc={['boss@customer.example']} />);
  fireEvent.change(screen.getByLabelText('Message body'), { target: { value: 'Sent from Cc.' } });
  fireEvent.keyDown(screen.getByLabelText('Add Cc address'), { key: 'Enter', ctrlKey: true });
  expect(onSubmit).toHaveBeenCalledWith('outgoing', 'Sent from Cc.', [], ['boss@customer.example']);
});

test('a refused reply keeps the Cc list with the text', async () => {
  const onSubmit = vi.fn().mockRejectedValue(new OutboundRefusedError('trial_outbound_restricted', 'server text'));
  render(<ReplyComposer onSubmit={onSubmit} suggestedCc={['boss@customer.example']} />);
  fireEvent.change(screen.getByLabelText('Message body'), { target: { value: 'Try.' } });
  fireEvent.click(screen.getByRole('button', { name: 'Send reply' }));
  expect(await screen.findByRole('alert')).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Remove boss@customer.example' })).toBeInTheDocument();
});

test('an invalid address typed into Cc blocks Send until it is fixed or cleared', () => {
  const onSubmit = vi.fn();
  render(<ReplyComposer onSubmit={onSubmit} />);
  fireEvent.click(screen.getByRole('button', { name: 'Cc' }));
  fireEvent.change(screen.getByLabelText('Message body'), { target: { value: 'Hello.' } });
  fireEvent.change(screen.getByLabelText('Add Cc address'), { target: { value: 'boss@' } });
  fireEvent.blur(screen.getByLabelText('Add Cc address'));
  expect(screen.getByRole('button', { name: 'Send reply' })).toBeDisabled();
  fireEvent.keyDown(screen.getByLabelText('Message body'), { key: 'Enter', ctrlKey: true });
  expect(onSubmit).not.toHaveBeenCalled();
  fireEvent.change(screen.getByLabelText('Add Cc address'), { target: { value: 'boss@customer.example' } });
  fireEvent.blur(screen.getByLabelText('Add Cc address'));
  expect(screen.getByRole('button', { name: 'Send reply' })).toBeEnabled();
  fireEvent.click(screen.getByRole('button', { name: 'Send reply' }));
  expect(onSubmit).toHaveBeenCalledWith('outgoing', 'Hello.', [], ['boss@customer.example']);
});

function withQueries(node: ReactNode) {
  return <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{node}</QueryClientProvider>;
}

test('Articles links a published article at the cursor as its title and address on two lines, plain text for the mail', async () => {
  serve({
    'GET /api/v1/help-center': () => json(200, { enabled: true, title: '', locale: 'en', content_version: 1, updated_at: null, url: 'https://desk.example.com/help/acme' }),
    'GET /api/v1/kb/articles': () => json(200, { items: [summary(7, 'Reset your password', 'published')], next_cursor: null }),
  });
  const onSubmit = vi.fn();
  render(withQueries(<ReplyComposer onSubmit={onSubmit} />));
  const body = screen.getByLabelText('Message body') as HTMLTextAreaElement;
  fireEvent.change(body, { target: { value: 'See:\nThanks' } });
  body.setSelectionRange(5, 5);
  const button = screen.getByRole('button', { name: 'Articles' });
  fireEvent.click(button);
  expect(button).toHaveAttribute('aria-expanded', 'true');
  await screen.findByRole('option', { name: 'Reset your password', selected: true });
  fireEvent.keyDown(screen.getByRole('combobox', { name: 'Search published articles' }), { key: 'Enter' });
  await waitFor(() => expect(body.value).toBe('See:\nReset your password\nhttps://desk.example.com/help/acme/a/7-reset-your-password\nThanks'));
  expect(screen.queryByRole('dialog', { name: 'Articles' })).not.toBeInTheDocument();
  expect(body).toHaveFocus();
  fireEvent.keyDown(body, { key: 'Enter', ctrlKey: true });
  expect(onSubmit).toHaveBeenCalledWith('outgoing', 'See:\nReset your password\nhttps://desk.example.com/help/acme/a/7-reset-your-password\nThanks');
});

test('Escape closes the articles, leaves the draft alone and returns to the box; the saved replies take their place', async () => {
  serve({ 'GET /api/v1/help-center': () => json(200, { enabled: false, title: '', locale: 'en', content_version: 1, updated_at: null, url: 'https://desk.example.com/help/acme' }) });
  render(withQueries(<ReplyComposer savedReplies={[refund]} />));
  const body = screen.getByLabelText('Message body') as HTMLTextAreaElement;
  fireEvent.change(body, { target: { value: 'My draft' } });
  fireEvent.click(screen.getByRole('button', { name: 'Articles' }));
  fireEvent.keyDown(await screen.findByRole('combobox', { name: 'Search published articles' }), { key: 'Escape' });
  expect(screen.queryByRole('dialog', { name: 'Articles' })).not.toBeInTheDocument();
  expect(body.value).toBe('My draft');
  expect(body).toHaveFocus();
  fireEvent.click(screen.getByRole('button', { name: 'Articles' }));
  await screen.findByRole('dialog', { name: 'Articles' });
  fireEvent.click(screen.getByRole('button', { name: 'Insert saved reply' }));
  expect(screen.getByRole('dialog', { name: 'Saved replies' })).toBeInTheDocument();
  expect(screen.queryByRole('dialog', { name: 'Articles' })).not.toBeInTheDocument();
});

test('in the middle of a line the article gets lines of its own, so no word sticks to its address', async () => {
  serve({
    'GET /api/v1/help-center': () => json(200, { enabled: true, title: '', locale: 'en', content_version: 1, updated_at: null, url: 'https://desk.example.com/help/acme' }),
    'GET /api/v1/kb/articles': () => json(200, { items: [summary(7, 'Reset your password', 'published')], next_cursor: null }),
  });
  render(withQueries(<ReplyComposer />));
  const body = screen.getByLabelText('Message body') as HTMLTextAreaElement;
  fireEvent.change(body, { target: { value: 'Read this guide today.' } });
  body.setSelectionRange(15, 15);
  fireEvent.click(screen.getByRole('button', { name: 'Articles' }));
  fireEvent.click(await screen.findByRole('option', { name: 'Reset your password' }));
  await waitFor(() => expect(body.value).toBe('Read this guide\nReset your password\nhttps://desk.example.com/help/acme/a/7-reset-your-password\n today.'));
});

test('the composer says when its reply box holds a reply not sent yet, and when that reply changes; a private note never counts', async () => {
  const onReplyDraft = vi.fn();
  const onReplyEdited = vi.fn();
  let accept: () => void = () => {};
  const onSubmit = vi.fn(() => new Promise<void>((resolve) => { accept = resolve; }));
  render(<ReplyComposer onSubmit={onSubmit} onReplyDraft={onReplyDraft} onReplyEdited={onReplyEdited} />);
  expect(onReplyDraft).toHaveBeenLastCalledWith(false);
  const body = screen.getByLabelText('Message body');
  fireEvent.change(body, { target: { value: 'Hi' } });
  expect(onReplyDraft).toHaveBeenLastCalledWith(true);
  fireEvent.change(body, { target: { value: 'Hi Mai' } });
  expect(onReplyEdited).toHaveBeenCalledTimes(2);
  expect(onReplyDraft).toHaveBeenCalledTimes(2);
  // The same text as a private note is not a reply to the customer.
  fireEvent.click(screen.getByRole('button', { name: 'Private note' }));
  expect(onReplyDraft).toHaveBeenLastCalledWith(false);
  fireEvent.change(body, { target: { value: 'Hi Mai, note' } });
  expect(onReplyEdited).toHaveBeenCalledTimes(2);
  fireEvent.click(screen.getByRole('button', { name: 'Reply' }));
  expect(onReplyDraft).toHaveBeenLastCalledWith(true);
  // Sent: the box is empty again.
  fireEvent.keyDown(body, { key: 'Enter', ctrlKey: true });
  await waitFor(() => expect(onSubmit).toHaveBeenCalled());
  accept();
  await waitFor(() => expect(onReplyDraft).toHaveBeenLastCalledWith(false));
});

const lanReply = {
  id: '99999999-9999-4999-8999-999999999999', conversation_id: '11111111-1111-4111-8111-111111111111', author_agent_id: '44444444-4444-4444-8444-444444444444',
  author_name: 'Lan', agent_token: null, ai_draft_id: null, ai_draft_outcome: null, agent_request_id: null, kind: 'outgoing' as const, body: 'Your refund left today.', body_html: null, origin: null,
  attachments: [], cc: [], created_at: '2026-10-22T09:00:00Z', delivery: null, email: null, reply_number: 3,
};

test('a reply says which replies its writer had on screen when they started, not when they press send', async () => {
  const onSubmit = vi.fn(async () => undefined);
  const { rerender } = render(<ReplyComposer onSubmit={onSubmit} latestReplyNumber={2} />);
  fireEvent.change(screen.getByLabelText('Message body'), { target: { value: 'Refund on its way' } });
  // Lan's reply arrives on screen while this one is being written: the mark stays at 2.
  rerender(<ReplyComposer onSubmit={onSubmit} latestReplyNumber={3} />);
  fireEvent.keyDown(screen.getByLabelText('Message body'), { key: 'Enter', ctrlKey: true });
  await waitFor(() => expect(onSubmit).toHaveBeenCalledWith('outgoing', 'Refund on its way', [], [], 2));
  // A note is never checked.
  fireEvent.click(screen.getByRole('button', { name: 'Private note' }));
  fireEvent.change(screen.getByLabelText('Message body'), { target: { value: 'FYI' } });
  fireEvent.keyDown(screen.getByLabelText('Message body'), { key: 'Enter', ctrlKey: true });
  await waitFor(() => expect(onSubmit).toHaveBeenLastCalledWith('note', 'FYI'));
});

test('someone replied meanwhile: the text stays, their reply shows, and Send anyway or Edit reply move the mark so it is asked once', async () => {
  const onSubmit = vi.fn<(...args: unknown[]) => Promise<unknown>>()
    .mockRejectedValueOnce(new ReplyConflictError('Someone replied', 3, [lanReply]))
    .mockResolvedValue(undefined);
  render(<ReplyComposer onSubmit={onSubmit} latestReplyNumber={2} timezone="UTC" />);
  const body = screen.getByLabelText('Message body');
  fireEvent.change(body, { target: { value: 'Refund on its way' } });
  fireEvent.keyDown(screen.getByLabelText('Message body'), { key: 'Enter', ctrlKey: true });
  expect(await screen.findByRole('alert')).toHaveTextContent('Someone replied while you were writing (1 new reply). Nothing was sent');
  const warning = screen.getByRole('region', { name: 'New replies' });
  expect(warning).toHaveTextContent('Lan');
  expect(warning).toHaveTextContent('Your refund left today.');
  expect(within(warning).getByRole('list', { name: 'New replies' })).toHaveAttribute('tabindex', '0');
  // The safe choice takes the focus, so the warning is where the person is.
  await waitFor(() => expect(within(warning).getByRole('button', { name: 'Edit reply' })).toHaveFocus());
  expect(body).toHaveValue('Refund on its way');
  fireEvent.click(within(warning).getByRole('button', { name: 'Edit reply' }));
  expect(screen.queryByRole('region', { name: 'New replies' })).toBeNull();
  expect(body).toHaveFocus();
  fireEvent.keyDown(screen.getByLabelText('Message body'), { key: 'Enter', ctrlKey: true });
  await waitFor(() => expect(onSubmit).toHaveBeenLastCalledWith('outgoing', 'Refund on its way', [], [], 3));
  await waitFor(() => expect(body).toHaveValue(''));
});

test('Send anyway sends the same text with the newest reply number', async () => {
  const onSubmit = vi.fn<(...args: unknown[]) => Promise<unknown>>()
    .mockRejectedValueOnce(new ReplyConflictError('Someone replied', 3, [{ ...lanReply, author_name: null, origin: { system: 'freshdesk', external_id: '7', kind: 'reply', from: null, to: [], cc: [], bcc: [] } }]))
    .mockResolvedValue(undefined);
  render(<ReplyComposer onSubmit={onSubmit} latestReplyNumber={2} timezone="UTC" />);
  fireEvent.change(screen.getByLabelText('Message body'), { target: { value: 'Refund on its way' } });
  fireEvent.keyDown(screen.getByLabelText('Message body'), { key: 'Enter', ctrlKey: true });
  await screen.findByRole('alert');
  const warning = screen.getByRole('region', { name: 'New replies' });
  // An imported reply keeps its own time and says where it came from, and the title does not call it someone replying.
  expect(screen.getByRole('alert')).toHaveTextContent('1 reply imported from Freshdesk arrived after you started writing');
  expect(warning).toHaveTextContent('Imported from Freshdesk');
  expect(warning).toHaveTextContent('Oct 22, 9:00 AM');
  fireEvent.click(within(warning).getByRole('button', { name: 'Send anyway' }));
  await waitFor(() => expect(onSubmit).toHaveBeenLastCalledWith('outgoing', 'Refund on its way', [], [], 3));
  await waitFor(() => expect(screen.getByLabelText('Message body')).toHaveValue(''));
});

// Send and resolve: the reply goes first; the status changes only once the server accepted the reply.
const deferred = () => {
  let settle!: { resolve: (value?: unknown) => void; reject: (error: unknown) => void };
  const promise = new Promise((resolve, reject) => { settle = { resolve, reject }; });
  return { promise, ...settle };
};

test('Send and resolve sends the reply, then resolves only after the send succeeded', async () => {
  const send = deferred();
  const onSubmit = vi.fn(() => send.promise);
  const onResolve = vi.fn().mockResolvedValue(undefined);
  render(<ReplyComposer onSubmit={onSubmit} onResolve={onResolve} />);
  const body = screen.getByLabelText('Message body');
  fireEvent.change(body, { target: { value: 'All sorted.' } });
  // One primary action stays Send reply; Send and resolve is the secondary one next to it.
  expect(screen.getByRole('button', { name: 'Send reply' })).toHaveClass('primary-action');
  const both = screen.getByRole('button', { name: 'Send and resolve' });
  expect(both).not.toHaveClass('primary-action');
  expect(both).toHaveAttribute('aria-keyshortcuts', 'Control+Shift+Enter Meta+Shift+Enter');
  fireEvent.click(both);
  expect(onSubmit).toHaveBeenCalledWith('outgoing', 'All sorted.');
  expect(onResolve).not.toHaveBeenCalled();
  send.resolve();
  await waitFor(() => expect(onResolve).toHaveBeenCalledTimes(1));
  await waitFor(() => expect(body).toHaveValue(''));
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
});

test('Ctrl or Cmd + Shift + Enter sends and resolves; Ctrl + Enter alone only sends', async () => {
  const onSubmit = vi.fn().mockResolvedValue(undefined);
  const onResolve = vi.fn().mockResolvedValue(undefined);
  render(<ReplyComposer onSubmit={onSubmit} onResolve={onResolve} />);
  const body = screen.getByLabelText('Message body');
  fireEvent.change(body, { target: { value: 'First' } });
  fireEvent.keyDown(body, { key: 'Enter', ctrlKey: true });
  await waitFor(() => expect(body).toHaveValue(''));
  expect(onResolve).not.toHaveBeenCalled();
  fireEvent.change(body, { target: { value: 'Second' } });
  fireEvent.keyDown(body, { key: 'Enter', metaKey: true, shiftKey: true });
  await waitFor(() => expect(onResolve).toHaveBeenCalledTimes(1));
  expect(onSubmit).toHaveBeenLastCalledWith('outgoing', 'Second');
});

test('a reply the server refuses leaves the status alone and keeps the text with the reason', async () => {
  const onSubmit = vi.fn().mockRejectedValue(new Error('API 500'));
  const onResolve = vi.fn().mockResolvedValue(undefined);
  render(<ReplyComposer onSubmit={onSubmit} onResolve={onResolve} />);
  const body = screen.getByLabelText('Message body');
  fireEvent.change(body, { target: { value: 'Keep me' } });
  fireEvent.click(screen.getByRole('button', { name: 'Send and resolve' }));
  expect(await screen.findByRole('alert')).toHaveTextContent(/could not be sent/i);
  expect(body).toHaveValue('Keep me');
  expect(onResolve).not.toHaveBeenCalled();
});

test('a reply sent but not resolved says so, and the reply stays sent', async () => {
  const onSubmit = vi.fn().mockResolvedValue(undefined);
  const onResolve = vi.fn().mockRejectedValue(new Error('API 500'));
  render(<ReplyComposer onSubmit={onSubmit} onResolve={onResolve} />);
  const body = screen.getByLabelText('Message body');
  fireEvent.change(body, { target: { value: 'Done here' } });
  fireEvent.keyDown(body, { key: 'Enter', ctrlKey: true, shiftKey: true });
  expect(await screen.findByRole('alert')).toHaveTextContent('Your reply was sent, but the conversation could not be resolved. Press Resolve to try again.');
  expect(body).toHaveValue('');
  expect(onSubmit).toHaveBeenCalledTimes(1);
});

test('a private note offers no Send and resolve, and Ctrl+Shift+Enter only adds the note', async () => {
  const onSubmit = vi.fn().mockResolvedValue(undefined);
  const onResolve = vi.fn().mockResolvedValue(undefined);
  render(<ReplyComposer onSubmit={onSubmit} onResolve={onResolve} />);
  fireEvent.click(screen.getByRole('button', { name: 'Private note' }));
  expect(screen.queryByRole('button', { name: 'Send and resolve' })).not.toBeInTheDocument();
  const body = screen.getByLabelText('Message body');
  fireEvent.change(body, { target: { value: 'Called them' } });
  fireEvent.keyDown(body, { key: 'Enter', ctrlKey: true, shiftKey: true });
  await waitFor(() => expect(onSubmit).toHaveBeenCalledWith('note', 'Called them'));
  await waitFor(() => expect(body).toHaveValue(''));
  expect(onResolve).not.toHaveBeenCalled();
});

test('without a way to resolve there is no Send and resolve', () => {
  render(<ReplyComposer onSubmit={vi.fn()} />);
  expect(screen.queryByRole('button', { name: 'Send and resolve' })).not.toBeInTheDocument();
});

test('Send anyway after someone replied still resolves when the reply was sent with Send and resolve', async () => {
  const onSubmit = vi.fn<(...args: unknown[]) => Promise<unknown>>()
    .mockRejectedValueOnce(new ReplyConflictError('Someone replied', 3, [lanReply]))
    .mockResolvedValue(undefined);
  const onResolve = vi.fn().mockResolvedValue(undefined);
  render(<ReplyComposer onSubmit={onSubmit} onResolve={onResolve} latestReplyNumber={2} timezone="UTC" />);
  fireEvent.change(screen.getByLabelText('Message body'), { target: { value: 'Refund on its way' } });
  fireEvent.click(screen.getByRole('button', { name: 'Send and resolve' }));
  const warning = await screen.findByRole('region', { name: 'New replies' });
  expect(onResolve).not.toHaveBeenCalled();
  fireEvent.click(within(warning).getByRole('button', { name: 'Send anyway' }));
  await waitFor(() => expect(onSubmit).toHaveBeenLastCalledWith('outgoing', 'Refund on its way', [], [], 3));
  await waitFor(() => expect(onResolve).toHaveBeenCalledTimes(1));
});

test('Ctrl+Shift+Enter from the Cc box sends and resolves', async () => {
  const onSubmit = vi.fn().mockResolvedValue(undefined);
  const onResolve = vi.fn().mockResolvedValue(undefined);
  render(<ReplyComposer onSubmit={onSubmit} onResolve={onResolve} />);
  fireEvent.change(screen.getByLabelText('Message body'), { target: { value: 'Copied' } });
  fireEvent.click(screen.getByRole('button', { name: 'Cc' }));
  fireEvent.keyDown(screen.getByLabelText('Add Cc address'), { key: 'Enter', ctrlKey: true, shiftKey: true });
  await waitFor(() => expect(onResolve).toHaveBeenCalledTimes(1));
});

test('after Send and resolve the focus comes back to the box, since the button goes with the open status', async () => {
  const onResolve = vi.fn().mockResolvedValue(undefined);
  render(<ReplyComposer onSubmit={vi.fn().mockResolvedValue(undefined)} onResolve={onResolve} />);
  const body = screen.getByLabelText('Message body');
  fireEvent.change(body, { target: { value: 'Closing this' } });
  body.blur();
  fireEvent.click(screen.getByRole('button', { name: 'Send and resolve' }));
  await waitFor(() => expect(onResolve).toHaveBeenCalled());
  await waitFor(() => expect(body).toHaveFocus());
});

test('with no channel to carry replies the box opens on a private note, and a reply says it cannot be sent', () => {
  const onSubmit = vi.fn();
  render(<ReplyComposer onSubmit={onSubmit} noReplies />);
  const box = screen.getByLabelText('Message body');
  expect(box).toHaveAttribute('placeholder', 'Write a private note. Only your team sees it.');
  fireEvent.click(screen.getByRole('button', { name: /^Reply/ }));
  expect(box).toHaveAttribute('placeholder', 'Replies to this conversation cannot be sent. Write a private note instead (N).');
  fireEvent.change(box, { target: { value: 'Hello' } });
  expect(screen.getByRole('button', { name: 'Send reply' })).toBeDisabled();
  fireEvent.keyDown(box, { key: 'Enter', ctrlKey: true });
  expect(onSubmit).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: /Private note/ }));
  fireEvent.click(screen.getByRole('button', { name: 'Add private note' }));
  expect(onSubmit.mock.calls[0]?.slice(0, 2)).toEqual(['note', 'Hello']);
});

test('an unsent reply survives the composer going away (a reload, Back) and comes back for the same conversation only', () => {
  window.localStorage.clear();
  const first = render(<ReplyComposer draftKey="ws-1:conv-1" onSubmit={vi.fn()} />);
  fireEvent.change(screen.getByLabelText('Message body'), { target: { value: 'Half a reply' } });
  first.unmount();
  const again = render(<ReplyComposer draftKey="ws-1:conv-1" onSubmit={vi.fn()} />);
  expect(screen.getByLabelText('Message body')).toHaveValue('Half a reply');
  again.unmount();
  render(<ReplyComposer draftKey="ws-1:conv-2" onSubmit={vi.fn()} />);
  expect(screen.getByLabelText('Message body')).toHaveValue('');
});

test('a draft is cleared once its reply is sent, and kept when the send is refused', async () => {
  window.localStorage.clear();
  const refused = vi.fn().mockRejectedValue(new Error('network'));
  const first = render(<ReplyComposer draftKey="ws-1:conv-3" onSubmit={refused} />);
  const body = screen.getByLabelText('Message body');
  fireEvent.change(body, { target: { value: 'Keep me' } });
  fireEvent.keyDown(body, { key: 'Enter', ctrlKey: true });
  expect(await screen.findByRole('alert')).toBeInTheDocument();
  first.unmount();
  const sent = vi.fn().mockResolvedValue(undefined);
  render(<ReplyComposer draftKey="ws-1:conv-3" onSubmit={sent} />);
  const restored = screen.getByLabelText('Message body');
  expect(restored).toHaveValue('Keep me');
  fireEvent.keyDown(restored, { key: 'Enter', ctrlKey: true });
  await waitFor(() => expect(restored).toHaveValue(''));
  expect(Object.keys(window.localStorage).filter((key) => key.includes('conv-3'))).toEqual([]);
});

test('a draft older than seven days is dropped when it is read, and a younger one comes back', () => {
  window.localStorage.clear();
  vi.useFakeTimers({ toFake: ['Date'] });
  try {
    vi.setSystemTime(new Date('2026-10-01T09:00:00Z'));
    for (const id of ['conv-old', 'conv-young']) {
      const view = render(<ReplyComposer draftKey={`ws-1:agent-1:${id}`} onSubmit={vi.fn()} />);
      fireEvent.change(screen.getByLabelText('Message body'), { target: { value: `Draft for ${id}` } });
      view.unmount();
    }
    vi.setSystemTime(new Date('2026-10-07T21:00:00Z'));
    const young = render(<ReplyComposer draftKey="ws-1:agent-1:conv-young" onSubmit={vi.fn()} />);
    expect(screen.getByLabelText('Message body')).toHaveValue('Draft for conv-young');
    young.unmount();
    vi.setSystemTime(new Date('2026-10-08T09:00:01Z'));
    render(<ReplyComposer draftKey="ws-1:agent-1:conv-old" onSubmit={vi.fn()} />);
    expect(screen.getByLabelText('Message body')).toHaveValue('');
    expect(Object.keys(window.localStorage).filter((key) => key.includes('conv-old'))).toEqual([]);
  } finally {
    vi.useRealTimers();
  }
});

test('three fast clicks on Add private note send one note', () => {
  const submit = vi.fn(() => new Promise(() => undefined));
  render(<ReplyComposer onSubmit={submit} />);
  fireEvent.click(screen.getByRole('button', { name: 'Private note' }));
  fireEvent.change(screen.getByLabelText('Message body'), { target: { value: 'Only once' } });
  const button = screen.getByRole('button', { name: 'Add private note' });
  act(() => { button.click(); button.click(); button.click(); });
  expect(submit).toHaveBeenCalledTimes(1);
  expect(button).toBeDisabled();
});

test('a reload while the send is in flight does not bring the sent text back, and a refused send keeps it', async () => {
  window.localStorage.clear();
  const stored = () => Object.keys(window.localStorage).filter((key) => key.includes('conv-5'));
  let refuse: (error: Error) => void = () => undefined;
  const first = render(<ReplyComposer draftKey="ws-1:agent-1:conv-5" onSubmit={() => new Promise((_, reject) => { refuse = reject; })} />);
  const body = screen.getByLabelText('Message body');
  fireEvent.change(body, { target: { value: 'Sent before the reload' } });
  fireEvent.keyDown(body, { key: 'Enter', ctrlKey: true });
  expect(stored()).toEqual([]);
  act(() => refuse(new Error('network')));
  expect(await screen.findByRole('alert')).toBeInTheDocument();
  expect(stored()).toHaveLength(1);
  first.unmount();
  render(<ReplyComposer draftKey="ws-1:agent-1:conv-5" onSubmit={vi.fn()} />);
  expect(screen.getByLabelText('Message body')).toHaveValue('Sent before the reload');
});

test('a draft changed in another tab shows up in the open box with a notice', () => {
  window.localStorage.clear();
  render(<ReplyComposer draftKey="ws-1:agent-1:conv-6" onSubmit={vi.fn()} />);
  fireEvent.change(screen.getByLabelText('Message body'), { target: { value: 'Tab A text' } });
  const key = 'colliedesk.reply-draft:ws-1:agent-1:conv-6';
  const newer = JSON.stringify({ kind: 'outgoing', body: 'Tab B text', saved_at: Date.now() });
  // The other tab has written the shared storage by the time this tab hears of it.
  window.localStorage.setItem(key, newer);
  act(() => { window.dispatchEvent(new StorageEvent('storage', { key, newValue: newer, storageArea: window.localStorage })); });
  expect(screen.getByLabelText('Message body')).toHaveValue('Tab B text');
  expect(screen.getByRole('status')).toHaveTextContent('Updated from another tab');
  act(() => { window.dispatchEvent(new StorageEvent('storage', { key: 'colliedesk.reply-draft:ws-1:agent-1:other', newValue: newer, storageArea: window.localStorage })); });
  expect(screen.getByLabelText('Message body')).toHaveValue('Tab B text');
});
