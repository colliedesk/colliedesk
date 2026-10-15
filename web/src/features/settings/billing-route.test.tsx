import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, expect, test, vi } from 'vitest';
import { BillingRoute } from './billing-route';

const api = vi.hoisted(() => ({ me: vi.fn(), config: vi.fn(), billing: vi.fn(), startCheckout: vi.fn(), openBillingPortal: vi.fn() }));
vi.mock('../../api/client', () => ({ api }));

const me = (role: 'admin' | 'agent') => ({
  agent: { id: '11111111-1111-4111-8111-111111111111', email: 'sam@example.com', display_name: '', role, timezone_override: null },
  workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'beta-desk', name: 'Beta Desk', timezone: 'UTC' },
  timezone: 'UTC',
});
const free = {
  plan: 'trial', trial_ends_at: '2026-10-15T09:00:00Z', locked: false, status: 'none', current_period_end: null, cancel_at_period_end: false, active_subscriptions: 0,
  limits: { agents: 10 }, usage: { agents: 1, pending_invites: 0, replies_last_24h: 0 },
};
const assign = vi.fn();

function renderRoute(props: { checkoutReturned?: boolean } = {}) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><BillingRoute {...props} /></QueryClientProvider>);
}

beforeEach(() => {
  vi.clearAllMocks();
  Object.defineProperty(window, 'location', { configurable: true, value: { assign } });
  api.config.mockResolvedValue({ cloud_sign_in: { enabled: true, publishable_key: 'pk' }, billing: { enabled: true } });
});

test('an admin upgrades: the server-made checkout URL is opened and nothing but the plan is sent', async () => {
  api.me.mockResolvedValue(me('admin'));
  api.billing.mockResolvedValue(free);
  api.startCheckout.mockResolvedValue({ url: 'https://polar.example/checkout/1' });
  renderRoute();
  fireEvent.click(await screen.findByRole('button', { name: /team, \$490 per year/i }));
  await waitFor(() => expect(assign).toHaveBeenCalledWith('https://polar.example/checkout/1'));
  expect(api.startCheckout).toHaveBeenCalledWith('team_yearly');
});

test('manage billing opens the portal URL', async () => {
  api.me.mockResolvedValue(me('admin'));
  api.billing.mockResolvedValue({ ...free, plan: 'team', status: 'active', current_period_end: '2026-11-15T00:00:00Z' });
  api.openBillingPortal.mockResolvedValue({ url: 'https://polar.example/portal/1' });
  renderRoute();
  fireEvent.click(await screen.findByRole('button', { name: /manage billing/i }));
  await waitFor(() => expect(assign).toHaveBeenCalledWith('https://polar.example/portal/1'));
});

test('a failed checkout shows an error and stays on the page', async () => {
  api.me.mockResolvedValue(me('admin'));
  api.billing.mockResolvedValue(free);
  api.startCheckout.mockRejectedValue(new Error('API 502'));
  renderRoute();
  fireEvent.click(await screen.findByRole('button', { name: /founding/i }));
  expect(await screen.findByRole('alert')).toHaveTextContent(/billing action failed/i);
  expect(assign).not.toHaveBeenCalled();
});

test('an agent is told billing is for admins and the billing API is never called', async () => {
  api.me.mockResolvedValue(me('agent'));
  renderRoute();
  expect(await screen.findByText(/only workspace admins/i)).toBeInTheDocument();
  expect(api.billing).not.toHaveBeenCalled();
});

test('a self-hosted instance shows no billing and never calls the billing API', async () => {
  api.me.mockResolvedValue(me('admin'));
  api.config.mockResolvedValue({ cloud_sign_in: { enabled: false, publishable_key: null }, billing: { enabled: false } });
  renderRoute();
  expect(await screen.findByText(/billing is not enabled/i)).toBeInTheDocument();
  expect(api.billing).not.toHaveBeenCalled();
});

test('after checkout the page tells the plan is being confirmed', async () => {
  api.me.mockResolvedValue(me('admin'));
  api.billing.mockResolvedValue(free);
  renderRoute({ checkoutReturned: true });
  expect(await screen.findByRole('status')).toHaveTextContent(/payment received/i);
});
