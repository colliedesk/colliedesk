import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { SettingsPage } from './settings-page';
import { SessionsSection } from './sessions-section';
import { builtInRoles, permissionsOf, rolesOf } from '../../test-roles';

test('creates an invite and exposes its one-time URL', () => {
  const onCreateInvite = vi.fn();
  render(<SettingsPage page="members" membersTab="invites" sessions={[]} invites={[]} timezone="UTC"
    canCreateInvites inviteRoles={builtInRoles}
    createdInvite={{ url: "https://desk.example.com/sign-in?token=once" }}
    onCreateInvite={onCreateInvite} />);

  fireEvent.change(screen.getByLabelText(/invite email/i), { target: { value: 'agent@example.com' } });
  fireEvent.click(screen.getByRole('button', { name: /create invite/i }));

  expect(onCreateInvite).toHaveBeenCalledWith('agent@example.com', [builtInRoles[1].id]);
  expect(screen.getByRole('textbox', { name: /one-time invite link/i })).toHaveValue('https://desk.example.com/sign-in?token=once');
});

test('does not offer invite creation without admin permission', () => {
  render(<SettingsPage page="members" sessions={[]} invites={[]} timezone="UTC" canCreateInvites={false} />);
  expect(screen.queryByRole('button', { name: /create invite/i })).not.toBeInTheDocument();
});

test('surfaces a failed revoke operation', () => {
  render(<SettingsPage page="members" sessions={[]} invites={[]} timezone="UTC" error="The revoke failed." />);
  expect(screen.getByRole('alert')).toHaveTextContent('The revoke failed.');
});

test('shows the state of every invite and offers revoke only while it is pending', () => {
  const base = { roles: rolesOf('agent'), created_by_email: 'admin@example.com', created_at: '2026-10-14T00:00:00Z', used_at: null, revoked_at: null };
  const future = new Date(Date.now() + 600_000).toISOString();
  const past = new Date(Date.now() - 600_000).toISOString();
  render(<SettingsPage page="members" membersTab="invites" sessions={[]} timezone="UTC" canCreateInvites canRevokeInvites invites={[
    { ...base, id: '11111111-1111-4111-8111-111111111111', email: 'pending@example.com', expires_at: future },
    { ...base, id: '22222222-2222-4222-8222-222222222222', email: 'accepted@example.com', expires_at: future, used_at: '2026-10-14T00:01:00Z' },
    { ...base, id: '33333333-3333-4333-8333-333333333333', email: 'revoked@example.com', expires_at: future, revoked_at: '2026-10-14T00:01:00Z' },
    { ...base, id: '44444444-4444-4444-8444-444444444444', email: 'expired@example.com', expires_at: past },
  ]} />);
  const invites = screen.getByRole('region', { name: 'Invites' });
  const row = (email: string) => within(invites).getByText(email).closest('article') as HTMLElement;
  expect(row('pending@example.com')).toHaveTextContent('Pending');
  expect(row('accepted@example.com')).toHaveTextContent('Accepted');
  expect(row('revoked@example.com')).toHaveTextContent('Revoked');
  expect(row('expired@example.com')).toHaveTextContent('Expired');
  // A row's actions are in its menu: a pending invite can get a new link or be revoked, an expired one a new link; an ended one has none.
  expect(within(invites).getAllByRole('button', { name: /^Actions for / }).map((button) => button.getAttribute('aria-label'))).toEqual(['Actions for pending@example.com', 'Actions for expired@example.com']);
  fireEvent.click(within(row('pending@example.com')).getByRole('button', { name: 'Actions for pending@example.com' }));
  expect(screen.getAllByRole('menuitem').map((item) => item.textContent)).toEqual(['New invite link', 'Revoke']);
  fireEvent.keyDown(screen.getByRole('menu'), { key: 'Escape' });
  fireEvent.click(within(row('expired@example.com')).getByRole('button', { name: 'Actions for expired@example.com' }));
  expect(screen.getAllByRole('menuitem').map((item) => item.textContent)).toEqual(['New invite link']);
  // The sessions, with their own Revoke buttons, are on Account now, not beside the invites.
  expect(screen.queryByRole('region', { name: 'Sessions' })).not.toBeInTheDocument();
});

test('an invite stays listed without Revoke for a person who may invite but not revoke', () => {
  render(<SettingsPage page="members" membersTab="invites" sessions={[]} timezone="UTC" canCreateInvites canRevokeInvites={false} invites={[
    { id: '11111111-1111-4111-8111-111111111111', email: 'pending@example.com', roles: rolesOf('agent'), created_by_email: 'admin@example.com',
      created_at: '2026-10-14T00:00:00Z', expires_at: new Date(Date.now() + 600_000).toISOString(), used_at: null, revoked_at: null },
  ]} />);
  const invites = screen.getByRole('region', { name: 'Invites' });
  expect(within(invites).getByText('pending@example.com')).toBeInTheDocument();
  fireEvent.click(within(invites).getByRole('button', { name: 'Actions for pending@example.com' }));
  expect(screen.getAllByRole('menuitem').map((item) => item.textContent)).toEqual(['New invite link']);
});

// Agent tokens are listed in AI & agents (ai-agents-sections.tsx), with the same rows as before.
test('a revoked or expired session is not listed, so it cannot be revoked again', () => {
  const base = { agent_id: '55555555-5555-4555-8555-555555555555', agent_email: 'sam@example.com', agent_name: 'Sam', label: 'Chrome', token: null, role: null, permissions: [], current: false, created_at: '2026-10-14T00:00:00Z', last_seen_at: '2026-10-14T00:00:00Z' };
  const future = new Date(Date.now() + 600_000).toISOString();
  render(<SettingsPage page="account" invites={[]} timezone="UTC" sessions={[
    { ...base, id: '11111111-1111-4111-8111-111111111111', label: 'Active device', expires_at: future, revoked_at: null },
    { ...base, id: '22222222-2222-4222-8222-222222222222', label: 'Revoked device', expires_at: future, revoked_at: '2026-10-14T00:01:00Z' },
    { ...base, id: '33333333-3333-4333-8333-333333333333', label: 'Old device', expires_at: new Date(Date.now() - 1000).toISOString(), revoked_at: null },
  ]} />);
  const sessions = screen.getByRole('region', { name: 'Sessions' });
  const row = (label: string) => within(sessions).getByText(label).closest('article') as HTMLElement;
  expect(row('Active device')).toHaveTextContent('Active');
  expect(within(sessions).queryByText('Revoked device')).not.toBeInTheDocument();
  expect(within(sessions).queryByText('Old device')).not.toBeInTheDocument();
  expect(within(sessions).getAllByRole('button', { name: 'Revoke' })).toHaveLength(1);
});

test('shows why an invite was refused next to the invite form', () => {
  render(<SettingsPage page="members" membersTab="invites" sessions={[]} invites={[]} timezone="UTC" canCreateInvites
    inviteError="The trial allows 10 agents, pending invites included." />);
  expect(screen.getByRole('alert')).toHaveTextContent(/trial allows 10 agents/i);
});

test('Sessions lists the browsers people signed in on and never an agent token; a device is revoked at once', () => {
  const base = { agent_id: '55555555-5555-4555-8555-555555555555', agent_email: 'sam@example.com', agent_name: 'Sam', role: null, current: false, created_at: '2026-10-14T00:00:00Z', last_seen_at: '2026-10-14T00:00:00Z', expires_at: '2037-10-14T09:30:00Z', revoked_at: null };
  const onRevokeSession = vi.fn();
  const sessions = [
    { ...base, id: '11111111-1111-4111-8111-111111111111', label: 'Claude Code', token: { kind: 'custom' as const, role: null, role_held: true, permissions: ['tags:read' as const], legacy_tier: null }, permissions: ['tags:read'] },
    { ...base, id: '55555555-5555-4555-8555-555555555556', label: 'Chrome', token: null, permissions: [] },
  ];
  render(<SessionsSection timezone="UTC" sessions={sessions} onRevokeSession={onRevokeSession} />);
  const devices = screen.getByRole('region', { name: 'Sessions' });
  // Agent tokens are listed once, on AI & agents.
  expect(within(devices).queryByText('Claude Code')).not.toBeInTheDocument();
  const chrome = within(devices).getByText('Chrome').closest('article') as HTMLElement;
  expect(chrome).not.toHaveTextContent(/Expires|Follows|Fixed/);
  fireEvent.click(within(chrome).getByRole('button', { name: 'Revoke' }));
  expect(onRevokeSession).toHaveBeenCalledWith('55555555-5555-4555-8555-555555555556');
  expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
});

test('shows the loading status while settings data is pending, without the forms', () => {
  render(<SettingsPage page="members" sessions={[]} invites={[]} timezone="UTC" loading canCreateInvites />);
  expect(screen.getByRole('status')).toHaveTextContent('Loading workspace…');
  expect(screen.queryByRole('button', { name: /create invite/i })).not.toBeInTheDocument();
});

const sessionBase = { agent_id: '55555555-5555-4555-8555-555555555555', agent_email: 'sam@example.com', agent_name: 'Sam', token: null, role: null, permissions: [], created_at: '2026-10-14T00:00:00Z',
  last_seen_at: '2026-10-14T00:00:00Z', expires_at: new Date(Date.now() + 600_000).toISOString(), revoked_at: null, current: false };

// Each page holds the sections the design puts on it, under its own title and the line saying what it holds;
// the menu links pages, never a section of another page.
test('settings is one page per menu item, each holding its own sections', () => {
  localStorage.setItem('colliedesk.autofix', 'on');
  const team = <section aria-labelledby="team-heading"><h2 id="team-heading">Team</h2></section>;
  const regions = () => screen.getAllByRole('region').map((region) => region.getAttribute('aria-labelledby'));
  const { unmount } = render(<SettingsPage page="account" sessions={[]} invites={[]} timezone="UTC" canManageWorkspace canCreateInvites />);
  expect(screen.getByRole('heading', { level: 1, name: 'Account' })).toBeInTheDocument();
  expect(screen.getByText('Your name, how Colliedesk works for you, and where you are signed in.')).toBeInTheDocument();
  expect(regions()).toEqual(['profile-heading', 'preferences-heading', 'sessions-heading', 'connected-apps-heading']);
  const nav = screen.getByRole('navigation', { name: 'Settings sections' });
  expect(within(nav).getAllByRole('link').filter((link) => link.getAttribute('href')!.includes('#'))).toEqual([]);
  expect(within(nav).getByRole('link', { name: 'Autofix' })).toHaveAttribute('href', '/settings/autofix');
  unmount();
  const general = render(<SettingsPage page="general" sessions={[]} invites={[]} timezone="UTC" canManageWorkspace numbersSection={<section aria-labelledby="numbers-heading"><h2 id="numbers-heading">Conversation numbers</h2></section>} />);
  expect(screen.getByRole('heading', { level: 1, name: 'General' })).toBeInTheDocument();
  expect(regions()).toEqual(['general-heading', 'numbers-heading']);
  general.unmount();
  // Members shows one tab's section at a time: the team, the invites, and under Advanced the roles.
  const roles = <section aria-labelledby="roles-heading"><h2 id="roles-heading">Roles</h2></section>;
  const members = render(<SettingsPage page="members" sessions={[]} invites={[]} timezone="UTC" canCreateInvites teamSection={team} rolesSection={roles} />);
  expect(screen.getByRole('heading', { level: 1, name: 'Members' })).toBeInTheDocument();
  expect(regions()).toEqual(['team-heading']);
  members.unmount();
  const invites = render(<SettingsPage page="members" membersTab="invites" sessions={[]} invites={[]} timezone="UTC" canCreateInvites teamSection={team} rolesSection={roles} />);
  expect(regions()).toEqual(['invites-heading']);
  invites.unmount();
  render(<SettingsPage page="members" membersTab="advanced" sessions={[]} invites={[]} timezone="UTC" canCreateInvites teamSection={team} rolesSection={roles} />);
  expect(regions()).toEqual(['roles-heading']);
  localStorage.removeItem('colliedesk.autofix');
});

test('Autofix is in preview: its link shows only in a browser that turned it on', () => {
  localStorage.removeItem('colliedesk.autofix');
  const off = render(<SettingsPage page="general" sessions={[]} invites={[]} timezone="UTC" canManageWorkspace canCreateInvites />);
  expect(within(screen.getByRole('navigation', { name: 'Settings sections' })).queryByRole('link', { name: 'Autofix' })).not.toBeInTheDocument();
  off.unmount();
  localStorage.setItem('colliedesk.autofix', 'on');
  render(<SettingsPage page="general" sessions={[]} invites={[]} timezone="UTC" canManageWorkspace canCreateInvites />);
  expect(within(screen.getByRole('navigation', { name: 'Settings sections' })).getByRole('link', { name: 'Autofix' })).toHaveAttribute('href', '/settings/autofix');
  localStorage.removeItem('colliedesk.autofix');
});

test('an agent gets no workspace-only sections or links', () => {
  render(<SettingsPage page="general" sessions={[]} invites={[]} timezone="UTC" workspaceTimezone="Europe/Berlin" />);
  const nav = screen.getByRole('navigation', { name: 'Settings sections' });
  expect(within(nav).queryByRole('link', { name: 'General' })).not.toBeInTheDocument();
  // Export all data and Import from Freshdesk are on Data.
  expect(within(nav).queryByRole('link', { name: 'Data' })).not.toBeInTheDocument();
  expect(screen.queryByRole('combobox', { name: 'Workspace timezone' })).not.toBeInTheDocument();
});

// The invites that add people are the tab beside the people: each tab holds its own section alone.
test('the People tab holds the team and the Invites tab the invites, never both at once', () => {
  const team = <section aria-labelledby="team-heading"><h2 id="team-heading">Team</h2></section>;
  const people = render(<SettingsPage page="members" sessions={[]} invites={[]} timezone="UTC" canCreateInvites teamSection={team} />);
  expect(screen.getByRole('region', { name: 'Team' })).toBeInTheDocument();
  expect(screen.queryByRole('region', { name: 'Invites' })).not.toBeInTheDocument();
  people.unmount();
  render(<SettingsPage page="members" membersTab="invites" sessions={[]} invites={[]} timezone="UTC" canCreateInvites teamSection={team} />);
  expect(screen.getByRole('region', { name: 'Invites' })).toBeInTheDocument();
  expect(screen.queryByRole('region', { name: 'Team' })).not.toBeInTheDocument();
});

test('theme and desktop notifications live together under Preferences', () => {
  vi.stubGlobal('Notification', class { static permission = 'default'; static requestPermission = vi.fn(); });
  render(<SettingsPage page="account" sessions={[]} invites={[]} timezone="UTC" />);
  const preferences = screen.getByRole('region', { name: 'Preferences' });
  expect(within(preferences).getByRole('radio', { name: 'Dark' })).toBeInTheDocument();
  expect(within(preferences).getByRole('checkbox', { name: /desktop notifications/i })).toBeInTheDocument();
  vi.unstubAllGlobals();
});

test('Preferences offers the setup guide, ticked while it shows, and a change is reported', () => {
  const onSetSetupGuideHidden = vi.fn();
  const { rerender } = render(<SettingsPage page="account" sessions={[]} invites={[]} timezone="UTC" setupGuideHidden={false} onSetSetupGuideHidden={onSetSetupGuideHidden} />);
  const box = within(screen.getByRole('region', { name: 'Preferences' })).getByRole('checkbox', { name: 'Show the setup guide' });
  expect(box).toBeChecked();
  fireEvent.click(box);
  expect(onSetSetupGuideHidden).toHaveBeenLastCalledWith(true);
  rerender(<SettingsPage page="account" sessions={[]} invites={[]} timezone="UTC" setupGuideHidden onSetSetupGuideHidden={onSetSetupGuideHidden} />);
  expect(screen.getByRole('checkbox', { name: 'Show the setup guide' })).not.toBeChecked();
  fireEvent.click(screen.getByRole('checkbox', { name: 'Show the setup guide' }));
  expect(onSetSetupGuideHidden).toHaveBeenLastCalledWith(false);
});

test('a chosen workspace timezone is saved when the list is left, no Save button', () => {
  const onSetWorkspaceTimezone = vi.fn();
  render(<SettingsPage page="general" sessions={[]} invites={[]} timezone="UTC" canManageWorkspace workspaceTimezone="UTC" onSetWorkspaceTimezone={onSetWorkspaceTimezone} />);
  const general = screen.getByRole('region', { name: 'General' });
  const select = within(general).getByRole('combobox', { name: 'Workspace timezone' });
  fireEvent.change(select, { target: { value: 'America/New_York' } });
  fireEvent.blur(select);
  expect(onSetWorkspaceTimezone).toHaveBeenCalledWith('America/New_York');
  // Nothing in General has a button of its own: the timezone saves at once, and the name waits for the page's Save.
  // A help tip is an ⓘ button that explains; only the buttons that act are counted.
  expect(within(general).queryAllByRole('button').filter((button) => !(button.getAttribute('aria-label') ?? '').startsWith('About '))).toEqual([]);
});

test('your timezone follows the workspace until you choose one, and choosing the workspace again clears it', () => {
  const onSetAgentTimezone = vi.fn();
  render(<SettingsPage page="account" sessions={[]} invites={[]} timezone="Europe/London" workspaceTimezone="Europe/Berlin" timezoneOverride="Europe/London" onSetAgentTimezone={onSetAgentTimezone} />);
  const select = within(screen.getByRole('region', { name: 'Profile' })).getByRole('combobox', { name: 'Your timezone' });
  expect(select).toHaveValue('Europe/London');
  expect(within(select).getByRole('option', { name: 'Same as workspace (Europe/Berlin)' })).toBeInTheDocument();
  fireEvent.change(select, { target: { value: 'Asia/Tokyo' } });
  fireEvent.keyDown(select, { key: 'Enter' });
  fireEvent.change(select, { target: { value: '' } });
  fireEvent.blur(select);
  expect(onSetAgentTimezone.mock.calls).toEqual([['Asia/Tokyo'], [null]]);
});

test('a stored timezone this browser does not list is still shown as chosen', () => {
  render(<SettingsPage page="account" sessions={[]} invites={[]} timezone="UTC" timezoneOverride="Mars/Olympus_Mons" />);
  expect(screen.getByRole('combobox', { name: 'Your timezone' })).toHaveValue('Mars/Olympus_Mons');
});

test('a timezone save shows Saving… or the failure next to its own control; Saved is the page toast', () => {
  const { rerender } = render(<SettingsPage page="account" sessions={[]} invites={[]} timezone="UTC" personalTimezoneSave={{ pending: true }} />);
  expect(screen.getByText('Saving…')).toBeInTheDocument();
  // Never disabled: a disabled control would throw keyboard focus out to the page.
  expect(screen.getByRole('combobox', { name: 'Your timezone' })).toBeEnabled();
  rerender(<SettingsPage page="account" sessions={[]} invites={[]} timezone="UTC" personalTimezoneSave={{ saved: true }} />);
  expect(screen.queryByRole('status')).not.toBeInTheDocument();
  rerender(<SettingsPage page="account" sessions={[]} invites={[]} timezone="UTC" personalTimezoneSave={{ error: 'Unknown timezone.' }} />);
  expect(screen.getByRole('alert')).toHaveTextContent('Unknown timezone.');
});

test('a session is named by its browser and system, and this device is marked with Sign out instead of Revoke', () => {
  const onRevokeSession = vi.fn();
  render(<SettingsPage page="account" invites={[]} timezone="UTC" onRevokeSession={onRevokeSession} sessions={[
    { ...sessionBase, id: '11111111-1111-4111-8111-111111111111', current: true,
      label: 'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36' },
    { ...sessionBase, id: '22222222-2222-4222-8222-222222222222',
      label: 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1' },
    { ...sessionBase, id: '33333333-3333-4333-8333-333333333333', label: 'Cloud sign-in' },
  ]} />);
  const sessions = screen.getByRole('region', { name: 'Sessions' });
  const here = within(sessions).getByText('Chrome on Linux').closest('article') as HTMLElement;
  expect(here).toHaveTextContent('This device');
  fireEvent.click(within(here).getByRole('button', { name: 'Sign out' }));
  expect(onRevokeSession).toHaveBeenCalledWith('11111111-1111-4111-8111-111111111111');
  const phone = within(sessions).getByText('Safari on iPhone').closest('article') as HTMLElement;
  expect(phone).not.toHaveTextContent('This device');
  expect(within(phone).getByRole('button', { name: 'Revoke' })).toBeInTheDocument();
  expect(within(sessions).getByText('Cloud sign-in')).toBeInTheDocument();
});

test('an invite gives the Agent role unless other roles are picked, and needs at least one', () => {
  const onCreateInvite = vi.fn();
  render(<SettingsPage page="members" membersTab="invites" sessions={[]} invites={[]} timezone="UTC" canCreateInvites inviteRoles={builtInRoles} onCreateInvite={onCreateInvite} />);
  const picker = screen.getByRole('button', { name: 'Roles: Agent' });
  fireEvent.click(picker);
  fireEvent.click(screen.getByRole('checkbox', { name: /Admin/ }));
  fireEvent.click(screen.getByRole('checkbox', { name: /Agent/ }));
  expect(screen.getByRole('button', { name: 'Roles: Admin' })).toBeInTheDocument();
  fireEvent.click(screen.getByRole('checkbox', { name: /Admin/ }));
  expect(screen.getByRole('alert')).toHaveTextContent('Everyone needs at least one role.');
  expect(screen.getByRole('button', { name: /create invite/i })).toBeDisabled();
  fireEvent.click(screen.getByRole('checkbox', { name: /Agent/ }));
  fireEvent.change(screen.getByLabelText(/invite email/i), { target: { value: 'b@example.com' } });
  fireEvent.click(screen.getByRole('button', { name: /create invite/i }));
  expect(onCreateInvite).toHaveBeenCalledWith('b@example.com', [builtInRoles[1].id]);
});

test('browsing the timezone list with arrow keys saves only where it stops, once, and keeps focus', () => {
  vi.useFakeTimers();
  const onSetWorkspaceTimezone = vi.fn();
  render(<SettingsPage page="general" sessions={[]} invites={[]} timezone="UTC" canManageWorkspace workspaceTimezone="UTC" onSetWorkspaceTimezone={onSetWorkspaceTimezone} />);
  const select = screen.getByRole('combobox', { name: 'Workspace timezone' });
  select.focus();
  for (const zone of ['Africa/Abidjan', 'Africa/Accra', 'Africa/Addis_Ababa']) fireEvent.change(select, { target: { value: zone } });
  expect(onSetWorkspaceTimezone).not.toHaveBeenCalled();
  act(() => { vi.advanceTimersByTime(2000); });
  expect(onSetWorkspaceTimezone.mock.calls).toEqual([['Africa/Addis_Ababa']]);
  expect(select).toHaveFocus();
  fireEvent.blur(select);
  expect(onSetWorkspaceTimezone).toHaveBeenCalledTimes(1);
  vi.useRealTimers();
});

test('a stored zone that this browser lists under its old name appears once', () => {
  render(<SettingsPage page="account" sessions={[]} invites={[]} timezone="UTC" workspaceTimezone="UTC" timezoneOverride="Asia/Ho_Chi_Minh" />);
  const select = screen.getByRole('combobox', { name: 'Your timezone' });
  expect(select).toHaveValue('Asia/Ho_Chi_Minh');
  const vietnam = within(select).getAllByRole('option').filter((option) => ['Asia/Ho_Chi_Minh', 'Asia/Saigon'].includes(option.getAttribute('value') ?? ''));
  expect(vietnam).toHaveLength(1);
});

test('after a failed timezone save, leaving the list again retries it', () => {
  const onSetAgentTimezone = vi.fn();
  const { rerender } = render(<SettingsPage page="account" sessions={[]} invites={[]} timezone="UTC" onSetAgentTimezone={onSetAgentTimezone} />);
  const select = screen.getByRole('combobox', { name: 'Your timezone' });
  fireEvent.change(select, { target: { value: 'Asia/Tokyo' } });
  fireEvent.blur(select);
  rerender(<SettingsPage page="account" sessions={[]} invites={[]} timezone="UTC" onSetAgentTimezone={onSetAgentTimezone} personalTimezoneSave={{ error: 'Unknown timezone.' }} />);
  fireEvent.blur(select);
  expect(onSetAgentTimezone.mock.calls).toEqual([['Asia/Tokyo'], ['Asia/Tokyo']]);
});

test('a browser that cannot list timezones still offers the common ones', () => {
  const original = Intl.supportedValuesOf;
  // @ts-expect-error older browsers (Safari before 15.4) have no Intl.supportedValuesOf
  delete Intl.supportedValuesOf;
  try {
    render(<SettingsPage page="account" sessions={[]} invites={[]} timezone="UTC" />);
    const options = within(screen.getByRole('combobox', { name: 'Your timezone' })).getAllByRole('option').map((option) => option.getAttribute('value'));
    for (const zone of ['UTC', 'America/New_York', 'Europe/London', 'Asia/Ho_Chi_Minh', 'Australia/Sydney']) expect(options).toContain(zone);
  } finally {
    Intl.supportedValuesOf = original;
  }
});

test('an admin of a billed workspace finds Plan and billing last, under Data and billing', () => {
  render(<SettingsPage page="general" sessions={[]} invites={[]} timezone="UTC" canManageWorkspace canCreateInvites showBilling />);
  const links = within(screen.getByRole('navigation', { name: 'Settings sections' })).getAllByRole('link');
  expect(links.at(-1)).toHaveTextContent('Plan and billing');
  expect(links.at(-1)).toHaveAttribute('href', '/settings/billing');
});

test('your display name is saved with the page Save, trimmed, and saving an empty field clears it', async () => {
  const onSetDisplayName = vi.fn();
  render(<SettingsPage page="account" sessions={[]} invites={[]} timezone="UTC" email="ada@example.com" displayName="Ada" onSetDisplayName={onSetDisplayName} />);
  const profile = screen.getByRole('region', { name: 'Profile' });
  const input = within(profile).getByRole('textbox', { name: 'Display name' });
  expect(input).toHaveValue('Ada');
  expect(input).toHaveAttribute('placeholder', 'ada@example.com');
  expect(input).toHaveAttribute('maxLength', '80');
  fireEvent.change(input, { target: { value: '  Ada Lovelace ' } });
  expect(within(profile).queryByRole('button', { name: /save/i })).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  expect(onSetDisplayName).toHaveBeenLastCalledWith('Ada Lovelace');
  fireEvent.change(input, { target: { value: '' } });
  // While a save is on its way the bar says Saving…; it is Save again once the answer is in.
  fireEvent.click(await screen.findByRole('button', { name: 'Save' }));
  expect(onSetDisplayName).toHaveBeenLastCalledWith('');
});

test('a display name save shows Saving…, Saved or why it was refused', () => {
  const { rerender } = render(<SettingsPage page="account" sessions={[]} invites={[]} timezone="UTC" displayName="" displayNameSave={{ pending: true }} />);
  expect(screen.getByText('Saving…')).toBeInTheDocument();
  rerender(<SettingsPage page="account" sessions={[]} invites={[]} timezone="UTC" displayName="Ada" displayNameSave={{ saved: true }} />);
  expect(screen.getByRole('status')).toHaveTextContent('Saved');
  expect(screen.getByRole('textbox', { name: 'Display name' })).toHaveValue('Ada');
  rerender(<SettingsPage page="account" sessions={[]} invites={[]} timezone="UTC" displayName="Ada" displayNameSave={{ error: 'display_name must be a string of at most 80 characters.' }} />);
  expect(screen.getByRole('alert')).toHaveTextContent('at most 80 characters');
});


// ---- the invite being typed belongs to the page, not to the Invites tab

const inviteProps = { sessions: [] as never[], timezone: 'UTC', canCreateInvites: true, inviteRoles: builtInRoles, invites: [] as never[] };
const closingTabAsks = () => !window.dispatchEvent(new Event('beforeunload', { cancelable: true }));

test('an email typed on Invites still counts as unsaved when People is the tab shown, and draws no bar', async () => {
  const view = render(<SettingsPage page="members" membersTab="invites" {...inviteProps} />);
  expect(closingTabAsks()).toBe(false);
  fireEvent.change(screen.getByLabelText(/invite email/i), { target: { value: 'new@example.com' } });
  await waitFor(() => expect(closingTabAsks()).toBe(true));
  view.rerender(<SettingsPage page="members" membersTab="people" {...inviteProps} />);
  expect(closingTabAsks()).toBe(true);
  expect(screen.queryByRole('region', { name: 'Unsaved changes' })).toBeNull();
  view.rerender(<SettingsPage page="members" membersTab="invites" {...inviteProps} />);
  expect(screen.getByLabelText(/invite email/i)).toHaveValue('new@example.com');
});

test('once an invite is created its form is empty again and nothing is unsaved', async () => {
  const view = render(<SettingsPage page="members" membersTab="invites" {...inviteProps} />);
  fireEvent.change(screen.getByLabelText(/invite email/i), { target: { value: 'new@example.com' } });
  await waitFor(() => expect(closingTabAsks()).toBe(true));
  view.rerender(<SettingsPage page="members" membersTab="invites" {...inviteProps} createdInvite={{ url: 'https://desk.example.com/sign-in?token=once' }} />);
  await waitFor(() => expect(screen.getByLabelText(/invite email/i)).toHaveValue(''));
  await waitFor(() => expect(closingTabAsks()).toBe(false));
});

test('picking the default role again is not a change: only an email, or a role other than the default, is unsaved', async () => {
  render(<SettingsPage page="members" membersTab="invites" {...inviteProps} />);
  fireEvent.click(screen.getByRole('button', { name: /Agent/ }));
  const admin = screen.getByRole('checkbox', { name: /Admin/ });
  fireEvent.click(admin);
  await waitFor(() => expect(closingTabAsks()).toBe(true));
  fireEvent.click(admin);
  await waitFor(() => expect(closingTabAsks()).toBe(false));
});

// The server refuses an invite whose roles hold a permission the person lacks (403 role_exceeds_yours): the menu does not offer it.
// The person here holds the Agent role's permissions and the right to invite, not the Admin role's.
test('New invite link is not offered for an invite whose roles hold a permission the person lacks', () => {
  const [agent, admin] = [rolesOf('agent')[0], rolesOf('admin')[0]];
  const base = { created_by_email: 'admin@example.com', created_at: '2026-10-14T00:00:00Z', used_at: null, revoked_at: null, expires_at: new Date(Date.now() + 600_000).toISOString() };
  render(<SettingsPage page="members" membersTab="invites" sessions={[]} timezone="UTC" canCreateInvites canRevokeInvites inviteRoles={builtInRoles}
    myPermissions={[...permissionsOf('agent'), 'team:invite']} invites={[
    { ...base, id: '11111111-1111-4111-8111-111111111111', email: 'agent@example.com', roles: [agent] },
    { ...base, id: '22222222-2222-4222-8222-222222222222', email: 'admin@example.com', roles: [admin] },
  ]} />);
  fireEvent.click(screen.getByRole('button', { name: 'Actions for agent@example.com' }));
  expect(screen.getAllByRole('menuitem').map((item) => item.textContent)).toEqual(['New invite link', 'Revoke']);
  fireEvent.keyDown(screen.getByRole('menu'), { key: 'Escape' });
  fireEvent.click(screen.getByRole('button', { name: 'Actions for admin@example.com' }));
  expect(screen.getAllByRole('menuitem').map((item) => item.textContent)).toEqual(['Revoke']);
});

test('an invite that turns revoked is announced', () => {
  const pending = { id: '11111111-1111-4111-8111-111111111111', email: 'pending@example.com', roles: rolesOf('agent'), created_by_email: 'admin@example.com',
    created_at: '2026-10-14T00:00:00Z', expires_at: new Date(Date.now() + 600_000).toISOString(), used_at: null, revoked_at: null };
  const props = { page: 'members' as const, membersTab: 'invites' as const, sessions: [], timezone: 'UTC', canCreateInvites: true, canRevokeInvites: true };
  const { rerender } = render(<SettingsPage {...props} invites={[pending]} />);
  rerender(<SettingsPage {...props} invites={[{ ...pending, revoked_at: '2026-10-14T00:01:00Z' }]} />);
  expect(screen.getByText('Invite for pending@example.com revoked.')).toHaveAttribute('role', 'status');
});
