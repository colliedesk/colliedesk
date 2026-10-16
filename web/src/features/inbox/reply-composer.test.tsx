import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
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
