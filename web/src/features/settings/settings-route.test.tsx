import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { SettingsRoute } from './settings-route';

const api = vi.hoisted(() => ({
  me: vi.fn(), sessions: vi.fn(), invites: vi.fn(), emailChannel: vi.fn(), agents: vi.fn(), updateAgent: vi.fn(),
  createInvite: vi.fn(), revokeInvite: vi.fn(), revokeSession: vi.fn(), updateWorkspaceTimezone: vi.fn(), updateAgentTimezone: vi.fn(),
  saveEmailChannel: vi.fn(), updateWorkspaceSlug: vi.fn(), rotateEmailSecret: vi.fn(), verifyEmailDomain: vi.fn(), disconnectEmailChannel: vi.fn(),
  exportWorkspace: vi.fn(), config: vi.fn(), webForm: vi.fn(), saveWebForm: vi.fn(),
  savedReplies: vi.fn(), createSavedReply: vi.fn(), updateSavedReply: vi.fn(), deleteSavedReply: vi.fn(),
  connectGmail: vi.fn(), disconnectGmail: vi.fn(),
}));
vi.mock('../../api/client', () => ({ api, PlanLimitError: class extends Error {}, InvalidRequestError: class extends Error {} }));

const me = (role: 'admin' | 'agent') => ({
  agent: { id: '11111111-1111-4111-8111-111111111111', email: 'sam@example.com', display_name: '', role, timezone_override: null },
  workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'beta-desk', name: 'Beta Desk', timezone: 'UTC' },
  timezone: 'UTC', plan: null,
});

const teammate = (id: string, email: string, overrides = {}) => ({
  id, email, display_name: '', role: 'agent', timezone_override: null, deactivated_at: null, last_active_at: null, open_conversations: 0, ...overrides,
});
const SAM = '11111111-1111-4111-8111-111111111111';
const ANN = '33333333-3333-4333-8333-333333333333';

function show(role: 'admin' | 'agent', sessions: unknown[] = [], emailChannel?: unknown) {
  api.me.mockResolvedValue(me(role));
  api.config.mockResolvedValue({ billing: { enabled: false }, cloud_sign_in: { enabled: false, publishable_key: null }, product_analytics: { enabled: false, key: null, host: null, realm: null } });
  api.agents.mockResolvedValue({ items: [teammate(SAM, 'sam@example.com', { role }), teammate(ANN, 'ann@example.com', { open_conversations: 1 })] });
  api.updateAgent.mockResolvedValue(teammate(ANN, 'ann@example.com', { deactivated_at: '2026-10-15T10:00:00Z' }));
  api.sessions.mockResolvedValue({ items: sessions });
  api.savedReplies.mockResolvedValue({ items: [] });
  api.invites.mockResolvedValue({ items: [] });
  api.webForm.mockResolvedValue({ enabled: false, allowed_origins: [], submit_path: '/api/v1/public/forms/beta-desk' });
  api.emailChannel.mockResolvedValue(emailChannel ?? { channel: null, inbound_url: 'https://desk.example.com/x', smtp: { configured: false, host: '', port: 587, starttls: true, authenticated: false }, from_policy: 'verified', sending_from: null });
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><SettingsRoute /></QueryClientProvider>);
}

test('an admin finds Export all data in Settings', async () => {
  show('admin');
  expect(await screen.findByRole('button', { name: 'Export all data' })).toBeInTheDocument();
});

test('an agent does not see Export all data', async () => {
  show('agent');
  await screen.findByRole('heading', { name: 'Sessions' });
  expect(screen.queryByRole('button', { name: 'Export all data' })).not.toBeInTheDocument();
});

test('an admin deactivates a teammate and the lists reload', async () => {
  show('admin');
  fireEvent.click(await screen.findByRole('button', { name: 'Deactivate ann@example.com' }));
  fireEvent.click(within(screen.getByRole('group', { name: 'Deactivate ann@example.com?' })).getByRole('button', { name: 'Yes, deactivate' }));
  await waitFor(() => expect(api.updateAgent).toHaveBeenCalledWith(ANN, { active: false }));
  await waitFor(() => expect(api.agents.mock.calls.length).toBeGreaterThan(1));
});

test('an agent sees the team but no controls', async () => {
  show('agent');
  const team = await screen.findByRole('region', { name: 'Team' });
  expect(await within(team).findByText('ann@example.com')).toBeInTheDocument();
  expect(within(team).queryByRole('button')).not.toBeInTheDocument();
});

test('a chosen timezone saves once, shows Saving…, then Saved', async () => {
  show('admin');
  let finish: () => void = () => {};
  api.updateWorkspaceTimezone.mockReturnValue(new Promise<void>((resolve) => { finish = resolve; }));
  const select = await screen.findByRole('combobox', { name: 'Workspace timezone' });
  fireEvent.change(select, { target: { value: 'Asia/Tokyo' } });
  fireEvent.blur(select);
  expect(await screen.findByText('Saving…')).toBeInTheDocument();
  fireEvent.blur(select);
  expect(api.updateWorkspaceTimezone).toHaveBeenCalledTimes(1);
  expect(api.updateWorkspaceTimezone.mock.calls[0][0]).toBe('Asia/Tokyo');
  finish();
  expect(await screen.findByText('Saved')).toHaveAttribute('role', 'status');
  expect(select).toBeEnabled();
});

test('a failed timezone save shows the failure next to its control', async () => {
  show('admin');
  api.updateWorkspaceTimezone.mockRejectedValue(new Error('boom'));
  const zone = await screen.findByRole('combobox', { name: 'Workspace timezone' });
  fireEvent.change(zone, { target: { value: 'Asia/Tokyo' } });
  fireEvent.blur(zone);
  const alert = await screen.findByRole('alert');
  expect(alert).toHaveTextContent('The settings action failed. Try again.');
  expect(alert.closest('.settings-card')).toHaveTextContent('Workspace timezone');
});

test('the web form save shows Saved after it succeeds', async () => {
  show('admin');
  api.saveWebForm.mockResolvedValue(undefined);
  fireEvent.click(await screen.findByRole('button', { name: /save web form/i }));
  await waitFor(() => expect(screen.getByText('Saved')).toHaveAttribute('role', 'status'));
});

test('Settings shows the loading status until its data arrives', async () => {
  show('admin');
  api.me.mockReturnValue(new Promise(() => {}));
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><SettingsRoute /></QueryClientProvider>);
  expect(screen.getAllByRole('status')[0]).toHaveTextContent('Loading workspace…');
});

test('a plain agent manages the shared saved replies and the list reloads after each change', async () => {
  const stored = { id: '88888888-8888-4888-8888-888888888888', title: 'Closing', body: 'Thanks.', created_by_agent_id: null, created_at: '2026-10-15T00:00:00Z', updated_at: '2026-10-15T00:00:00Z' };
  show('agent');
  api.createSavedReply.mockResolvedValue(stored);
  api.deleteSavedReply.mockResolvedValue(undefined);
  await screen.findByRole('heading', { name: 'Saved replies' });
  fireEvent.click(screen.getByRole('button', { name: 'New saved reply' }));
  api.savedReplies.mockResolvedValue({ items: [stored] });
  fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Closing' } });
  fireEvent.change(screen.getByLabelText('Text'), { target: { value: 'Thanks.' } });
  fireEvent.click(screen.getByRole('button', { name: 'Add saved reply' }));
  await waitFor(() => expect(api.createSavedReply).toHaveBeenCalledWith('Closing', 'Thanks.'));
  expect(await screen.findByText('Thanks.')).toBeInTheDocument();
  api.savedReplies.mockResolvedValue({ items: [] });
  fireEvent.click(screen.getByRole('button', { name: 'Delete Closing' }));
  fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
  await waitFor(() => expect(api.deleteSavedReply).toHaveBeenCalledWith(stored.id, stored.updated_at));
  expect(await screen.findByText('No saved replies yet.')).toBeInTheDocument();
});

test('signing out this device from Sessions reloads who is signed in, so the signed-out screen follows', async () => {
  show('agent', [{ id: '44444444-4444-4444-8444-444444444444', agent_id: SAM, agent_email: 'sam@example.com', label: 'Cloud sign-in',
    current: true, created_at: '2026-10-14T00:00:00Z', last_seen_at: '2026-10-14T00:00:00Z', expires_at: '2099-01-01T00:00:00Z', revoked_at: null }]);
  api.revokeSession.mockResolvedValue(undefined);
  const sessions = await screen.findByRole('region', { name: 'Sessions' });
  const signOut = await within(sessions).findByRole('button', { name: 'Sign out' });
  const before = api.me.mock.calls.length;
  fireEvent.click(signOut);
  await waitFor(() => expect(api.me.mock.calls.length).toBeGreaterThan(before));
});

const withChannel = {
  channel: {
    support_address: 'support@example.com', secret_set: true, secret_rotated_at: '2026-10-14T09:00:00Z', domain: 'example.com', domain_verified_at: null,
    dns_record_name: '_colliedesk.example.com', dns_record_value: 'colliedesk-verification=abc', dns_records: [], dns_records_expired_at: null,
    created_at: '2026-10-14T08:00:00Z', updated_at: '2026-10-14T09:00:00Z',
  },
  inbound_url: 'https://desk.example.com/x', smtp: { configured: true, host: 'smtp.example.net', port: 587, starttls: true, authenticated: true },
  from_policy: 'any', sending_from: 'support@example.com', hosted_address: null, domain_verification: 'txt', sending_pause: null,
  gmail: { available: true, missing_settings: [], redirect_uri: 'https://desk.example.com/api/v1/channels/email/gmail/callback', connection: null },
};

test('Connect Gmail sends the browser to Google, and the outcome it comes back with is shown once', async () => {
  const assign = vi.fn();
  const original = window.location;
  window.history.replaceState(null, '', '/settings?gmail_error=denied');
  Object.defineProperty(window, 'location', { configurable: true, value: { ...original, assign, search: '?gmail_error=denied', pathname: '/settings', hash: '' } });
  try {
    api.connectGmail.mockResolvedValue({ url: 'https://accounts.google.com/o/oauth2/v2/auth?state=x' });
    show('admin', [], withChannel);
    expect(await screen.findByText('Gmail was not connected: access was cancelled at Google.')).toBeInTheDocument();
    fireEvent.click(await screen.findByRole('button', { name: 'Connect Gmail' }));
    await waitFor(() => expect(assign).toHaveBeenCalledWith('https://accounts.google.com/o/oauth2/v2/auth?state=x'));
  } finally {
    Object.defineProperty(window, 'location', { configurable: true, value: original });
  }
});
