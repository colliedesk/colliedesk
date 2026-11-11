import { fireEvent, render, screen, within } from '@testing-library/react';
import { beforeAll, expect, test, vi } from 'vitest';
import type { Conversation } from '../../api/schemas';
import { InboxPage } from './inbox-page';
import { presenceViewPart } from './presence-part';

const row = (id: string, subject: string, status: Conversation['status'] = 'open'): Conversation => ({ latest_reply_number: 0,
  id, contact_id: '22222222-2222-4222-8222-222222222222', contact_name: 'RankBoost Team', subject, status, priority: 'normal',
  assignee_id: null, tags: [], channel: 'email', origin: null, created_at: '2026-11-06T02:05:00Z', updated_at: '2026-11-06T02:05:00Z',
});
const pills = row('11111111-1111-4111-8111-111111111111', 'Rank #1 on Google');
const wallet = row('33333333-3333-4333-8333-333333333333', 'Your wallet is locked');
const sender = { id: '22222222-2222-4222-8222-222222222222', name: 'RankBoost Team', email: 'offers@rankboost.example' };
const pick = (subject: RegExp) => fireEvent.click(screen.getByRole('checkbox', { name: subject }));
const bar = () => within(screen.getByRole('group', { name: /bulk actions/i }));

beforeAll(async () => {
  presenceViewPart.preload();
  await import('./presence-view');
});

test('Spam in the bulk bar files the selection in Spam, names what happens next, and Undo takes it back', async () => {
  const onSpam = vi.fn().mockResolvedValue({ updated: 2, senders: [sender] });
  render(<InboxPage conversations={[pills, wallet]} onSpam={onSpam} onBulkUpdate={vi.fn()} />);
  pick(/rank #1/i);
  pick(/wallet/i);
  fireEvent.click(bar().getByRole('button', { name: 'Spam' }));
  expect(onSpam).toHaveBeenCalledWith([pills.id, wallet.id], true);
  const toast = await screen.findByRole('status', { name: 'Spam' });
  expect(toast).toHaveTextContent('Moved 2 conversations to Spam. Later mail from offers@rankboost.example goes there too.');
  fireEvent.click(within(toast).getByRole('button', { name: 'Undo' }));
  expect(onSpam).toHaveBeenLastCalledWith([pills.id, wallet.id], false);
});

test('the open conversation has Spam beside Resolve, and a spam one offers Not spam instead', async () => {
  const onSpam = vi.fn().mockResolvedValue({ updated: 1, senders: [sender] });
  const { rerender } = render(<InboxPage conversations={[pills]} selected={pills} onSpam={onSpam} onSetStatus={vi.fn()} />);
  const header = within(screen.getByRole('region', { name: 'Timeline' }));
  fireEvent.click(header.getByRole('button', { name: 'Spam' }));
  expect(onSpam).toHaveBeenCalledWith([pills.id], true);
  expect(await screen.findByRole('status', { name: 'Spam' })).toHaveTextContent('Moved to Spam. Later mail from offers@rankboost.example goes there too.');

  const filed = { ...pills, status: 'spam' as const };
  rerender(<InboxPage conversations={[]} selected={filed} onSpam={onSpam} onSetStatus={vi.fn()} />);
  expect(header.queryByRole('button', { name: /^resolve/i })).not.toBeInTheDocument();
  fireEvent.click(header.getByRole('button', { name: 'Not spam' }));
  expect(onSpam).toHaveBeenLastCalledWith([pills.id], false);
});

test('the Spam view says what Spam does, and its bulk bar offers Not spam and no status change', () => {
  const onSpam = vi.fn().mockResolvedValue({ updated: 2, senders: [sender] });
  const spam = [{ ...pills, status: 'spam' as const }, { ...wallet, status: 'spam' as const }];
  render(<InboxPage conversations={spam} query={{ status: 'spam' }} onQuery={vi.fn()} onSpam={onSpam} onBulkUpdate={vi.fn()} />);
  expect(screen.getByRole('radio', { name: 'Spam' })).toBeChecked();
  expect(screen.getByText(/no notification, no clock, not in Results/)).toBeInTheDocument();
  pick(/rank #1/i);
  expect(bar().queryByRole('button', { name: 'Resolve' })).not.toBeInTheDocument();
  expect(bar().queryByLabelText('Set status', { selector: 'summary' })).not.toBeInTheDocument();
  fireEvent.click(bar().getByRole('button', { name: 'Not spam' }));
  expect(onSpam).toHaveBeenCalledWith([pills.id], false);
});
