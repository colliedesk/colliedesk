import { fireEvent, render, screen } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import type { EmailChannelSettings } from '../../api/schemas';
import { EmailChannelSection } from './email-channel-section';

const disconnected: EmailChannelSettings = {
  channel: null,
  inbound_url: 'https://desk.example.com/api/v1/channels/email/inbound',
  smtp: { configured: false, host: '', port: 587, starttls: true, authenticated: false },
  from_policy: 'verified',
  sending_from: null,
};
const connected: EmailChannelSettings = {
  ...disconnected,
  channel: {
    support_address: 'support@example.com', secret_set: true, secret_rotated_at: '2026-10-14T09:00:00Z',
    domain: 'example.com', domain_verified_at: null, dns_record_name: '_colliedesk.example.com', dns_record_value: 'colliedesk-verification=abc123',
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
  expect(onDisconnect).toHaveBeenCalled();
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
