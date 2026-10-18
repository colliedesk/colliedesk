import { fireEvent, render, screen, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import type { EmailChannelSettings } from '../../api/schemas';
import { EmailChannelSection } from './email-channel-section';

const disconnected: EmailChannelSettings = {
  channel: null,
  inbound_url: 'https://desk.example.com/api/v1/channels/email/inbound',
  smtp: { configured: false, host: '', port: 587, starttls: true, authenticated: false },
  from_policy: 'verified',
  sending_from: null,
  hosted_address: null,
  domain_verification: 'txt',
  sending_pause: null,
};
const connected: EmailChannelSettings = {
  ...disconnected,
  channel: {
    support_address: 'support@example.com', secret_set: true, secret_rotated_at: '2026-10-14T09:00:00Z',
    domain: 'example.com', domain_verified_at: null, dns_record_name: '_colliedesk.example.com', dns_record_value: 'colliedesk-verification=abc123', dns_records: [],
    created_at: '2026-10-14T08:00:00Z', updated_at: '2026-10-14T09:00:00Z',
  },
  sending_from: 'reply+…@mail.colliedesk.example',
  smtp: { configured: true, host: 'smtp.example.net', port: 587, starttls: true, authenticated: true },
};

test('connects a support address and shows the inbound URL', () => {
  const onSave = vi.fn();
  render(<EmailChannelSection settings={disconnected} timezone="UTC" canManage onSave={onSave} />);
  expect(screen.getByRole('textbox', { name: /inbound url/i })).toHaveValue(disconnected.inbound_url);
  expect(screen.getByText(/outgoing email is not configured/i)).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /disconnect email/i })).not.toBeInTheDocument();
  fireEvent.change(screen.getByRole('textbox', { name: /support address/i }), { target: { value: 'help@example.com' } });
  fireEvent.click(screen.getByRole('button', { name: /save support address/i }));
  expect(onSave).toHaveBeenCalledWith('help@example.com');
});

test('rotates the inbound secret, shows it once and disconnects', () => {
  const onRotate = vi.fn();
  const onDisconnect = vi.fn();
  const { rerender } = render(<EmailChannelSection settings={connected} timezone="UTC" canManage onRotate={onRotate} onDisconnect={onDisconnect} />);
  expect(screen.getByRole('textbox', { name: /support address/i })).toHaveValue('support@example.com');
  expect(screen.getByText(/smtp\.example\.net:587/)).toBeInTheDocument();
  expect(screen.queryByRole('textbox', { name: /inbound secret/i })).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: /rotate inbound secret/i }));
  expect(onRotate).toHaveBeenCalled();
  rerender(<EmailChannelSection settings={connected} timezone="UTC" canManage createdSecret="key.secret" onRotate={onRotate} onDisconnect={onDisconnect} />);
  expect(screen.getByRole('textbox', { name: /inbound secret/i })).toHaveValue('key.secret');
  fireEvent.click(screen.getByRole('button', { name: /disconnect email/i }));
  expect(onDisconnect).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Yes, disconnect' }));
  expect(onDisconnect).toHaveBeenCalled();
});

test('disconnecting asks first, says what stops, and can be cancelled', () => {
  const onDisconnect = vi.fn();
  render(<EmailChannelSection settings={connected} timezone="UTC" canManage onDisconnect={onDisconnect} />);
  fireEvent.click(screen.getByRole('button', { name: /disconnect email/i }));
  const question = screen.getByRole('group', { name: 'Disconnect email?' });
  expect(question).toHaveTextContent('mail sent to support@example.com stops arriving and replies are no longer emailed');
  expect(within(question).getByRole('button', { name: 'Cancel' })).toHaveFocus();
  fireEvent.click(within(question).getByRole('button', { name: 'Cancel' }));
  expect(onDisconnect).not.toHaveBeenCalled();
  expect(screen.getByRole('button', { name: /disconnect email/i })).toHaveFocus();
});

test('offers to create the first secret when none is set', () => {
  render(<EmailChannelSection settings={{ ...connected, channel: { ...connected.channel!, secret_set: false, secret_rotated_at: null } }} timezone="UTC" canManage />);
  expect(screen.getByRole('button', { name: /create inbound secret/i })).toBeInTheDocument();
});

test('agents see the channel read-only', () => {
  render(<EmailChannelSection settings={connected} timezone="UTC" canManage={false} />);
  expect(screen.getByText('support@example.com')).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /save support address/i })).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /rotate inbound secret/i })).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /disconnect email/i })).not.toBeInTheDocument();
});

test('shows the DNS record to verify the domain and the current sender', () => {
  const onVerifyDomain = vi.fn();
  const { rerender } = render(<EmailChannelSection settings={connected} timezone="UTC" canManage onVerifyDomain={onVerifyDomain} />);
  expect(screen.getByRole('textbox', { name: /txt record name/i })).toHaveValue('_colliedesk.example.com');
  expect(screen.getByRole('textbox', { name: /txt record value/i })).toHaveValue('colliedesk-verification=abc123');
  expect(screen.getByText(/replies are sent from reply\+…@mail\.colliedesk\.example/i)).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: /verify domain/i }));
  expect(onVerifyDomain).toHaveBeenCalled();
  const verified = { ...connected, sending_from: 'support@example.com', channel: { ...connected.channel!, domain_verified_at: '2026-10-14T10:00:00Z' } };
  rerender(<EmailChannelSection settings={verified} timezone="UTC" canManage onVerifyDomain={onVerifyDomain} />);
  expect(screen.getByText(/example\.com verified/i)).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /verify domain/i })).not.toBeInTheDocument();
});

test('warns when replies cannot be emailed', () => {
  render(<EmailChannelSection settings={{ ...connected, sending_from: null }} timezone="UTC" canManage />);
  expect(screen.getByRole('status')).toHaveTextContent(/replies cannot be emailed yet/i);
});

const hosted: EmailChannelSettings = { ...connected, hosted_address: 'support@acme.colliedesk.com' };

test('shows the hosted forwarding address with a copy button and the Gmail and Outlook steps', async () => {
  const writeText = vi.fn().mockResolvedValue(undefined);
  Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
  render(<EmailChannelSection settings={hosted} timezone="UTC" canManage workspaceSlug="acme" />);
  expect(screen.getByRole('textbox', { name: /forwarding address/i })).toHaveValue('support@acme.colliedesk.com');
  fireEvent.click(screen.getByRole('button', { name: /copy forwarding address/i }));
  expect(writeText).toHaveBeenCalledWith('support@acme.colliedesk.com');
  expect(await screen.findByText(/copied/i)).toBeInTheDocument();
  expect(screen.getByText(/forwarding and pop\/imap/i)).toBeInTheDocument();
  expect(screen.getByText(/outlook: settings → mail → forwarding/i)).toBeInTheDocument();
  expect(screen.queryByRole('textbox', { name: /inbound url/i })).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /rotate inbound secret/i })).not.toBeInTheDocument();
});

test('offers no forwarding address when the server does not host mail', () => {
  render(<EmailChannelSection settings={{ ...connected, hosted_address: null }} timezone="UTC" canManage />);
  expect(screen.queryByRole('textbox', { name: /forwarding address/i })).not.toBeInTheDocument();
  expect(screen.getByRole('textbox', { name: /inbound url/i })).toBeInTheDocument();
});

test('an admin renames the workspace address and warns that the old one stops working', () => {
  const onSaveSlug = vi.fn();
  render(<EmailChannelSection settings={hosted} timezone="UTC" canManage workspaceSlug="acme" onSaveSlug={onSaveSlug} />);
  expect(screen.getByText(/old address stops receiving mail/i)).toBeInTheDocument();
  fireEvent.change(screen.getByRole('textbox', { name: /workspace address name/i }), { target: { value: 'acme-support' } });
  fireEvent.click(screen.getByRole('button', { name: /save address name/i }));
  expect(onSaveSlug).toHaveBeenCalledWith('acme-support');
});

test('an agent sees the forwarding address but cannot rename it', () => {
  render(<EmailChannelSection settings={hosted} timezone="UTC" canManage={false} workspaceSlug="acme" />);
  expect(screen.getByRole('textbox', { name: /forwarding address/i })).toHaveValue('support@acme.colliedesk.com');
  expect(screen.queryByRole('textbox', { name: /workspace address name/i })).not.toBeInTheDocument();
});

test('the support address save shows Saving…, refuses a second submit, then Saved until edited', () => {
  const onSave = vi.fn();
  const { rerender } = render(<EmailChannelSection settings={connected} timezone="UTC" canManage onSave={onSave} addressSave={{ pending: true }} />);
  expect(screen.getByRole('button', { name: 'Saving…' })).toBeDisabled();
  fireEvent.submit(screen.getByRole('textbox', { name: /support address/i }));
  expect(onSave).not.toHaveBeenCalled();
  rerender(<EmailChannelSection settings={connected} timezone="UTC" canManage onSave={onSave} addressSave={{ saved: true }} />);
  expect(screen.getByRole('button', { name: /save support address/i })).toBeEnabled();
  expect(screen.getByText('Saved')).toHaveAttribute('role', 'status');
  fireEvent.change(screen.getByRole('textbox', { name: /support address/i }), { target: { value: 'help@example.com' } });
  expect(screen.queryByText('Saved')).not.toBeInTheDocument();
});

test('a failed support address save shows its message next to the form', () => {
  render(<EmailChannelSection settings={connected} timezone="UTC" canManage addressSave={{ error: 'Address refused.' }} />);
  expect(screen.getByRole('alert')).toHaveTextContent('Address refused.');
});

test('the address name save shows Saving… and Saved separately from the support address', () => {
  const onSaveSlug = vi.fn();
  const { rerender } = render(<EmailChannelSection settings={hosted} timezone="UTC" canManage workspaceSlug="acme" onSaveSlug={onSaveSlug} slugSave={{ pending: true }} />);
  expect(screen.getByRole('button', { name: 'Saving…' })).toBeDisabled();
  expect(screen.getByRole('button', { name: /save support address/i })).toBeEnabled();
  fireEvent.submit(screen.getByRole('textbox', { name: /workspace address name/i }));
  expect(onSaveSlug).not.toHaveBeenCalled();
  rerender(<EmailChannelSection settings={hosted} timezone="UTC" canManage workspaceSlug="acme" slugSave={{ saved: true }} />);
  expect(screen.getByRole('status')).toHaveTextContent('Saved');
});

test('the inbound URL has its own copy button', async () => {
  const writeText = vi.fn().mockResolvedValue(undefined);
  Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
  render(<EmailChannelSection settings={disconnected} timezone="UTC" canManage />);
  fireEvent.click(screen.getByRole('button', { name: 'Copy inbound URL' }));
  expect(writeText).toHaveBeenCalledWith(disconnected.inbound_url);
  expect(await screen.findByText(/copied/i)).toBeInTheDocument();
});

test('outgoing email that is not configured reads as a warning, not a footnote', () => {
  render(<EmailChannelSection settings={disconnected} timezone="UTC" canManage />);
  expect(screen.getByRole('note')).toHaveTextContent(/outgoing email is not configured/i);
});

const provider: EmailChannelSettings = {
  ...connected,
  domain_verification: 'provider',
  channel: {
    ...connected.channel!,
    dns_records: [
      { record: 'DKIM', type: 'TXT', name: 'resend._domainkey', value: 'p=MIGfMA0GCSqGSIb3DQEBAQUAA4GN', priority: null, status: 'verified' },
      { record: 'SPF', type: 'MX', name: 'send', value: 'feedback-smtp.us-east-1.amazonses.com', priority: 10, status: 'pending' },
    ],
  },
};

test('asks for the TXT proof first and lists the provider DNS records with their status', () => {
  const onVerifyDomain = vi.fn();
  render(<EmailChannelSection settings={provider} timezone="UTC" canManage onVerifyDomain={onVerifyDomain} />);
  expect(screen.getByRole('textbox', { name: /txt record name/i })).toHaveValue('_colliedesk.example.com');
  expect(screen.getByRole('textbox', { name: /txt record value/i })).toHaveValue('colliedesk-verification=abc123');
  const table = screen.getByRole('table', { name: /dns records to publish/i });
  const rows = within(table).getAllByRole('row');
  expect(rows).toHaveLength(3);
  expect(within(rows[1]).getByText('resend._domainkey')).toBeInTheDocument();
  expect(within(rows[1]).getByText('p=MIGfMA0GCSqGSIb3DQEBAQUAA4GN')).toBeInTheDocument();
  expect(within(rows[1]).getByText('Verified')).toBeInTheDocument();
  expect(within(rows[2]).getByText('MX')).toBeInTheDocument();
  expect(within(rows[2]).getByText('10')).toBeInTheDocument();
  expect(within(rows[2]).getByText('Pending')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: /verify domain/i }));
  expect(onVerifyDomain).toHaveBeenCalled();
});

test('disables Verify domain while a check is running', () => {
  render(<EmailChannelSection settings={provider} timezone="UTC" canManage verifyPending />);
  expect(screen.getByRole('button', { name: /checking/i })).toBeDisabled();
});

test('says the records are not available yet when the provider has not created the domain', () => {
  render(<EmailChannelSection settings={{ ...provider, channel: { ...provider.channel!, dns_records: [] } }} timezone="UTC" canManage />);
  expect(screen.queryByRole('table')).not.toBeInTheDocument();
  expect(screen.getByText(/select verify domain to get the dns records/i)).toBeInTheDocument();
  expect(screen.getByRole('button', { name: /verify domain/i })).toBeInTheDocument();
});

test('shows why the last verification did not succeed', () => {
  render(<EmailChannelSection settings={provider} timezone="UTC" canManage verifyError="Publish the DNS records listed in the email settings." />);
  expect(screen.getByRole('alert')).toHaveTextContent('Publish the DNS records listed in the email settings.');
});

test('shows that sending is paused after spam complaints, why, and that only support can lift it', () => {
  const paused: EmailChannelSettings = { ...connected, sending_pause: { paused_at: '2026-10-18T08:30:00Z', reason: '3 spam complaints on 100 replies sent in the last 7 days (3.00%).' } };
  render(<EmailChannelSection settings={paused} timezone="UTC" canManage />);
  const note = screen.getByRole('alert', { name: /email sending is paused/i });
  expect(note).toHaveTextContent('3 spam complaints on 100 replies sent in the last 7 days (3.00%).');
  expect(note).toHaveTextContent(/replies you write stay queued/i);
  expect(note).toHaveTextContent(/contact support/i);
});

test('shows no pause notice for a workspace that is sending', () => {
  render(<EmailChannelSection settings={connected} timezone="UTC" canManage />);
  expect(screen.queryByText(/email sending is paused/i)).not.toBeInTheDocument();
});
