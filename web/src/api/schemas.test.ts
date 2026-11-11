import { describe, expect, it } from 'vitest';
import { channelListSchema } from './channels';
import { contactListSchema, conversationPageSchema, emailChannelSettingsSchema } from './schemas';
import { channelLabel } from '../features/inbox/filter-messages';
import { contactAddress } from '../features/contacts/contact-messages';

const known = {
  id: '11111111-1111-4111-8111-111111111111', name: 'Ann', email: 'ann@example.com', organization: null, merged_into_id: null,
  created_at: '2026-10-24T10:00:00Z', updated_at: '2026-10-24T10:00:00Z',
};

describe('contact without email', () => {
  it('parses a list where a customer is known only on a channel', () => {
    const list = contactListSchema.parse({ items: [known, { ...known, id: '22222222-2222-4222-8222-222222222222', name: 'Minh', email: null,
      identities: [{ channel_id: '33333333-3333-4333-8333-333333333333', kind: 'chat_user', value: '4410233', display_name: 'Minh T.' }] }] });
    expect(list.items).toHaveLength(2);
    expect(contactAddress(list.items[1])).toBe('Minh T.');
    expect(contactAddress(list.items[0])).toBe('ann@example.com');
    expect(contactAddress({ email: null, identities: [] })).toBe('No email');
  });
});

describe('a channel type this app does not know', () => {
  // A chat type (a Zalo OA, the loopback stand-in of end-to-end builds) is a name the server's registry
  // gives; the inbox and Settings → Channels list it instead of failing to load.
  it('parses conversations and channels of it and names it by its type', () => {
    const page = conversationPageSchema.parse({ items: [{ latest_reply_number: 0, id: '44444444-4444-4444-8444-444444444444', contact_id: known.id,
      contact_name: 'Minh', subject: 'Stock export', status: 'open', priority: 'normal', assignee_id: null, tags: [], channel: 'line_oa', origin: null,
      created_at: '2026-10-24T10:00:00Z', updated_at: '2026-10-24T10:00:00Z' }], page: 1, page_size: 50, has_next_page: false });
    expect(page.items[0].channel).toBe('line_oa');
    expect(channelLabel('line_oa')).toBe('line_oa');
    const list = channelListSchema.parse({ items: [{ id: '55555555-5555-4555-8555-555555555555', type: 'line_oa', name: 'Line Shop', enabled: true,
      status: { state: 'working', reason_code: null, message: null }, summary: '', capabilities: [], held_count: 0, unsent_count: 0, last_inbound_at: null,
      created_at: '2026-10-24T10:00:00Z', updated_at: '2026-10-24T10:00:00Z' }],
    types: [{ type: 'line_oa', available: true, count: 1, default_channel_id: null }] });
    expect(list.items[0].type).toBe('line_oa');
  });
});

describe('the mail switch of the email settings', () => {
  const settings = (mailSwitch: unknown) => ({
    channel: null, inbound_url: 'https://desk.example.com/api/v1/channels/email/inbound', from_policy: 'verified', sending_from: null, hosted_address: null,
    domain_verification: 'txt', sending_pause: null, smtp: { configured: true, host: 'smtp.example.net', port: 587, starttls: true, authenticated: true },
    gmail: { available: true, missing_settings: [], off_reason: null, redirect_uri: 'https://desk.example.com/gmail', connection: null },
    microsoft: { available: true, missing_settings: [], off_reason: null, redirect_uri: 'https://desk.example.com/ms', admin_consent_redirect_uri: 'https://desk.example.com/ms-admin',
      admin_consent_url: null, large_attachment_bytes: 3145728, server_credentials_refused: false, connection: null },
    mail_through_mailbox: false, answers_before_forwarding: 'colliedesk', mail_switch: mailSwitch,
  });
  const support = { channel_id: '55555555-5555-4555-8555-555555555555', name: 'Support', support_address: 'support@acme.com', old_help_desk: true };
  const billing = { channel_id: '66666666-6666-4666-8666-666666666666', name: 'Billing', support_address: 'billing@acme.com', old_help_desk: false };

  // One malformed mailbox must not hide the line for the others: only that entry is dropped.
  it('drops only a malformed mailbox and keeps the others', () => {
    const parsed = emailChannelSettingsSchema.parse(settings({ imported: true, mailboxes: [support, { channel_id: 'not-a-uuid', name: 'Broken' }, billing] }));
    expect(parsed.mail_switch).toEqual({ imported: true, mailboxes: [support, billing] });
  });

  it('reads an older server without a mail switch as nothing to move', () => {
    expect(emailChannelSettingsSchema.parse(settings(undefined)).mail_switch).toEqual({ imported: false, mailboxes: [] });
  });
});
