import { render, screen, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { permissionsOf } from '../../test-roles';
import { SettingsNav } from './settings-nav';
import type { Shown } from './settings-nav';

afterEach(() => { delete (Element.prototype as { scrollIntoView?: unknown }).scrollIntoView; });

const everything: Shown = { general: true, team: true, channels: true, savedReplies: true, exportData: true, billing: true, importData: true, ai: true, agents: true, conversations: true, invites: true, roles: true };
const links = (nav: HTMLElement) => within(nav).getAllByRole('link').map((link) => [link.textContent, link.getAttribute('href')]);

// On a narrow screen the nav is one sideways row; the page's own link is brought into it, also when the
// links before it arrive later (the workspace pages show once who is signed in is known).
test('the current page is brought into the row again when links appear before it', () => {
  const scrolled = vi.fn();
  Element.prototype.scrollIntoView = scrolled;
  const few: Shown = { general: false, team: false, channels: true, savedReplies: true, exportData: false, billing: false };
  const { rerender } = render(<SettingsNav page="/settings/channels" shown={few} />);
  const calls = scrolled.mock.calls.length;
  rerender(<SettingsNav page="/settings/channels" shown={{ ...few, general: true, team: true, ai: true }} />);
  expect(scrolled.mock.calls.length).toBeGreaterThan(calls);
  expect(scrolled.mock.contexts.at(-1)).toHaveTextContent('Channels');
});

// What each row of the menu says and opens, as the design lists it: nine items and one reserved, no sub-items.
const rowsOf = (nav: HTMLElement) => [...nav.querySelectorAll(':scope > div > ul > li > a')].map((link) =>
  [link.querySelector('.settings-nav-label')?.textContent, link.getAttribute('href'), link.querySelector('.settings-nav-tag')?.textContent ?? null]);

test('the menu is the nine items and the reserved one of the design, grouped, with no sub-items', () => {
  render(<SettingsNav page="/settings/account" shown={everything} />);
  const nav = screen.getByRole('navigation', { name: 'Settings sections' });
  expect(rowsOf(nav)).toEqual([
    ['Account', '/settings/account', null],
    ['General', '/settings/general', null], ['Members', '/settings/members', null], ['Channels', '/settings/channels', null], ['Response targets', '/settings/targets', null],
    ['AI & agents', '/settings/ai', null], ['Saved replies', '/settings/saved-replies', null], ['Conversation fields', '/settings/fields', 'Coming'],
    ['Data', '/settings/data', null], ['Plan and billing', '/settings/billing', null],
  ]);
  expect(within(nav).getByText('Workspace')).toBeInTheDocument();
  expect(within(nav).getByText('Data and billing')).toBeInTheDocument();
  expect(within(nav).getByRole('link', { name: 'Account' })).toHaveAttribute('aria-current', 'page');
  // No item is a section of another page, and no item lists pages under it.
  expect(links(nav).filter(([, href]) => href?.includes('#'))).toEqual([]);
  expect(nav.querySelectorAll('ul ul')).toHaveLength(0);
});

// Roles is part of Members and a channel's type home is part of Channels: each keeps its menu item marked as where the person is.
test('a role page keeps Members current, and a channel page keeps Channels current', () => {
  const { unmount } = render(<SettingsNav page="/settings/roles" shown={everything} />);
  expect(screen.getByRole('link', { name: 'Members' })).toHaveAttribute('aria-current', 'page');
  unmount();
  render(<SettingsNav page="/settings/channels/type/email" shown={everything} />);
  expect(screen.getByRole('link', { name: 'Channels' })).toHaveAttribute('aria-current', 'page');
});

// A page shows in the nav only when the person can see one of its sections, as sections did before.
test('a page whose every section the person cannot see is not in the nav', () => {
  render(<SettingsNav page="/settings/account" shown={{ general: false, team: false, channels: false, savedReplies: false, exportData: false, billing: false, importData: false, ai: false, agents: false, conversations: false, invites: false, roles: false }} />);
  const nav = screen.getByRole('navigation', { name: 'Settings sections' });
  expect(links(nav)).toEqual([['Account', '/settings/account']]);
  expect(within(nav).queryByText('Data and billing')).not.toBeInTheDocument();
});

test('General shows for conversation numbers alone, AI & agents for agent tokens alone, Data for the export alone', () => {
  render(<SettingsNav page="/settings/account" shown={{ general: false, team: false, channels: false, savedReplies: false, exportData: true, billing: false, importData: false, ai: false, agents: true, conversations: true }} />);
  const nav = screen.getByRole('navigation', { name: 'Settings sections' });
  expect(rowsOf(nav).map(([label]) => label)).toEqual(['Account', 'General', 'Response targets', 'AI & agents', 'Conversation fields', 'Data']);
});

// The help center's settings are in Knowledge base → Settings: the right to read the help center no longer puts General in the menu.
test('General does not show for the help center alone', () => {
  render(<SettingsNav page="/settings/account" shown={{ general: false, team: false, channels: false, savedReplies: false, exportData: false, billing: false, conversations: false, helpCenter: true } as Shown} />);
  expect(links(screen.getByRole('navigation', { name: 'Settings sections' })).map(([label]) => label)).toEqual(['Account']);
});

// What each role sees, from its permissions: an Agent has no workspace settings, no data and no billing.
function menuFor(role: 'admin' | 'agent', extra: Record<string, unknown> = {}) {
  const client = new QueryClient();
  client.setQueryData(['me'], { permissions: permissionsOf(role) });
  for (const [key, value] of Object.entries(extra)) client.setQueryData(JSON.parse(key), value);
  return render(<QueryClientProvider client={client}><SettingsNav page="/settings/account" shown={{ ...everything, billing: role === 'admin' }} /></QueryClientProvider>);
}
test('an admin sees every item and an agent everything but Data and Plan and billing', () => {
  const admin = menuFor('admin');
  expect(rowsOf(screen.getByRole('navigation', { name: 'Settings sections' })).map(([label, href]) => [label, href])).toEqual([
    ['Account', '/settings/account'], ['General', '/settings/general'], ['Members', '/settings/members'], ['Channels', '/settings/channels'], ['Response targets', '/settings/targets'],
    ['AI & agents', '/settings/ai'], ['Saved replies', '/settings/saved-replies'], ['Conversation fields', '/settings/fields'], ['Data', '/settings/data'], ['Plan and billing', '/settings/billing'],
  ]);
  admin.unmount();
  menuFor('agent');
  expect(rowsOf(screen.getByRole('navigation', { name: 'Settings sections' })).map(([label, href]) => [label, href])).toEqual([
    ['Account', '/settings/account'], ['General', '/settings/general'], ['Members', '/settings/members'], ['Channels', '/settings/channels'], ['Response targets', '/settings/targets'],
    ['AI & agents', '/settings/ai'], ['Saved replies', '/settings/saved-replies'], ['Conversation fields', '/settings/fields'],
  ]);
});

// One dot on Channels when a channel has a problem; one on Plan and billing only when a payment failed.
const problem = { types: [{ type: 'email', count: 1 }], items: [{ id: 'c1', type: 'email', name: 'Support', status: { state: 'needs_attention' } }] };
const fine = { types: [{ type: 'email', count: 1 }], items: [{ id: 'c1', type: 'email', name: 'Support', status: { state: 'working' } }] };
const dotOf = (name: string) => within(screen.getByRole('link', { name: new RegExp(`^${name}`) })).queryByRole('img');
test('Channels shows one dot when a channel needs attention and none when all work', () => {
  const { unmount } = menuFor('admin', { '["channels","counts"]': problem });
  expect(dotOf('Channels')).toHaveAccessibleName('A channel needs attention');
  unmount();
  menuFor('admin', { '["channels","counts"]': fine });
  expect(dotOf('Channels')).toBeNull();
});

test('Plan and billing shows a dot only when a payment failed', () => {
  const { unmount } = menuFor('admin', { '["billing"]': { status: 'past_due' } });
  expect(dotOf('Plan and billing')).toHaveAccessibleName('The last payment failed');
  unmount();
  menuFor('admin', { '["billing"]': { status: 'active' } });
  expect(dotOf('Plan and billing')).toBeNull();
});

// On a phone the nav is a list of its own (/settings) and each page goes back to it, as the inbox list does
// with a conversation: every page carries the way back, which the phone layout shows instead of the nav.
test('every settings page carries a way back to the list of pages, and the list itself does not', () => {
  const { unmount } = render(<SettingsNav page="/settings/members" shown={everything} />);
  expect(screen.getByRole('link', { name: 'All settings' })).toHaveAttribute('href', '/settings');
  expect(screen.getByRole('navigation', { name: 'Settings sections' })).not.toHaveClass('settings-nav-index');
  unmount();
  render(<SettingsNav page="/settings" shown={everything} />);
  expect(screen.queryByRole('link', { name: 'All settings' })).not.toBeInTheDocument();
  expect(screen.getByRole('navigation', { name: 'Settings sections' })).toHaveClass('settings-nav-index');
});
