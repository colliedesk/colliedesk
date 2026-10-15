import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { SettingsRoute } from './settings-route';

const api = vi.hoisted(() => ({
  me: vi.fn(), sessions: vi.fn(), invites: vi.fn(), emailChannel: vi.fn(),
  createInvite: vi.fn(), revokeInvite: vi.fn(), revokeSession: vi.fn(), updateWorkspaceTimezone: vi.fn(), updateAgentTimezone: vi.fn(),
  saveEmailChannel: vi.fn(), updateWorkspaceSlug: vi.fn(), rotateEmailSecret: vi.fn(), verifyEmailDomain: vi.fn(), disconnectEmailChannel: vi.fn(),
  exportWorkspace: vi.fn(),
}));
vi.mock('../../api/client', () => ({ api, PlanLimitError: class extends Error {} }));

const me = (role: 'admin' | 'agent') => ({
  agent: { id: '11111111-1111-4111-8111-111111111111', email: 'sam@example.com', display_name: '', role, timezone_override: null },
  workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'beta-desk', name: 'Beta Desk', timezone: 'UTC' },
  timezone: 'UTC', plan: null,
});

function show(role: 'admin' | 'agent') {
  api.me.mockResolvedValue(me(role));
  api.sessions.mockResolvedValue({ items: [] });
  api.invites.mockResolvedValue({ items: [] });
  api.emailChannel.mockResolvedValue({ channel: null, inbound_url: 'https://desk.example.com/x', smtp: { configured: false, host: '', port: 587, starttls: true, authenticated: false }, from_policy: 'verified', sending_from: null });
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
