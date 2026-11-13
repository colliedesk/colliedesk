import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import type { Me } from '../../api/schemas';
import { permissionsOf, rolesOf } from '../../test-roles';
import { CommandPalette } from './command-palette';
import { paletteEntries } from './palette-entries';
import type { PaletteSource } from './palette-entries';
import { rememberRecent } from './recent-items';

const api = vi.hoisted(() => ({ conversations: vi.fn() }));
const contacts = vi.hoisted(() => ({ searchContacts: vi.fn() }));
vi.mock('../../api/client', async (original) => ({ ...(await original<typeof import('../../api/client')>()), api }));
vi.mock('../contacts/contact-api', async (original) => ({ ...(await original<typeof import('../contacts/contact-api')>()), contactApi: contacts }));

const workspaceId = '22222222-2222-4222-8222-222222222222';
const meOf = (permissions: string[]): Me => ({
  agent: { id: '11111111-1111-4111-8111-111111111111', email: 'sam@example.com', display_name: 'Sam', roles: rolesOf('admin'), timezone_override: null },
  workspace: { id: workspaceId, slug: 'beta-desk', name: 'Beta Desk', timezone: 'UTC' }, timezone: 'UTC', token: null,
  permissions: permissions as Me['permissions'], plan: null, ai_features: { triage: false, draft: false }, onboarding_goal: null, setup_guide_hidden: false,
} as Me);
const admin = meOf(permissionsOf('admin'));

const conversation = (id: string, subject: string, status = 'open') => ({
  id, contact_id: '33333333-3333-4333-8333-333333333333', contact_name: 'Hana Sato', subject, status, priority: 'normal', assignee_id: null, tags: [],
  channel: 'email', origin: null, created_at: '2026-11-01T00:00:00Z', updated_at: '2026-11-01T00:00:00Z', latest_reply_number: 0,
});
const hana = { id: '33333333-3333-4333-8333-333333333333', name: 'Hana Sato', email: 'hana.sato@kumo.jp', organization: 'Kumo Travel', merged_into_id: null,
  created_at: '2026-01-02T00:00:00Z', updated_at: '2026-01-02T00:00:00Z' };
const found = {
  items: [conversation('44444444-4444-4444-8444-444444444441', 'Refund still not on my card'), conversation('44444444-4444-4444-8444-444444444442', 'Invoice copy for September', 'resolved')],
  page: 1, page_size: 5, status_counts: { open: 1, pending: 0, resolved: 1, closed: 0, spam: 3 },
};

beforeEach(() => {
  api.conversations.mockReset().mockResolvedValue(found);
  contacts.searchContacts.mockReset().mockResolvedValue({ items: [hana], has_more: false });
});
afterEach(() => localStorage.clear());

function open(source: Partial<PaletteSource> = {}, me: Me = admin) {
  const onNavigate = vi.fn();
  const onClose = vi.fn();
  const entries = paletteEntries(me, { inboxOpen: 23, mentionsUnread: 2, onNewConversation: vi.fn() });
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
    <CommandPalette workspaceId={workspaceId} {...entries} onNavigate={onNavigate} {...source} onClose={onClose} />
  </QueryClientProvider>);
  const input = screen.getByRole('combobox', { name: 'Search conversations, customers and pages' });
  return { input, onNavigate, onClose, list: () => screen.getByRole('listbox') };
}
const groupNames = () => within(screen.getByRole('listbox')).getAllByRole('group').map((group) => group.getAttribute('aria-label'));
const optionsOf = (group: string) => within(within(screen.getByRole('listbox')).getByRole('group', { name: group })).getAllByRole('option');

test('with nothing typed: Recent (newest first, at most five), Go to and Actions, and nothing is asked of the server', () => {
  for (let index = 1; index <= 6; index += 1) rememberRecent(workspaceId, { kind: 'conversation', id: `c${index}`, title: `Conversation ${index}`, detail: 'Hana Sato' });
  rememberRecent(workspaceId, { kind: 'customer', id: hana.id, title: 'Kumo Travel', detail: 'Team plan, 12 seats' });
  const { input } = open();
  expect(screen.getByRole('dialog', { name: 'Search' })).toBeInTheDocument();
  expect(input).toHaveFocus();
  expect(groupNames()).toEqual(['Recent', 'Go to', 'Actions']);
  expect(optionsOf('Recent').map((option) => option.textContent)).toEqual([
    expect.stringContaining('Kumo Travel'), expect.stringContaining('Conversation 6'), expect.stringContaining('Conversation 5'),
    expect.stringContaining('Conversation 4'), expect.stringContaining('Conversation 3')]);
  expect(optionsOf('Go to').map((option) => option.querySelector('.palette-name')?.textContent)).toEqual(['Inbox', 'Mentions', 'Knowledge base', 'Reports', 'Settings']);
  expect(optionsOf('Go to')[0]).toHaveTextContent('23 open');
  expect(optionsOf('Go to')[1]).toHaveTextContent('2 unread');
  expect(optionsOf('Actions').map((option) => option.querySelector('.palette-name')?.textContent)).toEqual(['New conversation', 'Invite teammates']);
  // The first row is the one Enter opens.
  expect(optionsOf('Recent')[0]).toHaveAttribute('aria-selected', 'true');
  expect(input).toHaveAttribute('aria-activedescendant', optionsOf('Recent')[0].id);
  expect(api.conversations).not.toHaveBeenCalled();
  expect(contacts.searchContacts).not.toHaveBeenCalled();
});

test('a browser that opened nothing yet shows no Recent heading', () => {
  open();
  expect(groupNames()).toEqual(['Go to', 'Actions']);
});

test('another workspace in the same browser keeps its own recent list', () => {
  rememberRecent('99999999-9999-4999-8999-999999999999', { kind: 'conversation', id: 'c1', title: 'Elsewhere' });
  open();
  expect(screen.queryByText('Elsewhere')).not.toBeInTheDocument();
});

test('typing groups conversations and customers, the match in bold, the first row chosen, and the last row sees all results', async () => {
  const { input } = open();
  fireEvent.change(input, { target: { value: 'hana' } });
  await screen.findByRole('group', { name: 'Customers' });
  expect(groupNames()).toEqual(['Conversations', 'Customers', null]);
  expect(api.conversations).toHaveBeenCalledWith('sort=due&search=hana&page=1&page_size=5&counts=true', expect.anything());
  expect(contacts.searchContacts).toHaveBeenCalledWith('hana', 5, expect.anything());
  const rows = optionsOf('Conversations');
  expect(rows[0]).toHaveTextContent('Refund still not on my card');
  expect(rows[0]).toHaveTextContent('Hana Sato · Open');
  expect(rows[1]).toHaveTextContent('Hana Sato · Resolved');
  expect(rows[0]).toHaveAttribute('aria-selected', 'true');
  expect(input).toHaveAttribute('aria-activedescendant', rows[0].id);
  const customer = optionsOf('Customers')[0];
  expect(customer.querySelector('.palette-name b')).toHaveTextContent(/^Hana$/);
  expect(customer).toHaveTextContent('hana.sato@kumo.jp · Kumo Travel');
  const options = within(screen.getByRole('listbox')).getAllByRole('option');
  const last = options[options.length - 1];
  expect(last).toHaveTextContent('See all results for “hana” in the inbox');
  // Spam is not searched by the inbox, so it is not counted.
  expect(last).toHaveTextContent('2 conversations');
});

test('See all results opens the inbox list searched in every status', async () => {
  const { input, onNavigate, onClose } = open();
  fireEvent.change(input, { target: { value: 'refund card' } });
  fireEvent.click(await screen.findByRole('option', { name: /See all results/ }));
  expect(onNavigate).toHaveBeenCalledWith('/?search=refund%20card&status=all');
  expect(onClose).toHaveBeenCalledWith(false);
});

test('Enter opens the chosen row, and the arrows move the choice', async () => {
  const { input, onNavigate } = open();
  fireEvent.change(input, { target: { value: 'hana' } });
  await screen.findByRole('group', { name: 'Customers' });
  fireEvent.keyDown(input, { key: 'Enter' });
  expect(onNavigate).toHaveBeenLastCalledWith('/conversations/44444444-4444-4444-8444-444444444441');
  fireEvent.keyDown(input, { key: 'ArrowDown' });
  fireEvent.keyDown(input, { key: 'ArrowDown' });
  expect(optionsOf('Customers')[0]).toHaveAttribute('aria-selected', 'true');
  fireEvent.keyDown(input, { key: 'Enter' });
  expect(onNavigate).toHaveBeenLastCalledWith(`/contacts/${hana.id}`);
  // Down from the last row wraps to the first, and up from the first to the last.
  fireEvent.keyDown(input, { key: 'ArrowDown' });
  fireEvent.keyDown(input, { key: 'ArrowDown' });
  expect(optionsOf('Conversations')[0]).toHaveAttribute('aria-selected', 'true');
  fireEvent.keyDown(input, { key: 'ArrowUp' });
  expect(screen.getByRole('option', { name: /See all results/ })).toHaveAttribute('aria-selected', 'true');
});

test('a place opens its page and an action runs; either closes the palette without taking the focus back', () => {
  const onNewConversation = vi.fn();
  const entries = paletteEntries(admin, { inboxOpen: 0, mentionsUnread: 0, onNewConversation });
  const { onNavigate, onClose } = open(entries);
  fireEvent.click(screen.getByRole('option', { name: /Reports/ }));
  expect(onNavigate).toHaveBeenCalledWith('/results');
  fireEvent.click(screen.getByRole('option', { name: /New conversation/ }));
  expect(onNewConversation).toHaveBeenCalledTimes(1);
  expect(onClose).toHaveBeenCalledWith(false);
  expect(onClose).not.toHaveBeenCalledWith(true);
});

test('Escape closes the palette and asks for the focus back', () => {
  const { input, onClose } = open();
  fireEvent.keyDown(input, { key: 'Escape' });
  expect(onClose).toHaveBeenCalledWith(true);
});

test('a session is offered only the places and actions its permissions allow, and is not searched where it may not read', async () => {
  const may = permissionsOf('agent').filter((name) => !['results:read', 'contacts:read', 'messages:read'].includes(name));
  const me = meOf(may);
  const entries = paletteEntries(me, { inboxOpen: 3, mentionsUnread: 1 });
  const { input } = open(entries, me);
  expect(optionsOf('Go to').map((option) => option.querySelector('.palette-name')?.textContent)).toEqual(['Inbox', 'Knowledge base', 'Settings']);
  // No pen without the right to start a conversation, no invite without team:invite.
  expect(screen.queryByRole('group', { name: 'Actions' })).not.toBeInTheDocument();
  fireEvent.change(input, { target: { value: 'hana' } });
  await screen.findByRole('group', { name: 'Conversations' });
  expect(contacts.searchContacts).not.toHaveBeenCalled();
  expect(screen.queryByRole('group', { name: 'Customers' })).not.toBeInTheDocument();
});

test('a session that may not read conversations searches no conversations and offers no inbox list', async () => {
  const me = meOf(['contacts:read']);
  const { input } = open(paletteEntries(me, {}), me);
  fireEvent.change(input, { target: { value: 'hana' } });
  await screen.findByRole('group', { name: 'Customers' });
  expect(api.conversations).not.toHaveBeenCalled();
  expect(screen.queryByRole('option', { name: /See all results/ })).not.toBeInTheDocument();
});

test('rows keep their place while the next results load', async () => {
  const { input } = open();
  fireEvent.change(input, { target: { value: 'hana' } });
  await screen.findByRole('group', { name: 'Customers' });
  api.conversations.mockReturnValue(new Promise(() => {}));
  contacts.searchContacts.mockReturnValue(new Promise(() => {}));
  fireEvent.change(input, { target: { value: 'hana s' } });
  await new Promise((resolve) => setTimeout(resolve, 400));
  expect(api.conversations).toHaveBeenCalledTimes(2);
  expect(optionsOf('Conversations')).toHaveLength(2);
  expect(optionsOf('Customers')).toHaveLength(1);
  expect(screen.getByRole('option', { name: /See all results/ })).toHaveTextContent('See all results for “hana s” in the inbox');
});

test('typing a page name finds the page too', async () => {
  api.conversations.mockResolvedValue({ ...found, items: [], status_counts: undefined });
  contacts.searchContacts.mockResolvedValue({ items: [], has_more: false });
  const { input } = open();
  fireEvent.change(input, { target: { value: 'sett' } });
  expect(await screen.findByRole('group', { name: 'Go to' })).toBeInTheDocument();
  expect(optionsOf('Go to').map((option) => option.querySelector('.palette-name')?.textContent)).toEqual(['Settings']);
});
