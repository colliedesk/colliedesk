import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { ReactElement } from 'react';
import { afterEach, expect, test } from 'vitest';
import { EmailChannelSection } from './email-channel-section';
import { NameForm } from './name-form';
import { SettingsEditsProvider } from './settings-edits';

afterEach(cleanup);

// Text that has its own button (a step that adds something) draws no save bar, but closing the tab or leaving the
// page with it typed must still ask: the browser's own question is how a closing tab asks.
const closingTabAsks = () => !window.dispatchEvent(new Event('beforeunload', { cancelable: true }));
const draw = (ui: ReactElement) => render(ui, { wrapper: SettingsEditsProvider });

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

test('an address being saved is still asked about on the way out: a save that fails must not lose it', async () => {
  const { AddressForm } = await import('../../channels/email/address-form');
  let refuse: (error: Error) => void = () => undefined;
  const view = draw(<QueryClientProvider client={new QueryClient({ defaultOptions: { mutations: { retry: false } } })}><AddressForm canManage save={() => new Promise((_, reject) => { refuse = reject; })} /></QueryClientProvider>);
  fireEvent.change(screen.getByRole('textbox', { name: /support address/i }), { target: { value: 'help@example.com' } });
  await waitFor(() => expect(closingTabAsks()).toBe(true));
  fireEvent.click(screen.getByRole('button', { name: /save support address/i }));
  expect(closingTabAsks()).toBe(true);
  refuse(new Error('refused'));
  await waitFor(() => expect(screen.queryByRole('button', { name: 'Saving…' })).toBeNull());
  expect(closingTabAsks()).toBe(true);
  view.unmount();
});
