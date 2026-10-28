import { expect, test } from 'vitest';
import type { EmailChannelSettings } from '../../api/schemas';
import { largeAttachmentWarning, outboundNotice } from './outbound-notice';

const base = {
  channel: { support_address: 'support@example.com' }, sending_from: null,
  smtp: { configured: true, host: 'smtp.example.net', port: 587, starttls: true, authenticated: true },
  gmail: { available: true, missing_settings: [], redirect_uri: 'https://desk.example.com/cb', connection: null },
} as unknown as EmailChannelSettings;
const gmail = { provider: 'google', address: 'support@example.com', status: 'connected', error_code: null, error: null, failed_at: null, connected_at: '2026-10-19T08:00:00Z' } as const;

test('the composer says why replies are not emailed, and Gmail is named when it is the cause', () => {
  expect(outboundNotice(undefined)).toBeUndefined();
  expect(outboundNotice({ ...base, sending_from: 'support@example.com' })).toBeUndefined();
  expect(outboundNotice(base)).toMatch(/verifies the support address domain/);
  expect(outboundNotice({ ...base, smtp: { ...base.smtp, configured: false } })).toMatch(/not configured on this server/);
  expect(outboundNotice({ ...base, sending_from: 'support@example.com', gmail: { ...base.gmail, connection: gmail } })).toBeUndefined();
  const failed = { ...base, gmail: { ...base.gmail, connection: { ...gmail, status: 'failed' as const, error_code: 'revoked' as const } } };
  expect(outboundNotice(failed, true)).toMatch(/Gmail stopped sending.*Reconnect it in Settings/);
  expect(outboundNotice(failed, false)).toMatch(/Gmail stopped sending.*Ask an admin/);
  expect(outboundNotice({ ...base, gmail: { ...base.gmail, available: false, connection: gmail } })).toMatch(/Gmail sending is not set up on this server/);
});

test('a Microsoft 365 connection is named when it is the cause, and large files through it are warned', () => {
  const ms = { ...gmail, provider: 'microsoft' as const };
  const microsoft = { available: true, missing_settings: [], redirect_uri: 'https://desk.example.com/cb', admin_consent_redirect_uri: 'https://desk.example.com/ac',
    admin_consent_url: null, large_attachment_bytes: 3145728, server_credentials_refused: false, connection: ms };
  const failed = { ...base, microsoft: { ...microsoft, connection: { ...ms, status: 'failed' as const, error_code: 'mailbox_unavailable' as const } } };
  expect(outboundNotice(failed, true)).toMatch(/Microsoft 365 stopped sending.*Reconnect it in Settings/);
  expect(outboundNotice(failed, false)).toMatch(/Ask an admin to reconnect Microsoft 365/);
  expect(outboundNotice({ ...base, microsoft: { ...microsoft, available: false } })).toMatch(/Microsoft 365 sending is not set up on this server/);
  expect(largeAttachmentWarning({ ...base, microsoft })).toEqual({ bytes: 3145728, text: expect.stringMatching(/Microsoft 365 may refuse attachments this large/) });
  expect(largeAttachmentWarning(failed)).toBeUndefined();
  expect(largeAttachmentWarning(base)).toBeUndefined();
});
