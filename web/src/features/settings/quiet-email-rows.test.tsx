import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render as renderInPage, screen, within } from '@testing-library/react';
import type { ReactElement, ReactNode } from 'react';
import { expect, test, vi } from 'vitest';
import type { EmailChannelSettings } from '../../api/schemas';
import { EmailChannelSection } from './email-channel-section';
import { SettingsEditsProvider } from './settings-edits';

// "Quiet by default": each row of the email channel keeps its label and one line of 60 characters or less. What the
// line used to say beyond that is the accessible description of its tip; the forwarding steps link to the guide.

const connected: EmailChannelSettings = {
  channel: {
    support_address: 'support@example.com', sender_name: 'Acme Support', secret_set: true, secret_rotated_at: '2026-10-14T09:00:00Z',
    domain: 'example.com', domain_verified_at: null, dns_record_name: '_colliedesk.example.com', dns_record_value: 'colliedesk-verification=abc123', dns_records: [], dns_records_expired_at: null,
    created_at: '2026-10-14T08:00:00Z', updated_at: '2026-10-14T09:00:00Z',
  },
  inbound_url: 'https://desk.example.com/api/v1/channels/email/inbound',
  smtp: { configured: true, host: 'smtp.example.net', port: 587, starttls: true, authenticated: true },
  from_policy: 'verified',
  sending_from: 'reply+…@mail.colliedesk.example',
  hosted_address: null,
  domain_verification: 'txt',
  sending_pause: null,
  gmail: { available: true, missing_settings: [], off_reason: null, redirect_uri: 'https://desk.example.com/api/v1/channels/email/gmail/callback', connection: null },
  microsoft: {
    available: false, missing_settings: [], off_reason: 'not_set_up',
    redirect_uri: 'https://desk.example.com/api/v1/channels/email/microsoft/callback',
    admin_consent_redirect_uri: 'https://desk.example.com/api/v1/channels/email/microsoft/admin-consent',
    admin_consent_url: null, large_attachment_bytes: 3145728, server_credentials_refused: false, connection: null,
  },
  mail_through_mailbox: false,
  answers_before_forwarding: 'colliedesk',
  mail_switch: { imported: false, mailboxes: [] },
};
const hosted = { ...connected, hosted_address: 'acme@in.colliedesk.com' };
const provider: EmailChannelSettings = { ...connected, domain_verification: 'provider' };

function render(ui: ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  client.setQueryData(['public-config'], { cloud_sign_in: { enabled: false, publishable_key: null }, billing: { enabled: false }, product_analytics: { enabled: false, key: null, host: null, realm: null } });
  return renderInPage(ui, { wrapper: ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}><SettingsEditsProvider>{children}</SettingsEditsProvider></QueryClientProvider> });
}
function line(text: string) {
  expect(text.length).toBeLessThanOrEqual(60);
  return screen.getByText(text);
}
const tip = (label: string) => screen.getByRole('button', { name: `About ${label}` });

test('the sender name says where customers see it in one line, and the tip says it belongs to the channel', () => {
  render(<EmailChannelSection settings={connected} timezone="UTC" canManage onSaveSenderName={vi.fn()} />);
  const field = screen.getByRole('textbox', { name: 'Sender name' });
  expect(field).toHaveAccessibleDescription(/^Customers see it in the From line of every reply\./);
  line('Customers see it in the From line of every reply.');
  expect(field).toHaveAccessibleDescription(/It belongs to this channel; renaming the workspace does not change it\.$/);
});

test('the forwarding address says what to forward in one line, and the tip says to save the support address first', () => {
  render(<EmailChannelSection settings={hosted} timezone="UTC" canManage onSaveSlug={vi.fn()} />);
  line('Forward your support mailbox to this address.');
  expect(tip('Forwarding address')).toHaveAccessibleDescription('Save your support address above first; mail is accepted once it is saved.');
});

test('the Gmail and Outlook steps are one line with a tip that holds them and a link to the guide', () => {
  render(<EmailChannelSection settings={hosted} timezone="UTC" canManage />);
  line('Set up forwarding in Gmail or Outlook.');
  // The steps are in the guide, which holds every one of them; the tip is one sentence and the link.
  expect(tip('Forwarding steps')).toHaveAccessibleDescription('Step-by-step for Gmail, Google Workspace and Microsoft 365.');
  const layer = document.getElementById(tip('Forwarding steps').getAttribute('aria-controls')!)!;
  expect(within(layer).getByRole('link', { name: 'Learn more', hidden: true })).toHaveAttribute('href', 'https://colliedesk.com/docs/email-channel');
});

test('the workspace address name warns in one line, and the tip says what stops working', () => {
  render(<EmailChannelSection settings={hosted} timezone="UTC" canManage onSaveSlug={vi.fn()} />);
  line('The old address and help center links stop working at once.');
  expect(tip('Workspace address name')).toHaveAccessibleDescription('Changing the name changes the forwarding address and the help center address. The old address stops receiving mail at once and old help center links stop working, so update your mailbox forwarding and links too.');
});

test('proving the domain says what to add in one line, and the tip says what comes next', () => {
  render(<EmailChannelSection part="replies" settings={provider} timezone="UTC" canManage />);
  line('Add this TXT record to prove you control the domain.');
  expect(tip('Domain verification')).toHaveAccessibleDescription('Then select Verify domain. Colliedesk then lists the DKIM and SPF records to add. Changing the support address resets verification.');
});

test('the DNS records say where to add them in one line, and the tip says what they do', () => {
  const records = [{ record: 'DKIM', type: 'TXT', name: 'resend._domainkey', value: 'p=MIGfMA0GCSqGSIb3DQEBAQUAA4GN', priority: null, status: 'verified' }];
  render(<EmailChannelSection part="replies" settings={{ ...provider, channel: { ...provider.channel!, dns_records: records as never } }} timezone="UTC" canManage />);
  line('Add these records at your DNS provider.');
  expect(tip('DNS records')).toHaveAccessibleDescription('Then select Verify domain. They let Colliedesk sign your replies with your own domain. Changing the support address to another domain starts a new list.');
});

test('a verified domain says when in one line, and the tip says every mailbox on it shares it', () => {
  render(<EmailChannelSection part="replies" settings={{ ...connected, channel: { ...connected.channel!, domain_verified_at: '2026-10-15T00:00:00Z' } }} timezone="UTC" canManage />);
  expect(screen.getByText(/^example\.com verified .*\.$/)).toBeVisible();
  expect(tip('Domain verification')).toHaveAccessibleDescription('The verification is for example.com: every mailbox on it shares it.');
});

test('server details say who they are for in one line, and the tip says nothing there needs changing to send', () => {
  render(<EmailChannelSection settings={connected} timezone="UTC" canManage />);
  fireEvent.click(screen.getByText('Server details'));
  line('For whoever runs this server.');
  expect(tip('Server details')).toHaveAccessibleDescription('Nothing here needs changing to send replies.');
});
