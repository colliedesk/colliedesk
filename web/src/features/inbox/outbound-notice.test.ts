import { expect, test } from 'vitest';
import type { EmailChannelSettings } from '../../api/schemas';
import { outboundNotice } from './outbound-notice';

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
