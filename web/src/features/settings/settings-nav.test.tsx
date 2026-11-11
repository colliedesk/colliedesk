import { render, screen, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
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

// One item per page, in the order of the design: Account; the workspace; then data and billing.
test('each item of the nav opens its own page under /settings, grouped as in the design', () => {
  render(<SettingsNav page="/settings/account" shown={everything} />);
  const nav = screen.getByRole('navigation', { name: 'Settings sections' });
  const top = [...nav.querySelectorAll(':scope > div > ul > li > a')].map((link) => [link.textContent, link.getAttribute('href')]);
  expect(top).toEqual([
    ['Account', '/settings/account'],
    ['General', '/settings/general'], ['Members', '/settings/members'], ['Channels', '/settings/channels'], ['Response targets', '/settings/targets'],
    ['AI & agents', '/settings/ai'], ['Saved replies', '/settings/saved-replies'],
    ['Data', '/settings/data'], ['Plan and billing', '/settings/billing'],
  ]);
  expect(within(nav).getByText('Workspace')).toBeInTheDocument();
  expect(within(nav).getByText('Data and billing')).toBeInTheDocument();
  expect(within(nav).getByRole('link', { name: 'Account' })).toHaveAttribute('aria-current', 'page');
  // No item is a section of another page any more.
  expect(links(nav).filter(([, href]) => href?.includes('#'))).toEqual([]);
});

// Roles is part of Members: a role's own page keeps Members marked as where the person is.
test('Members stays the current page on a role page', () => {
  render(<SettingsNav page="/settings/roles" shown={everything} />);
  expect(screen.getByRole('link', { name: 'Members' })).toHaveAttribute('aria-current', 'page');
});

// A page shows in the nav only when the person can see one of its sections, as sections did before.
test('a page whose every section the person cannot see is not in the nav', () => {
  render(<SettingsNav page="/settings/account" shown={{ general: false, team: false, channels: false, savedReplies: false, exportData: false, billing: false, importData: false, ai: false, agents: false, conversations: false, invites: false, roles: false, helpCenter: false }} />);
  const nav = screen.getByRole('navigation', { name: 'Settings sections' });
  expect(links(nav)).toEqual([['Account', '/settings/account']]);
  expect(within(nav).queryByText('Data and billing')).not.toBeInTheDocument();
});

test('General shows for conversation numbers alone, AI & agents for agent tokens alone, Data for the export alone', () => {
  render(<SettingsNav page="/settings/account" shown={{ general: false, team: false, channels: false, savedReplies: false, exportData: true, billing: false, importData: false, ai: false, agents: true, conversations: true }} />);
  const nav = screen.getByRole('navigation', { name: 'Settings sections' });
  expect(links(nav).map(([label]) => label)).toEqual(['Account', 'General', 'Response targets', 'AI & agents', 'Data']);
});

// Email, Web form and Help chat are named places under Channels: Settings → Email is one click from any settings page.
test('Channels lists Email, Web form and Help chat under it, each opening its type home', () => {
  const shown: Shown = { general: true, team: true, channels: true, savedReplies: true, exportData: true, billing: false };
  render(<SettingsNav page="/settings/channels/type/email" shown={shown} />);
  const channels = screen.getByRole('link', { name: 'Channels' });
  expect(channels).toHaveAttribute('href', '/settings/channels');
  const group = screen.getByRole('list', { name: 'Channels' });
  expect(within(group).getAllByRole('link').map((link) => [link.textContent, link.getAttribute('href')])).toEqual([
    ['Email', '/settings/channels/type/email'], ['Web form', '/settings/channels/type/web_form'], ['Help chat', '/settings/channels/type/help_chat'], ['Zalo OA', '/settings/channels/type/zalo'],
  ]);
  expect(within(group).getByRole('link', { name: 'Email' })).toHaveAttribute('aria-current', 'page');
  expect(channels).not.toHaveAttribute('aria-current');
});

test('the channel list itself stays the current page at /settings/channels', () => {
  render(<SettingsNav page="/settings/channels" shown={{ general: false, team: false, channels: true, savedReplies: true, exportData: false, billing: false }} />);
  expect(screen.getByRole('link', { name: 'Channels' })).toHaveAttribute('aria-current', 'page');
  expect(screen.getByRole('link', { name: 'Email' })).not.toHaveAttribute('aria-current');
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
