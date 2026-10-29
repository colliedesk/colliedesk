import { fireEvent, render, screen, within } from '@testing-library/react';
import { beforeAll, expect, test, vi } from 'vitest';
import { InboxPage } from './inbox-page';
import { presenceViewPart } from './presence-part';

const conversation = { latest_reply_number: 0,
  id: '11111111-1111-4111-8111-111111111111', contact_id: '22222222-2222-4222-8222-222222222222', contact_name: 'Mai Linh',
  subject: 'Example request', status: 'open' as const, priority: 'normal' as const, assignee_id: null, tags: [], channel: 'email' as const, origin: null,
  created_at: '2026-10-14T00:00:00Z', updated_at: '2026-10-14T00:00:00Z',
};
const mai = {
  id: conversation.contact_id, name: 'Mai Linh', email: 'mai@customer.example', organization: null, merged_into_id: null,
  created_at: '2026-01-02T00:00:00Z', updated_at: '2026-01-02T00:00:00Z',
};


// The presence chunk loads when somebody else is on a conversation, a reply is refused or a notice shows;
// start with it there, so no test ends while it is still on its way (vitest refuses a module asked for
// after the test environment is gone).
beforeAll(async () => {
  presenceViewPart.preload();
  await import('./presence-view');
});

test('the open conversation names its customer and opens their page', () => {
  render(<InboxPage selected={conversation} conversations={[conversation]} />);
  const link = within(screen.getByRole('region', { name: /timeline/i })).getByRole('link', { name: "Open Mai Linh's page" });
  expect(link).toHaveAttribute('href', `/contacts/${mai.id}`);
});

test('the customers a search finds go under the filters, above the conversations', () => {
  render(<InboxPage conversations={[conversation]} contactMatches={<nav aria-label="Customers">found</nav>} />);
  const list = screen.getByRole('region', { name: /conversation list/i });
  expect(within(list).getByRole('navigation', { name: 'Customers' })).toBeInTheDocument();
});

test('the inbox offers to start a new conversation', () => {
  const onNewConversation = vi.fn();
  render(<InboxPage onNewConversation={onNewConversation} />);
  fireEvent.click(screen.getByRole('button', { name: 'New conversation' }));
  expect(onNewConversation).toHaveBeenCalled();
});
