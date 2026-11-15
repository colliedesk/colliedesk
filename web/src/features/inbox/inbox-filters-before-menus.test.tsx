import { render, screen } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { InboxFilters } from './inbox-filters';

// The Filter menu's chunk never arrives here, so what shows is the inert control that holds its place.
vi.mock('./filter-panel', () => new Promise(() => {}));

const zalo = { id: 'dddddddd-dddd-4ddd-8ddd-dddddddddddd', name: 'Zalo OA' };

test('before the Filter menu loads, its place already shows the filters that are on and how many', () => {
  render(<InboxFilters tags={[]} query={{ priority: 'urgent', assignee: 'me', status: 'pending', search: 'refund' }} channels={[zalo]} />);
  const count = screen.getByLabelText('2 filters active');
  expect(count).toHaveTextContent(/^2$/);
  expect(count.closest('button')).toHaveAttribute('data-active');
});

test('before the Filter menu loads, the channel the title names is not counted, and a channel alone shows no count', () => {
  const { unmount } = render(<InboxFilters tags={[]} query={{ channel_id: zalo.id, priority: 'urgent' }} channels={[zalo]} />);
  expect(screen.getByLabelText('1 filter active')).toBeInTheDocument();
  unmount();
  render(<InboxFilters tags={[]} query={{ channel_id: zalo.id, status: 'all' }} channels={[zalo]} />);
  expect(screen.queryByLabelText(/filters? active/)).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Filter' })).not.toHaveAttribute('data-active');
});
