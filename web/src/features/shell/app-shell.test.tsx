import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, within } from '@testing-library/react';
import type { ReactNode } from 'react';
import { expect, test, vi } from 'vitest';
import { permissionsOf, rolesOf } from '../../test-roles';
import { AppShell } from './app-shell';
import type { PaletteSource } from './palette-entries';
import type { SidebarChannel, SidebarView } from './sidebar-items';
import { sidebarStorageKey } from './sidebar-layout';

const me = {
  agent: { id: '11111111-1111-4111-8111-111111111111', email: 'sam@example.com', display_name: '', roles: rolesOf('agent'), timezone_override: null },
  workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'beta-desk', name: 'Beta Desk', timezone: 'UTC' },
  timezone: 'UTC',
  token: null, permissions: permissionsOf('agent'),
  plan: null,
  ai_features: { triage: false, draft: false }, onboarding_goal: null, setup_guide_hidden: false,
};

test('shows the page content next to the rail', () => {
  render(<AppShell me={me}><p>inbox</p></AppShell>);
  expect(screen.getByText('inbox')).toBeInTheDocument();
});

test('a signed-out visitor is told how to get back in instead of seeing an empty workspace', () => {
  render(<AppShell signedOut><p>inbox</p></AppShell>);
  expect(screen.getByRole('alert')).toHaveTextContent('You are signed out. Ask your workspace admin for a new link.');
  expect(screen.queryByText('inbox')).not.toBeInTheDocument();
});

test('a signed-out visitor of a self-hosted instance keeps the pair hint', () => {
  render(<AppShell signedOut><p>inbox</p></AppShell>);
  expect(screen.getByText(/collie pair/)).toBeInTheDocument();
  // The command is a value to copy, so it stands alone; the sentence before it stays short.
  expect(screen.getByText('collie pair --workspace <slug> --email <your email>').tagName).toBe('CODE');
});

test('the workspace and the signed-in address sit in an always-visible bar, not in a tooltip', () => {
  render(<AppShell me={me}><p>inbox</p></AppShell>);
  const bar = screen.getByRole('region', { name: 'Signed in as' });
  expect(bar).toHaveTextContent('Beta Desk');
  expect(bar).toHaveTextContent('sam@example.com');
  expect(bar.closest('[data-tip]')).toBeNull();
});

test('Escape dismisses open tooltips and the next pointer move brings them back', () => {
  render(<AppShell me={me}><p>inbox</p></AppShell>);
  fireEvent.keyDown(document.body, { key: 'Escape' });
  expect(document.documentElement).toHaveAttribute('data-tips-off');
  fireEvent.pointerMove(document.body);
  expect(document.documentElement).not.toHaveAttribute('data-tips-off');
});

test('a signed-out visitor gets no navigation and no account menu that lead nowhere', () => {
  render(<AppShell signedOut links={<a href="/">Inbox</a>}><p>inbox</p></AppShell>);
  expect(screen.queryByRole('link', { name: 'Inbox' })).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /^Account/ })).not.toBeInTheDocument();
});

test('the avatar opens an account menu with who is signed in and Sign out', () => {
  const signOut = vi.fn();
  render(<AppShell me={me} onSignOut={signOut}><p>inbox</p></AppShell>);
  const button = screen.getByRole('button', { name: /^Account/ });
  expect(button).toHaveAttribute('aria-expanded', 'false');
  fireEvent.click(button);
  expect(button).toHaveAttribute('aria-expanded', 'true');
  const menu = screen.getByRole('group', { name: 'Account' });
  // Without a display name the person is their address.
  expect(menu).toHaveTextContent('sam@example.com');
  expect(menu).toHaveTextContent('Agent');
  fireEvent.click(screen.getByRole('button', { name: 'Sign out' }));
  expect(signOut).toHaveBeenCalledTimes(1);
});

test('the account menu names the person and their role, and leaves the workspace and address to the bar above', () => {
  render(<AppShell me={{ ...me, agent: { ...me.agent, display_name: 'Sam Lee', roles: rolesOf('admin') } }} onSignOut={() => {}}><p>inbox</p></AppShell>);
  fireEvent.click(screen.getByRole('button', { name: /^Account/ }));
  const menu = screen.getByRole('group', { name: 'Account' });
  expect(menu).toHaveTextContent('Sam Lee');
  expect(menu).toHaveTextContent('Admin');
  expect(menu).not.toHaveTextContent('sam@example.com');
  expect(menu).not.toHaveTextContent('Beta Desk');
  expect(screen.getAllByRole('button').filter((button) => menu.contains(button)).map((button) => button.textContent)).toEqual(['Report a problem', 'Sign out']);
});

test('Escape closes the account menu and returns focus to the avatar', () => {
  render(<AppShell me={me} onSignOut={() => {}}><p>inbox</p></AppShell>);
  const button = screen.getByRole('button', { name: /^Account/ });
  fireEvent.click(button);
  fireEvent.keyDown(screen.getByRole('button', { name: 'Sign out' }), { key: 'Escape' });
  expect(screen.queryByRole('button', { name: 'Sign out' })).not.toBeInTheDocument();
  expect(button).toHaveFocus();
});

test('a failed sign out says so in the menu', () => {
  render(<AppShell me={me} onSignOut={() => {}} signOutFailed><p>inbox</p></AppShell>);
  fireEvent.click(screen.getByRole('button', { name: /^Account/ }));
  expect(screen.getByRole('alert')).toHaveTextContent('Sign out failed. Try again.');
});

test('the account menu closes when focus moves out of it', () => {
  render(<AppShell me={me} onSignOut={() => {}}><button type="button">elsewhere</button></AppShell>);
  fireEvent.click(screen.getByRole('button', { name: /^Account/ }));
  fireEvent.blur(screen.getByRole('button', { name: 'Sign out' }), { relatedTarget: screen.getByRole('button', { name: 'elsewhere' }) });
  expect(screen.queryByRole('button', { name: 'Sign out' })).not.toBeInTheDocument();
});

test('Sign out cannot be sent twice while the first request runs', () => {
  render(<AppShell me={me} onSignOut={() => {}} signingOut><p>inbox</p></AppShell>);
  fireEvent.click(screen.getByRole('button', { name: /^Account/ }));
  expect(screen.getByRole('button', { name: 'Sign out' })).toBeDisabled();
});

test('Report a problem in the account menu opens the dialog, and closing it returns focus to the avatar', async () => {
  render(<AppShell me={me} appVersion="0.0.41" onSignOut={() => {}}><p>inbox</p></AppShell>);
  const avatar = screen.getByRole('button', { name: /^Account/ });
  fireEvent.click(avatar);
  fireEvent.click(screen.getByRole('button', { name: 'Report a problem' }));
  const dialog = await screen.findByRole('dialog', { name: 'Report a problem' });
  expect(screen.queryByRole('group', { name: 'Account' })).not.toBeInTheDocument();
  fireEvent.click(screen.getByText('What will be sent'));
  expect(dialog).toHaveTextContent('0.0.41');
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  expect(avatar).toHaveFocus();
});

test('the sidebar starts open with page names, folds into its rail and opens again, and the browser keeps the choice', () => {
  localStorage.removeItem(sidebarStorageKey);
  const { container, unmount } = render(<AppShell me={me}><p>inbox</p></AppShell>);
  expect(container.querySelector('.app-shell')).toHaveAttribute('data-sidebar', 'expanded');
  fireEvent.click(screen.getByRole('button', { name: 'Collapse sidebar' }));
  expect(container.querySelector('.app-shell')).toHaveAttribute('data-sidebar', 'collapsed');
  expect(JSON.parse(localStorage.getItem(sidebarStorageKey)!)).toEqual({ width: 248, collapsed: true });
  unmount();
  const again = render(<AppShell me={me}><p>inbox</p></AppShell>);
  expect(again.container.querySelector('.app-shell')).toHaveAttribute('data-sidebar', 'collapsed');
  fireEvent.click(screen.getByRole('button', { name: 'Expand sidebar' }));
  expect(JSON.parse(localStorage.getItem(sidebarStorageKey)!)).toEqual({ width: 248, collapsed: false });
});

test('a browser that kept a folded sidebar from before the sidebar opened by default starts open, with its width', () => {
  localStorage.removeItem(sidebarStorageKey);
  localStorage.setItem('colliedesk.sidebar', JSON.stringify({ width: 300, collapsed: true }));
  const { container } = render(<AppShell me={me}><p>inbox</p></AppShell>);
  expect(container.querySelector('.app-shell')).toHaveAttribute('data-sidebar', 'expanded');
  expect(screen.getByRole('separator', { name: 'Resize sidebar' })).toHaveAttribute('aria-valuenow', '300');
  localStorage.removeItem('colliedesk.sidebar');
});

test('the sidebar edge resizes with the keyboard within its limits and remembers the width', () => {
  localStorage.setItem(sidebarStorageKey, JSON.stringify({ width: 9999, collapsed: false }));
  const { container } = render(<AppShell me={me}><p>inbox</p></AppShell>);
  const edge = screen.getByRole('separator', { name: 'Resize sidebar' });
  expect(edge).toHaveAttribute('aria-valuenow', '360');
  fireEvent.keyDown(edge, { key: 'ArrowLeft' });
  expect(edge).toHaveAttribute('aria-valuenow', '344');
  expect((container.querySelector('.app-shell') as HTMLElement).style.getPropertyValue('--sidebar-user-width')).toBe('344px');
  fireEvent.keyDown(edge, { key: 'Home' });
  expect(edge).toHaveAttribute('aria-valuenow', '56');
  fireEvent.doubleClick(edge);
  expect(JSON.parse(localStorage.getItem(sidebarStorageKey)!)).toEqual({ width: 248, collapsed: false });
  localStorage.removeItem(sidebarStorageKey);
});

test('the sidebar head names the workspace beside the logo, and the person at its foot shows their name and address', () => {
  localStorage.removeItem(sidebarStorageKey);
  render(<AppShell me={{ ...me, agent: { ...me.agent, display_name: 'Sam Lee' } }} brand={<a href="/">Colliedesk</a>}><p>inbox</p></AppShell>);
  const identity = screen.getByRole('region', { name: 'Signed in as' });
  const rail = screen.getByRole('navigation', { name: 'Main' });
  // The open sidebar is what names the workspace and the person: no second bar repeats them.
  expect(identity).toContainElement(rail);
  expect(within(rail).getByText('Beta Desk')).toBeInTheDocument();
  const person = within(rail).getByRole('button', { name: /^Account/ });
  expect(person).toHaveTextContent('Sam Lee');
  expect(person).toHaveTextContent('sam@example.com');
  expect(screen.getAllByRole('region', { name: 'Signed in as' })).toHaveLength(1);
});

test('the workspace name in the sidebar head is plain text with the full name in its tooltip, and takes no tab stop', () => {
  localStorage.removeItem(sidebarStorageKey);
  render(<AppShell me={{ ...me, workspace: { ...me.workspace, name: 'Northwind Outdoors' } }} brand={<a href="/">Colliedesk</a>}><p>inbox</p></AppShell>);
  const rail = screen.getByRole('navigation', { name: 'Main' });
  const name = within(rail).getByText('Northwind Outdoors');
  // The text is the whole name (the ellipsis is drawn by the style), so a screen reader reads it once.
  expect(name.closest('[data-tip]')).toHaveAttribute('data-tip', 'Northwind Outdoors');
  expect(rail.querySelector('.workspace-name')).not.toHaveAttribute('tabindex');
  expect(rail.querySelector('.workspace-name')).not.toHaveAttribute('role');
  expect(rail.querySelector('.workspace-name')).not.toHaveAttribute('aria-label');
  expect(within(rail).queryByRole('group', { name: 'Northwind Outdoors' })).not.toBeInTheDocument();
  // Tab goes from the logo straight to the next control of the head.
  const stops = Array.from(rail.querySelectorAll<HTMLElement>('.rail-head a, .rail-head button, .rail-head [tabindex]'));
  expect(stops[0]).toHaveTextContent('Colliedesk');
  // Nothing sits between the logo and the first button of the head (here the sidebar toggle).
  expect(stops[1]).toHaveAttribute('aria-label', 'Collapse sidebar');
});

test('folded into its rail, the sidebar leaves the workspace and the address to the top bar', () => {
  localStorage.setItem(sidebarStorageKey, JSON.stringify({ width: 248, collapsed: true }));
  render(<AppShell me={me} page={{ title: 'Inbox', icon: null }}><p>inbox</p></AppShell>);
  const identity = screen.getByRole('region', { name: 'Signed in as' });
  expect(identity).not.toContainElement(screen.getByRole('navigation', { name: 'Main' }));
  expect(identity.closest('.app-topbar')).not.toBeNull();
  expect(identity).toHaveTextContent('Beta Desk');
  expect(identity).toHaveTextContent('sam@example.com');
  expect(screen.getAllByRole('region', { name: 'Signed in as' })).toHaveLength(1);
  localStorage.removeItem(sidebarStorageKey);
});

test('the top bar names the page and links to the docs', () => {
  render(<AppShell me={me} page={{ title: 'Reports', icon: <svg data-testid="page-icon" /> }}><p>results</p></AppShell>);
  const bar = screen.getByRole('banner');
  expect(within(bar).getByText('Reports')).toBeInTheDocument();
  expect(within(bar).getByTestId('page-icon')).toBeInTheDocument();
  const help = within(bar).getByRole('link', { name: 'Help' });
  expect(help).toHaveAttribute('href', 'https://colliedesk.com/docs');
  expect(help).toHaveAttribute('target', '_blank');
});

// What the palette offers; its own tests cover the rows. Nothing is asked of the server while nothing is typed.
const palette: PaletteSource = { places: [{ key: 'inbox', label: 'Inbox', href: '/', icon: null }], actions: [], searchConversations: true, searchCustomers: true, onNavigate: () => {} };
const withQueries = (ui: ReactNode) => <QueryClientProvider client={new QueryClient()}>{ui}</QueryClientProvider>;

test('the top bar holds one search field named for the workspace, with its shortcut at the end, and the sidebar has none', () => {
  render(withQueries(<AppShell me={me} palette={palette} page={{ title: 'Inbox', icon: null }}><p>inbox</p></AppShell>));
  const field = within(screen.getByRole('banner')).getByRole('button', { name: 'Search Beta Desk' });
  expect(field).toHaveAttribute('aria-keyshortcuts', 'Control+K Meta+K');
  expect(field).toHaveAttribute('aria-haspopup', 'dialog');
  // Not a Mac in the tests: the shortcut reads Ctrl K.
  expect(field).toHaveTextContent('Ctrl K');
  expect(within(screen.getByRole('navigation', { name: 'Main' })).queryByRole('button', { name: /Search/ })).not.toBeInTheDocument();
});

test('Ctrl K and ⌘K open the search palette from anywhere, and Escape closes it and gives the focus back', async () => {
  render(withQueries(<AppShell me={me} palette={palette}><input aria-label="Reply" /></AppShell>));
  const reply = screen.getByRole('textbox', { name: 'Reply' });
  reply.focus();
  // A plain k is typing, not the shortcut.
  fireEvent.keyDown(reply, { key: 'k' });
  expect(screen.queryByRole('dialog', { name: 'Search' })).not.toBeInTheDocument();
  fireEvent.keyDown(reply, { key: 'k', ctrlKey: true });
  const input = await screen.findByRole('combobox', { name: 'Search conversations, customers and pages' });
  expect(input).toHaveFocus();
  fireEvent.keyDown(input, { key: 'Escape' });
  expect(screen.queryByRole('dialog', { name: 'Search' })).not.toBeInTheDocument();
  expect(reply).toHaveFocus();
  fireEvent.keyDown(document.body, { key: 'K', metaKey: true });
  expect(await screen.findByRole('combobox', { name: 'Search conversations, customers and pages' })).toHaveFocus();
});

test('a click on the field opens the palette, and closing it returns the focus to the field', async () => {
  render(withQueries(<AppShell me={me} palette={palette}><p>inbox</p></AppShell>));
  const field = screen.getByRole('button', { name: 'Search Beta Desk' });
  field.focus();
  fireEvent.click(field);
  const input = await screen.findByRole('combobox', { name: 'Search conversations, customers and pages' });
  fireEvent.keyDown(input, { key: 'Escape' });
  expect(field).toHaveFocus();
});

test('without a search to offer, the top bar has no field and the shortcut does nothing', () => {
  render(<AppShell me={me}><p>inbox</p></AppShell>);
  expect(screen.queryByRole('button', { name: /^Search/ })).not.toBeInTheDocument();
  fireEvent.keyDown(document.body, { key: 'k', ctrlKey: true });
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
});

test('the sidebar head holds the workspace, then Quick start, then the pen that starts a new conversation', () => {
  const onNewConversation = vi.fn();
  const { container, rerender } = render(<AppShell me={me} quickStart={<button type="button">Quick start, 2 steps left</button>} onNewConversation={onNewConversation}><p>inbox</p></AppShell>);
  const head = container.querySelector('.rail-head') as HTMLElement;
  const start = within(head).getByRole('button', { name: 'New conversation' });
  expect(within(head).getAllByRole('button').map((button) => button.getAttribute('aria-label') ?? button.textContent).slice(0, 2))
    .toEqual(['Quick start, 2 steps left', 'New conversation']);
  expect(start).toHaveAttribute('data-tip', 'New conversation');
  // The window it opens gives the focus back to it.
  expect(start).toHaveAttribute('data-opens', 'new-conversation');
  fireEvent.click(start);
  expect(onNewConversation).toHaveBeenCalledTimes(1);
  rerender(<AppShell me={me}><p>inbox</p></AppShell>);
  expect(screen.queryByRole('button', { name: 'New conversation' })).not.toBeInTheDocument();
});

const views: SidebarView[] = [
  { key: 'v1', label: 'Mine, open', href: '/?status=open&assignee=me', mark: 'open', current: true },
  { key: 'v2', label: 'Urgent', href: '/?priority=urgent&status=all', mark: 'urgent', current: false },
];
const channels: SidebarChannel[] = [
  { key: 'c1', label: 'support@northwind.help', href: '/?channel_id=c1', type: 'email', warn: false, current: false },
  { key: 'c2', label: 'Chat widget', href: '/?channel_id=c2', type: 'help_chat', warn: true, current: false },
];

test('the saved views and the channels are listed under their names, each a link to its inbox list', () => {
  render(<AppShell me={me} views={views} channels={channels}><p>inbox</p></AppShell>);
  const rail = screen.getByRole('navigation', { name: 'Main' });
  const viewList = within(rail).getByRole('list', { name: 'Views' });
  expect(within(viewList).getByRole('link', { name: 'Mine, open' })).toHaveAttribute('href', '/?status=open&assignee=me');
  expect(within(viewList).getByRole('link', { name: 'Mine, open' })).toHaveAttribute('aria-current', 'page');
  expect(within(viewList).getByRole('link', { name: 'Urgent' })).not.toHaveAttribute('aria-current');
  const channelList = within(rail).getByRole('list', { name: 'Channels' });
  expect(within(channelList).getByRole('link', { name: 'support@northwind.help' })).toHaveAttribute('href', '/?channel_id=c1');
  // The warning dot is not only a colour: its link says what it means.
  expect(within(channelList).getByRole('link', { name: 'Chat widget, needs attention' })).toHaveAttribute('href', '/?channel_id=c2');
});

test('with no saved view and no channel, the sidebar shows neither heading', () => {
  render(<AppShell me={me} views={[]} channels={[]}><p>inbox</p></AppShell>);
  expect(screen.queryByRole('list', { name: 'Views' })).not.toBeInTheDocument();
  expect(screen.queryByText('Views')).not.toBeInTheDocument();
  expect(screen.queryByText('Channels')).not.toBeInTheDocument();
});

test('Invite teammates is at the foot of the sidebar for someone who may invite, and opens Members', () => {
  const { rerender } = render(<AppShell me={me} canInvite><p>inbox</p></AppShell>);
  expect(within(screen.getByRole('navigation', { name: 'Main' })).getByRole('link', { name: 'Invite teammates' })).toHaveAttribute('href', '/settings/members');
  rerender(<AppShell me={me}><p>inbox</p></AppShell>);
  expect(screen.queryByRole('link', { name: 'Invite teammates' })).not.toBeInTheDocument();
});

test('signed out, the frame offers no search, no Quick start, no new conversation, no lists and no invite', () => {
  render(<AppShell signedOut palette={palette} quickStart={<button type="button">Quick start</button>} onNewConversation={() => {}} views={views} channels={channels} canInvite><p>inbox</p></AppShell>);
  expect(screen.queryByRole('button', { name: /^Search/ })).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Quick start' })).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'New conversation' })).not.toBeInTheDocument();
  expect(screen.queryByRole('list', { name: 'Views' })).not.toBeInTheDocument();
  expect(screen.queryByRole('link', { name: 'Invite teammates' })).not.toBeInTheDocument();
});
