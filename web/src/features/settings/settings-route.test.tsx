import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { SettingsRoute } from './settings-route';

const api = vi.hoisted(() => ({
  me: vi.fn(), sessions: vi.fn(), invites: vi.fn(), emailChannel: vi.fn(),
  createInvite: vi.fn(), revokeInvite: vi.fn(), revokeSession: vi.fn(), updateWorkspaceTimezone: vi.fn(), updateAgentTimezone: vi.fn(),
  saveEmailChannel: vi.fn(), updateWorkspaceSlug: vi.fn(), rotateEmailSecret: vi.fn(), verifyEmailDomain: vi.fn(), disconnectEmailChannel: vi.fn(),
  exportWorkspace: vi.fn(), webForm: vi.fn(), saveWebForm: vi.fn(),
}));
vi.mock('../../api/client', () => ({ api, PlanLimitError: class extends Error {}, InvalidRequestError: class extends Error {} }));

const me = (role: 'admin' | 'agent') => ({
  agent: { id: '11111111-1111-4111-8111-111111111111', email: 'sam@example.com', display_name: '', role, timezone_override: null },
  workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'beta-desk', name: 'Beta Desk', timezone: 'UTC' },
  timezone: 'UTC', plan: null,
});

function show(role: 'admin' | 'agent') {
  api.me.mockResolvedValue(me(role));
  api.sessions.mockResolvedValue({ items: [] });
  api.invites.mockResolvedValue({ items: [] });
  api.webForm.mockResolvedValue({ enabled: false, allowed_origins: [], submit_path: '/api/v1/public/forms/beta-desk' });
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

test('a timezone save shows Saving…, accepts one request, then Saved', async () => {
  show('admin');
  let finish: () => void = () => {};
  api.updateWorkspaceTimezone.mockReturnValue(new Promise<void>((resolve) => { finish = resolve; }));
  const save = await screen.findByRole('button', { name: /save workspace timezone/i });
  fireEvent.click(save);
  const saving = (await screen.findAllByRole('button', { name: 'Saving…' }))[0];
  expect(saving).toBeDisabled();
  fireEvent.click(saving);
  expect(api.updateWorkspaceTimezone).toHaveBeenCalledTimes(1);
  finish();
  expect(await screen.findByText('Saved')).toHaveAttribute('role', 'status');
  expect(screen.getByRole('button', { name: /save workspace timezone/i })).toBeEnabled();
});

test('a failed timezone save shows the failure next to its form', async () => {
  show('admin');
  api.updateWorkspaceTimezone.mockRejectedValue(new Error('boom'));
  fireEvent.click(await screen.findByRole('button', { name: /save workspace timezone/i }));
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
