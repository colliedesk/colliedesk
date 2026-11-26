import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render as renderPage, screen, waitFor, within } from '@testing-library/react';
import type { ReactElement } from 'react';
import { beforeAll, expect, test, vi } from 'vitest';
import type { Conversation } from '../../api/schemas';
import { InboxPage } from './inbox-page';
import { presenceViewPart } from './presence-part';

const row = (id: string, subject: string, status: Conversation['status'] = 'open'): Conversation => ({ latest_reply_number: 0, help_center: null,
  id, contact_id: '22222222-2222-4222-8222-222222222222', contact_name: 'Ada Lovelace', subject, status, priority: 'normal',
  assignee_id: null, tags: [], channel: 'email', origin: null, created_at: '2026-11-06T02:05:00Z', updated_at: '2026-11-06T02:05:00Z',
});
const refund = row('11111111-1111-4111-8111-111111111111', 'Refund for order 1042');
const invoice = row('33333333-3333-4333-8333-333333333333', 'Invoice address');
const zaloRef = { id: '12121212-1212-4212-8212-121212121212', type: 'zalo', name: 'Hảo Đạt OA', enabled: true, deleted: false, replies_via: null };
// The header's Request article button reads the query cache.
const render = (page: ReactElement) => renderPage(page, { wrapper: ({ children }) => <QueryClientProvider client={new QueryClient()}>{children}</QueryClientProvider> });
const header = () => within(screen.getByRole('region', { name: 'Timeline' }));

beforeAll(async () => {
  presenceViewPart.preload();
  await import('./presence-view');
});

test('Resolve in the header confirms with a toast, and Undo puts it back quietly where it was', async () => {
  const onSetStatus = vi.fn().mockResolvedValue(undefined);
  render(<InboxPage conversations={[refund]} selected={refund} onSetStatus={onSetStatus} />);
  fireEvent.click(header().getByRole('button', { name: 'Resolve' }));
  expect(onSetStatus).toHaveBeenCalledWith(refund.id, 'resolved', undefined);
  const toast = await screen.findByRole('status', { name: 'Resolved' });
  expect(toast).toHaveTextContent('Conversation resolved.');
  fireEvent.click(within(toast).getByRole('button', { name: 'Undo' }));
  expect(onSetStatus).toHaveBeenLastCalledWith(refund.id, 'open', { notify: false });
  await waitFor(() => expect(screen.queryByRole('status', { name: 'Resolved' })).not.toBeInTheDocument());
});

test('Undo of a pending conversation brings it back to Pending, not Open', async () => {
  const waiting = { ...refund, status: 'pending' as const };
  const onSetStatus = vi.fn().mockResolvedValue(undefined);
  render(<InboxPage conversations={[waiting]} selected={waiting} onSetStatus={onSetStatus} />);
  fireEvent.click(header().getByRole('button', { name: 'Resolve' }));
  fireEvent.click(within(await screen.findByRole('status', { name: 'Resolved' })).getByRole('button', { name: 'Undo' }));
  expect(onSetStatus).toHaveBeenLastCalledWith(refund.id, 'pending', { notify: false });
});

test('the toast names the chat that told the customer, since Undo cannot take that message back', async () => {
  const chat = { ...refund, channel_ref: zaloRef, status_notices: ['resolved' as const] };
  render(<InboxPage conversations={[chat]} selected={chat} onSetStatus={vi.fn().mockResolvedValue(undefined)} />);
  fireEvent.click(header().getByRole('button', { name: 'Resolve and tell the customer on Hảo Đạt OA' }));
  expect(await screen.findByRole('status', { name: 'Resolved' })).toHaveTextContent('Conversation resolved. Hảo Đạt OA told the customer.');
});

test('Send and resolve confirms with the same toast once the status changed', async () => {
  const onSetStatus = vi.fn().mockResolvedValue(undefined);
  const onMessage = vi.fn().mockResolvedValue(undefined);
  render(<InboxPage conversations={[refund]} selected={refund} onSetStatus={onSetStatus} onMessage={onMessage} />);
  fireEvent.change(screen.getByLabelText('Message body'), { target: { value: 'Refunded today' } });
  fireEvent.click(screen.getByRole('button', { name: 'Send and resolve' }));
  expect(await screen.findByRole('status', { name: 'Resolved' })).toHaveTextContent('Conversation resolved.');
  expect(onSetStatus).toHaveBeenCalledWith(refund.id, 'resolved', undefined);
});

test('a refused resolve shows no toast, and opening another conversation clears the toast', async () => {
  const onSetStatus = vi.fn().mockRejectedValueOnce(new Error('refused')).mockResolvedValue(undefined);
  const { rerender } = render(<InboxPage conversations={[refund, invoice]} selected={refund} onSetStatus={onSetStatus} />);
  fireEvent.click(header().getByRole('button', { name: 'Resolve' }));
  expect(await screen.findByText('The status could not be changed. Try again.')).toBeInTheDocument();
  expect(screen.queryByRole('status', { name: 'Resolved' })).not.toBeInTheDocument();

  fireEvent.click(header().getByRole('button', { name: 'Resolve' }));
  expect(await screen.findByRole('status', { name: 'Resolved' })).toBeInTheDocument();
  rerender(<InboxPage conversations={[refund, invoice]} selected={invoice} onSetStatus={onSetStatus} />);
  expect(screen.queryByRole('status', { name: 'Resolved' })).not.toBeInTheDocument();
});

test('the toast leaves by itself after 8 seconds, so it never covers the reply box for long, but stays while the pointer is on it', async () => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  try {
    render(<InboxPage conversations={[refund]} selected={refund} onSetStatus={vi.fn().mockResolvedValue(undefined)} />);
    fireEvent.click(header().getByRole('button', { name: 'Resolve' }));
    const toast = await screen.findByRole('status', { name: 'Resolved' });
    fireEvent.mouseEnter(toast);
    await act(() => vi.advanceTimersByTimeAsync(9000));
    expect(screen.getByRole('status', { name: 'Resolved' })).toBeInTheDocument();
    fireEvent.mouseLeave(toast);
    await act(() => vi.advanceTimersByTimeAsync(7000));
    expect(screen.getByRole('status', { name: 'Resolved' })).toBeInTheDocument();
    await act(() => vi.advanceTimersByTimeAsync(1500));
    expect(screen.queryByRole('status', { name: 'Resolved' })).not.toBeInTheDocument();
  } finally {
    vi.useRealTimers();
  }
});
