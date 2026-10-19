import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { useState } from 'react';
import { expect, test, vi } from 'vitest';
import type { InboxQuery } from '../../lib/inbox-query';
import { InboxFilters } from './inbox-filters';

const billing = { id: '33333333-3333-4333-8333-333333333333', name: 'Billing', color: '#64748b' };
const vip = { id: '44444444-4444-4444-8444-444444444444', name: 'VIP', color: '#64748b' };
const tags = [billing, vip];
const ann = { id: '22222222-2222-4222-8222-222222222222', email: 'ann@example.com', display_name: 'Ann', role: 'agent' as const, timezone_override: null, deactivated_at: null, last_active_at: null, open_conversations: 0 };

// Filters follows the query it is given, like the inbox does with the address.
function Harness({ initial = {}, onChange }: { initial?: InboxQuery; onChange?: (query: InboxQuery) => void }) {
  const [query, setQuery] = useState<InboxQuery>(initial);
  return <InboxFilters tags={tags} agents={[ann]} timezone="Asia/Ho_Chi_Minh" query={query} onChange={(next) => { setQuery(next); onChange?.(next); }} />;
}

// The menu's contents load on their own chunk; the first test that opens it waits for it.
const openMenu = async () => {
  fireEvent.click(screen.getByText('Filter'));
  await screen.findByRole('checkbox', { name: 'Urgent' });
  return screen.getByRole('group', { name: 'Filter' });
};

test('status is a row of tabs, All first, Open selected so the inbox starts on what needs an answer', () => {
  render(<Harness />);
  const status = screen.getByRole('radiogroup', { name: 'Status' });
  expect(within(status).getAllByRole('radio').map((item) => item.getAttribute('value'))).toEqual(['all', 'open', 'pending', 'resolved', 'closed']);
  expect(within(status).getByRole('radio', { name: 'Open' })).toBeChecked();
});

test('choosing a status tab filters the list', () => {
  const onChange = vi.fn();
  render(<Harness onChange={onChange} />);
  fireEvent.click(screen.getByRole('radio', { name: 'Pending' }));
  expect(onChange).toHaveBeenLastCalledWith({ status: 'pending' });
});

test('the Filter menu picks several priorities, assignees and tags, and counts the filters that are on', async () => {
  const onChange = vi.fn();
  render(<Harness onChange={onChange} />);
  const menu = await openMenu();
  fireEvent.click(within(menu).getByRole('checkbox', { name: 'Urgent' }));
  fireEvent.click(within(menu).getByRole('checkbox', { name: 'High' }));
  fireEvent.click(within(menu).getByRole('checkbox', { name: 'Unassigned' }));
  fireEvent.click(within(menu).getByRole('checkbox', { name: 'Ann' }));
  fireEvent.click(within(menu).getByRole('checkbox', { name: 'Billing' }));
  fireEvent.click(within(menu).getByRole('checkbox', { name: 'VIP' }));
  fireEvent.click(within(menu).getByRole('checkbox', { name: 'Has every chosen tag' }));
  expect(onChange).toHaveBeenLastCalledWith({ priority: 'urgent,high', assignee: `unassigned,${ann.id}`, tag: `${billing.id},${vip.id}`, tag_match: 'all' });
  expect(screen.getByLabelText('3 filters active')).toBeInTheDocument();
});

test('who replies next, attention flags, channel and source each narrow the list', async () => {
  const onChange = vi.fn();
  render(<Harness onChange={onChange} />);
  const menu = await openMenu();
  fireEvent.click(within(menu).getByRole('radio', { name: 'Waiting on us' }));
  fireEvent.click(within(menu).getByRole('checkbox', { name: 'Reply draft waiting for approval' }));
  fireEvent.click(within(menu).getByRole('checkbox', { name: 'Reply delivery failed' }));
  fireEvent.click(within(menu).getByRole('checkbox', { name: 'Web form' }));
  fireEvent.click(within(menu).getByRole('radio', { name: 'Imported' }));
  expect(onChange).toHaveBeenLastCalledWith({ awaiting: 'team', has_pending_draft: true, delivery_failed: true, channel: 'web_form', source: 'imported' });
  fireEvent.click(within(menu).getByRole('radio', { name: 'Anyone' }));
  expect(onChange).toHaveBeenLastCalledWith({ has_pending_draft: true, delivery_failed: true, channel: 'web_form', source: 'imported' });
});

test('a date range is picked in the agent timezone and sent as UTC instants, the end day included', async () => {
  const onChange = vi.fn();
  render(<Harness onChange={onChange} />);
  const menu = await openMenu();
  const created = within(menu).getByRole('group', { name: 'Created' });
  fireEvent.change(within(created).getByLabelText('From'), { target: { value: '2026-10-01' } });
  fireEvent.change(within(created).getByLabelText('To'), { target: { value: '2026-10-07' } });
  // Ho Chi Minh City is UTC+7: October 1 starts at 17:00 UTC the day before; the range ends where October 8 starts.
  expect(onChange).toHaveBeenLastCalledWith({ created_after: '2026-09-30T17:00:00.000Z', created_before: '2026-10-07T17:00:00.000Z' });
  expect(within(created).getByLabelText('To')).toHaveValue('2026-10-07');
});

test('every active filter shows as a chip that removes it, and Clear all goes back to the Open view', async () => {
  const onChange = vi.fn();
  render(<Harness initial={{ status: 'pending', priority: 'high,urgent', tag: billing.id, awaiting: 'customer', organization: 'Acme', search: 'refund' }} onChange={onChange} />);
  const chips = await screen.findByRole('list', { name: 'Active filters' });
  expect(within(chips).getAllByRole('listitem').map((item) => item.textContent)).toEqual([
    // In the order of the Filter menu.
    'Priority: High', 'Priority: Urgent', 'Tags: Billing', 'Who replies next: Waiting on the customer', 'Organization: Acme',
  ]);
  fireEvent.click(within(chips).getByRole('button', { name: 'Remove filter Priority: High' }));
  expect(onChange).toHaveBeenLastCalledWith({ status: 'pending', priority: 'urgent', tag: billing.id, awaiting: 'customer', organization: 'Acme', search: 'refund' });
  fireEvent.click(screen.getByRole('button', { name: 'Clear all' }));
  expect(onChange).toHaveBeenLastCalledWith({});
  expect(screen.queryByRole('list', { name: 'Active filters' })).not.toBeInTheDocument();
  expect(screen.getByRole('radio', { name: 'Open' })).toBeChecked();
  expect(screen.getByRole('searchbox', { name: 'Search conversations' })).toHaveValue('');
});

// The address updates after a navigation, a moment after each key. Typed text must not jump back meanwhile.
function LaggingHarness() {
  const [query, setQuery] = useState<InboxQuery>({ priority: 'urgent' });
  return <InboxFilters tags={tags} timezone="UTC" query={query} onChange={(next) => { setTimeout(() => setQuery(next), 0); }} />;
}

test('text typed faster than the address updates is kept, and a change from elsewhere still shows', async () => {
  vi.useFakeTimers();
  try {
    render(<LaggingHarness />);
    await act(async () => { await import('./filter-panel'); });
    const search = screen.getByRole('searchbox', { name: 'Search conversations' });
    fireEvent.change(search, { target: { value: 'i' } });
    fireEvent.change(search, { target: { value: 'in' } });
    await act(async () => { vi.advanceTimersByTime(150); });
    fireEvent.change(search, { target: { value: 'inv' } });
    expect(search).toHaveValue('inv');
    await act(async () => { vi.runAllTimers(); });
    expect(search).toHaveValue('inv');
    fireEvent.click(screen.getByRole('button', { name: 'Clear all' }));
    await act(async () => { vi.runAllTimers(); });
    expect(search).toHaveValue('');
  } finally {
    vi.useRealTimers();
  }
});

test('a date is applied once it is a whole day, never while the year is half typed', async () => {
  const onChange = vi.fn();
  render(<Harness onChange={onChange} />);
  const created = within(await openMenu()).getByRole('group', { name: 'Created' });
  fireEvent.change(within(created).getByLabelText('From'), { target: { value: '0002-03-15' } });
  expect(onChange).not.toHaveBeenCalled();
  expect(within(created).getByLabelText('From')).toHaveValue('0002-03-15');
  fireEvent.change(within(created).getByLabelText('From'), { target: { value: '2025-03-15' } });
  expect(onChange).toHaveBeenLastCalledWith({ created_after: '2025-03-14T17:00:00.000Z' });
});

// A form inside a form is invalid HTML: the browser submits the outer one natively (a full page load to
// /?view-scope=on) and the saved view is never saved. The filters are a search region, not a form.
test('the filters are not a form, so the forms of the views menu submit on their own', () => {
  render(<InboxFilters tags={tags} query={{}} views={<form aria-label="Save view form" />} />);
  expect(screen.getByRole('search', { name: 'Inbox' })).toBeInTheDocument();
  expect(screen.getByRole('form', { name: 'Save view form' }).parentElement?.closest('form')).toBeNull();
});

// On a phone the chips scroll sideways; Clear all sits beside the scrolling list, never inside it.
test('Clear all is outside the list of chips, so scrolling the chips never hides it', async () => {
  render(<Harness initial={{ priority: 'urgent,high,normal,low', channel: 'email,web_form,imported', awaiting: 'team' }} />);
  const chips = await screen.findByRole('list', { name: 'Active filters' });
  expect(chips.contains(screen.getByRole('button', { name: 'Clear all' }))).toBe(false);
});

test('the sort menu orders the list', () => {
  const onChange = vi.fn();
  render(<Harness onChange={onChange} />);
  fireEvent.change(screen.getByRole('combobox', { name: 'Sort' }), { target: { value: 'waiting_longest' } });
  expect(onChange).toHaveBeenLastCalledWith({ sort: 'waiting_longest' });
});

test('search and Filter come before the status tabs in tab order, as they do on screen', () => {
  render(<Harness />);
  const search = screen.getByRole('searchbox', { name: 'Search conversations' });
  const firstTab = screen.getByRole('radio', { name: 'All' });
  expect(search.compareDocumentPosition(firstTab) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
});

test('the Filter menu closes when keyboard focus leaves it and on Escape', async () => {
  render(<><Harness /><button type="button">Elsewhere</button></>);
  await openMenu();
  const menu = screen.getByText('Filter').closest('details')!;
  menu.open = true;
  fireEvent.blur(screen.getByRole('checkbox', { name: 'Urgent' }), { relatedTarget: screen.getByRole('button', { name: 'Elsewhere' }) });
  expect(menu.open).toBe(false);
  menu.open = true;
  fireEvent.blur(screen.getByRole('checkbox', { name: 'Urgent' }), { relatedTarget: screen.getByRole('checkbox', { name: 'High' }) });
  expect(menu.open).toBe(true);
  fireEvent.keyDown(screen.getByRole('checkbox', { name: 'High' }), { key: 'Escape' });
  expect(menu.open).toBe(false);
});

test('searching from the default Open view searches every status, like Freshdesk does', async () => {
  const onChange = vi.fn();
  render(<Harness onChange={onChange} />);
  fireEvent.change(screen.getByRole('searchbox', { name: 'Search conversations' }), { target: { value: 'invoice' } });
  await waitFor(() => expect(onChange).toHaveBeenLastCalledWith({ search: 'invoice', status: 'all' }));
  expect(screen.getByRole('radio', { name: 'All' })).toBeChecked();
});

test('a status the agent picked stays when they search', async () => {
  const onChange = vi.fn();
  render(<Harness onChange={onChange} />);
  fireEvent.click(screen.getByRole('radio', { name: 'Pending' }));
  fireEvent.change(screen.getByRole('searchbox', { name: 'Search conversations' }), { target: { value: 'invoice' } });
  await waitFor(() => expect(onChange).toHaveBeenLastCalledWith({ status: 'pending', search: 'invoice' }));
});

test('typing a word sends one search after the pause, not one per key', async () => {
  vi.useFakeTimers();
  try {
    const onChange = vi.fn();
    render(<Harness onChange={onChange} />);
    const search = screen.getByRole('searchbox', { name: 'Search conversations' });
    for (const text of ['r', 're', 'ref', 'refu', 'refun', 'refund']) {
      fireEvent.change(search, { target: { value: text } });
      await act(async () => { vi.advanceTimersByTime(40); });
    }
    expect(onChange).not.toHaveBeenCalled();
    await act(async () => { vi.advanceTimersByTime(100); });
    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenLastCalledWith({ search: 'refund', status: 'all' });
  } finally {
    vi.useRealTimers();
  }
});
