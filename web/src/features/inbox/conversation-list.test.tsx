import { act, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import type { Conversation } from '../../api/schemas';
import { ConversationList } from './conversation-list';

const row = (updated_at: string): Conversation => ({
  id: '11111111-1111-4111-8111-111111111111', contact_id: '22222222-2222-4222-8222-222222222222', contact_name: 'Mai Linh', subject: 'Example request',
  status: 'open', priority: 'normal', assignee_id: null, tags: [], channel: 'email', origin: null, created_at: updated_at, updated_at,
});

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

test('a row exposes its full timestamp to a screen reader and to keyboard focus, not only to a mouse hover', () => {
  vi.setSystemTime(new Date('2026-10-15T12:00:00Z'));
  render(<ConversationList items={[row('2026-10-15T09:05:00Z')]} timezone="UTC" page={1} hasNextPage={false} />);
  const button = screen.getByRole('button', { name: /example request/i });
  expect(button).toHaveAccessibleName(/Oct 15, 9:05 AM/);
  expect(button).toHaveAttribute('data-tip', 'Oct 15, 9:05 AM');
  expect(button).not.toHaveAttribute('title');
});

test('the Today group becomes Earlier when midnight passes while the page stays open', () => {
  vi.setSystemTime(new Date('2026-10-15T23:58:30Z'));
  render(<ConversationList items={[row('2026-10-15T22:00:00Z')]} timezone="UTC" page={1} hasNextPage={false} />);
  expect(screen.getByRole('heading', { name: 'Today' })).toBeInTheDocument();
  act(() => { vi.advanceTimersByTime(120_000); });
  expect(screen.getByRole('heading', { name: 'Earlier' })).toBeInTheDocument();
});

test('the group follows the agent timezone: 23:30 UTC is already tomorrow in Ho Chi Minh', () => {
  vi.setSystemTime(new Date('2026-10-15T16:59:30Z'));
  render(<ConversationList items={[row('2026-10-15T10:00:00Z')]} timezone="Asia/Ho_Chi_Minh" page={1} hasNextPage={false} />);
  expect(screen.getByRole('heading', { name: 'Today' })).toBeInTheDocument();
  act(() => { vi.advanceTimersByTime(90_000); });
  expect(screen.getByRole('heading', { name: 'Earlier' })).toBeInTheDocument();
});

test('a screen reader hears which conversation is open, and only that one', () => {
  vi.setSystemTime(new Date('2026-10-15T12:00:00Z'));
  const other = { ...row('2026-10-15T08:00:00Z'), id: '33333333-3333-4333-8333-333333333333', subject: 'Other request' };
  render(<ConversationList items={[row('2026-10-15T09:05:00Z'), other]} selectedId={other.id} timezone="UTC" page={1} hasNextPage={false} />);
  expect(screen.getByRole('button', { name: /other request/i })).toHaveAttribute('aria-current', 'true');
  expect(screen.getByRole('button', { name: /example request/i })).not.toHaveAttribute('aria-current');
});

test('the first load shows a loading status, never "No conversations" or the get-started guide', () => {
  render(<ConversationList items={[]} timezone="UTC" page={1} hasNextPage={false} loading />);
  expect(screen.getByRole('status')).toHaveTextContent('Loading workspace…');
  expect(screen.queryByText(/no conversations/i)).not.toBeInTheDocument();
  expect(screen.queryByRole('region', { name: /get started/i })).not.toBeInTheDocument();
});

test('a refetch keeps the previous rows, marks the list busy and shows no loading status', () => {
  vi.setSystemTime(new Date('2026-10-15T12:00:00Z'));
  render(<ConversationList items={[row('2026-10-15T09:05:00Z')]} timezone="UTC" page={1} hasNextPage={false} refreshing />);
  expect(screen.getByRole('button', { name: /example request/i })).toBeInTheDocument();
  expect(screen.queryByRole('status')).not.toBeInTheDocument();
  expect(document.querySelector('.conversation-scroll')).toHaveAttribute('aria-busy', 'true');
});

test('a settled list is not marked busy', () => {
  render(<ConversationList items={[]} timezone="UTC" page={1} hasNextPage={false} filtered />);
  expect(document.querySelector('.conversation-scroll')).toHaveAttribute('aria-busy', 'false');
});
