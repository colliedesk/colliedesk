import { fireEvent, render, screen, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { SettingsPage } from './settings-page';

test('creates an invite and exposes its one-time URL', () => {
  const onCreateInvite = vi.fn();
  render(<SettingsPage sessions={[]} invites={[]} timezone="UTC"
    canCreateInvites
    createdInviteUrl="https://desk.example.com/redeem?token=once"
    onCreateInvite={onCreateInvite} />);

  fireEvent.change(screen.getByLabelText(/invite email/i), { target: { value: 'agent@example.com' } });
  fireEvent.click(screen.getByRole('button', { name: /create invite/i }));

  expect(onCreateInvite).toHaveBeenCalledWith('agent@example.com', 'agent');
  expect(screen.getByRole('textbox', { name: /one-time invite link/i })).toHaveValue('https://desk.example.com/redeem?token=once');
});

test('does not offer invite creation without admin permission', () => {
  render(<SettingsPage sessions={[]} invites={[]} timezone="UTC" canCreateInvites={false} />);
  expect(screen.queryByRole('button', { name: /create invite/i })).not.toBeInTheDocument();
});

test('updates workspace and personal timezone settings', () => {
  const onSetWorkspaceTimezone = vi.fn();
  const onSetAgentTimezone = vi.fn();
  render(<SettingsPage sessions={[]} invites={[]} timezone="UTC" canCreateInvites canManageWorkspace
    workspaceTimezone="UTC" timezoneOverride="Europe/London"
    onSetWorkspaceTimezone={onSetWorkspaceTimezone} onSetAgentTimezone={onSetAgentTimezone} />);

  fireEvent.change(screen.getByRole('textbox', { name: /workspace timezone/i }), { target: { value: 'America/New_York' } });
  fireEvent.click(screen.getByRole('button', { name: /save workspace timezone/i }));
  fireEvent.change(screen.getByRole('textbox', { name: /personal timezone override/i }), { target: { value: 'Asia/Tokyo' } });
  fireEvent.click(screen.getByRole('button', { name: /save personal timezone/i }));
  fireEvent.click(screen.getByRole('button', { name: /clear personal timezone/i }));

  expect(onSetWorkspaceTimezone).toHaveBeenCalledWith('America/New_York');
  expect(onSetAgentTimezone.mock.calls).toEqual([['Asia/Tokyo'], [null]]);
});

test('surfaces a failed revoke operation', () => {
  render(<SettingsPage sessions={[]} invites={[]} timezone="UTC" error="The revoke failed." />);
  expect(screen.getByRole('alert')).toHaveTextContent('The revoke failed.');
});

test('reflects timezone values loaded after the settings page mounts', () => {
  const { rerender } = render(<SettingsPage sessions={[]} invites={[]} timezone="UTC" canManageWorkspace />);
  rerender(<SettingsPage sessions={[]} invites={[]} timezone="America/New_York" canManageWorkspace
    workspaceTimezone="America/New_York" timezoneOverride="Europe/London" />);
  expect(screen.getByRole('textbox', { name: /workspace timezone/i })).toHaveValue('America/New_York');
  expect(screen.getByRole('textbox', { name: /personal timezone override/i })).toHaveValue('Europe/London');
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
  const base = { agent_id: '55555555-5555-4555-8555-555555555555', agent_email: 'sam@example.com', label: 'Chrome', created_at: '2026-10-14T00:00:00Z', last_seen_at: '2026-10-14T00:00:00Z' };
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
