import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { ReactElement } from 'react';
import { afterEach, expect, test } from 'vitest';
import { builtInRoles } from '../../test-roles';
import { EmailChannelSection } from './email-channel-section';
import { InvitesSection } from './invites-section';
import { NameForm } from './name-form';
import { SettingsEditsProvider } from './settings-edits';

afterEach(cleanup);

// Text that has its own button (a step that adds something) draws no save bar, but closing the tab or leaving the
// page with it typed must still ask: the browser's own question is how a closing tab asks.
const closingTabAsks = () => !window.dispatchEvent(new Event('beforeunload', { cancelable: true }));
const draw = (ui: ReactElement) => render(ui, { wrapper: SettingsEditsProvider });

test('an invite being typed counts as unsaved, and a sent or emptied one does not', async () => {
  draw(<InvitesSection invites={[]} timezone="UTC" canCreateInvites roles={builtInRoles} />);
  expect(closingTabAsks()).toBe(false);
  fireEvent.change(screen.getByLabelText('Invite email'), { target: { value: 'new@example.com' } });
  await waitFor(() => expect(closingTabAsks()).toBe(true));
  expect(screen.queryByRole('region', { name: 'Unsaved changes' })).toBeNull();
  fireEvent.change(screen.getByLabelText('Invite email'), { target: { value: '' } });
  await waitFor(() => expect(closingTabAsks()).toBe(false));
});

test('a support address or workspace address name being typed in the email setup counts as unsaved', async () => {
  const connected = { channel: { id: 'c1', support_address: 'help@example.com', sender_name: 'Acme', domain: 'example.com', domain_verified_at: null, status: { state: 'working' } }, inbound_url: 'https://desk.example.com/in',
    smtp: { configured: false, host: '', port: 587, starttls: true, authenticated: false }, gmail: { connection: null }, microsoft: { connection: null } } as never;
  draw(<EmailChannelSection settings={connected} timezone="UTC" canManage onSave={() => undefined} />);
  expect(closingTabAsks()).toBe(false);
  fireEvent.change(screen.getByRole('textbox', { name: /support address/i }), { target: { value: 'other@example.com' } });
  await waitFor(() => expect(closingTabAsks()).toBe(true));
});

test('a name typed into a step that has its own button counts as unsaved', async () => {
  draw(<NameForm label="Workspace address name" saveLabel="Save address name" hint="Where it shows" value="acme" canManage />);
  expect(closingTabAsks()).toBe(false);
  fireEvent.change(screen.getByLabelText('Workspace address name'), { target: { value: 'acme-desk' } });
  await waitFor(() => expect(closingTabAsks()).toBe(true));
});
