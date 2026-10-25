import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render as renderInPage, screen, within } from '@testing-library/react';
import type { ReactElement, ReactNode } from 'react';
import { afterEach, expect, test, vi } from 'vitest';
import type { api } from '../../api/client';
import type { GmailSettings } from '../../api/schemas';
import { GmailConnection } from './gmail-connection';
import { gmailReturn } from './gmail-return';

const available: GmailSettings = {
  available: true, missing_settings: [], redirect_uri: 'https://desk.example.com/api/v1/channels/email/gmail/callback', connection: null,
};
const connected: GmailSettings = {
  ...available,
  connection: { provider: 'google', address: 'support@example.com', status: 'connected', error_code: null, error: null, failed_at: null, connected_at: '2026-10-19T08:00:00Z' },
};
const failed: GmailSettings = {
  ...available,
  connection: {
    ...connected.connection!, status: 'failed', error_code: 'revoked', failed_at: '2026-10-19T09:00:00Z',
    error: 'Google no longer accepts the Gmail connection (access was removed or expired). Reconnect Gmail in Settings, then retry the reply.',
  },
};

type PublicConfig = Awaited<ReturnType<typeof api.config>>;
const selfHosted: PublicConfig = {
  cloud_sign_in: { enabled: false, publishable_key: null }, billing: { enabled: false }, signup: { enabled: false },
  limits: { outgoing_attachments_bytes: 10485760, outgoing_attachments_files: 10 },
  product_analytics: { enabled: false, key: null, host: null, realm: null },
  ai: { server_provider: false },
};
const cloud: PublicConfig = {
  ...selfHosted, cloud_sign_in: { enabled: true, publishable_key: 'pk_test' }, billing: { enabled: true }, signup: { enabled: true },
};

// The block reads the server's public config to know whether it runs on Colliedesk Cloud.
function render(ui: ReactElement, config: PublicConfig = selfHosted) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  client.setQueryData(['public-config'], config);
  return renderInPage(ui, { wrapper: ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider> });
}

afterEach(() => window.history.replaceState(null, '', '/'));

test('on Colliedesk Cloud the Connect button comes with the Limited Use statement and a link to how Google data is handled', () => {
  render(<GmailConnection gmail={available} supportAddress="support@example.com" timezone="UTC" canManage />, cloud);
  expect(screen.getByText(/adheres to the Google API Services User Data Policy, including the Limited Use requirements/)).toBeInTheDocument();
  expect(screen.getByRole('link', { name: 'Privacy policy' })).toHaveAttribute('href', 'https://colliedesk.com/privacy#google-user-data');
});

test('on Colliedesk Cloud the statement also comes before a reconnect sends the admin back to Google', () => {
  render(<GmailConnection gmail={failed} supportAddress="support@example.com" timezone="UTC" canManage />, cloud);
  expect(screen.getByRole('link', { name: 'Privacy policy' })).toBeInTheDocument();
});

test('a self-hosted server does not point to the privacy policy of Colliedesk Cloud', () => {
  render(<GmailConnection gmail={available} supportAddress="support@example.com" timezone="UTC" canManage />);
  expect(screen.getByText(/cannot read the mailbox/)).toBeInTheDocument();
  expect(screen.queryByRole('link', { name: 'Privacy policy' })).not.toBeInTheDocument();
  expect(screen.queryByText(/Limited Use/)).not.toBeInTheDocument();
});

test('analytics that name the realm decide, like the server does', () => {
  render(<GmailConnection gmail={available} supportAddress="support@example.com" timezone="UTC" canManage />,
    { ...cloud, product_analytics: { enabled: true, key: 'k', host: 'https://h', realm: 'self-hosted' } });
  expect(screen.queryByRole('link', { name: 'Privacy policy' })).not.toBeInTheDocument();
});

test('an admin connects Gmail from the email channel', () => {
  const onConnect = vi.fn();
  render(<GmailConnection gmail={available} supportAddress="support@example.com" timezone="UTC" canManage onConnect={onConnect} />);
  expect(screen.getByRole('heading', { name: 'Send from Gmail' })).toBeInTheDocument();
  expect(screen.getByText(/cannot read the mailbox/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Connect Gmail' }));
  expect(onConnect).toHaveBeenCalled();
});

test('while Google opens the button says so and cannot be pressed twice', () => {
  render(<GmailConnection gmail={available} supportAddress="support@example.com" timezone="UTC" canManage connectPending />);
  expect(screen.getByRole('button', { name: 'Opening Google…' })).toBeDisabled();
});

test('a connected account shows the address and disconnects only after confirming', () => {
  const onDisconnect = vi.fn();
  render(<GmailConnection gmail={connected} supportAddress="support@example.com" timezone="UTC" canManage onDisconnect={onDisconnect} />);
  expect(screen.getByText(/Replies are sent through Gmail from support@example.com/)).toBeInTheDocument();
  expect(screen.queryByText(/Customers see/)).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Disconnect Gmail' }));
  expect(onDisconnect).not.toHaveBeenCalled();
  const question = screen.getByRole('group', { name: 'Disconnect Gmail?' });
  expect(within(question).getByRole('button', { name: 'Cancel' })).toHaveFocus();
  fireEvent.click(within(question).getByRole('button', { name: 'Yes, disconnect Gmail' }));
  expect(onDisconnect).toHaveBeenCalled();
});

test('a Gmail address other than the support address says where answers go', () => {
  const other = { ...connected, connection: { ...connected.connection!, address: 'acme.help@gmail.com' } };
  render(<GmailConnection gmail={other} supportAddress="support@example.com" timezone="UTC" canManage />);
  expect(screen.getByText('Customers see acme.help@gmail.com as the sender. Their answers still go to support@example.com, so keep forwarding it here.')).toBeInTheDocument();
});

test('a failed connection is an alert with the reason and a reconnect, never silent', () => {
  const onConnect = vi.fn();
  render(<GmailConnection gmail={failed} supportAddress="support@example.com" timezone="UTC" canManage onConnect={onConnect} />);
  const alert = screen.getByRole('alert');
  expect(alert).toHaveTextContent('Gmail stopped sending');
  expect(alert).toHaveTextContent('Google no longer accepts the connection');
  expect(alert).toHaveTextContent('not sent any other way');
  fireEvent.click(within(alert).getByRole('button', { name: 'Reconnect Gmail' }));
  expect(onConnect).toHaveBeenCalled();
});

test('agents see the state but no buttons', () => {
  render(<GmailConnection gmail={failed} supportAddress="support@example.com" timezone="UTC" canManage={false} />);
  expect(screen.getByRole('alert')).toHaveTextContent('Gmail stopped sending');
  expect(screen.queryByRole('button')).not.toBeInTheDocument();
});

test('a server without Google settings names them and the redirect URI to register', () => {
  const missing: GmailSettings = { ...available, available: false, missing_settings: ['GOOGLE_OAUTH_CLIENT_ID', 'MAILBOX_TOKEN_KEYS'] };
  render(<GmailConnection gmail={missing} supportAddress="support@example.com" timezone="UTC" canManage />);
  expect(screen.getByText(/GOOGLE_OAUTH_CLIENT_ID, MAILBOX_TOKEN_KEYS/)).toBeInTheDocument();
  expect(screen.getByRole('textbox', { name: 'Redirect URI to register in Google Cloud' })).toHaveValue(available.redirect_uri);
  expect(screen.queryByRole('button', { name: 'Connect Gmail' })).not.toBeInTheDocument();
});

test('a connection the server can no longer use says why instead of claiming replies go through Gmail', () => {
  render(<GmailConnection gmail={{ ...connected, available: false, missing_settings: ['MAILBOX_TOKEN_KEYS'] }} supportAddress="support@example.com" timezone="UTC" canManage />);
  expect(screen.getByText(/not set up on this server.*MAILBOX_TOKEN_KEYS/)).toBeInTheDocument();
  expect(screen.queryByText(/Replies are sent through Gmail/)).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Disconnect Gmail' })).toBeInTheDocument();
});

test('the outcome Google sent back is read once from the address and explained, never echoed', () => {
  window.history.replaceState(null, '', '/settings?gmail_error=send_not_granted');
  const refused = gmailReturn();
  expect(refused).toEqual({ kind: 'error', reason: 'send_not_granted' });
  expect(window.location.search).toBe('');
  window.history.replaceState(null, '', '/settings?gmail=connected');
  expect(gmailReturn()).toEqual({ kind: 'connected' });
  expect(gmailReturn()).toBeUndefined();
  const { rerender } = render(<GmailConnection gmail={available} supportAddress="support@example.com" timezone="UTC" canManage returned={refused} />);
  expect(screen.getByRole('alert')).toHaveTextContent('Send email on your behalf');
  rerender(<GmailConnection gmail={available} supportAddress="support@example.com" timezone="UTC" canManage returned={{ kind: 'error', reason: '<script>' }} />);
  expect(screen.getByRole('alert')).toHaveTextContent('Gmail could not be connected. Try again.');
});

test('a teammate who is not an admin is told who can connect Gmail', () => {
  render(<GmailConnection gmail={available} supportAddress="support@example.com" timezone="UTC" canManage={false} />);
  expect(screen.getByText('Only an admin can connect Gmail.')).toBeInTheDocument();
});

test('on the Settings page the failure does not send you to Settings, and the long invitation is gone once connected', () => {
  const { rerender } = render(<GmailConnection gmail={failed} supportAddress="support@example.com" timezone="UTC" canManage />);
  expect(screen.getByRole('alert')).not.toHaveTextContent('in Settings');
  expect(screen.getByRole('alert')).toHaveTextContent(/access was removed or expired/);
  rerender(<GmailConnection gmail={connected} supportAddress="support@example.com" timezone="UTC" canManage />);
  expect(screen.queryByText(/Connect the Gmail account of your support address/)).not.toBeInTheDocument();
  expect(screen.getByText(/Colliedesk can only send; it cannot read the mailbox/)).toBeInTheDocument();
});

test('Escape closes the disconnect question like Cancel', () => {
  const onDisconnect = vi.fn();
  render(<GmailConnection gmail={connected} supportAddress="support@example.com" timezone="UTC" canManage onDisconnect={onDisconnect} />);
  fireEvent.click(screen.getByRole('button', { name: 'Disconnect Gmail' }));
  fireEvent.keyDown(screen.getByRole('group', { name: 'Disconnect Gmail?' }), { key: 'Escape' });
  expect(screen.queryByRole('group', { name: 'Disconnect Gmail?' })).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Disconnect Gmail' })).toHaveFocus();
  expect(onDisconnect).not.toHaveBeenCalled();
});

test('the outcome is shown in the block', () => {
  render(<GmailConnection gmail={connected} supportAddress="support@example.com" timezone="UTC" canManage returned={{ kind: 'connected' }} />);
  expect(screen.getByRole('status')).toHaveTextContent('Gmail is connected.');
});

test('a Gmail account that is not the support address keeps sending and says to connect the matching one', () => {
  render(<GmailConnection gmail={{ ...connected, address_matches: false, connection: { ...connected.connection!, address: 'old@example.com' } }} supportAddress="support@example.com" timezone="UTC" canManage />);
  expect(screen.getByRole('note')).toHaveTextContent('old@example.com is not this channel\'s support address. Replies still leave through it; connect Gmail again with support@example.com to match.');
});

test('an account another channel sends through is refused, with a link to that channel', () => {
  render(<GmailConnection gmail={available} supportAddress="support@example.com" timezone="UTC" canManage
    returned={{ kind: 'error', reason: 'account_in_use', channel: '99999999-9999-4999-8999-999999999999' }} />);
  const alert = screen.getByRole('alert');
  expect(alert).toHaveTextContent('another email channel of this workspace already sends through this account');
  expect(within(alert).getByRole('link')).toHaveAttribute('href', '/settings/channels/99999999-9999-4999-8999-999999999999?tab=replies');
});

test('the channel a refused account belongs to is read from the address and cleaned from it', () => {
  window.history.replaceState(null, '', '/settings/channels/abc?tab=replies&gmail_error=account_in_use&gmail_channel=def');
  expect(gmailReturn()).toEqual({ kind: 'error', reason: 'account_in_use', channel: 'def' });
  expect(window.location.search).toBe('?tab=replies');
});

test('Google signing in another account says which account to choose', () => {
  render(<GmailConnection gmail={available} supportAddress="support@example.com" timezone="UTC" canManage returned={{ kind: 'error', reason: 'address_mismatch' }} />);
  expect(screen.getByRole('alert')).toHaveTextContent('Google signed in another account. A channel sends as its own support address. Select Connect Gmail and choose support@example.com at Google.');
});
