import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { useState } from 'react';
import { beforeEach, expect, test, vi } from 'vitest';
import type { InboxQuery } from '../../lib/inbox-query';
import { InboxFilters } from './inbox-filters';

// The workspace's channels, read once the menu opens or a channel is filtered on: one of each type unless a test says more.
const channelList = vi.hoisted(() => ({ listAll: vi.fn(async (): Promise<unknown> => ({ items: [], types: [] })) }));
vi.mock('../../api/channels', () => ({ channelsApi: channelList }));
beforeEach(() => { channelList.listAll.mockResolvedValue({ items: [], types: [] }); });

const billing = { id: '33333333-3333-4333-8333-333333333333', name: 'Billing', color: '#64748b' };
const vip = { id: '44444444-4444-4444-8444-444444444444', name: 'VIP', color: '#64748b' };
const tags = [billing, vip];
// Ann is the signed-in agent; Bo is her teammate.
const ann = { id: '22222222-2222-4222-8222-222222222222', email: 'ann@example.com', display_name: 'Ann', role: 'agent' as const, timezone_override: null, deactivated_at: null, last_active_at: null, open_conversations: 0 };
const bo = { ...ann, id: '99999999-9999-4999-8999-999999999999', email: 'bo@example.com', display_name: 'Bo' };

// Filters follows the query it is given, like the inbox does with the address.
function Harness({ initial = {}, onChange }: { initial?: InboxQuery; onChange?: (query: InboxQuery) => void }) {
  const [query, setQuery] = useState<InboxQuery>(initial);
  return <InboxFilters tags={tags} agents={[ann, bo]} currentAgentId={ann.id} timezone="Asia/Ho_Chi_Minh" query={query} onChange={(next) => { setQuery(next); onChange?.(next); }} />;
}

const labelOf = (box: HTMLElement) => (box as HTMLInputElement).labels?.[0]?.textContent;

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
  fireEvent.click(within(menu).getByRole('checkbox', { name: 'Bo' }));
  fireEvent.click(within(menu).getByRole('checkbox', { name: 'Billing' }));
  fireEvent.click(within(menu).getByRole('checkbox', { name: 'VIP' }));
  fireEvent.click(within(menu).getByRole('checkbox', { name: 'Has every chosen tag' }));
  expect(onChange).toHaveBeenLastCalledWith({ priority: 'urgent,high', assignee: `unassigned,${bo.id}`, tag: `${billing.id},${vip.id}`, tag_match: 'all' });
  expect(screen.getByLabelText('3 filters active')).toBeInTheDocument();
});

test('Assignee lists Me first, then the other teammates, then Unassigned', async () => {
  const onChange = vi.fn();
  render(<Harness onChange={onChange} />);
  const assignee = within(await openMenu()).getByRole('group', { name: 'Assignee' });
  expect(within(assignee).getAllByRole('checkbox').map(labelOf)).toEqual(['Me', 'Bo', 'Unassigned']);
  fireEvent.click(within(assignee).getByRole('checkbox', { name: 'Me' }));
  expect(onChange).toHaveBeenLastCalledWith({ assignee: 'me' });
});

test('the switch for every chosen tag shows once two tags are chosen, or when a link already sets it', async () => {
  const { unmount } = render(<Harness />);
  const menu = await openMenu();
  fireEvent.click(within(menu).getByRole('checkbox', { name: 'Billing' }));
  expect(within(menu).queryByRole('checkbox', { name: 'Has every chosen tag' })).not.toBeInTheDocument();
  fireEvent.click(within(menu).getByRole('checkbox', { name: 'VIP' }));
  expect(within(menu).getByRole('checkbox', { name: 'Has every chosen tag' })).not.toBeChecked();
  unmount();
  render(<Harness initial={{ tag: billing.id, tag_match: 'all' }} />);
  expect(within(await openMenu()).getByRole('checkbox', { name: 'Has every chosen tag' })).toBeChecked();
});

test('Needs attention holds Waiting on us and a failed delivery; a channel narrows the list', async () => {
  channelList.listAll.mockResolvedValue({ items: [form], types: [{ type: 'web_form' }] });
  const onChange = vi.fn();
  render(<Harness onChange={onChange} />);
  const menu = await openMenu();
  const attention = within(menu).getByRole('group', { name: 'Needs attention' });
  expect(within(attention).getAllByRole('checkbox').map(labelOf)).toEqual(['Waiting on us', 'Reply delivery failed']);
  fireEvent.click(within(attention).getByRole('checkbox', { name: 'Waiting on us' }));
  fireEvent.click(within(attention).getByRole('checkbox', { name: 'Reply delivery failed' }));
  fireEvent.click(await within(menu).findByRole('checkbox', { name: 'Web form' }));
  expect(onChange).toHaveBeenLastCalledWith({ awaiting: 'team', delivery_failed: true, channel: 'web_form' });
  fireEvent.click(within(attention).getByRole('checkbox', { name: 'Waiting on us' }));
  expect(onChange).toHaveBeenLastCalledWith({ delivery_failed: true, channel: 'web_form' });
});

test('Channel lists a channel type only once the workspace has one, never Problem report, and keeps a value already chosen', async () => {
  channelList.listAll.mockResolvedValue({ items: [support], types: [{ type: 'email' }] });
  render(<Harness initial={{ channel: 'help_chat,problem_report' }} />);
  const channel = within(await openMenu()).getByRole('group', { name: 'Channel' });
  await waitFor(() => expect(within(channel).getAllByRole('checkbox').map(labelOf)).toEqual(['Email', 'Help chat', 'Imported', 'Started by the team', 'Problem report']));
});

test('Source, Organization, who replies next and the date ranges are not in the menu; a link that sets them still shows chips', async () => {
  const onChange = vi.fn();
  render(<Harness initial={{ source: 'imported', organization: 'Acme', created_after: '2026-09-30T17:00:00.000Z', created_before: '2026-10-07T17:00:00.000Z' }} onChange={onChange} />);
  const menu = await openMenu();
  for (const name of ['Who replies next', 'Source', 'Created', 'Last activity']) expect(within(menu).queryByRole('group', { name })).not.toBeInTheDocument();
  expect(within(menu).queryByRole('textbox')).not.toBeInTheDocument();
  expect(within(menu).queryByRole('radio')).not.toBeInTheDocument();
  for (const name of ['Assigned to anyone', 'Reply draft waiting for approval']) expect(within(menu).queryByRole('checkbox', { name })).not.toBeInTheDocument();
  expect(screen.getByLabelText('3 filters active')).toBeInTheDocument();
  const chips = screen.getByRole('list', { name: 'Active filters' });
  // Ho Chi Minh City is UTC+7: the range starts at 17:00 UTC on September 30 and ends where October 8 starts.
  expect(within(chips).getAllByRole('listitem').map((item) => item.textContent)).toEqual(['Source: Imported', 'Organization: Acme', 'Created: 2026-10-01 to 2026-10-07']);
  fireEvent.click(within(chips).getByRole('button', { name: 'Remove filter Created: 2026-10-01 to 2026-10-07' }));
  expect(onChange).toHaveBeenLastCalledWith({ source: 'imported', organization: 'Acme' });
});

test('every active filter shows as a chip that removes it, and Clear all goes back to the Open view', async () => {
  const onChange = vi.fn();
  render(<Harness initial={{ status: 'pending', priority: 'high,urgent', assignee: 'me,anyone', tag: billing.id, awaiting: 'customer', has_pending_draft: true, organization: 'Acme', search: 'refund' }} onChange={onChange} />);
  const chips = await screen.findByRole('list', { name: 'Active filters' });
  expect(within(chips).getAllByRole('listitem').map((item) => item.textContent)).toEqual([
    // In the order of the Filter menu; a value the menu no longer offers keeps its chip.
    'Priority: High', 'Priority: Urgent', 'Assignee: Me', 'Assignee: Assigned to anyone', 'Tags: Billing', 'Waiting on the customer', 'Reply draft waiting for approval', 'Organization: Acme',
  ]);
  fireEvent.click(within(chips).getByRole('button', { name: 'Remove filter Priority: High' }));
  expect(onChange).toHaveBeenLastCalledWith({ status: 'pending', priority: 'urgent', assignee: 'me,anyone', tag: billing.id, awaiting: 'customer', has_pending_draft: true, organization: 'Acme', search: 'refund' });
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

test('the sort menu offers latest activity, waiting longest and priority, and orders the list', async () => {
  const onChange = vi.fn();
  render(<Harness onChange={onChange} />);
  const sort = screen.getByRole('combobox', { name: 'Sort' });
  await within(sort).findByRole('option', { name: 'Latest activity' });
  expect(within(sort).getAllByRole('option').map((option) => option.textContent)).toEqual(['Latest activity', 'Waiting longest', 'Priority']);
  fireEvent.change(sort, { target: { value: 'waiting_longest' } });
  expect(onChange).toHaveBeenLastCalledWith({ sort: 'waiting_longest' });
});

test('a sort set by a link that the menu does not offer still shows as chosen', async () => {
  render(<Harness initial={{ sort: 'created_asc' }} />);
  const sort = screen.getByRole('combobox', { name: 'Sort' });
  await within(sort).findByRole('option', { name: 'Latest activity' });
  expect(within(sort).getAllByRole('option').map((option) => option.textContent)).toEqual(['Latest activity', 'Waiting longest', 'Priority', 'Oldest first']);
  expect(sort).toHaveValue('created_asc');
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

const support = { id: '55555555-5555-4555-8555-555555555555', type: 'email', name: 'Support', enabled: true };
const sales = { id: '66666666-6666-4666-8666-666666666666', type: 'email', name: 'Sales', enabled: true };
const careers = { id: '77777777-7777-4777-8777-777777777777', type: 'email', name: 'Careers', enabled: true, deleted_at: '2026-10-20T00:00:00Z' };
const form = { id: '88888888-8888-4888-8888-888888888888', type: 'web_form', name: 'Contact form', enabled: true };
const twoMailboxes = { items: [support, sales, careers, form], types: [{ type: 'email' }, { type: 'web_form' }] };

test('a type with two channels lists each of them, so one mailbox can be chosen; a type with one does not', async () => {
  channelList.listAll.mockResolvedValue(twoMailboxes);
  const onChange = vi.fn();
  render(<Harness onChange={onChange} />);
  const menu = await openMenu();
  const mailboxes = await within(menu).findByRole('group', { name: 'Email channels' });
  // A removed channel is offered only once it is chosen.
  expect(within(mailboxes).getAllByRole('checkbox').map((box) => (box as HTMLInputElement).labels?.[0]?.textContent)).toEqual(['Support', 'Sales']);
  expect(within(menu).queryByRole('group', { name: 'Web form channels' })).not.toBeInTheDocument();
  fireEvent.click(within(mailboxes).getByRole('checkbox', { name: 'Sales' }));
  expect(onChange).toHaveBeenLastCalledWith({ channel_id: sales.id });
});

test('the chip of a channel names it, and says when it was removed', async () => {
  channelList.listAll.mockResolvedValue(twoMailboxes);
  render(<Harness initial={{ channel_id: `${sales.id},${careers.id}` }} />);
  const chips = await screen.findByRole('list', { name: 'Active filters' });
  await waitFor(() => expect(within(chips).getAllByRole('listitem').map((item) => item.textContent)).toEqual(['Channel: Sales', 'Channel: Careers (removed)']));
});

test('a link to a code shows it as a chip that removes it', async () => {
  const onChange = vi.fn();
  render(<Harness initial={{ status: 'all', ref: 'CHD-2026-0142' }} onChange={onChange} />);
  const chips = await screen.findByRole('list', { name: 'Active filters' });
  expect(within(chips).getAllByRole('listitem').map((item) => item.textContent)).toContain('Reference: CHD-2026-0142');
  fireEvent.click(within(chips).getByRole('button', { name: 'Remove filter Reference: CHD-2026-0142' }));
  expect(onChange).toHaveBeenLastCalledWith({ status: 'all' });
});

test('Targets filters what is late or due soon, shows as chips, and is offered once a listed conversation has targets', async () => {
  const onChange = vi.fn();
  render(<Harness initial={{ sla: 'overdue' }} onChange={onChange} />);
  const chips = await screen.findByRole('list', { name: 'Active filters' });
  expect(within(chips).getAllByRole('listitem').map((item) => item.textContent)).toContain('Targets: Overdue');
  const menu = await openMenu();
  const targets = within(menu).getByRole('group', { name: 'Targets' });
  fireEvent.click(within(targets).getByRole('checkbox', { name: 'Due soon' }));
  expect(onChange).toHaveBeenLastCalledWith({ sla: 'overdue,due_soon' });
});
