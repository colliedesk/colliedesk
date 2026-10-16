import { act, fireEvent, render, screen, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { SettingsPage } from './settings-page';

test('creates an invite and exposes its one-time URL', () => {
  const onCreateInvite = vi.fn();
  render(<SettingsPage sessions={[]} invites={[]} timezone="UTC"
    canCreateInvites
    createdInviteUrl="https://desk.example.com/sign-in?token=once"
    onCreateInvite={onCreateInvite} />);

  fireEvent.change(screen.getByLabelText(/invite email/i), { target: { value: 'agent@example.com' } });
  fireEvent.click(screen.getByRole('button', { name: /create invite/i }));

  expect(onCreateInvite).toHaveBeenCalledWith('agent@example.com', 'agent');
  expect(screen.getByRole('textbox', { name: /one-time invite link/i })).toHaveValue('https://desk.example.com/sign-in?token=once');
});

test('does not offer invite creation without admin permission', () => {
  render(<SettingsPage sessions={[]} invites={[]} timezone="UTC" canCreateInvites={false} />);
  expect(screen.queryByRole('button', { name: /create invite/i })).not.toBeInTheDocument();
});

test('surfaces a failed revoke operation', () => {
  render(<SettingsPage sessions={[]} invites={[]} timezone="UTC" error="The revoke failed." />);
  expect(screen.getByRole('alert')).toHaveTextContent('The revoke failed.');
});

test('shows the state of every invite and offers revoke only while it is pending', () => {
  const base = { role_ceiling: 'agent' as const, created_by_email: 'admin@example.com', created_at: '2026-10-14T00:00:00Z', used_at: null, revoked_at: null };
  const future = new Date(Date.now() + 600_000).toISOString();
  const past = new Date(Date.now() - 600_000).toISOString();
  render(<SettingsPage sessions={[]} timezone="UTC" canCreateInvites invites={[
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
  expect(within(invites).getAllByRole('button', { name: 'Revoke' })).toHaveLength(1);
  expect(screen.getByRole('region', { name: 'Sessions' })).toBeInTheDocument();
});

test('a revoked or expired session is shown as such and cannot be revoked again', () => {
  const base = { agent_id: '55555555-5555-4555-8555-555555555555', agent_email: 'sam@example.com', label: 'Chrome', current: false, created_at: '2026-10-14T00:00:00Z', last_seen_at: '2026-10-14T00:00:00Z' };
  const future = new Date(Date.now() + 600_000).toISOString();
  render(<SettingsPage invites={[]} timezone="UTC" sessions={[
    { ...base, id: '11111111-1111-4111-8111-111111111111', label: 'Active device', expires_at: future, revoked_at: null },
    { ...base, id: '22222222-2222-4222-8222-222222222222', label: 'Revoked device', expires_at: future, revoked_at: '2026-10-14T00:01:00Z' },
    { ...base, id: '33333333-3333-4333-8333-333333333333', label: 'Old device', expires_at: new Date(Date.now() - 1000).toISOString(), revoked_at: null },
  ]} />);
  const sessions = screen.getByRole('region', { name: 'Sessions' });
  const row = (label: string) => within(sessions).getByText(label).closest('article') as HTMLElement;
  expect(row('Active device')).toHaveTextContent('Active');
  expect(row('Revoked device')).toHaveTextContent('Revoked');
  expect(row('Old device')).toHaveTextContent('Expired');
  expect(within(sessions).getAllByRole('button', { name: 'Revoke' })).toHaveLength(1);
});

test('shows why an invite was refused next to the invite form', () => {
  render(<SettingsPage sessions={[]} invites={[]} timezone="UTC" canCreateInvites
    inviteError="The trial allows 10 agents, pending invites included." />);
  expect(screen.getByRole('alert')).toHaveTextContent(/trial allows 10 agents/i);
});

test('shows the loading status while settings data is pending, without the forms', () => {
  render(<SettingsPage sessions={[]} invites={[]} timezone="UTC" loading canCreateInvites />);
  expect(screen.getByRole('status')).toHaveTextContent('Loading workspace…');
  expect(screen.queryByRole('button', { name: /create invite/i })).not.toBeInTheDocument();
});

const sessionBase = { agent_id: '55555555-5555-4555-8555-555555555555', agent_email: 'sam@example.com', created_at: '2026-10-14T00:00:00Z',
  last_seen_at: '2026-10-14T00:00:00Z', expires_at: new Date(Date.now() + 600_000).toISOString(), revoked_at: null, current: false };

test('settings is split into your account and the workspace, with a link to every section', () => {
  render(<SettingsPage sessions={[]} invites={[]} timezone="UTC" canManageWorkspace canCreateInvites
    teamSection={<section aria-labelledby="team-heading"><h2 id="team-heading">Team</h2></section>} exportSection={<p>export</p>} />);
  const nav = screen.getByRole('navigation', { name: 'Settings sections' });
  const links = within(nav).getAllByRole('link').map((link) => [link.textContent, link.getAttribute('href')]);
  expect(links).toEqual([
    ['Profile', '#profile-heading'], ['Preferences', '#preferences-heading'],
    ['General', '#general-heading'], ['Team', '#team-heading'], ['Invites', '#invites-heading'], ['Sessions', '#sessions-heading'],
    ['Export all data', '#export-heading'],
  ]);
  for (const [, href] of links) expect(document.getElementById(href!.slice(1))).not.toBeNull();
});

test('an agent gets no workspace-only sections or links', () => {
  render(<SettingsPage sessions={[]} invites={[]} timezone="UTC" workspaceTimezone="Europe/Berlin" />);
  const nav = screen.getByRole('navigation', { name: 'Settings sections' });
  expect(within(nav).queryByRole('link', { name: 'General' })).not.toBeInTheDocument();
  expect(within(nav).queryByRole('link', { name: 'Export all data' })).not.toBeInTheDocument();
  expect(screen.queryByRole('combobox', { name: 'Workspace timezone' })).not.toBeInTheDocument();
});

test('invites sit right after the team they add to', () => {
  render(<SettingsPage sessions={[]} invites={[]} timezone="UTC" canCreateInvites
    teamSection={<section aria-labelledby="team-heading"><h2 id="team-heading">Team</h2></section>} />);
  const team = screen.getByRole('region', { name: 'Team' });
  expect(team.nextElementSibling).toBe(screen.getByRole('region', { name: 'Invites' }));
});

test('theme and desktop notifications live together under Preferences', () => {
  vi.stubGlobal('Notification', class { static permission = 'default'; static requestPermission = vi.fn(); });
  render(<SettingsPage sessions={[]} invites={[]} timezone="UTC" />);
  const preferences = screen.getByRole('region', { name: 'Preferences' });
  expect(within(preferences).getByRole('radio', { name: 'Dark' })).toBeInTheDocument();
  expect(within(preferences).getByRole('checkbox', { name: /desktop notifications/i })).toBeInTheDocument();
  vi.unstubAllGlobals();
});

test('a chosen workspace timezone is saved when the list is left, no Save button', () => {
  const onSetWorkspaceTimezone = vi.fn();
  render(<SettingsPage sessions={[]} invites={[]} timezone="UTC" canManageWorkspace workspaceTimezone="UTC" onSetWorkspaceTimezone={onSetWorkspaceTimezone} />);
  const general = screen.getByRole('region', { name: 'General' });
  const select = within(general).getByRole('combobox', { name: 'Workspace timezone' });
  fireEvent.change(select, { target: { value: 'America/New_York' } });
  fireEvent.blur(select);
  expect(onSetWorkspaceTimezone).toHaveBeenCalledWith('America/New_York');
  expect(within(general).queryByRole('button')).not.toBeInTheDocument();
});

test('your timezone follows the workspace until you choose one, and choosing the workspace again clears it', () => {
  const onSetAgentTimezone = vi.fn();
  render(<SettingsPage sessions={[]} invites={[]} timezone="Europe/London" workspaceTimezone="Europe/Berlin" timezoneOverride="Europe/London" onSetAgentTimezone={onSetAgentTimezone} />);
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
  render(<SettingsPage sessions={[]} invites={[]} timezone="UTC" timezoneOverride="Mars/Olympus_Mons" />);
  expect(screen.getByRole('combobox', { name: 'Your timezone' })).toHaveValue('Mars/Olympus_Mons');
});

test('a timezone save shows Saving…, Saved or the failure next to its own control', () => {
  const { rerender } = render(<SettingsPage sessions={[]} invites={[]} timezone="UTC" personalTimezoneSave={{ pending: true }} />);
  expect(screen.getByText('Saving…')).toBeInTheDocument();
  // Never disabled: a disabled control would throw keyboard focus out to the page.
  expect(screen.getByRole('combobox', { name: 'Your timezone' })).toBeEnabled();
  rerender(<SettingsPage sessions={[]} invites={[]} timezone="UTC" personalTimezoneSave={{ saved: true }} />);
  expect(screen.getByRole('status')).toHaveTextContent('Saved');
  rerender(<SettingsPage sessions={[]} invites={[]} timezone="UTC" personalTimezoneSave={{ error: 'Unknown timezone.' }} />);
  expect(screen.getByRole('alert')).toHaveTextContent('Unknown timezone.');
});

test('a session is named by its browser and system, and this device is marked with Sign out instead of Revoke', () => {
  const onRevokeSession = vi.fn();
  render(<SettingsPage invites={[]} timezone="UTC" onRevokeSession={onRevokeSession} sessions={[
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

test('an invite asks for a role, not a ceiling', () => {
  render(<SettingsPage sessions={[]} invites={[]} timezone="UTC" canCreateInvites />);
  expect(screen.getByRole('combobox', { name: 'Role' })).toHaveValue('agent');
});

test('browsing the timezone list with arrow keys saves only where it stops, once, and keeps focus', () => {
  vi.useFakeTimers();
  const onSetWorkspaceTimezone = vi.fn();
  render(<SettingsPage sessions={[]} invites={[]} timezone="UTC" canManageWorkspace workspaceTimezone="UTC" onSetWorkspaceTimezone={onSetWorkspaceTimezone} />);
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
  render(<SettingsPage sessions={[]} invites={[]} timezone="UTC" workspaceTimezone="UTC" timezoneOverride="Asia/Ho_Chi_Minh" />);
  const select = screen.getByRole('combobox', { name: 'Your timezone' });
  expect(select).toHaveValue('Asia/Ho_Chi_Minh');
  const vietnam = within(select).getAllByRole('option').filter((option) => ['Asia/Ho_Chi_Minh', 'Asia/Saigon'].includes(option.getAttribute('value') ?? ''));
  expect(vietnam).toHaveLength(1);
});

test('after a failed timezone save, leaving the list again retries it', () => {
  const onSetAgentTimezone = vi.fn();
  const { rerender } = render(<SettingsPage sessions={[]} invites={[]} timezone="UTC" onSetAgentTimezone={onSetAgentTimezone} />);
  const select = screen.getByRole('combobox', { name: 'Your timezone' });
  fireEvent.change(select, { target: { value: 'Asia/Tokyo' } });
  fireEvent.blur(select);
  rerender(<SettingsPage sessions={[]} invites={[]} timezone="UTC" onSetAgentTimezone={onSetAgentTimezone} personalTimezoneSave={{ error: 'Unknown timezone.' }} />);
  fireEvent.blur(select);
  expect(onSetAgentTimezone.mock.calls).toEqual([['Asia/Tokyo'], ['Asia/Tokyo']]);
});
