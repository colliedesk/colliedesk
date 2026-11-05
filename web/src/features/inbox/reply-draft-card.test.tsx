import { fireEvent, render, screen, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import type { AIDraft } from './ai-draft-api';
import { EarlierDrafts, ReplyDraftCard } from './reply-draft-card';

const conversation = '22222222-2222-4222-8222-222222222222';
const invoice = '66666666-6666-4666-8666-666666666666';
const photo = '77777777-7777-4777-8777-777777777777';

function written(change: Partial<AIDraft> = {}): AIDraft {
  return { id: '55555555-5555-4555-8555-555555555555', conversation_id: conversation, status: 'ready', source: 'written', cc: null,
    written_by: { token_label: 'Claude', agent_name: 'Ada' }, files: [], error_code: null, prompt: '', text: 'Here is your invoice.', missing_info: null,
    sources: [], warnings: [], model: null, outcome: null, similarity: null, sent_message_id: null, requested_by_agent_id: null, automatic: false,
    answers_message_id: null, reply_needed: null, created_at: '2026-11-05T09:00:00Z', ready_at: '2026-11-05T09:00:00Z', decided_at: null,
    decided_by_name: null, restored_at: null, ...change };
}

const files = [
  { id: invoice, filename: 'invoice.pdf', content_type: 'application/pdf', size: 2048, created_at: '2026-11-05T09:00:00Z' },
  { id: photo, filename: 'printer.png', content_type: 'image/png', size: 512, created_at: '2026-11-05T09:00:00Z' },
];

test('a draft shows its files, and the person drops one before sending', () => {
  const onSend = vi.fn(() => Promise.resolve());
  render(<ReplyDraftCard draft={written({ files })} customer="Ada" onSend={onSend} onDiscard={vi.fn()} />);
  const attached = screen.getByRole('group', { name: /files/i });
  expect(within(attached).getByRole('link', { name: 'invoice.pdf' })).toHaveAttribute('href', `/api/v1/conversations/${conversation}/ai-drafts/55555555-5555-4555-8555-555555555555/files/${invoice}`);
  fireEvent.click(within(attached).getByRole('checkbox', { name: 'Send printer.png' }));
  fireEvent.click(screen.getByRole('button', { name: 'Send' }));
  expect(onSend).toHaveBeenCalledWith({ files: [invoice] });
});

test('a draft sent as it is keeps every file', () => {
  const onSend = vi.fn(() => Promise.resolve());
  render(<ReplyDraftCard draft={written({ files })} customer="Ada" onSend={onSend} onDiscard={vi.fn()} />);
  fireEvent.click(screen.getByRole('button', { name: 'Send' }));
  expect(onSend).toHaveBeenCalledWith(undefined);
});

test('earlier drafts say how they ended and come back', () => {
  const onRestore = vi.fn(() => Promise.resolve());
  const replaced = written({ id: '88888888-8888-4888-8888-888888888888', text: 'First try.', outcome: 'superseded', decided_at: '2026-11-05T09:10:00Z', files });
  const discarded = written({ id: '99999999-9999-4999-8999-999999999999', text: 'Older try.', outcome: 'discarded', decided_by_name: 'Grace', decided_at: '2026-11-05T09:20:00Z' });
  render(<EarlierDrafts drafts={[replaced, discarded]} timezone="UTC" onRestore={onRestore} />);
  fireEvent.click(screen.getByText('Earlier drafts (2)'));
  // One item per draft: the ones with a Bring back button (a draft's files are items of their own list).
  const items = screen.getAllByRole('listitem').filter((item) => within(item).queryByRole('button', { name: 'Bring back' }));
  expect(items).toHaveLength(2);
  expect(items[0]).toHaveTextContent('First try.');
  expect(items[0]).toHaveTextContent('Replaced by a newer draft');
  expect(within(items[0]).getByRole('link', { name: 'invoice.pdf' })).toBeInTheDocument();
  expect(items[1]).toHaveTextContent('Discarded by Grace');
  fireEvent.click(within(items[1]).getByRole('button', { name: 'Bring back' }));
  expect(onRestore).toHaveBeenCalledWith(discarded);
});

test('someone who may not bring a draft back still reads the earlier ones', () => {
  render(<EarlierDrafts drafts={[written({ outcome: 'expired', text: 'Old words.' })]} timezone="UTC" />);
  fireEvent.click(screen.getByText('Earlier drafts (1)'));
  expect(screen.getByText('Old words.')).toBeInTheDocument();
  expect(screen.getByText('Expired after a week')).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Bring back' })).not.toBeInTheDocument();
});

test('older drafts load a page at a time', async () => {
  const newest = written({ id: '88888888-8888-4888-8888-888888888888', text: 'Newer try.', outcome: 'superseded' });
  const oldest = written({ id: '99999999-9999-4999-8999-999999999999', text: 'Oldest try.', outcome: 'expired' });
  const onLoadOlder = vi.fn(() => Promise.resolve({ drafts: [oldest], next: null }));
  render(<EarlierDrafts drafts={[newest]} nextBefore={newest.id} onLoadOlder={onLoadOlder} timezone="UTC" onRestore={vi.fn()} />);
  fireEvent.click(screen.getByText('Earlier drafts (1)'));
  fireEvent.click(screen.getByRole('button', { name: 'Show older drafts' }));
  expect(onLoadOlder).toHaveBeenCalledWith(newest.id);
  expect(await screen.findByText('Oldest try.')).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Show older drafts' })).not.toBeInTheDocument();
});
