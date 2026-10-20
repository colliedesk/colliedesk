import { QueryClientProvider } from '@tanstack/react-query';
import { RouterProvider, createMemoryHistory, createRouter } from '@tanstack/react-router';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { routeTree } from './app';
import { readCheckoutIntent, rememberCheckoutIntent } from './features/settings/checkout-intent';
import { createQueryClient } from './query-client';
import { resetProductAnalytics } from './lib/product-analytics';

// Only PostHog's own wrapper calls are observed; the rest of the module is the real one.
vi.mock('./lib/product-analytics', async (original) => ({ ...(await original<typeof import('./lib/product-analytics')>()), resetProductAnalytics: vi.fn() }));

const fetchMock = vi.fn();
vi.stubGlobal('fetch', fetchMock);
afterEach(() => { fetchMock.mockReset(); window.sessionStorage.clear(); });

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status });
const locked = { error: { code: 'workspace_locked', message: 'This workspace is locked because its trial or subscription ended.' } };
const meBody = (role: 'admin' | 'agent', plan: unknown) => ({
  agent: { id: '11111111-1111-4111-8111-111111111111', email: 'sam@example.com', display_name: '', role, timezone_override: null },
  workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'beta-desk', name: 'Beta Desk', timezone: 'UTC' },
  timezone: 'UTC', scope: null, plan,
});
const config = { cloud_sign_in: { enabled: true, publishable_key: 'pk' }, billing: { enabled: true }, signup: { enabled: true }, limits: { outgoing_attachments_bytes: 10485760, outgoing_attachments_files: 10 } };
const billing = {
  plan: 'none', trial_ends_at: '2026-10-01T09:00:00Z', locked: true, status: 'none', current_period_end: null, cancel_at_period_end: false,
  active_subscriptions: 0, complimentary: false, complimentary_until: null, checkout_plans: ['team_monthly', 'team_yearly', 'team25_monthly', 'team25_yearly', 'founding'],
  limits: { agents: 0, replies_per_24h: null, storage_bytes: 2147483648, storage_warn_bytes: 1717986918 }, usage: { agents: 1, pending_invites: 0, replies_last_24h: 0, storage_bytes: 0 },
};

function serve(handlers: Record<string, () => Response>) {
  fetchMock.mockImplementation((url: string) => {
    const handler = handlers[url.split('?')[0]];
    return Promise.resolve(handler ? handler() : json(404, { error: { code: 'not_found', message: 'no' } }));
  });
}

function open(path = '/') {
  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: [path] }) });
  render(<QueryClientProvider client={createQueryClient()}><RouterProvider router={router} /></QueryClientProvider>);
}

test('a locked workspace sees the locked screen with the identity bar and an admin\'s plan choices', async () => {
  serve({
    '/api/v1/auth/me': () => json(200, meBody('admin', { plan: 'none', trial_ends_at: '2026-10-01T09:00:00Z', locked: true, trial_extended: false })),
    '/api/v1/config': () => json(200, config),
    '/api/v1/billing': () => json(200, billing),
    '/api/v1/conversations': () => json(402, locked),
  });
  open();
  expect(await screen.findByRole('heading', { level: 1, name: 'Your free trial has ended' })).toBeInTheDocument();
  expect(screen.getByRole('region', { name: 'Signed in as' })).toHaveTextContent('Beta Desk');
  expect(screen.getByRole('button', { name: /team, \$69 per month/i })).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Export all data' })).toBeInTheDocument();
});

test('a 402 workspace_locked from the inbox replaces the inbox with the locked screen', async () => {
  serve({
    '/api/v1/auth/me': () => json(200, meBody('agent', { plan: 'trial', trial_ends_at: '2026-10-30T09:00:00Z', locked: false, trial_extended: false })),
    '/api/v1/config': () => json(200, config),
    '/api/v1/conversations': () => json(402, locked),
    '/api/v1/agents': () => json(402, locked),
    '/api/v1/tags': () => json(402, locked),
    '/api/v1/channels/email': () => json(402, locked),
  });
  open();
  expect(await screen.findByRole('heading', { level: 1, name: 'This workspace is locked' })).toBeInTheDocument();
  expect(screen.getByText(/ask a workspace admin to choose a plan/i)).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Export all data' })).not.toBeInTheDocument();
  expect(screen.getByRole('region', { name: 'Signed in as' })).toBeInTheDocument();
});

test('the inbox activity poll runs on every signed-in page, not only in the inbox', async () => {
  serve({
    '/api/v1/auth/me': () => json(200, meBody('agent', { plan: 'trial', trial_ends_at: '2026-10-30T09:00:00Z', locked: false, trial_extended: false })),
    '/api/v1/config': () => json(200, config),
    '/api/v1/inbox/activity': () => json(200, { now: '2026-10-15T10:00:00Z', new_conversations: [], assigned_to_me: [], updated: [] }),
  });
  open('/settings');
  await waitFor(() => expect(fetchMock.mock.calls.some(([url]) => String(url).startsWith('/api/v1/inbox/activity'))).toBe(true));
});

test('a locked workspace does not poll for inbox activity', async () => {
  serve({
    '/api/v1/auth/me': () => json(200, meBody('admin', { plan: 'none', trial_ends_at: '2026-10-01T09:00:00Z', locked: true, trial_extended: false })),
    '/api/v1/config': () => json(200, config),
    '/api/v1/billing': () => json(200, billing),
  });
  open();
  await screen.findByRole('heading', { level: 1, name: 'Your free trial has ended' });
  expect(fetchMock.mock.calls.some(([url]) => String(url).startsWith('/api/v1/inbox/activity'))).toBe(false);
});

const trialPlan = { plan: 'trial', trial_ends_at: '2026-10-30T09:00:00Z', locked: false, trial_extended: false };
const trialBilling = { ...billing, plan: 'trial', locked: false };

test('the billing link with buy=founding remembers the intent for a visitor who is signed out', async () => {
  serve({
    '/api/v1/auth/me': () => json(401, { error: { code: 'unauthorized', message: 'Sign in.' } }),
    '/api/v1/config': () => json(200, config),
  });
  open('/settings/billing?buy=founding');
  await screen.findByRole('alert');
  expect(readCheckoutIntent()).toBe('founding');
});

test('the billing link with buy=founding starts the founding checkout for a signed-in trial admin', async () => {
  serve({
    '/api/v1/auth/me': () => json(200, meBody('admin', trialPlan)),
    '/api/v1/config': () => json(200, config),
    '/api/v1/billing': () => json(200, trialBilling),
    '/api/v1/billing/checkout': () => json(200, { url: 'https://polar.example/checkout/founding' }),
    '/api/v1/inbox/activity': () => json(200, { now: '2026-10-15T10:00:00Z', new_conversations: [], assigned_to_me: [], updated: [] }),
  });
  open('/settings/billing?buy=founding');
  await waitFor(() => expect(fetchMock.mock.calls.filter(([url]) => String(url) === '/api/v1/billing/checkout')).toHaveLength(1));
  const [, init] = fetchMock.mock.calls.find(([url]) => String(url) === '/api/v1/billing/checkout')!;
  expect(JSON.parse(String(init.body))).toEqual({ plan: 'founding' });
  expect(readCheckoutIntent()).toBeUndefined();
});

test('after signing in with a pairing link the remembered intent leads to the billing page and its checkout', async () => {
  rememberCheckoutIntent('founding');
  serve({
    '/api/v1/auth/redeem': () => json(200, { agent: meBody('admin', trialPlan).agent, workspace: meBody('admin', trialPlan).workspace }),
    '/api/v1/auth/me': () => json(200, meBody('admin', trialPlan)),
    '/api/v1/config': () => json(200, config),
    '/api/v1/billing': () => json(200, trialBilling),
    '/api/v1/billing/checkout': () => json(200, { url: 'https://polar.example/checkout/founding' }),
    '/api/v1/inbox/activity': () => json(200, { now: '2026-10-15T10:00:00Z', new_conversations: [], assigned_to_me: [], updated: [] }),
  });
  open('/sign-in?token=pairing-token');
  fireEvent.click(await screen.findByRole('button', { name: /sign in|continue|open/i }));
  await waitFor(() => expect(fetchMock.mock.calls.filter(([url]) => String(url) === '/api/v1/billing/checkout')).toHaveLength(1));
});

test('a locked workspace drops the remembered plan and shows its own plan choices', async () => {
  rememberCheckoutIntent('founding');
  serve({
    '/api/v1/auth/me': () => json(200, meBody('admin', { plan: 'none', trial_ends_at: '2026-10-01T09:00:00Z', locked: true, trial_extended: false })),
    '/api/v1/config': () => json(200, config),
    '/api/v1/billing': () => json(200, billing),
  });
  open('/settings/billing?buy=founding');
  await screen.findByRole('heading', { level: 1, name: 'Your free trial has ended' });
  await waitFor(() => expect(readCheckoutIntent()).toBeUndefined());
  expect(fetchMock.mock.calls.some(([url]) => String(url) === '/api/v1/billing/checkout')).toBe(false);
});

test('going back to the billing page after the checkout was started does not start another one', async () => {
  serve({
    '/api/v1/auth/me': () => json(200, meBody('admin', trialPlan)),
    '/api/v1/config': () => json(200, config),
    '/api/v1/billing': () => json(200, trialBilling),
    '/api/v1/billing/checkout': () => json(200, { url: 'https://polar.example/checkout/founding' }),
    '/api/v1/inbox/activity': () => json(200, { now: '2026-10-15T10:00:00Z', new_conversations: [], assigned_to_me: [], updated: [] }),
  });
  const history = createMemoryHistory({ initialEntries: ['/settings/billing?buy=founding'] });
  const router = createRouter({ routeTree, history });
  const checkouts = () => fetchMock.mock.calls.filter(([url]) => String(url) === '/api/v1/billing/checkout').length;
  const view = render(<QueryClientProvider client={createQueryClient()}><RouterProvider router={router} /></QueryClientProvider>);
  await waitFor(() => expect(checkouts()).toBe(1));
  expect(router.state.location.search).not.toHaveProperty('buy');
  view.unmount();
  render(<QueryClientProvider client={createQueryClient()}><RouterProvider router={createRouter({ routeTree, history })} /></QueryClientProvider>);
  await screen.findByRole('button', { name: /team, \$69 per month/i });
  expect(checkouts()).toBe(1);
});

test('billing lives in Settings: the rail keeps Settings current, the settings nav marks Plan and billing, the banner does not offer the page it is on', async () => {
  serve({
    '/api/v1/auth/me': () => json(200, meBody('admin', trialPlan)),
    '/api/v1/config': () => json(200, config),
    '/api/v1/billing': () => json(200, trialBilling),
    '/api/v1/inbox/activity': () => json(200, { now: '2026-10-15T10:00:00Z', new_conversations: [], assigned_to_me: [], updated: [] }),
  });
  open('/settings/billing');
  await screen.findByRole('heading', { name: 'Plan and billing' });
  const rail = screen.getByRole('navigation', { name: 'Main' });
  expect(within(rail).queryByRole('link', { name: 'Billing' })).not.toBeInTheDocument();
  expect(within(rail).getByRole('link', { name: 'Settings' })).toHaveAttribute('aria-current', 'page');
  const sections = screen.getByRole('navigation', { name: 'Settings sections' });
  expect(within(sections).getByRole('link', { name: 'Plan and billing' })).toHaveAttribute('aria-current', 'page');
  expect(within(sections).getByRole('link', { name: 'Team' })).toHaveAttribute('href', '/settings#team-heading');
  expect(screen.getByRole('region', { name: /plan notices/i })).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Choose a plan' })).not.toBeInTheDocument();
});

test('signing out forgets the agent for analytics only after the server ended the session', async () => {
  let loggedOut = false;
  fetchMock.mockImplementation((url: string) => {
    const path = url.split('?')[0];
    if (path === '/api/v1/auth/logout') { loggedOut = true; return Promise.resolve(new Response(null, { status: 204 })); }
    if (path === '/api/v1/auth/me') return Promise.resolve(loggedOut ? json(401, { error: { code: 'unauthorized', message: 'no' } }) : json(200, meBody('agent', null)));
    if (path === '/api/v1/config') return Promise.resolve(json(200, config));
    return Promise.resolve(json(200, { items: [], page: 1, page_size: 50 }));
  });
  open('/settings');
  vi.mocked(resetProductAnalytics).mockClear();
  fireEvent.click(await screen.findByRole('button', { name: /account|sam@example.com/i }));
  expect(resetProductAnalytics).not.toHaveBeenCalled();
  fireEvent.click(await screen.findByRole('button', { name: 'Sign out' }));
  await waitFor(() => expect(resetProductAnalytics).toHaveBeenCalledTimes(1));
});
