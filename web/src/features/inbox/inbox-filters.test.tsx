import { fireEvent, render, screen, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { InboxFilters } from './inbox-filters';

const tags = [{ id: '33333333-3333-4333-8333-333333333333', name: 'Billing', color: '#64748b' }];

test('status is a row of tabs, All first and selected', () => {
  render(<InboxFilters tags={tags} />);
  const status = screen.getByRole('radiogroup', { name: 'Status' });
  expect(within(status).getAllByRole('radio').map((item) => item.getAttribute('value'))).toEqual(['', 'open', 'pending', 'resolved', 'closed']);
  expect(within(status).getByRole('radio', { name: 'All' })).toBeChecked();
});

test('choosing a status tab filters the list', () => {
  const onChange = vi.fn();
  render(<InboxFilters tags={tags} onChange={onChange} />);
  fireEvent.click(screen.getByRole('radio', { name: 'Pending' }));
  expect(onChange).toHaveBeenLastCalledWith({ search: '', status: 'pending', assignee: '', tag: '' });
});

test('assignee and tag live in one Filter menu that counts what is active', () => {
  const onChange = vi.fn();
  render(<InboxFilters tags={tags} onChange={onChange} />);
  expect(screen.getByText('Filter', { selector: 'summary *, summary' })).toBeInTheDocument();
  fireEvent.change(screen.getByLabelText('Assignee'), { target: { value: 'unassigned' } });
  fireEvent.change(screen.getByLabelText('Tag'), { target: { value: tags[0].id } });
  expect(onChange).toHaveBeenLastCalledWith({ search: '', status: '', assignee: 'unassigned', tag: tags[0].id });
  expect(screen.getByLabelText('2 filters active')).toBeInTheDocument();
});

test('search and Filter come before the status tabs in tab order, as they do on screen', () => {
  render(<InboxFilters tags={tags} />);
  const search = screen.getByRole('searchbox', { name: 'Search conversations' });
  const firstTab = screen.getByRole('radio', { name: 'All' });
  expect(search.compareDocumentPosition(firstTab) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
});

test('the Filter menu closes when keyboard focus leaves it', () => {
  render(<><InboxFilters tags={tags} /><button type="button">Elsewhere</button></>);
  const menu = screen.getByText('Filter').closest('details')!;
  menu.open = true;
  fireEvent.blur(screen.getByLabelText('Tag'), { relatedTarget: screen.getByRole('button', { name: 'Elsewhere' }) });
  expect(menu.open).toBe(false);
});

test('moving focus between controls inside the Filter menu keeps it open', () => {
  render(<InboxFilters tags={tags} />);
  const menu = screen.getByText('Filter').closest('details')!;
  menu.open = true;
  fireEvent.blur(screen.getByLabelText('Assignee'), { relatedTarget: screen.getByLabelText('Tag') });
  expect(menu.open).toBe(true);
});
