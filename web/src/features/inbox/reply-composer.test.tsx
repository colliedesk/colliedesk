import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { AttachmentsTooLargeError, AttachmentStorageError } from '../../api/attachments-error';
import { OutboundRefusedError } from '../../api/outbound-error';
import { ReplyComposer } from './reply-composer';

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

test('a reply over the daily cap says when to retry and keeps the text', async () => {
  const onSubmit = vi.fn().mockRejectedValue(new OutboundRefusedError('trial_outbound_limit', 'cap', 5400));
  render(<ReplyComposer onSubmit={onSubmit} />);
  const body = screen.getByLabelText('Message body');
  fireEvent.change(body, { target: { value: 'Hello again' } });
  fireEvent.keyDown(body, { key: 'Enter', ctrlKey: true });
  expect(await screen.findByRole('alert')).toHaveTextContent(/200 replies in 24 hours.*try again in about 2 hours/i);
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
  id, title, body, created_by_agent_id: null, created_at: '2026-10-15T00:00:00Z', updated_at: '2026-10-15T00:00:00Z',
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
