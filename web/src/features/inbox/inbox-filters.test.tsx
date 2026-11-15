import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { useState } from 'react';
import type { ReactNode } from 'react';
import { beforeEach, expect, test, vi } from 'vitest';
import type { InboxQuery } from '../../lib/inbox-query';
import { permissionsOf, rolesOf } from '../../test-roles';
import { InboxFilters } from './inbox-filters';

// The workspace's channels, read once the menu opens or a channel is filtered on: one of each type unless a test says more.
const channelList = vi.hoisted(() => ({ listAll: vi.fn(async (): Promise<unknown> => ({ items: [], types: [] })) }));
vi.mock('../../api/channels', () => ({ channelsApi: channelList }));
beforeEach(() => { channelList.listAll.mockResolvedValue({ items: [], types: [] }); });

const billing = { id: '33333333-3333-4333-8333-333333333333', name: 'Billing', color: '#64748b' };
const vip = { id: '44444444-4444-4444-8444-444444444444', name: 'VIP', color: '#64748b' };
const tags = [billing, vip];
// Ann is the signed-in agent; Bo is her teammate.
const ann = { id: '22222222-2222-4222-8222-222222222222', email: 'ann@example.com', display_name: 'Ann', roles: rolesOf('agent'), manageable: true, timezone_override: null, deactivated_at: null, last_active_at: null, open_conversations: 0 };
const bo = { ...ann, id: '99999999-9999-4999-8999-999999999999', email: 'bo@example.com', display_name: 'Bo' };

// The workspace's channels and saved views, as the sidebar lists them: they name the place the list is in.
const zalo = { id: 'dddddddd-dddd-4ddd-8ddd-dddddddddddd', name: 'Zalo OA' };
const mailbox = { id: 'eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee', name: 'support@northwind.help' };
const urgentView = { id: 'ffffffff-ffff-4fff-8fff-ffffffffffff', name: 'Urgent', filters: { priority: ['urgent' as const] } };

// Filters follows the query it is given, like the inbox does with the address.
function Harness({ initial = {}, onChange, dueSort, counts, views }: { initial?: InboxQuery; onChange?: (query: InboxQuery) => void; dueSort?: string; counts?: Partial<Record<string, number>>; views?: ReactNode }) {
  const [query, setQuery] = useState<InboxQuery>(initial);
  return <InboxFilters tags={tags} agents={[ann, bo]} currentAgentId={ann.id} timezone="Asia/Ho_Chi_Minh" query={query} dueSort={dueSort} counts={counts} views={views}
    channels={[zalo, mailbox]} savedViews={[urgentView]} onChange={(next) => { setQuery(next); onChange?.(next); }} />;
}

const labelOf = (box: HTMLElement) => (box as HTMLInputElement).labels?.[0]?.textContent;

// The menu's contents load on their own chunk; the first test that opens it waits for it.
const openMenu = async () => {
  fireEvent.click(screen.getByText('Filter'));
  await screen.findByRole('checkbox', { name: 'Urgent' });
  return screen.getByRole('group', { name: 'Filter' });
};

// The Sort menu once its chunk has replaced the inert control that holds its place.
// The words beside the sort icon (the options of the menu are not spans).
const sortLabel = () => screen.getByText(/./, { selector: '.list-controls span.sort-label' });

const loadedSort = () => waitFor(() => {
  const sort = screen.getByRole('combobox', { name: 'Sort' });
  expect(within(sort).getByRole('option', { name: 'Priority' })).toBeInTheDocument();
  return sort;
});

const title = () => screen.getByRole('heading', { level: 2 });
const picker = () => screen.getByRole('button', { name: /^Status: / });
const openStatusMenu = () => {
  fireEvent.click(picker());
  return screen.getByRole('menu', { name: 'Status' });
};
// Each item of the status menu as it reads: its name, then the count beside it.
const menuRows = (menu: HTMLElement) => within(menu).getAllByRole('menuitemradio').map((item) => item.textContent);

test('the title says where the list is: every channel at rest', () => {
  render(<Harness />);
  expect(title()).toHaveTextContent(/^All channels$/);
});

test('a channel opened from the sidebar titles the list with its name, under any status', () => {
  render(<Harness initial={{ channel_id: zalo.id, status: 'all' }} />);
  expect(title()).toHaveTextContent(/^Zalo OA$/);
});

test('a saved view on screen titles the list with its name', () => {
  render(<Harness initial={{ priority: 'urgent', status: 'all' }} />);
  expect(title()).toHaveTextContent(/^Urgent$/);
});

test('two channels at once are not one place: the title says every channel and each channel keeps its chip', async () => {
  render(<Harness initial={{ channel_id: `${zalo.id},${mailbox.id}` }} />);
  expect(title()).toHaveTextContent(/^All channels$/);
  const chips = await screen.findByRole('list', { name: 'Active filters' });
  expect(within(chips).getAllByRole('listitem')).toHaveLength(2);
});

test('the status picker shows the status on screen, its dot and its count', () => {
  render(<Harness counts={{ open: 6, pending: 0, resolved: 3, closed: 7, spam: 0, all: 16 }} />);
  expect(picker()).toHaveAccessibleName('Status: Open');
  expect(picker()).toHaveTextContent(/^Open6$/);
  expect(picker().querySelector('.status-dot')).toHaveAttribute('data-status', 'open');
});

test('the status menu lists every status with its count, zero included, then All statuses after a line', () => {
  render(<Harness initial={{ status: 'pending' }} counts={{ open: 6, pending: 0, resolved: 3, closed: 7, spam: 0, all: 16 }} />);
  const menu = openStatusMenu();
  expect(menuRows(menu)).toEqual(['Open6', 'Pending0', 'Resolved3', 'Closed7', 'All statuses16']);
  expect(within(menu).getByRole('separator')).toBeInTheDocument();
  expect(within(menu).getByRole('menuitemradio', { name: 'Pending' })).toHaveAttribute('aria-checked', 'true');
  expect(within(menu).getByRole('menuitemradio', { name: 'Open' })).toHaveAttribute('aria-checked', 'false');
});

test('Spam is listed after Closed once it holds a conversation, or while it is the status on screen', () => {
  const { unmount } = render(<Harness counts={{ open: 6, pending: 0, resolved: 3, closed: 7, spam: 2, all: 16 }} />);
  expect(menuRows(openStatusMenu())).toEqual(['Open6', 'Pending0', 'Resolved3', 'Closed7', 'Spam2', 'All statuses16']);
  unmount();
  render(<Harness initial={{ status: 'spam' }} counts={{ open: 6, pending: 0, resolved: 3, closed: 7, spam: 0, all: 16 }} />);
  expect(picker()).toHaveAccessibleName('Status: Spam');
  expect(menuRows(openStatusMenu())).toEqual(['Open6', 'Pending0', 'Resolved3', 'Closed7', 'Spam0', 'All statuses16']);
  // The count is not part of an item's name.
  expect(screen.getByRole('menuitemradio', { name: 'Spam' })).toHaveAttribute('aria-checked', 'true');
});

test('one click on a status filters the list and closes the menu, back on the picker', () => {
  const onChange = vi.fn();
  render(<Harness onChange={onChange} />);
  fireEvent.click(within(openStatusMenu()).getByRole('menuitemradio', { name: 'Pending' }));
  expect(onChange).toHaveBeenLastCalledWith({ status: 'pending' });
  expect(screen.queryByRole('menu', { name: 'Status' })).not.toBeInTheDocument();
  expect(picker()).toHaveFocus();
  expect(picker()).toHaveAccessibleName('Status: Pending');
  fireEvent.click(within(openStatusMenu()).getByRole('menuitemradio', { name: 'All statuses' }));
  expect(onChange).toHaveBeenLastCalledWith({ status: 'all' });
});

test('Open picked while no status was chosen keeps the default address', () => {
  const onChange = vi.fn();
  render(<Harness onChange={onChange} />);
  fireEvent.click(within(openStatusMenu()).getByRole('menuitemradio', { name: 'Open' }));
  expect(onChange).toHaveBeenLastCalledWith({});
});

test('the status menu works from the keyboard: the arrows move, Enter picks, Escape closes it on the picker', () => {
  const onChange = vi.fn();
  render(<Harness onChange={onChange} />);
  fireEvent.keyDown(picker(), { key: 'ArrowDown' });
  const menu = screen.getByRole('menu', { name: 'Status' });
  // The menu opens on the status on screen.
  expect(within(menu).getByRole('menuitemradio', { name: 'Open' })).toHaveFocus();
  fireEvent.keyDown(menu, { key: 'ArrowDown' });
  expect(within(menu).getByRole('menuitemradio', { name: 'Pending' })).toHaveFocus();
  fireEvent.keyDown(menu, { key: 'ArrowUp' });
  fireEvent.keyDown(menu, { key: 'ArrowUp' });
  expect(within(menu).getByRole('menuitemradio', { name: 'All statuses' })).toHaveFocus();
  fireEvent.keyDown(menu, { key: 'Escape' });
  expect(screen.queryByRole('menu', { name: 'Status' })).not.toBeInTheDocument();
  expect(picker()).toHaveFocus();
  expect(onChange).not.toHaveBeenCalled();
  fireEvent.keyDown(picker(), { key: 'Enter' });
  fireEvent.keyDown(screen.getByRole('menu', { name: 'Status' }), { key: 'ArrowDown' });
  fireEvent.keyDown(screen.getByRole('menuitemradio', { name: 'Pending' }), { key: 'Enter' });
  expect(onChange).toHaveBeenLastCalledWith({ status: 'pending' });
  expect(picker()).toHaveFocus();
});

test('a status menu left open closes on a press outside it', () => {
  render(<><Harness /><button type="button">Elsewhere</button></>);
  openStatusMenu();
  fireEvent.pointerDown(screen.getByRole('button', { name: 'Elsewhere' }));
  expect(screen.queryByRole('menu', { name: 'Status' })).not.toBeInTheDocument();
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
  for (const name of ['Assigned to anyone', 'Reply draft waiting to be sent']) expect(within(menu).queryByRole('checkbox', { name })).not.toBeInTheDocument();
  expect(screen.getByLabelText('3 filters active')).toBeInTheDocument();
  const chips = screen.getByRole('list', { name: 'Active filters' });
  // Ho Chi Minh City is UTC+7: the range starts at 17:00 UTC on September 30 and ends where October 8 starts.
  expect(within(chips).getAllByRole('listitem').map((item) => item.textContent)).toEqual(['Source: Imported', 'Organization: Acme', 'Created: 2026-10-01 to 2026-10-07']);
  fireEvent.click(within(chips).getByRole('button', { name: 'Remove filter Created: 2026-10-01 to 2026-10-07' }));
  expect(onChange).toHaveBeenLastCalledWith({ source: 'imported', organization: 'Acme' });
});

test('every active filter shows as a chip that removes it, and Clear goes back to the Open view', async () => {
  const onChange = vi.fn();
  render(<Harness initial={{ status: 'pending', priority: 'high,urgent', assignee: 'me,anyone', tag: billing.id, awaiting: 'customer', has_pending_draft: true, organization: 'Acme', search: 'refund' }} onChange={onChange} />);
  const chips = await screen.findByRole('list', { name: 'Active filters' });
  expect(within(chips).getAllByRole('listitem').map((item) => item.textContent)).toEqual([
    // In the order of the Filter menu; a value the menu no longer offers keeps its chip.
    'Priority: High', 'Priority: Urgent', 'Assignee: Me', 'Assignee: Assigned to anyone', 'Tag: Billing', 'Waiting on the customer', 'Reply draft waiting to be sent', 'Organization: Acme',
  ]);
  fireEvent.click(within(chips).getByRole('button', { name: 'Remove filter Priority: High' }));
  expect(onChange).toHaveBeenLastCalledWith({ status: 'pending', priority: 'urgent', assignee: 'me,anyone', tag: billing.id, awaiting: 'customer', has_pending_draft: true, organization: 'Acme', search: 'refund' });
  fireEvent.click(screen.getByRole('button', { name: 'Clear' }));
  expect(onChange).toHaveBeenLastCalledWith({});
  expect(screen.queryByRole('list', { name: 'Active filters' })).not.toBeInTheDocument();
  expect(picker()).toHaveAccessibleName('Status: Open');
  // Nothing is filtered or searched any more: the list is back to its title.
  expect(screen.queryByRole('searchbox')).not.toBeInTheDocument();
  expect(title()).toHaveTextContent(/^All channels$/);
});

// The address updates after a navigation, a moment after each key. Typed text must not jump back meanwhile.
function LaggingHarness() {
  const [query, setQuery] = useState<InboxQuery>({ priority: 'urgent' });
  return <InboxFilters tags={tags} timezone="UTC" query={query} focusSearch onChange={(next) => { setTimeout(() => setQuery(next), 0); }} />;
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
    fireEvent.click(screen.getByRole('button', { name: 'Clear' }));
    await act(async () => { vi.runAllTimers(); });
    expect(search).toHaveValue('');
  } finally {
    vi.useRealTimers();
  }
});

// A form inside a form is invalid HTML: the browser submits the outer one natively (a full page load to
// /?view-scope=on) and the saved view is never saved. The filters are a search region, not a form.
test('the filters are not a form, so the forms of the views menu submit on their own', () => {
  // The views menu sits beside the search box, which a search shows.
  render(<InboxFilters tags={tags} query={{ search: 'refund', status: 'all' }} views={<form aria-label="Save view form" />} />);
  expect(screen.getByRole('search', { name: 'Inbox' })).toBeInTheDocument();
  expect(screen.getByRole('form', { name: 'Save view form' }).parentElement?.closest('form')).toBeNull();
});

// Clear sits after the chips, never inside their list, so it stays one press away however many chips wrap.
test('Clear is outside the list of chips', async () => {
  render(<Harness initial={{ priority: 'urgent,high,normal,low', channel: 'email,web_form,imported', awaiting: 'team' }} />);
  const chips = await screen.findByRole('list', { name: 'Active filters' });
  expect(chips.contains(screen.getByRole('button', { name: 'Clear' }))).toBe(false);
});

test('the list starts on Due first; the sort menu also offers latest activity, waiting longest and priority', async () => {
  const onChange = vi.fn();
  render(<Harness onChange={onChange} />);
  const sort = screen.getByRole('combobox', { name: 'Sort' });
  await within(sort).findByRole('option', { name: 'Due first' });
  expect(within(sort).getAllByRole('option').map((option) => option.textContent)).toEqual(['Due first', 'Latest activity', 'Waiting longest', 'Priority']);
  expect(sort).toHaveValue('due');
  fireEvent.change(sort, { target: { value: 'updated_desc' } });
  expect(onChange).toHaveBeenLastCalledWith({ sort: 'updated_desc' });
  fireEvent.change(sort, { target: { value: 'due' } });
  expect(onChange).toHaveBeenLastCalledWith({});
});

test('while reply targets are off the list on Due first is ordered Waiting longest, and the Sort menu says Waiting longest', async () => {
  const onChange = vi.fn();
  // The default order and a saved view or link that asks for due read the same.
  for (const initial of [{}, { sort: 'due' }]) {
    const { unmount } = render(<Harness initial={initial} dueSort="waiting_longest" onChange={onChange} />);
    const sort = await loadedSort();
    expect(sort).toHaveValue('waiting_longest');
    // Nothing is due, so Due first is not offered beside the order it gives.
    expect(within(sort).getAllByRole('option').map((option) => option.textContent)).toEqual(['Latest activity', 'Waiting longest', 'Priority']);
    // The order in use reads in words beside the sort icon.
    expect(sortLabel()).toHaveTextContent(/^Waiting longest$/);
    unmount();
  }
  render(<Harness dueSort="waiting_longest" onChange={onChange} />);
  const sort = await loadedSort();
  fireEvent.change(sort, { target: { value: 'priority' } });
  expect(onChange).toHaveBeenLastCalledWith({ sort: 'priority' });
  expect(sort).toHaveValue('priority');
  // Waiting longest is then the way back to the list's default order, which follows the targets once they are on.
  fireEvent.change(sort, { target: { value: 'waiting_longest' } });
  expect(onChange).toHaveBeenLastCalledWith({});
  expect(sort).toHaveValue('waiting_longest');
});

test('a sort set by a link that the menu does not offer still shows as chosen', async () => {
  render(<Harness initial={{ sort: 'created_asc' }} />);
  const sort = screen.getByRole('combobox', { name: 'Sort' });
  await within(sort).findByRole('option', { name: 'Latest activity' });
  expect(within(sort).getAllByRole('option').map((option) => option.textContent)).toEqual(['Due first', 'Latest activity', 'Waiting longest', 'Priority', 'Oldest first']);
  expect(sort).toHaveValue('created_asc');
});

// The header reads in two rows: the title with Search and Filter, then the status and the order. Chips and the
// saved views show only while something narrows the list.
const follows = (first: Node, next: Node) => Boolean(first.compareDocumentPosition(next) & Node.DOCUMENT_POSITION_FOLLOWING);

test('at rest the header is the title with Search and Filter, then the status and the order, on screen and for Tab', async () => {
  render(<Harness views={<details aria-label="Saved views" />} counts={{ open: 6, pending: 0, resolved: 3, closed: 7, spam: 0, all: 16 }} />);
  await loadedSort();
  expect(screen.queryByRole('searchbox')).not.toBeInTheDocument();
  expect(screen.queryByRole('list', { name: 'Active filters' })).not.toBeInTheDocument();
  // The saved views are listed in the sidebar; the header offers them only while the list is narrowed.
  expect(screen.queryByRole('group', { name: 'Saved views' })).not.toBeInTheDocument();
  expect(sortLabel()).toHaveTextContent(/^Due first$/);
  const order = [title(), screen.getByRole('button', { name: 'Search' }), screen.getByText('Filter'), picker(), screen.getByRole('combobox', { name: 'Sort' })];
  for (let index = 1; index < order.length; index++) expect(follows(order[index - 1], order[index])).toBe(true);
});

test('before the menus have loaded, the order in use already reads beside the sort icon', () => {
  render(<Harness dueSort="waiting_longest" />);
  expect(sortLabel()).toHaveTextContent(/^Waiting longest$/);
});

test('a filter shows its count beside Filter, the saved views after Filter, and the chips under the status row', async () => {
  render(<Harness initial={{ priority: 'urgent', assignee: 'me' }} views={<details aria-label="Saved views" />} />);
  expect(screen.queryByRole('searchbox')).not.toBeInTheDocument();
  const count = await screen.findByLabelText('2 filters active');
  expect(count).toHaveTextContent(/^2$/);
  const chips = await screen.findByRole('list', { name: 'Active filters' });
  expect(within(chips).getAllByRole('listitem').map((item) => item.textContent)).toEqual(['Priority: Urgent', 'Assignee: Me']);
  const order = [screen.getByText('Filter'), screen.getByRole('group', { name: 'Saved views' }), picker(), chips];
  for (let index = 1; index < order.length; index++) expect(follows(order[index - 1], order[index])).toBe(true);
});

test('the channel the title names has no chip and is not counted on Filter; the other filters still are', async () => {
  render(<Harness initial={{ channel_id: zalo.id, priority: 'urgent' }} />);
  expect(title()).toHaveTextContent(/^Zalo OA$/);
  expect(await screen.findByLabelText('1 filter active')).toBeInTheDocument();
  const chips = await screen.findByRole('list', { name: 'Active filters' });
  expect(within(chips).getAllByRole('listitem').map((item) => item.textContent)).toEqual(['Priority: Urgent']);
});

test('a channel alone shows no chip and no Filter count: the title says it', async () => {
  render(<Harness initial={{ channel_id: zalo.id, status: 'all' }} />);
  await loadedSort();
  expect(screen.queryByRole('list', { name: 'Active filters' })).not.toBeInTheDocument();
  expect(screen.queryByLabelText(/filters? active/)).not.toBeInTheDocument();
});

test('status, order and search have their own places and never show as chips', async () => {
  render(<Harness initial={{ status: 'pending', sort: 'priority', search: 'refund' }} />);
  await loadedSort();
  expect(screen.queryByRole('list', { name: 'Active filters' })).not.toBeInTheDocument();
  expect(screen.queryByLabelText(/filters? active/)).not.toBeInTheDocument();
});

// The app's Search (the top bar, or Clear filters in the list) asks the list for its search field.
function SearchHarness({ onChange, initial = {} }: { onChange?: (query: InboxQuery) => void; initial?: InboxQuery }) {
  const [query, setQuery] = useState<InboxQuery>(initial);
  const [signal, setSignal] = useState(0);
  return <><button type="button" onClick={() => setSignal((count) => count + 1)}>App search</button>
    <InboxFilters tags={tags} query={query} focusSignal={signal} channels={[zalo]} onChange={(next) => { setQuery(next); onChange?.(next); }} /></>;
}
const openSearch = () => fireEvent.click(screen.getByRole('button', { name: 'App search' }));

test('the search icon turns the title into a search field that says where it searches', () => {
  render(<SearchHarness />);
  fireEvent.click(screen.getByRole('button', { name: 'Search' }));
  const search = screen.getByRole('searchbox', { name: 'Search conversations' });
  expect(search).toHaveFocus();
  expect(screen.queryByRole('heading', { level: 2 })).not.toBeInTheDocument();
  expect(screen.getByText('in all channels')).toBeInTheDocument();
  // The search icon is the field now; Filter stays beside it.
  expect(screen.queryByRole('button', { name: 'Search' })).not.toBeInTheDocument();
  expect(follows(search, screen.getByText('Filter'))).toBe(true);
});

test('a search inside a channel says it searches that channel', () => {
  render(<SearchHarness initial={{ channel_id: zalo.id, search: 'refund', status: 'all' }} />);
  expect(screen.getByRole('searchbox', { name: 'Search conversations' })).toHaveValue('refund');
  expect(screen.getByText('in Zalo OA')).toBeInTheDocument();
});

test('the app\'s Search opens the search field with the focus in it, and the words typed search every status', async () => {
  const onChange = vi.fn();
  render(<SearchHarness onChange={onChange} />);
  expect(screen.queryByRole('searchbox')).not.toBeInTheDocument();
  openSearch();
  const search = screen.getByRole('searchbox', { name: 'Search conversations' });
  expect(search).toHaveFocus();
  fireEvent.change(search, { target: { value: 'invoice' } });
  await waitFor(() => expect(onChange).toHaveBeenLastCalledWith({ search: 'invoice', status: 'all' }));
});

test('Escape on an empty search field brings the title back and the focus to the search icon; so does the focus leaving the header', () => {
  render(<><SearchHarness /><button type="button">Elsewhere</button></>);
  openSearch();
  fireEvent.keyDown(screen.getByRole('searchbox', { name: 'Search conversations' }), { key: 'Escape' });
  expect(screen.queryByRole('searchbox')).not.toBeInTheDocument();
  expect(title()).toHaveTextContent(/^All channels$/);
  expect(screen.getByRole('button', { name: 'Search' })).toHaveFocus();
  openSearch();
  fireEvent.blur(screen.getByRole('searchbox', { name: 'Search conversations' }), { relatedTarget: screen.getByRole('button', { name: 'Elsewhere' }) });
  expect(screen.queryByRole('searchbox')).not.toBeInTheDocument();
});

test('Escape keeps an empty search field open while a filter is on', () => {
  render(<SearchHarness initial={{ priority: 'urgent' }} />);
  openSearch();
  fireEvent.keyDown(screen.getByRole('searchbox', { name: 'Search conversations' }), { key: 'Escape' });
  expect(screen.getByRole('searchbox', { name: 'Search conversations' })).toBeInTheDocument();
});

test('the clear button empties the search and brings the title back', () => {
  const onChange = vi.fn();
  render(<SearchHarness initial={{ search: 'refund', status: 'all' }} onChange={onChange} />);
  fireEvent.click(screen.getByRole('button', { name: 'Clear search' }));
  expect(onChange).toHaveBeenLastCalledWith({ status: 'all' });
  expect(screen.queryByRole('searchbox')).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Search' })).toHaveFocus();
});

test('a search field with words in it stays open when the focus leaves it', () => {
  render(<><SearchHarness /><button type="button">Elsewhere</button></>);
  openSearch();
  const search = screen.getByRole('searchbox', { name: 'Search conversations' });
  fireEvent.change(search, { target: { value: 'refund' } });
  fireEvent.blur(search, { relatedTarget: screen.getByRole('button', { name: 'Elsewhere' }) });
  expect(screen.getByRole('searchbox', { name: 'Search conversations' })).toHaveValue('refund');
});

test('while rows are ticked, select all and the bulk actions take the status row; the title stays', () => {
  render(<InboxFilters tags={tags} query={{}} selection={<div role="toolbar" aria-label="Bulk actions" />} />);
  expect(screen.getByRole('toolbar', { name: 'Bulk actions' })).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /^Status: / })).not.toBeInTheDocument();
  expect(screen.queryByRole('combobox', { name: 'Sort' })).not.toBeInTheDocument();
  expect(title()).toHaveTextContent(/^All channels$/);
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
  render(<SearchHarness onChange={onChange} />);
  openSearch();
  fireEvent.change(screen.getByRole('searchbox', { name: 'Search conversations' }), { target: { value: 'invoice' } });
  await waitFor(() => expect(onChange).toHaveBeenLastCalledWith({ search: 'invoice', status: 'all' }));
  expect(picker()).toHaveAccessibleName('Status: All statuses');
});

test('a status the agent picked stays when they search', async () => {
  const onChange = vi.fn();
  render(<SearchHarness onChange={onChange} />);
  fireEvent.click(within(openStatusMenu()).getByRole('menuitemradio', { name: 'Pending' }));
  openSearch();
  fireEvent.change(screen.getByRole('searchbox', { name: 'Search conversations' }), { target: { value: 'invoice' } });
  await waitFor(() => expect(onChange).toHaveBeenLastCalledWith({ status: 'pending', search: 'invoice' }));
});

test('typing a word sends one search after the pause, not one per key', async () => {
  vi.useFakeTimers();
  try {
    const onChange = vi.fn();
    render(<SearchHarness onChange={onChange} />);
    openSearch();
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
