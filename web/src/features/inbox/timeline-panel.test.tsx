import { fireEvent, render, screen, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import type { Message } from '../../api/schemas';
import { TimelinePanel } from './timeline-panel';

const base = { conversation_id: '11111111-1111-4111-8111-111111111111', author_agent_id: null, created_at: '2026-10-14T00:00:00Z', delivery: null, email: null };
const incoming: Message = {
  ...base, id: '22222222-2222-4222-8222-222222222222', kind: 'incoming', body: 'Please help.',
  email: { from: 'ceo@customer.example', authentication: 'fail', attachments: [{ filename: 'invoice.pdf', content_type: 'application/pdf' }] },
};
const failed: Message = {
  ...base, id: '33333333-3333-4333-8333-333333333333', kind: 'outgoing', body: 'Answer.',
  delivery: { status: 'failed', attempts: 3, last_error: '550 mailbox unavailable', sent_at: null },
};
const queued: Message = { ...base, id: '44444444-4444-4444-8444-444444444444', kind: 'outgoing', body: 'Later.', delivery: { status: 'queued', attempts: 0, last_error: null, sent_at: null } };
const note: Message = { ...base, id: '55555555-5555-4555-8555-555555555555', kind: 'note', body: 'Internal.' };

test('shows the sender, a verification warning and attachments that were not imported', () => {
  render(<TimelinePanel items={[incoming]} timezone="UTC" />);
  const message = screen.getByRole('article');
  expect(within(message).getByText('ceo@customer.example')).toBeInTheDocument();
  expect(within(message).getByText(/failed sender verification/i)).toBeInTheDocument();
  expect(within(message).getByText(/1 attachment not imported: invoice\.pdf/i)).toBeInTheDocument();
});

test('shows delivery status and retries a failed reply', () => {
  const onRetryDelivery = vi.fn();
  render(<TimelinePanel items={[failed, queued, note]} timezone="UTC" onRetryDelivery={onRetryDelivery} />);
  const [failedItem, queuedItem, noteItem] = screen.getAllByRole('article');
  expect(within(failedItem).getByText(/delivery failed/i)).toHaveAttribute('title', '550 mailbox unavailable');
  fireEvent.click(within(failedItem).getByRole('button', { name: /retry delivery/i }));
  expect(onRetryDelivery).toHaveBeenCalledWith(failed.id);
  expect(within(queuedItem).getByText(/queued/i)).toBeInTheDocument();
  expect(within(queuedItem).queryByRole('button', { name: /retry delivery/i })).not.toBeInTheDocument();
  expect(within(noteItem).queryByText(/queued|sent|failed/i)).not.toBeInTheDocument();
});
