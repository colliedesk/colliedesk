import { fireEvent, render, screen } from '@testing-library/react';
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
