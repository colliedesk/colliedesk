import { fireEvent, render, screen, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { InboxPage } from './inbox-page';

const conversation = {
  id: '11111111-1111-4111-8111-111111111111', contact_id: '22222222-2222-4222-8222-222222222222', contact_name: 'Mai Linh',
  subject: 'Example request', status: 'open' as const, priority: 'normal' as const, assignee_id: null, tags: [], channel: 'email' as const, origin: null,
  created_at: '2026-10-14T00:00:00Z', updated_at: '2026-10-14T00:00:00Z',
};
const mai = {
  id: conversation.contact_id, name: 'Mai Linh', email: 'mai@customer.example', organization: null, merged_into_id: null,
  created_at: '2026-01-02T00:00:00Z', updated_at: '2026-01-02T00:00:00Z',
};

test('the open conversation names its customer and opens their page', () => {
  const onOpenContact = vi.fn();
  render(<InboxPage selected={conversation} conversations={[conversation]} onOpenContact={onOpenContact} />);
  const link = within(screen.getByRole('region', { name: /timeline/i })).getByRole('link', { name: "Open Mai Linh's page" });
  expect(link).toHaveAttribute('href', `/contacts/${mai.id}`);
  fireEvent.click(link);
  expect(onOpenContact).toHaveBeenCalledWith(mai.id);
});

test('customers a search finds are listed above the conversations, each opening their page', () => {
  const onOpenContact = vi.fn();
  render(<InboxPage conversations={[conversation]} contactMatches={[mai]} onOpenContact={onOpenContact} />);
  const found = screen.getByRole('navigation', { name: 'Customers' });
  fireEvent.click(within(found).getByRole('link', { name: /mai@customer.example/ }));
  expect(onOpenContact).toHaveBeenCalledWith(mai.id);
});

test('the inbox offers to start a new conversation', () => {
  const onNewConversation = vi.fn();
  render(<InboxPage onNewConversation={onNewConversation} />);
  fireEvent.click(screen.getByRole('button', { name: 'New conversation' }));
  expect(onNewConversation).toHaveBeenCalled();
});
