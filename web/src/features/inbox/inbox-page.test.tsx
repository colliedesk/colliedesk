import { fireEvent, render, screen, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { InboxPage } from './inbox-page';

const conversation = {
  id: '11111111-1111-4111-8111-111111111111',
  contact_id: '22222222-2222-4222-8222-222222222222',
  subject: 'Example request',
  status: 'open' as const,
  priority: 'normal' as const,
  assignee_id: null,
  tags: [],
  created_at: '2026-10-14T00:00:00Z',
  updated_at: '2026-10-14T00:00:00Z',
};

test('shows the dense inbox filters and three-pane conversation workspace', () => {
  render(<InboxPage />);
  const filters = screen.getByRole('form', { name: /inbox/i });
  expect(screen.getByRole('searchbox', { name: /search conversations/i })).toBeInTheDocument();
  expect(within(filters).getByLabelText(/status/i)).toBeInTheDocument();
  expect(within(filters).getByLabelText(/assignee/i)).toBeInTheDocument();
  expect(within(filters).getByLabelText(/tag/i)).toBeInTheDocument();
  expect(screen.getByRole('region', { name: /conversation list/i })).toBeInTheDocument();
  expect(screen.getByRole('region', { name: /timeline/i })).toBeInTheDocument();
  expect(screen.getByRole('complementary', { name: /conversation details/i })).toBeInTheDocument();
  expect(screen.getByRole('textbox', { name: /message body/i })).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: /^private note$/i }));
  expect(screen.getByRole('button', { name: /add private note/i })).toBeInTheDocument();
});

test('adds and removes tags through atomic conversation tag callbacks', () => {
  const onAddTag = vi.fn();
  const onRemoveTag = vi.fn();
  const tag = { id: '33333333-3333-4333-8333-333333333333', name: 'Billing', color: '#64748b' };
  const { rerender } = render(<InboxPage selected={conversation} conversations={[conversation]} tags={[tag]} onAddTag={onAddTag} onRemoveTag={onRemoveTag} />);
  fireEvent.click(screen.getByRole('checkbox', { name: /billing/i }));
  expect(onAddTag).toHaveBeenCalledWith(conversation.id, tag.id);
  rerender(<InboxPage selected={{ ...conversation, tags: [tag] }} conversations={[conversation]} tags={[tag]} onAddTag={onAddTag} onRemoveTag={onRemoveTag} />);
  fireEvent.click(screen.getByRole('checkbox', { name: /billing/i }));
  expect(onRemoveTag).toHaveBeenLastCalledWith(conversation.id, tag.id);
});

test('creates a tag inline from conversation details', () => {
  const onCreateTag = vi.fn();
  render(<InboxPage selected={conversation} conversations={[conversation]} onCreateTag={onCreateTag} />);
  fireEvent.change(screen.getByRole('textbox', { name: /new tag name/i }), { target: { value: 'Escalated' } });
  fireEvent.click(screen.getByRole('button', { name: /create tag/i }));
  expect(onCreateTag).toHaveBeenCalledWith(conversation.id, 'Escalated');
});

test('moves between conversation pages', () => {
  const onPage = vi.fn();
  render(<InboxPage page={2} hasNextPage onPage={onPage} />);
  fireEvent.click(screen.getByRole('button', { name: /previous page/i }));
  fireEvent.click(screen.getByRole('button', { name: /next page/i }));
  expect(onPage.mock.calls).toEqual([[1], [3]]);
});

test('renders message Markdown in the timeline', () => {
  render(<InboxPage selected={conversation} conversations={[conversation]} timeline={[{
    id: '44444444-4444-4444-8444-444444444444',
    conversation_id: conversation.id,
    author_agent_id: null,
    kind: 'incoming',
    body: 'Use the **secure link**.',
    created_at: '2026-10-14T00:00:00Z',
    delivery: null,
    email: null,
  }]} />);
  expect(screen.getByText('secure link').tagName).toBe('STRONG');
  expect(screen.getByText('Incoming')).toBeInTheDocument();
});
