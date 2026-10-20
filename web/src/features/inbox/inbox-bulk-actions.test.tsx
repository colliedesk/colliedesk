import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import type { Conversation } from '../../api/schemas';
import { InboxPage } from './inbox-page';

const row = (id: string, subject: string): Conversation => ({
  id, contact_id: '22222222-2222-4222-8222-222222222222', contact_name: 'Mai Linh', subject, status: 'open', priority: 'normal',
  assignee_id: null, tags: [], channel: 'email', origin: null, created_at: '2026-10-14T00:00:00Z', updated_at: '2026-10-14T00:00:00Z',
});
const alpha = row('11111111-1111-4111-8111-111111111111', 'Alpha request');
const beta = row('33333333-3333-4333-8333-333333333333', 'Beta request');
const gamma = row('55555555-5555-4555-8555-555555555555', 'Gamma request');
const me = '66666666-6666-4666-8666-666666666666';
const colleague = { id: '77777777-7777-4777-8777-777777777777', email: 'sam@example.com', display_name: 'Sam Teammate', role: 'agent' as const, timezone_override: null, deactivated_at: null, last_active_at: null, open_conversations: 0 };
const departed = { ...colleague, id: '99999999-9999-4999-8999-999999999990', email: 'gone@example.com', display_name: 'Gone Teammate', deactivated_at: '2026-10-10T00:00:00Z' };
const billing = { id: '88888888-8888-4888-8888-888888888888', name: 'Billing', color: '#64748b' };

function renderInbox(onBulkUpdate = vi.fn().mockResolvedValue({ updated: 2 })) {
  render(<InboxPage conversations={[alpha, beta, gamma]} agents={[colleague]} tags={[billing]} currentAgentId={me} onBulkUpdate={onBulkUpdate} />);
  return onBulkUpdate;
}
const pick = (subject: RegExp) => fireEvent.click(screen.getByRole('checkbox', { name: subject }));
const bar = () => screen.getByRole('group', { name: /bulk actions/i });

test('shows no action bar until a row is selected', () => {
  renderInbox();
  expect(screen.queryByRole('group', { name: /bulk actions/i })).not.toBeInTheDocument();
  pick(/alpha request/i);
  expect(within(bar()).getByText('1 selected')).toBeInTheDocument();
});

test('the action bar offers resolve, status, assign, tags and clear as named icon controls, and no delete', () => {
  renderInbox();
  pick(/alpha request/i);
  pick(/beta request/i);
  const toolbar = within(bar());
  expect(toolbar.getByText('2 selected')).toBeInTheDocument();
  for (const label of ['Resolve', 'Clear selection']) expect(toolbar.getByRole('button', { name: label })).toHaveAttribute('data-tip', label);
  for (const label of ['Set status', 'Assign to', 'Tags']) expect(toolbar.getByLabelText(label, { selector: 'summary' })).toHaveAttribute('data-tip', label);
  expect(toolbar.queryByText('Add tag')).not.toBeInTheDocument();
  expect(toolbar.queryByText('Remove tag')).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /delete/i })).not.toBeInTheDocument();
});

test('resolve changes the selection in one press', async () => {
  const onBulkUpdate = renderInbox();
  pick(/alpha request/i);
  pick(/gamma request/i);
  fireEvent.click(within(bar()).getByRole('button', { name: 'Resolve' }));
  expect(onBulkUpdate).toHaveBeenCalledWith([alpha.id, gamma.id], { status: 'resolved' });
  expect(await screen.findByText('Updated 2 conversations')).toBeInTheDocument();
});

test('the filters stay in view while rows are selected', () => {
  renderInbox();
  pick(/alpha request/i);
  expect(screen.getByRole('searchbox', { name: 'Search conversations' })).toBeVisible();
});

test('Escape in the action bar clears the selection and returns focus to select all', () => {
  renderInbox();
  pick(/alpha request/i);
  fireEvent.keyDown(within(bar()).getByRole('button', { name: 'Resolve' }), { key: 'Escape' });
  expect(screen.queryByRole('group', { name: /bulk actions/i })).not.toBeInTheDocument();
  expect(screen.getByRole('checkbox', { name: 'Select all on this page' })).toHaveFocus();
});

test('Escape inside an open menu closes only the menu', () => {
  renderInbox();
  pick(/alpha request/i);
  const status = within(bar()).getByLabelText('Set status', { selector: 'summary' });
  const menu = status.closest('details')!;
  menu.open = true;
  fireEvent.keyDown(within(menu).getByRole('button', { name: 'Pending' }), { key: 'Escape' });
  expect(menu.open).toBe(false);
  expect(within(bar()).getByText('1 selected')).toBeInTheDocument();
});

test('select all on this page selects every row, and clear selection removes the bar', () => {
  renderInbox();
  fireEvent.click(screen.getByRole('checkbox', { name: 'Select all on this page' }));
  expect(within(bar()).getByText('3 selected')).toBeInTheDocument();
  fireEvent.click(within(bar()).getByRole('button', { name: 'Clear selection' }));
  expect(screen.queryByRole('group', { name: /bulk actions/i })).not.toBeInTheDocument();
  expect(screen.getByRole('checkbox', { name: /alpha request/i })).not.toBeChecked();
});

test('set status sends the selected ids once, then clears the selection and announces the result', async () => {
  const onBulkUpdate = renderInbox();
  pick(/alpha request/i);
  pick(/beta request/i);
  fireEvent.click(within(within(bar()).getByRole('group', { name: 'Set status' })).getByRole('button', { name: 'Resolved' }));
  expect(onBulkUpdate).toHaveBeenCalledTimes(1);
  expect(onBulkUpdate).toHaveBeenCalledWith([alpha.id, beta.id], { status: 'resolved' });
  expect(await screen.findByText('Updated 2 conversations')).toHaveAttribute('aria-live', 'polite');
  expect(screen.queryByRole('group', { name: /bulk actions/i })).not.toBeInTheDocument();
  expect(screen.getByRole('checkbox', { name: /alpha request/i })).not.toBeChecked();
});

test('says "1 conversation" for a single row', async () => {
  renderInbox(vi.fn().mockResolvedValue({ updated: 1 }));
  pick(/gamma request/i);
  fireEvent.click(within(within(bar()).getByRole('group', { name: 'Set status' })).getByRole('button', { name: 'Closed' }));
  expect(await screen.findByText('Updated 1 conversation')).toHaveAttribute('aria-live', 'polite');
});

test('assign to me, to a teammate and to nobody', async () => {
  const onBulkUpdate = renderInbox();
  pick(/alpha request/i);
  const assign = () => within(within(bar()).getByRole('group', { name: 'Assign to' }));
  fireEvent.click(assign().getByRole('button', { name: 'Me' }));
  await waitFor(() => expect(onBulkUpdate).toHaveBeenLastCalledWith([alpha.id], { assignee_id: me }));
  pick(/alpha request/i);
  fireEvent.click(assign().getByRole('button', { name: 'Sam Teammate' }));
  await waitFor(() => expect(onBulkUpdate).toHaveBeenLastCalledWith([alpha.id], { assignee_id: colleague.id }));
  pick(/alpha request/i);
  fireEvent.click(assign().getByRole('button', { name: 'Unassigned' }));
  await waitFor(() => expect(onBulkUpdate).toHaveBeenLastCalledWith([alpha.id], { assignee_id: null }));
});

test('the tags menu shows which selected rows carry a tag and toggles it for all of them', async () => {
  const vip = { id: '44444444-4444-4444-8444-444444444444', name: 'VIP', color: '#64748b' };
  const spam = { id: '12121212-1212-4121-8121-121212121212', name: 'Spam', color: '#64748b' };
  const onBulkUpdate = vi.fn().mockResolvedValue({ updated: 2 });
  const tagged = { ...alpha, tags: [billing, vip] };
  const half = { ...beta, tags: [billing] };
  render(<InboxPage conversations={[tagged, half, gamma]} tags={[billing, vip, spam]} onBulkUpdate={onBulkUpdate} />);
  pick(/alpha request/i);
  pick(/beta request/i);
  const tagsMenu = () => within(within(bar()).getByRole('group', { name: 'Tags' }));
  expect(tagsMenu().getByRole('button', { name: 'Billing' })).toHaveAttribute('aria-pressed', 'true');
  expect(tagsMenu().getByRole('button', { name: 'VIP' })).toHaveAttribute('aria-pressed', 'mixed');
  expect(tagsMenu().getByRole('button', { name: 'Spam' })).toHaveAttribute('aria-pressed', 'false');
  fireEvent.click(tagsMenu().getByRole('button', { name: 'Billing' }));
  await waitFor(() => expect(onBulkUpdate).toHaveBeenLastCalledWith([alpha.id, beta.id], { remove_tag_ids: [billing.id] }));
  pick(/alpha request/i);
  pick(/beta request/i);
  fireEvent.click(tagsMenu().getByRole('button', { name: 'VIP' }));
  await waitFor(() => expect(onBulkUpdate).toHaveBeenLastCalledWith([alpha.id, beta.id], { add_tag_ids: [vip.id] }));
});

test('a refused update shows an alert next to the bar and keeps the selection', async () => {
  renderInbox(vi.fn().mockRejectedValue(new Error('refused')));
  pick(/alpha request/i);
  pick(/beta request/i);
  fireEvent.click(within(within(bar()).getByRole('group', { name: 'Set status' })).getByRole('button', { name: 'Pending' }));
  expect(await screen.findByRole('alert')).toHaveTextContent(/nothing was changed/i);
  expect(within(bar()).getByText('2 selected')).toBeInTheDocument();
  expect(screen.getByRole('checkbox', { name: /beta request/i })).toBeChecked();
});

test('selecting a row does not open its conversation', () => {
  const onSelect = vi.fn();
  render(<InboxPage conversations={[alpha]} onSelect={onSelect} />);
  pick(/alpha request/i);
  expect(onSelect).not.toHaveBeenCalled();
});

test('a row checkbox is a native checkbox in the tab order, so Space toggles it', () => {
  renderInbox();
  const box = screen.getByRole('checkbox', { name: /alpha request/i });
  expect(box.tagName).toBe('INPUT');
  expect(box).not.toHaveAttribute('tabindex', '-1');
});

test('rows that leave the list drop out of the selection', () => {
  const onBulkUpdate = vi.fn();
  const { rerender } = render(<InboxPage conversations={[alpha, beta]} onBulkUpdate={onBulkUpdate} />);
  pick(/alpha request/i);
  pick(/beta request/i);
  rerender(<InboxPage conversations={[beta]} onBulkUpdate={onBulkUpdate} />);
  expect(within(bar()).getByText('1 selected')).toBeInTheDocument();
});

test('the bulk result line is a live region from the start, yet an idle inbox shows no status', () => {
  renderInbox();
  expect(document.querySelector('.bulk-message[aria-live="polite"]')).toBeEmptyDOMElement();
  expect(screen.queryByRole('status')).not.toBeInTheDocument();
});

test('the inbox heading stays in the page while rows are selected', () => {
  renderInbox();
  pick(/alpha request/i);
  expect(screen.getByRole('heading', { level: 1, name: 'Inbox' })).toBeInTheDocument();
});

test('rows with the same subject still have different checkbox names', () => {
  const later = { ...row('99999999-9999-4999-8999-999999999999', 'Alpha request'), updated_at: '2026-10-14T05:30:00Z' };
  render(<InboxPage conversations={[alpha, later]} />);
  const names = screen.getAllByRole('checkbox', { name: /^select alpha request/i }).map((box) => box.getAttribute('aria-label'));
  expect(new Set(names).size).toBe(2);
});

test('after a successful change focus moves to select all on this page', async () => {
  renderInbox();
  pick(/alpha request/i);
  fireEvent.click(within(within(bar()).getByRole('group', { name: 'Set status' })).getByRole('button', { name: 'Resolved' }));
  await waitFor(() => expect(screen.getByRole('checkbox', { name: 'Select all on this page' })).toHaveFocus());
});

test('after clear selection focus moves to select all on this page', () => {
  renderInbox();
  pick(/alpha request/i);
  fireEvent.click(within(bar()).getByRole('button', { name: 'Clear selection' }));
  expect(screen.getByRole('checkbox', { name: 'Select all on this page' })).toHaveFocus();
});

test('with no rows left after a change, focus moves to the list pane', async () => {
  let rerenderInbox!: (conversations: Conversation[]) => void;
  const onBulkUpdate = vi.fn(async () => { rerenderInbox([]); });
  const view = render(<InboxPage conversations={[alpha]} onBulkUpdate={onBulkUpdate} />);
  rerenderInbox = (conversations) => view.rerender(<InboxPage conversations={conversations} onBulkUpdate={onBulkUpdate} />);
  pick(/alpha request/i);
  fireEvent.click(within(within(bar()).getByRole('group', { name: 'Set status' })).getByRole('button', { name: 'Closed' }));
  await waitFor(() => expect(screen.getByRole('region', { name: /conversation list/i })).toHaveFocus());
});

test('after a refused change focus is on the menu that was used', async () => {
  renderInbox(vi.fn().mockRejectedValue(new Error('refused')));
  pick(/alpha request/i);
  fireEvent.click(within(within(bar()).getByRole('group', { name: 'Set status' })).getByRole('button', { name: 'Pending' }));
  await screen.findByRole('alert');
  expect(within(bar()).getByLabelText('Set status', { selector: 'summary' })).toHaveFocus();
});

test('the assign menu lists only active agents', () => {
  render(<InboxPage conversations={[alpha]} agents={[colleague, departed]} currentAgentId={me} onBulkUpdate={vi.fn()} />);
  pick(/alpha request/i);
  const assign = within(within(bar()).getByRole('group', { name: 'Assign to' }));
  expect(assign.getByRole('button', { name: 'Sam Teammate' })).toBeInTheDocument();
  expect(assign.queryByRole('button', { name: 'Gone Teammate' })).not.toBeInTheDocument();
});
