import { fireEvent, render, screen, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import type { Message } from '../../api/schemas';
import { TimelinePanel } from './timeline-panel';

const base = { conversation_id: '11111111-1111-4111-8111-111111111111', author_agent_id: null, author_name: null, body_html: null, origin: null,
  attachments: [], created_at: '2026-10-14T00:00:00Z', delivery: null, email: null };
const incoming: Message = {
  ...base, id: '22222222-2222-4222-8222-222222222222', kind: 'incoming', body: 'Please help.',
  email: { from: 'ceo@customer.example', authentication: 'fail', attachments: [{ filename: 'invoice.pdf', content_type: 'application/pdf' }], original_attachment_id: null },
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

test('renders customer email as plain text without remote images or disguised links', () => {
  const hostile: Message = {
    ...base, id: '66666666-6666-4666-8666-666666666666', kind: 'incoming',
    email: { from: 'x@customer.example', authentication: 'none', attachments: [], original_attachment_id: null },
    body: '# Not a heading\n![](https://tracker.example/pixel.png?id=agent)\n[https://bank.example](https://evil.example/login)\nSee https://docs.example.com/help.',
  };
  const { container } = render(<TimelinePanel items={[hostile]} timezone="UTC" />);
  expect(container.querySelector('img')).toBeNull();
  expect(container.querySelector('h1')).toBeNull();
  expect(screen.getByText(/# Not a heading/)).toBeInTheDocument();
  const links = screen.getAllByRole('link');
  for (const link of links) {
    expect(link.textContent).toBe(link.getAttribute('href'));
    expect(link).toHaveAttribute('rel', 'noopener noreferrer nofollow');
  }
  expect(links.map((link) => link.getAttribute('href'))).toContain('https://docs.example.com/help');
});

test('lists stored email files and offers the original message separately', () => {
  const original = { id: '77777777-7777-4777-8777-777777777777', message_id: '88888888-8888-4888-8888-888888888888', filename: 'original-message.eml.gz',
    content_type: 'application/gzip', size: 2048, sha256: 'a'.repeat(64), created_at: '2026-10-14T00:00:00Z' };
  const invoice = { ...original, id: '99999999-9999-4999-8999-999999999999', filename: 'invoice.pdf', content_type: 'application/pdf', size: 1024, sha256: 'b'.repeat(64) };
  const stored: Message = {
    ...base, id: original.message_id, kind: 'incoming', body: 'Invoice attached.', attachments: [original, invoice],
    email: { from: 'v@customer.example', authentication: 'pass', attachments: [{ filename: 'invoice.pdf', content_type: 'application/pdf' }], original_attachment_id: original.id },
  };
  render(<TimelinePanel items={[stored]} timezone="UTC" />);
  const message = screen.getByRole('article');
  expect(within(message).getByRole('link', { name: /download original email/i })).toHaveAttribute('href', `/api/v1/attachments/${original.id}`);
  const files = within(message).getByRole('list', { name: /attachments/i });
  expect(within(files).getByRole('link', { name: 'invoice.pdf' })).toHaveAttribute('href', `/api/v1/attachments/${invoice.id}`);
  expect(within(files).queryByText('original-message.eml.gz')).not.toBeInTheDocument();
  expect(within(message).queryByText(/not imported/i)).not.toBeInTheDocument();
});
