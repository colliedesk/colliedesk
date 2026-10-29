import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render as renderInPage, screen, within } from '@testing-library/react';
import type { ReactElement, ReactNode } from 'react';
import { afterEach, expect, test, vi } from 'vitest';
import type { api } from '../../api/client';
import type { GmailSettings, MailboxConnection as Connection, MicrosoftSettings } from '../../api/schemas';
import { MailboxConnection, MicrosoftRequestNote } from './gmail-connection';
import { mailboxReturn } from './gmail-return';

const gmail: GmailSettings = {
  available: true, missing_settings: [], off_reason: null, redirect_uri: 'https://desk.example.com/api/v1/channels/email/gmail/callback', connection: null,
};
const approval = 'https://login.microsoftonline.com/organizations/v2.0/adminconsent?client_id=c&state=ch';
const microsoft: MicrosoftSettings = {
  available: true, missing_settings: [], off_reason: null, redirect_uri: 'https://desk.example.com/api/v1/channels/email/microsoft/callback',
  admin_consent_redirect_uri: 'https://desk.example.com/api/v1/channels/email/microsoft/admin-consent', admin_consent_url: approval,
  large_attachment_bytes: 3145728, server_credentials_refused: false, connection: null,
};
const msConnection: Connection = {
  provider: 'microsoft', address: 'support@example.com', status: 'connected', error_code: null, error: null, failed_at: null, connected_at: '2026-10-25T14:03:00Z',
};
const gmailConnection: Connection = { ...msConnection, provider: 'google' };

type PublicConfig = Awaited<ReturnType<typeof api.config>>;
const selfHosted: PublicConfig = {
  cloud_sign_in: { enabled: false, publishable_key: null }, billing: { enabled: false, switched_off: false }, signup: { enabled: false },
  limits: { outgoing_attachments_bytes: 10485760, outgoing_attachments_files: 10 },
  product_analytics: { enabled: false, key: null, host: null, realm: null },
  ai: { server_provider: false },
  autofix: { available: false, missing_settings: [], off_reason: null },
  features: { gmail: { enabled: true }, microsoft: { enabled: true }, autofix: { enabled: false }, ai_server: { enabled: false }, hosted_mail: { enabled: false },
    cloud_sign_in: { enabled: false }, billing: { enabled: false }, signup: { enabled: false } },
};
const cloud: PublicConfig = { ...selfHosted, cloud_sign_in: { enabled: true, publishable_key: 'pk_test' }, billing: { enabled: true, switched_off: false }, signup: { enabled: true } };

function render(ui: ReactElement, config: PublicConfig = selfHosted) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  client.setQueryData(['public-config'], config);
  return renderInPage(ui, { wrapper: ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider> });
}

const block = (props: Partial<Parameters<typeof MailboxConnection>[0]> = {}) =>
  <MailboxConnection gmail={gmail} microsoft={microsoft} supportAddress="support@example.com" timezone="UTC" canManage {...props} />;

afterEach(() => window.history.replaceState(null, '', '/'));

test('with both providers on the server, an admin chooses Gmail or Microsoft 365, and the admin approval link is one fold away', () => {
  const onConnect = vi.fn();
  render(block({ onConnect }));
  expect(screen.getByRole('heading', { name: 'Send from your mailbox' })).toBeInTheDocument();
  expect(screen.getByText(/Connect the mailbox of your support address/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Connect Microsoft 365' }));
  expect(onConnect).toHaveBeenLastCalledWith('microsoft');
  fireEvent.click(screen.getByRole('button', { name: 'Connect Gmail' }));
  expect(onConnect).toHaveBeenLastCalledWith('google');
  const fold = screen.getByText('Work account? Your IT admin may need to approve Colliedesk first').closest('details')!;
  expect(fold).not.toHaveAttribute('open');
  expect(within(fold).getByRole('textbox', { name: 'Approval link for your Microsoft 365 admin' })).toHaveValue(approval);
  expect(within(fold).getByRole('button', { name: 'Copy link' })).toBeInTheDocument();
});

test('while Microsoft opens, its button says so and no button can start another sign-in', () => {
  render(block({ connectPending: 'microsoft' }));
  expect(screen.getByRole('button', { name: 'Opening Microsoft…' })).toBeDisabled();
  expect(screen.getByRole('button', { name: 'Connect Gmail' })).toBeDisabled();
});

test('a provider the server lacks is folded away for the admin, with whom to ask, the guide and both redirect URIs', () => {
  render(block({ microsoft: { ...microsoft, available: false, admin_consent_url: null, missing_settings: [], off_reason: 'not_set_up' } }));
  expect(screen.queryByRole('button', { name: 'Connect Microsoft 365' })).not.toBeInTheDocument();
  const fold = screen.getByText('Microsoft 365 is not set up on this server').closest('details')!;
  expect(fold).toHaveTextContent('ask whoever runs it');
  expect(fold).not.toHaveTextContent('MICROSOFT_OAUTH');
  expect(within(fold).getByRole('link', { name: 'Self-hosting guide: send replies through Microsoft 365' })).toHaveAttribute('href', 'https://github.com/colliedesk/colliedesk/blob/main/docs/self-host/email.md#send-replies-through-microsoft-365');
  expect(within(fold).getByRole('textbox', { name: 'Redirect URI to register in Microsoft Entra' })).toHaveValue(microsoft.redirect_uri);
  expect(within(fold).getByRole('textbox', { name: /Second redirect URI/ })).toHaveValue(microsoft.admin_consent_redirect_uri);
  expect(screen.getByRole('button', { name: 'Connect Gmail' })).toBeInTheDocument();
});

test('a teammate sees the state without buttons or setup details', () => {
  render(block({ canManage: false, microsoft: { ...microsoft, available: false } }));
  expect(screen.getByText('Only an admin can connect a mailbox.')).toBeInTheDocument();
  expect(screen.queryByRole('button')).not.toBeInTheDocument();
  expect(screen.queryByText('Microsoft 365 is not set up on this server')).not.toBeInTheDocument();
});

test('a connected Microsoft mailbox says where replies leave from and offers another account, Gmail instead, or disconnect', () => {
  const onConnect = vi.fn();
  render(block({ microsoft: { ...microsoft, connection: msConnection }, onConnect }));
  expect(screen.getByText(/^Replies are sent through Microsoft 365 from support@example.com, connected Oct 25, 2:03 PM\./)).toBeInTheDocument();
  expect(screen.getByText(/stay in its Sent Items/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Use another Microsoft account' }));
  expect(onConnect).toHaveBeenLastCalledWith('microsoft');
  expect(screen.getByRole('button', { name: 'Use Gmail instead' })).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Disconnect Microsoft 365' })).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Connect Gmail' })).not.toBeInTheDocument();
  // Connected, the admin approval is done: its link is not offered again.
  expect(screen.queryByText('Work account? Your IT admin may need to approve Colliedesk first')).not.toBeInTheDocument();
});

test('switching provider asks first, says what happens to waiting replies, and Cancel is the default', () => {
  const onConnect = vi.fn();
  render(block({ gmail: { ...gmail, connection: gmailConnection }, onConnect }));
  fireEvent.click(screen.getByRole('button', { name: 'Use Microsoft 365 instead' }));
  const question = screen.getByRole('group', { name: 'Connect Microsoft 365 instead?' });
  expect(question).toHaveTextContent('Connecting Microsoft 365 replaces Gmail for this channel. Replies still waiting for Gmail fail and can be retried.');
  expect(within(question).getByRole('button', { name: 'Cancel' })).toHaveFocus();
  fireEvent.keyDown(question, { key: 'Escape' });
  expect(screen.getByRole('button', { name: 'Use Microsoft 365 instead' })).toHaveFocus();
  expect(onConnect).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Use Microsoft 365 instead' }));
  fireEvent.click(screen.getByRole('button', { name: 'Continue to Microsoft' }));
  expect(onConnect).toHaveBeenCalledWith('microsoft');
});

test('disconnecting Microsoft 365 is honest that Microsoft still lists the app, and links where to remove it', () => {
  const onDisconnect = vi.fn();
  render(block({ microsoft: { ...microsoft, connection: msConnection }, onDisconnect }));
  fireEvent.click(screen.getByRole('button', { name: 'Disconnect Microsoft 365' }));
  const question = screen.getByRole('group', { name: 'Disconnect Microsoft 365?' });
  expect(question).toHaveTextContent('Colliedesk deletes its access to support@example.com. Microsoft still lists Colliedesk among the account\'s apps until you remove it there');
  expect(within(question).getByRole('link', { name: 'My Apps' })).toHaveAttribute('href', 'https://myapps.microsoft.com');
  expect(within(question).getByRole('link', { name: 'Microsoft account permissions' })).toHaveAttribute('href', 'https://account.live.com/consent/Manage');
  expect(within(question).getByRole('button', { name: 'Cancel' })).toHaveFocus();
  fireEvent.click(within(question).getByRole('button', { name: 'Yes, disconnect Microsoft 365' }));
  expect(onDisconnect).toHaveBeenCalledWith('microsoft');
});

test.each([
  ['revoked', 'access was removed, the password was reset, or a sign-in policy of your organization now blocks it'],
  ['permission', 'Microsoft 365 refused to send from this mailbox'],
  ['mailbox_unavailable', 'no Exchange Online mailbox any more'],
] as const)('a Microsoft connection that stopped (%s) is an alert with the reason and a reconnect', (code, reason) => {
  const onConnect = vi.fn();
  render(block({ microsoft: { ...microsoft, connection: { ...msConnection, status: 'failed', error_code: code, failed_at: '2026-10-25T15:00:00Z' } }, onConnect }));
  const alert = screen.getByRole('alert');
  expect(alert).toHaveTextContent('Microsoft 365 stopped sending');
  expect(alert).toHaveTextContent(reason);
  expect(alert).toHaveTextContent('not sent through Microsoft 365 and not sent any other way');
  fireEvent.click(within(alert).getByRole('button', { name: 'Reconnect Microsoft 365' }));
  expect(onConnect).toHaveBeenCalledWith('microsoft');
  // A sign-in policy may need the admin's approval again before the reconnect.
  expect(screen.getByText('Work account? Your IT admin may need to approve Colliedesk first')).toBeInTheDocument();
});

test('when Microsoft refuses the server\'s secret the connection stays, and the admin is told reconnecting does not help', () => {
  render(block({ microsoft: { ...microsoft, connection: msConnection, server_credentials_refused: true } }));
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  expect(screen.getByRole('note')).toHaveTextContent('whoever runs this server must replace its Microsoft app secret');
  expect(screen.getByRole('note')).not.toHaveTextContent('MICROSOFT_OAUTH');
  expect(screen.getByRole('note')).toHaveTextContent('reconnecting does not help');
});

test('a Microsoft mailbox that is not the support address says to connect the matching one', () => {
  render(block({ microsoft: { ...microsoft, address_matches: false, connection: { ...msConnection, address: 'old@example.com' } } }));
  expect(screen.getByRole('note')).toHaveTextContent('old@example.com is not this channel\'s support address. Replies still leave through it; connect Microsoft 365 again with support@example.com to match.');
});

test('Microsoft saying an admin must approve opens the approval link with the outcome', () => {
  render(block({ returned: { kind: 'error', reason: 'denied', provider: 'microsoft' } }));
  expect(screen.getByRole('alert')).toHaveTextContent('If Microsoft said you need admin approval, send your Microsoft 365 admin the approval link below.');
  expect(screen.getByText('Work account? Your IT admin may need to approve Colliedesk first').closest('details')).toHaveAttribute('open');
});

test('Microsoft signing in another mailbox names aliases and shared mailboxes and which mailbox to choose', () => {
  render(block({ returned: { kind: 'error', reason: 'address_mismatch', provider: 'microsoft' } }));
  expect(screen.getByRole('alert')).toHaveTextContent('Microsoft signed in another mailbox. A channel sends as its own support address. If support@example.com is an alias or a shared mailbox in Microsoft 365, it cannot be connected yet. Select Connect Microsoft 365 and choose support@example.com at Microsoft.');
});

test.each([
  ['signed_out', 'sign in to Colliedesk in this browser'],
  ['forbidden', 'only an admin of an active workspace can connect it'],
  ['expired', 'the Microsoft sign-in took too long or was already used'],
  ['wrong_person', 'someone else started this connection'],
  ['send_not_granted', 'Microsoft did not grant permission to send email'],
  ['no_refresh_token', 'Microsoft did not grant lasting access'],
  ['mailbox_unavailable', 'created with another provider\'s address (for example a Gmail address)'],
  ['account_in_use', 'another email channel of this workspace already sends through this account'],
  ['email_channel_missing', 'save a support address first'],
  ['server_misconfigured', 'whoever runs this server must replace its Microsoft app secret'],
  ['microsoft_unavailable', 'Microsoft could not be reached'],
  ['admin_consent_failed', 'The approval was not completed at Microsoft'],
  ['<script>', 'Microsoft 365 could not be connected. Try again.'],
])('the outcome %s Microsoft sent back is explained, never echoed', (reason, text) => {
  render(block({ returned: { kind: 'error', reason, provider: 'microsoft' } }));
  expect(screen.getByRole('alert')).toHaveTextContent(text);
  expect(screen.getByRole('alert')).not.toHaveTextContent('<script>');
});

test('the success and an admin\'s approval are statuses', () => {
  const { rerender } = render(block({ microsoft: { ...microsoft, connection: msConnection }, returned: { kind: 'connected', provider: 'microsoft' } }));
  expect(screen.getByRole('status')).toHaveTextContent('Microsoft 365 is connected.');
  rerender(block({ returned: { kind: 'admin_consent_done', provider: 'microsoft' } }));
  expect(screen.getByRole('status')).toHaveTextContent('Your Microsoft 365 admin approved Colliedesk. Select Connect Microsoft 365 to finish.');
});

test('Microsoft\'s outcome is read once from the address, with its provider, and cleaned', () => {
  window.history.replaceState(null, '', '/settings/channels/abc?tab=replies&microsoft_error=account_in_use&microsoft_channel=def');
  expect(mailboxReturn()).toEqual({ kind: 'error', reason: 'account_in_use', channel: 'def', provider: 'microsoft' });
  expect(window.location.search).toBe('?tab=replies');
  window.history.replaceState(null, '', '/settings/channels/abc?tab=replies&microsoft=admin_consent_done');
  expect(mailboxReturn()).toEqual({ kind: 'admin_consent_done', provider: 'microsoft' });
  expect(mailboxReturn()).toBeUndefined();
  // Only Microsoft has an admin approval; a Gmail one is not read.
  window.history.replaceState(null, '', '/settings?gmail=admin_consent_done');
  expect(mailboxReturn()).toBeUndefined();
});

test('on Colliedesk Cloud each provider links how its data is handled; Microsoft gets no Limited Use statement', () => {
  render(block({ gmail: { ...gmail, available: false } }), cloud);
  expect(screen.getByText(/How Colliedesk handles data from Microsoft/)).toBeInTheDocument();
  expect(screen.getByRole('link', { name: 'Privacy policy' })).toHaveAttribute('href', 'https://colliedesk.com/privacy#microsoft-user-data');
  expect(screen.queryByText(/Limited Use/)).not.toBeInTheDocument();
});

test('the approver of an agent\'s request to connect Microsoft 365 reads that a work account may need its admin first', () => {
  render(<MicrosoftRequestNote />);
  expect(screen.getByText('Approving opens Microsoft\'s sign-in page. A work account may need its Microsoft 365 admin\'s approval first.')).toBeInTheDocument();
});

test('on Colliedesk Cloud a server without Microsoft settings leaves Microsoft 365 out, with no settings to name', () => {
  const missing = { ...microsoft, available: false, admin_consent_url: null, missing_settings: ['MICROSOFT_OAUTH_CLIENT_ID' as const, 'MICROSOFT_OAUTH_CLIENT_SECRET' as const] };
  render(block({ microsoft: missing }), cloud);
  expect(screen.queryByText(/Microsoft 365/)).not.toBeInTheDocument();
  expect(screen.queryByText(/MICROSOFT_OAUTH/)).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Connect Gmail' })).toBeInTheDocument();
});

test('a self-hosted admin still sees that Microsoft 365 is not set up and whom to ask, without server variable names', () => {
  const missing = { ...microsoft, available: false, admin_consent_url: null, missing_settings: ['MICROSOFT_OAUTH_CLIENT_ID' as const] };
  render(block({ microsoft: missing }));
  expect(screen.getByText('Microsoft 365 is not set up on this server')).toBeInTheDocument();
  expect(screen.getByText(/ask whoever runs it/)).toBeInTheDocument();
  expect(screen.queryByText(/MICROSOFT_OAUTH_CLIENT_ID/)).not.toBeInTheDocument();
});

test('Microsoft asking for an admin\'s approval while a channel already sends offers the approval link there too', () => {
  const { rerender } = render(block({ gmail: { ...gmail, connection: gmailConnection }, returned: { kind: 'error', reason: 'denied', provider: 'microsoft' } }));
  const fold = () => screen.getByText('Work account? Your IT admin may need to approve Colliedesk first').closest('details')!;
  expect(fold()).toHaveAttribute('open');
  expect(within(fold() as HTMLElement).getByRole('textbox', { name: 'Approval link for your Microsoft 365 admin' })).toHaveValue(approval);
  rerender(block({ microsoft: { ...microsoft, connection: msConnection }, returned: { kind: 'error', reason: 'denied', provider: 'microsoft' } }));
  expect(fold()).toHaveAttribute('open');
});

test('an outcome names the button that is on screen now', () => {
  const { rerender } = render(block({ microsoft: { ...microsoft, connection: msConnection }, returned: { kind: 'error', reason: 'address_mismatch', provider: 'microsoft' } }));
  expect(screen.getByRole('alert')).toHaveTextContent('Select Use another Microsoft account and choose support@example.com at Microsoft.');
  rerender(block({ gmail: { ...gmail, connection: gmailConnection }, returned: { kind: 'admin_consent_done', provider: 'microsoft' } }));
  expect(screen.getByRole('status')).toHaveTextContent('Your Microsoft 365 admin approved Colliedesk. Select Use Microsoft 365 instead to finish.');
});

test('a server that lost its Microsoft settings says what to set, not that a wrong secret is being retried', () => {
  render(block({ microsoft: { ...microsoft, available: false, missing_settings: [], off_reason: 'not_set_up', connection: msConnection, server_credentials_refused: true } }));
  expect(screen.getByRole('note')).toHaveTextContent('Microsoft 365 sending is not set up on this server');
  expect(screen.queryByText(/misconfigured/)).not.toBeInTheDocument();
});

test('the approval fold names the button that starts the sign-in now', () => {
  render(block({ gmail: { ...gmail, connection: gmailConnection }, returned: { kind: 'error', reason: 'denied', provider: 'microsoft' } }));
  expect(screen.getByText(/After they approve, select Use Microsoft 365 instead again\./)).toBeInTheDocument();
});

// Switched off by the operator: available is false with nothing missing.
const gmailOff: GmailSettings = { ...gmail, available: false, off_reason: 'switched_off' };

test('Gmail switched off keeps its connection: the replies wait, said plainly, and it can still be disconnected', () => {
  render(block({ gmail: { ...gmailOff, connection: gmailConnection }, microsoft: { ...microsoft, connection: null } }));
  expect(screen.getByRole('note')).toHaveTextContent('Gmail sending is turned off on this server: replies wait in the outbox and are sent once it is on again.');
  expect(screen.queryByText(/not set up on this server/)).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Use another Gmail account' })).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Disconnect Gmail' })).toBeInTheDocument();
});

test('a provider switched off is one neutral line for a self-hosted admin, without the setup guide', () => {
  render(block({ gmail: gmailOff }));
  expect(screen.getByText('Gmail is not turned on on this server.')).toBeInTheDocument();
  expect(screen.queryByText(/not set up on this server/)).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Connect Gmail' })).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Connect Microsoft 365' })).toBeInTheDocument();
});

test('on Colliedesk Cloud a provider switched off is not mentioned, and with every provider off the block is gone', () => {
  render(block({ gmail: gmailOff }), cloud);
  expect(screen.queryByText('Gmail is not turned on on this server.')).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Connect Microsoft 365' })).toBeInTheDocument();
  const { container } = render(block({ gmail: gmailOff, microsoft: { ...microsoft, available: false, admin_consent_url: null, off_reason: 'switched_off' } }), cloud);
  expect(container).toBeEmptyDOMElement();
});
