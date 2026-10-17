import { fireEvent, render, screen } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { agentRequest } from '../requests/fixtures';
import { ReplyDraftCard } from './reply-draft-card';

const draft = agentRequest({ kind: 'reply_draft', method: 'POST', summary: 'Reply to the customer: Hello Ada',
  body: { kind: 'outgoing', body: 'Hello Ada, try restarting the printer.' }, requested_by: { ...agentRequest().requested_by, token_label: 'Claude drafts', token_scope: 'draft' } });

test('shows the draft above the composer with who wrote it, and Send sends it as written', () => {
  const onSend = vi.fn().mockResolvedValue(undefined);
  render(<ReplyDraftCard draft={draft} onSend={onSend} onDiscard={vi.fn()} />);
  expect(screen.getByRole('region', { name: 'Reply draft by Ada Admin via Claude drafts' })).toBeInTheDocument();
  expect(screen.getByText('Hello Ada, try restarting the printer.')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Send' }));
  expect(onSend).toHaveBeenCalledWith(undefined);
});

test('Edit lets the person change the text, and Send sends the edited text', () => {
  const onSend = vi.fn().mockResolvedValue(undefined);
  render(<ReplyDraftCard draft={draft} onSend={onSend} onDiscard={vi.fn()} />);
  fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
  const text = screen.getByLabelText('Draft text');
  expect(text).toHaveFocus();
  fireEvent.change(text, { target: { value: 'Hello Ada, a technician will call you.' } });
  fireEvent.click(screen.getByRole('button', { name: 'Send' }));
  expect(onSend).toHaveBeenCalledWith('Hello Ada, a technician will call you.');
});

test('Discard throws the draft away; a failed send keeps it with the reason', async () => {
  const onDiscard = vi.fn().mockResolvedValue(undefined);
  const onSend = vi.fn().mockRejectedValue(new Error('API 500'));
  render(<ReplyDraftCard draft={draft} onSend={onSend} onDiscard={onDiscard} />);
  fireEvent.click(screen.getByRole('button', { name: 'Send' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('could not be sent');
  expect(screen.getByText('Hello Ada, try restarting the printer.')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Discard' }));
  expect(onDiscard).toHaveBeenCalled();
});
