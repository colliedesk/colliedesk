import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, expect, test, vi } from 'vitest';
import { LockedRoute } from './locked-route';

const api = vi.hoisted(() => ({ billing: vi.fn(), startCheckout: vi.fn(), openBillingPortal: vi.fn(), logout: vi.fn() }));
vi.mock('../../api/client', () => ({ api, InvalidRequestError: class InvalidRequestError extends Error {} }));
const embed = vi.hoisted(() => ({ create: vi.fn() }));
vi.mock('@polar-sh/checkout/embed', () => ({ PolarEmbedCheckout: { create: embed.create } }));

const me = {
  agent: { id: '11111111-1111-4111-8111-111111111111', email: 'sam@example.com', display_name: '', role: 'admin' as const, timezone_override: null },
  workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'beta-desk', name: 'Beta Desk', timezone: 'UTC' },
  timezone: 'UTC', scope: null, permissions: [], plan: null, ai_features: { triage: false, draft: false }, onboarding_goal: null,
};
const locked = {
  plan: 'none', trial_ends_at: '2026-10-01T09:00:00Z', locked: true, status: 'none', current_period_end: null, cancel_at_period_end: false, active_subscriptions: 0, checkout_refusals: [], storage_upgrade_plan: null, checkout_plans: ['team_monthly', 'team_yearly'],
  limits: { agents: 10, replies_per_24h: 200, storage_bytes: 2147483648, storage_warn_bytes: 1717986918 }, usage: { agents: 1, pending_invites: 0, replies_last_24h: 0, storage_bytes: 0 },
};
const assign = vi.fn();

beforeEach(() => {
  vi.clearAllMocks();
  Object.defineProperty(window, 'location', { configurable: true, value: { assign } });
  api.billing.mockResolvedValue(locked);
  api.startCheckout.mockResolvedValue({ url: 'https://polar.example/checkout/9' });
  embed.create.mockResolvedValue({ addEventListener: vi.fn() });
});

function renderLocked() {
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><LockedRoute me={me} /></QueryClientProvider>);
}

test('a locked workspace buys in the overlay, without leaving the app', async () => {
  renderLocked();
  fireEvent.click(await screen.findByRole('button', { name: /team, \$69 per month/i }));
  await waitFor(() => expect(embed.create).toHaveBeenCalledWith('https://polar.example/checkout/9', expect.anything()));
  expect(assign).not.toHaveBeenCalled();
});

test('a locked workspace still reaches the hosted checkout when the overlay cannot open', async () => {
  embed.create.mockRejectedValue(new Error('script blocked'));
  renderLocked();
  fireEvent.click(await screen.findByRole('button', { name: /team, \$69 per month/i }));
  await waitFor(() => expect(assign).toHaveBeenCalledWith('https://polar.example/checkout/9'));
});

test('a second click while the checkout is starting does not start another', async () => {
  embed.create.mockReturnValue(new Promise(() => {}));
  renderLocked();
  const button = await screen.findByRole('button', { name: /team, \$69 per month/i });
  fireEvent.click(button);
  await waitFor(() => expect(embed.create).toHaveBeenCalledTimes(1));
  expect(button).toBeDisabled();
  fireEvent.click(button);
  expect(api.startCheckout).toHaveBeenCalledTimes(1);
});
