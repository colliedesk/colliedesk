import { QueryClientProvider } from '@tanstack/react-query';
import { RouterProvider, createMemoryHistory, createRouter } from '@tanstack/react-router';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeAll, expect, test, vi } from 'vitest';
import { routeTree } from './app';
import { forgetPermissions } from './api/client';
import { readCheckoutIntent, rememberCheckoutIntent } from './features/settings/checkout-intent';
import { readPlanIntent, rememberPlanIntent } from './features/settings/plan-intent';
import { createQueryClient } from './query-client';
import { resetProductAnalytics } from './lib/product-analytics';
import { presenceViewPart } from './features/inbox/presence-part';
import { permissionsOf, rolesOf } from './test-roles';
import { autofixPart } from './features/autofix/autofix-part';

// Only PostHog's own wrapper calls are observed; the rest of the module is the real one.
// Clerk draws its own sign-in form from the network; the page around it is what is under test here.
// Clerk is the identity provider, the external boundary: its session state and sign-out are faked here.
const clerk = vi.hoisted(() => ({ signedIn: false, signOut: vi.fn() }));
vi.mock('@clerk/clerk-react', () => ({
  ClerkProvider: ({ children }: { children: React.ReactNode }) => children,
  SignIn: () => <div>Clerk widget</div>,
  useAuth: () => ({ getToken: vi.fn(), isLoaded: true, isSignedIn: clerk.signedIn }),
  useClerk: () => ({ signOut: clerk.signOut }),
}));
vi.mock('./lib/product-analytics', async (original) => ({ ...(await original<typeof import('./lib/product-analytics')>()), resetProductAnalytics: vi.fn() }));

const fetchMock = vi.fn();
vi.stubGlobal('fetch', fetchMock);
afterEach(() => { fetchMock.mockReset(); window.sessionStorage.clear(); clerk.signedIn = false; clerk.signOut.mockReset(); });

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status });
const locked = { error: { code: 'workspace_locked', message: 'This workspace is locked because its trial or subscription ended.' } };
const meBody = (role: 'admin' | 'agent', plan: unknown) => ({
  agent: { id: '11111111-1111-4111-8111-111111111111', email: 'sam@example.com', display_name: '', roles: rolesOf(role), timezone_override: null },
  workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'beta-desk', name: 'Beta Desk', timezone: 'UTC' },
  timezone: 'UTC', token: null, permissions: permissionsOf(role), plan,
});
const config = { cloud_sign_in: { enabled: true, publishable_key: 'pk' }, billing: { enabled: true }, signup: { enabled: true }, limits: { outgoing_attachments_bytes: 10485760, outgoing_attachments_files: 10 },
  product_analytics: { enabled: false, key: null, host: null, realm: null }, ai: { server_provider: false } };
const selfHostedConfig = { ...config, cloud_sign_in: { enabled: false, publishable_key: null }, billing: { enabled: false }, signup: { enabled: false } };
// Founding is full in these fixtures, so the server sells the plan ladder.
const billing = {
  plan: 'none', trial_ends_at: '2026-10-01T09:00:00Z', locked: true, status: 'none', current_period_end: null, cancel_at_period_end: false,
  active_subscriptions: 0, complimentary: false, complimentary_until: null, checkout_refusals: [], storage_upgrade_plan: null, purchased_plan: null, migration_requested_at: null, checkout_plans: ['team_monthly', 'team_yearly', 'team25_monthly', 'team25_yearly'],
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
  return router;
}


// The presence chunk loads once a conversation is open; start with it there, so no test ends while it is
// still on its way (vitest refuses a module asked for after the test environment is gone).
beforeAll(async () => {
  presenceViewPart.preload();
  await import('./features/inbox/presence-view');
  autofixPart.preload();
  await import('./features/autofix/autofix-chunk');
});

test('a locked workspace sees the locked screen with the identity bar and an admin\'s plan choices', async () => {
  serve({
    '/api/v1/auth/me': () => json(200, meBody('admin', { plan: 'none', trial_ends_at: '2026-10-01T09:00:00Z', locked: true, trial_extended: false, agents_over: 0, seat_paused: false })),
    '/api/v1/config': () => json(200, config),
    '/api/v1/billing': () => json(200, billing),
    '/api/v1/conversations': () => json(402, locked),
  });
  open();
  expect(await screen.findByRole('heading', { level: 1, name: 'Your trial has ended' })).toBeInTheDocument();
  expect(screen.getByRole('region', { name: 'Signed in as' })).toHaveTextContent('Beta Desk');
  expect(screen.getByRole('button', { name: /team, \$69 per month/i })).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Export all data' })).toBeInTheDocument();
});

test('a locked admin can still take the workspace out: Export all data asks for its download link while the workspace is locked', async () => {
  const assign = vi.fn();
  const original = window.location;
  Object.defineProperty(window, 'location', { configurable: true, value: { ...original, assign, search: '' } });
  try {
    serve({
      '/api/v1/auth/me': () => json(200, meBody('admin', { plan: 'none', trial_ends_at: '2026-10-01T09:00:00Z', locked: true, trial_extended: false, agents_over: 0, seat_paused: false })),
      '/api/v1/config': () => json(200, config),
      '/api/v1/billing': () => json(200, billing),
      '/api/v1/conversations': () => json(402, locked),
      '/api/v1/export/link': () => json(200, { url: '/api/v1/export/download?token=once', expires_in_seconds: 60 }),
    });
    open();
    fireEvent.click(await screen.findByRole('button', { name: 'Export all data' }));
    await waitFor(() => expect(assign).toHaveBeenCalledWith('/api/v1/export/download?token=once'));
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/export/link', expect.objectContaining({ method: 'POST' }));
  } finally {
    Object.defineProperty(window, 'location', { configurable: true, value: original });
  }
});

test('a 402 workspace_locked from the inbox replaces the inbox with the locked screen', async () => {
  serve({
    '/api/v1/auth/me': () => json(200, meBody('agent', { plan: 'trial', trial_ends_at: '2026-10-30T09:00:00Z', locked: false, trial_extended: false, agents_over: 0, seat_paused: false, admins: ['Maya Reyes', 'Daniel Kim'] })),
    '/api/v1/config': () => json(200, config),
    '/api/v1/conversations': () => json(402, locked),
    '/api/v1/agents': () => json(402, locked),
    '/api/v1/tags': () => json(402, locked),
    '/api/v1/channels/email': () => json(402, locked),
  });
  open();
  expect(await screen.findByRole('heading', { level: 1, name: 'This workspace is locked' })).toBeInTheDocument();
  expect(screen.getByText('Nothing is deleted. Ask an admin to choose a plan: Maya Reyes, Daniel Kim.')).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Export all data' })).not.toBeInTheDocument();
  expect(screen.getByRole('region', { name: 'Signed in as' })).toBeInTheDocument();
});

test('the inbox activity poll runs on every signed-in page, not only in the inbox', async () => {
  serve({
    '/api/v1/auth/me': () => json(200, meBody('agent', { plan: 'trial', trial_ends_at: '2026-10-30T09:00:00Z', locked: false, trial_extended: false, agents_over: 0, seat_paused: false })),
    '/api/v1/config': () => json(200, config),
    '/api/v1/inbox/activity': () => json(200, { now: '2026-10-15T10:00:00Z', new_conversations: [], assigned_to_me: [], updated: [] }),
  });
  open('/settings');
  await waitFor(() => expect(fetchMock.mock.calls.some(([url]) => String(url).startsWith('/api/v1/inbox/activity'))).toBe(true));
});

test('a locked workspace does not poll for inbox activity', async () => {
  serve({
    '/api/v1/auth/me': () => json(200, meBody('admin', { plan: 'none', trial_ends_at: '2026-10-01T09:00:00Z', locked: true, trial_extended: false, agents_over: 0, seat_paused: false })),
    '/api/v1/config': () => json(200, config),
    '/api/v1/billing': () => json(200, billing),
  });
  open();
  await screen.findByRole('heading', { level: 1, name: 'Your trial has ended' });
  expect(fetchMock.mock.calls.some(([url]) => String(url).startsWith('/api/v1/inbox/activity'))).toBe(false);
});

const trialPlan = { plan: 'trial', trial_ends_at: '2026-10-30T09:00:00Z', locked: false, trial_extended: false, agents_over: 0, seat_paused: false };
const trialBilling = { ...billing, plan: 'trial', locked: false, checkout_plans: [...billing.checkout_plans, 'founding'] };

test('the billing link with buy=founding sends a signed-out visitor straight to sign-up as a buyer, keeping the plan', async () => {
  serve({
    '/api/v1/auth/me': () => json(401, { error: { code: 'unauthorized', message: 'Sign in.' } }),
    '/api/v1/config': () => json(200, config),
  });
  const router = open('/settings/billing?buy=founding');
  expect(await screen.findByRole('heading', { level: 1, name: 'Join Founding' })).toBeInTheDocument();
  expect(router.state.location.pathname).toBe('/sign-in');
  expect(router.state.location.search).toEqual({ buy: 'founding' });
  expect(screen.queryByText(/you are signed out/i)).not.toBeInTheDocument();
  expect(screen.queryByText(/ask your workspace admin/i)).not.toBeInTheDocument();
  expect(readCheckoutIntent()).toBe('founding');
});

test('after a Founding visit, the plain sign-in page in the same tab is a sign-in again, not Join Founding', async () => {
  serve({ '/api/v1/config': () => json(200, config) });
  const router = open('/sign-in?buy=founding');
  expect(await screen.findByRole('heading', { level: 1, name: 'Join Founding' })).toBeInTheDocument();
  // The identity provider returns to the same address, plan included: the buyer stays a buyer.
  await router.navigate({ to: '/sign-in', search: { buy: 'founding' } });
  expect(screen.getByRole('heading', { level: 1, name: 'Join Founding' })).toBeInTheDocument();
  expect(readCheckoutIntent()).toBe('founding');
  // "Log in" on the website opens the plain address.
  await router.navigate({ to: '/sign-in' });
  expect(await screen.findByRole('heading', { level: 1, name: 'Sign in to Colliedesk' })).toBeInTheDocument();
  expect(readCheckoutIntent()).toBeUndefined();
});

test('the Team 25 trial link keeps its plan through the sign-in page until a plain sign-in forgets it', async () => {
  serve({ '/api/v1/config': () => json(200, config) });
  const router = open('/sign-in?intent=trial&plan=team25');
  expect(await screen.findByRole('heading', { level: 1, name: 'Start your free trial' })).toBeInTheDocument();
  expect(readPlanIntent()).toBe('team25');
  await router.navigate({ to: '/sign-in' });
  expect(await screen.findByRole('heading', { level: 1, name: 'Sign in to Colliedesk' })).toBeInTheDocument();
  expect(readPlanIntent()).toBeUndefined();
});

test('in a trial workspace that came for Team 25, Buy Team 25 opens the billing page with Team 25 first and marked, and buys nothing', async () => {
  rememberPlanIntent('team25');
  serve({
    '/api/v1/auth/me': () => json(200, meBody('admin', trialPlan)),
    '/api/v1/config': () => json(200, config),
    '/api/v1/billing': () => json(200, trialBilling),
    '/api/v1/inbox/activity': () => json(200, { now: '2026-10-15T10:00:00Z', new_conversations: [], assigned_to_me: [], updated: [] }),
  });
  const router = open('/');
  fireEvent.click(await screen.findByRole('button', { name: 'Buy Team 25' }));
  await screen.findByRole('heading', { name: 'Plan and billing' });
  const cards = await screen.findAllByRole('article');
  expect(cards.map((card) => card.getAttribute('aria-label'))).toEqual(['Team 25', 'Founding']);
  expect(within(cards[0]).getByText('Your pick')).toBeInTheDocument();
  expect(router.state.location.pathname).toBe('/settings/billing');
  expect(fetchMock.mock.calls.some(([url]) => String(url) === '/api/v1/billing/checkout')).toBe(false);
});

test('the website trial link titles the sign-in page for a new team', async () => {
  serve({ '/api/v1/config': () => json(200, config) });
  open('/sign-in?intent=trial');
  expect(await screen.findByRole('heading', { level: 1, name: 'Start your free trial' })).toBeInTheDocument();
  expect(screen.queryByRole('heading', { name: 'Sign in to Colliedesk' })).not.toBeInTheDocument();
});

// On the cloud the way back in is the sign-in page itself, for a known page and an unknown one alike.
for (const path of ['/settings/billing', '/no-such-route']) {
  test(`a signed-out visit to ${path} on the cloud lands on the sign-in page`, async () => {
    serve({
      '/api/v1/auth/me': () => json(401, { error: { code: 'unauthorized', message: 'Sign in.' } }),
      '/api/v1/config': () => json(200, config),
    });
    const router = open(path);
    expect(await screen.findByRole('heading', { level: 1, name: 'Sign in to Colliedesk' })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe('/sign-in');
    expect(screen.getByRole('alert')).toHaveTextContent('You are signed out.');
    expect(screen.queryByText(/ask your workspace admin for a new link/i)).not.toBeInTheDocument();
  });
}

test('a signed-out visit to an unknown page on a self-hosted server keeps the signed-out screen and its pairing hint', async () => {
  serve({
    '/api/v1/auth/me': () => json(401, { error: { code: 'unauthorized', message: 'Sign in.' } }),
    '/api/v1/config': () => json(200, selfHostedConfig),
  });
  const router = open('/no-such-route');
  expect(await screen.findByRole('alert')).toHaveTextContent('You are signed out. Ask your workspace admin for a new link.');
  expect(screen.getByText(/collie pair/)).toBeInTheDocument();
  expect(router.state.location.pathname).toBe('/no-such-route');
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

test('an invite link shows the workspace it joins before it is used, and joins with one press', async () => {
  serve({
    '/api/v1/auth/invite-preview': () => json(200, { state: 'pending', workspace_name: 'Beta Desk', invited_by: 'Mai Tran', email: 'sam@example.com', roles: ['Agent'], expires_at: '2026-11-12T14:30:00Z' }),
    '/api/v1/auth/redeem': () => json(200, { agent: meBody('agent', trialPlan).agent, workspace: meBody('agent', trialPlan).workspace }),
    '/api/v1/auth/me': () => json(200, meBody('agent', trialPlan)),
    '/api/v1/config': () => json(200, config),
    '/api/v1/inbox/activity': () => json(200, { now: '2026-10-15T10:00:00Z', new_conversations: [], assigned_to_me: [], updated: [] }),
  });
  const router = open('/sign-in?token=invite-token');
  expect(await screen.findByRole('heading', { level: 1, name: 'Join Beta Desk' })).toBeInTheDocument();
  expect(screen.getByText('Mai Tran invited you to the support team.')).toBeInTheDocument();
  expect(fetchMock.mock.calls.some(([url]) => String(url) === '/api/v1/auth/invite-preview?token=invite-token')).toBe(true);
  expect(fetchMock.mock.calls.some(([url]) => String(url) === '/api/v1/auth/redeem')).toBe(false);
  fireEvent.click(screen.getByRole('button', { name: 'Join Beta Desk' }));
  await waitFor(() => expect(router.state.location.pathname).toBe('/'));
  expect(fetchMock.mock.calls.filter(([url]) => String(url) === '/api/v1/auth/redeem')).toHaveLength(1);
});

test('a link the invite preview does not describe (a pairing link, an unknown token) keeps the link page', async () => {
  serve({
    '/api/v1/auth/invite-preview': () => json(401, { error: { code: 'invalid_link', message: 'This link is invalid, expired, revoked, or already used.' } }),
    '/api/v1/auth/redeem': () => json(401, { error: { code: 'invalid_link', message: 'This link is invalid, expired, revoked, or already used.' } }),
    '/api/v1/config': () => json(200, selfHostedConfig),
  });
  open('/sign-in?token=pairing-token');
  expect(await screen.findByRole('heading', { level: 1, name: 'Open your secure link' })).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Continue to Colliedesk' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('This link is invalid, expired, or already used. Ask your workspace admin for a new link.');
});

test('a locked workspace drops the remembered plan and shows its own plan choices', async () => {
  rememberCheckoutIntent('founding');
  serve({
    '/api/v1/auth/me': () => json(200, meBody('admin', { plan: 'none', trial_ends_at: '2026-10-01T09:00:00Z', locked: true, trial_extended: false, agents_over: 0, seat_paused: false })),
    '/api/v1/config': () => json(200, config),
    '/api/v1/billing': () => json(200, billing),
  });
  open('/settings/billing?buy=founding');
  await screen.findByRole('heading', { level: 1, name: 'Your trial has ended' });
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
  await screen.findByRole('button', { name: 'Choose Founding, $99 per quarter for life' });
  expect(checkouts()).toBe(1);
});

test('billing lives in Settings: the rail keeps Settings current, the settings nav marks Plan and billing, and the trial strip is not repeated on the page that says the same', async () => {
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
  expect(within(sections).getByRole('link', { name: 'Members' })).toHaveAttribute('href', '/settings/members');
  expect(screen.queryByRole('region', { name: /plan notices/i })).not.toBeInTheDocument();
  expect(screen.getByText(/ends oct 1, 2026/i)).toBeInTheDocument();
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

test('signing out forgets every unsent reply and note kept in this browser, and nothing else', async () => {
  let loggedOut = false;
  fetchMock.mockImplementation((url: string) => {
    const path = url.split('?')[0];
    if (path === '/api/v1/auth/logout') { loggedOut = true; return Promise.resolve(new Response(null, { status: 204 })); }
    if (path === '/api/v1/auth/me') return Promise.resolve(loggedOut ? json(401, { error: { code: 'unauthorized', message: 'no' } }) : json(200, meBody('agent', null)));
    if (path === '/api/v1/config') return Promise.resolve(json(200, config));
    return Promise.resolve(json(200, { items: [], page: 1, page_size: 50 }));
  });
  window.localStorage.clear();
  window.localStorage.setItem('colliedesk.reply-draft:ws-1:agent-1:conv-1', JSON.stringify({ kind: 'note', body: 'Private note', saved_at: Date.now() }));
  window.localStorage.setItem('colliedesk.reply-draft:ws-1:conv-2', JSON.stringify({ kind: 'outgoing', body: 'Older format' }));
  window.localStorage.setItem('colliedesk.other', 'kept');
  open('/settings');
  fireEvent.click(await screen.findByRole('button', { name: /account|sam@example.com/i }));
  fireEvent.click(await screen.findByRole('button', { name: 'Sign out' }));
  await waitFor(() => expect(Object.keys(window.localStorage)).toEqual(['colliedesk.other']));
});

test('a paused agent sees why and can sign out, and nothing else', async () => {
  serve({
    '/api/v1/auth/me': () => json(200, meBody('agent', { plan: 'small', trial_ends_at: '2026-10-01T09:00:00Z', locked: false, trial_extended: false, agents_over: 1, seat_paused: true })),
    '/api/v1/config': () => json(200, config),
  });
  open();
  expect(await screen.findByRole('heading', { level: 1, name: 'Your access is paused' })).toBeInTheDocument();
  expect(screen.getByText(/small plan has more agents than it allows/i)).toBeInTheDocument();
  expect(screen.getAllByRole('button', { name: 'Sign out' }).length).toBeGreaterThan(0);
  expect(screen.queryByRole('link', { name: 'Inbox' })).not.toBeInTheDocument();
  expect(fetchMock.mock.calls.some(([url]) => String(url).startsWith('/api/v1/inbox/activity'))).toBe(false);
});

test('a paused admin can still open the billing portal from the paused screen', async () => {
  const assign = vi.fn();
  const original = window.location;
  Object.defineProperty(window, 'location', { configurable: true, value: { ...original, assign } });
  serve({
    '/api/v1/auth/me': () => json(200, meBody('admin', { plan: 'solo', trial_ends_at: '2026-10-01T09:00:00Z', locked: false, trial_extended: false, agents_over: 1, seat_paused: true })),
    '/api/v1/config': () => json(200, config),
    '/api/v1/billing/portal': () => json(200, { url: 'https://polar.test/portal' }),
  });
  open();
  fireEvent.click(await screen.findByRole('button', { name: 'Manage billing' }));
  await waitFor(() => expect(assign).toHaveBeenCalledWith('https://polar.test/portal'));
  Object.defineProperty(window, 'location', { configurable: true, value: original });
});

test('the rail opens the knowledge base, which marks Knowledge base as the current destination', async () => {
  serve({
    '/api/v1/auth/me': () => json(200, meBody('agent', null)),
    '/api/v1/config': () => json(200, config),
    '/api/v1/kb/collections': () => json(200, { items: [] }),
    '/api/v1/kb/articles': () => json(200, { items: [], next_cursor: null }),
  });
  open('/knowledge');
  expect(await screen.findByRole('heading', { level: 1, name: 'Knowledge base' })).toBeInTheDocument();
  const rail = screen.getByRole('navigation', { name: 'Main' });
  expect(within(rail).getByRole('link', { name: 'Knowledge base' })).toHaveAttribute('aria-current', 'page');
  expect(within(rail).getByRole('link', { name: 'Inbox' })).not.toHaveAttribute('aria-current');
});

test('the Colliedesk logo at the top of the rail goes back to the inbox from any page', async () => {
  serve({
    '/api/v1/auth/me': () => json(200, meBody('agent', null)),
    '/api/v1/config': () => json(200, config),
    '/api/v1/kb/collections': () => json(200, { items: [] }),
    '/api/v1/kb/articles': () => json(200, { items: [], next_cursor: null }),
  });
  const router = open('/knowledge');
  expect(await screen.findByRole('heading', { level: 1, name: 'Knowledge base' })).toBeInTheDocument();
  const rail = screen.getByRole('navigation', { name: 'Main' });
  fireEvent.click(within(rail).getByRole('link', { name: 'Colliedesk' }));
  await waitFor(() => expect(router.state.location.pathname).toBe('/'));
  expect(within(rail).getByRole('link', { name: 'Inbox' })).toHaveAttribute('aria-current', 'page');
});

test('a settings page shows its title at once while its own chunk loads, then the sections', async () => {
  serve({
    '/api/v1/auth/me': () => json(200, meBody('agent', trialPlan)),
    '/api/v1/config': () => json(200, config),
    '/api/v1/inbox/activity': () => json(200, { now: '2026-10-15T10:00:00Z', new_conversations: [], assigned_to_me: [], updated: [] }),
  });
  open('/settings/account');
  const title = await screen.findByRole('heading', { level: 1, name: 'Account' });
  expect(title).toBeInTheDocument();
  // The sections arrive with the chunk; the title stays where it was.
  await waitFor(() => expect(document.querySelector('.settings-page[aria-busy="true"]')).toBeNull());
  expect(screen.getByRole('heading', { level: 1, name: 'Account' })).toBeInTheDocument();
});

const settingsServer = () => serve({
  '/api/v1/auth/me': () => json(200, meBody('admin', trialPlan)),
  '/api/v1/config': () => json(200, selfHostedConfig),
  '/api/v1/inbox/activity': () => json(200, { now: '2026-10-15T10:00:00Z', new_conversations: [], assigned_to_me: [], updated: [] }),
  '/api/v1/auth/sessions': () => json(200, { items: [] }),
});
const where = (router: ReturnType<typeof open>) => `${router.state.location.pathname}${router.state.location.hash ? `#${router.state.location.hash}` : ''}`;

// Settings is one page per item of its menu (#528): /settings itself opens Account.
test('Settings opens Account, at its own address', async () => {
  settingsServer();
  const router = open('/settings');
  await waitFor(() => expect(router.state.location.pathname).toBe('/settings/account'));
  expect(await screen.findByRole('heading', { level: 1, name: 'Account' })).toBeInTheDocument();
});

// The sections of the old one-page Settings are linked from bookmarks, emails, MCP answers and guides.
test('an old link to a section of the one Settings page opens the page that holds it now', async () => {
  settingsServer();
  for (const [from, to] of [['/settings#invites-heading', '/settings/members#invites-heading'], ['/settings#agents-heading', '/settings/ai#agents-heading'],
    ['/settings#export-heading', '/settings/data#export-heading'], ['/settings#sessions-heading', '/settings/account#sessions-heading'],
    ['/settings/roles', '/settings/members#roles-heading'], ['/settings/numbers', '/settings/general#numbers-heading'], ['/settings/import-freshdesk', '/settings/data']]) {
    const router = open(from);
    await waitFor(() => expect(where(router), from).toBe(to));
    cleanup();
  }
});

test('an old anchor followed while a settings page is open opens its page too, and Back returns', async () => {
  settingsServer();
  const router = open('/settings/account');
  await screen.findByRole('heading', { level: 1, name: 'Account' });
  await act(() => router.navigate({ href: '/settings#team-heading' }));
  await waitFor(() => expect(where(router)).toBe('/settings/members#team-heading'));
  expect(await screen.findByRole('heading', { level: 1, name: 'Members' })).toBeInTheDocument();
  await act(() => { router.history.back(); });
  await waitFor(() => expect(router.state.location.pathname).toBe('/settings/account'));
  expect(await screen.findByRole('heading', { level: 1, name: 'Account' })).toBeInTheDocument();
});

// The menu moves between pages through the router: Back and Forward walk them in order.
test('the settings menu opens each page at its address, and Back and Forward walk them', async () => {
  settingsServer();
  const router = open('/settings/account');
  const menu = await screen.findByRole('navigation', { name: 'Settings sections' });
  fireEvent.click(within(menu).getByRole('link', { name: 'General' }));
  await waitFor(() => expect(router.state.location.pathname).toBe('/settings/general'));
  expect(await screen.findByRole('heading', { level: 1, name: 'General' })).toBeInTheDocument();
  fireEvent.click(within(screen.getByRole('navigation', { name: 'Settings sections' })).getByRole('link', { name: 'Members' }));
  await waitFor(() => expect(router.state.location.pathname).toBe('/settings/members'));
  await act(() => { router.history.back(); });
  await waitFor(() => expect(router.state.location.pathname).toBe('/settings/general'));
  await act(() => { router.history.forward(); });
  await waitFor(() => expect(router.state.location.pathname).toBe('/settings/members'));
  expect(await screen.findByRole('heading', { level: 1, name: 'Members' })).toBeInTheDocument();
});

// The server answers me until the session ends, then 401, as after a real sign-out.
function serveUntilSignedOut(meResponse: () => Response, appConfig: unknown, extra: Record<string, () => Response> = {}) {
  let loggedOut = false;
  fetchMock.mockImplementation((url: string) => {
    const path = url.split('?')[0];
    if (path === '/api/v1/auth/logout') { loggedOut = true; return Promise.resolve(new Response(null, { status: 204 })); }
    if (path === '/api/v1/auth/me') return Promise.resolve(loggedOut ? json(401, { error: { code: 'unauthorized', message: 'no' } }) : meResponse());
    if (path === '/api/v1/config') return Promise.resolve(json(200, appConfig));
    const handler = extra[path];
    return Promise.resolve(handler ? handler() : json(200, { items: [], page: 1, page_size: 50 }));
  });
}

test('on the cloud, sign out from the account menu lands on the sign-in page and ends the identity provider session too', async () => {
  clerk.signedIn = true;
  clerk.signOut.mockImplementation(() => { clerk.signedIn = false; return Promise.resolve(); });
  serveUntilSignedOut(() => json(200, meBody('agent', null)), config);
  const router = open('/settings');
  fireEvent.click(await screen.findByRole('button', { name: /account|sam@example.com/i }));
  fireEvent.click(await screen.findByRole('button', { name: 'Sign out' }));

  expect(await screen.findByRole('heading', { level: 1, name: 'Sign in to Colliedesk' })).toBeInTheDocument();
  expect(router.state.location.pathname).toBe('/sign-in');
  expect(screen.getByRole('alert')).toHaveTextContent('You are signed out.');
  await waitFor(() => expect(clerk.signOut).toHaveBeenCalledTimes(1));
  // The next sign-in shows the form instead of signing the same person straight back in.
  expect(await screen.findByText('Clerk widget')).toBeInTheDocument();
  expect(screen.queryByRole('region', { name: 'Signed in as' })).not.toBeInTheDocument();
});

test('on the cloud, sign out from the locked screen ends the identity provider session too', async () => {
  clerk.signedIn = true;
  clerk.signOut.mockImplementation(() => { clerk.signedIn = false; return Promise.resolve(); });
  serveUntilSignedOut(() => json(200, meBody('admin', { plan: 'none', trial_ends_at: '2026-10-01T09:00:00Z', locked: true, trial_extended: false, agents_over: 0, seat_paused: false })), config,
    { '/api/v1/billing': () => json(200, billing) });
  const router = open();
  expect(await screen.findByRole('heading', { level: 1, name: 'Your trial has ended' })).toBeInTheDocument();
  // An admin's locked screen has no Sign out of its own: the way out is the one in the account menu.
  expect(screen.getByRole('button', { name: 'Export all data' })).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: /^Account/ }));
  fireEvent.click(screen.getByRole('button', { name: 'Sign out' }));

  expect(await screen.findByRole('heading', { level: 1, name: 'Sign in to Colliedesk' })).toBeInTheDocument();
  expect(router.state.location.pathname).toBe('/sign-in');
  await waitFor(() => expect(clerk.signOut).toHaveBeenCalledTimes(1));
});

test('signing in again after being signed out opens the inbox instead of bouncing back to sign-in', async () => {
  let signedIn = false;
  fetchMock.mockImplementation((url: string) => {
    const path = url.split('?')[0];
    if (path === '/api/v1/auth/redeem') { signedIn = true; return Promise.resolve(json(200, { agent: meBody('agent', null).agent, workspace: meBody('agent', null).workspace })); }
    if (path === '/api/v1/auth/me') return Promise.resolve(signedIn ? json(200, meBody('agent', null)) : json(401, { error: { code: 'unauthorized', message: 'no' } }));
    if (path === '/api/v1/config') return Promise.resolve(json(200, config));
    if (path === '/api/v1/inbox/activity') return Promise.resolve(json(200, { now: '2026-10-15T10:00:00Z', new_conversations: [], assigned_to_me: [], updated: [] }));
    return Promise.resolve(json(200, { items: [], page: 1, page_size: 50 }));
  });
  const router = open('/');
  await waitFor(() => expect(router.state.location.pathname).toBe('/sign-in'));
  await router.navigate({ to: '/sign-in', search: { token: 'invite-token' } });
  fireEvent.click(await screen.findByRole('button', { name: 'Continue to Colliedesk' }));

  expect(await screen.findByRole('region', { name: 'Signed in as' })).toHaveTextContent('Beta Desk');
  expect(router.state.location.pathname).toBe('/');
  expect(clerk.signOut).not.toHaveBeenCalled();
});

test('on a self-hosted server, signing out leaves no identity of the old session on screen', async () => {
  serveUntilSignedOut(() => json(200, meBody('agent', null)), selfHostedConfig);
  open('/settings');
  expect(await screen.findByRole('region', { name: 'Signed in as' })).toHaveTextContent('Beta Desk');
  fireEvent.click(await screen.findByRole('button', { name: /account|sam@example.com/i }));
  fireEvent.click(await screen.findByRole('button', { name: 'Sign out' }));

  expect(await screen.findByRole('alert')).toHaveTextContent('You are signed out. Ask your workspace admin for a new link.');
  expect(screen.getByText(/collie pair/)).toBeInTheDocument();
  expect(screen.queryByRole('region', { name: 'Signed in as' })).not.toBeInTheDocument();
});

test('an unknown page says so with a title and a way back to the inbox, and no rail destination is current', async () => {
  serve({
    '/api/v1/auth/me': () => json(200, meBody('agent', null)),
    '/api/v1/config': () => json(200, config),
    '/api/v1/inbox/activity': () => json(200, { now: '2026-10-15T10:00:00Z', new_conversations: [], assigned_to_me: [], updated: [] }),
  });
  const router = open('/no-such-route');
  const main = await screen.findByRole('main');
  expect(within(main).getByRole('heading', { level: 1, name: 'Page not found' })).toBeInTheDocument();
  expect(within(main).getByText('There is no page at this address.')).toBeInTheDocument();
  const rail = screen.getByRole('navigation', { name: 'Main' });
  for (const link of within(rail).getAllByRole('link')) expect(link).not.toHaveAttribute('aria-current');
  fireEvent.click(within(main).getByRole('link', { name: 'Back to Inbox' }));
  await waitFor(() => expect(router.state.location.pathname).toBe('/'));
  expect(within(rail).getByRole('link', { name: 'Inbox' })).toHaveAttribute('aria-current', 'page');
});

test('Autofix is in preview: without the browser\'s key there is no Fixes link, no status request and no Fixes page', async () => {
  const autofixConfig = { ...config, autofix: { available: true, missing_settings: [] } };
  const handlers = {
    '/api/v1/auth/me': () => json(200, meBody('admin', trialPlan)),
    '/api/v1/config': () => json(200, autofixConfig),
    '/api/v1/inbox/activity': () => json(200, { now: '2026-10-15T10:00:00Z', new_conversations: [], assigned_to_me: [], updated: [] }),
    '/api/v1/autofix/status': () => json(200, { connected_repos: 1, needs_person: 2, running: 0 }),
  };
  localStorage.removeItem('colliedesk.autofix');
  serve(handlers);
  open('/fixes');
  expect(await screen.findByRole('heading', { level: 1, name: 'Page not found' })).toBeInTheDocument();
  await waitFor(() => expect(fetchMock.mock.calls.some(([url]) => String(url).startsWith('/api/v1/inbox/activity'))).toBe(true));
  expect(fetchMock.mock.calls.some(([url]) => String(url).startsWith('/api/v1/autofix/status'))).toBe(false);
  expect(screen.queryByRole('link', { name: /^Fixes/ })).not.toBeInTheDocument();
});

test('with the browser\'s key on, Autofix shows: the Fixes link with its badge', async () => {
  localStorage.setItem('colliedesk.autofix', 'on');
  try {
    serve({
      '/api/v1/auth/me': () => json(200, meBody('admin', trialPlan)),
      '/api/v1/config': () => json(200, { ...config, autofix: { available: true, missing_settings: [] } }),
      '/api/v1/inbox/activity': () => json(200, { now: '2026-10-15T10:00:00Z', new_conversations: [], assigned_to_me: [], updated: [] }),
      '/api/v1/autofix/status': () => json(200, { connected_repos: 1, needs_person: 2, running: 0 }),
    });
    open('/settings');
    expect(await screen.findByRole('link', { name: 'Fixes, 2 need a person' })).toHaveAttribute('href', '/fixes');
  } finally {
    localStorage.removeItem('colliedesk.autofix');
  }
});


test('Review is on the rail only where a chat channel collects messages, with the cards that wait', async () => {
  const trial = meBody('agent', { plan: 'trial', trial_ends_at: '2026-10-30T09:00:00Z', locked: false, trial_extended: false, agents_over: 0, seat_paused: false });
  serve({
    '/api/v1/auth/me': () => json(200, trial),
    '/api/v1/config': () => json(200, config),
    '/api/v1/intake/summary': () => json(200, { available: true, waiting: 4, deciding: 0, scopes: [] }),
  });
  open('/settings');
  expect(await screen.findByRole('link', { name: 'Review, 4 waiting' })).toHaveAttribute('href', '/review');
  cleanup();
  serve({
    '/api/v1/auth/me': () => json(200, trial),
    '/api/v1/config': () => json(200, config),
    '/api/v1/intake/summary': () => json(200, { available: false, waiting: 0, deciding: 0, scopes: [] }),
  });
  open('/settings');
  await waitFor(() => expect(fetchMock.mock.calls.some(([url]) => String(url) === '/api/v1/intake/summary')).toBe(true));
  // The answer is in: nothing left to render from it.
  await act(async () => { await Promise.resolve(); });
  expect(screen.getByRole('link', { name: 'Settings' })).toBeInTheDocument();
  expect(screen.queryByRole('link', { name: /^Review/ })).not.toBeInTheDocument();
});

test('Review opened with a channel and no scope lists that channel\'s cards only', async () => {
  const trial = meBody('agent', { plan: 'trial', trial_ends_at: '2026-10-30T09:00:00Z', locked: false, trial_extended: false, agents_over: 0, seat_paused: false });
  const channel = '44444444-4444-4444-8444-444444444444';
  serve({
    '/api/v1/auth/me': () => json(200, trial),
    '/api/v1/config': () => json(200, config),
    '/api/v1/intake/summary': () => json(200, { available: true, waiting: 0, deciding: 0, scopes: [] }),
    '/api/v1/intake/gatherings': () => json(200, { items: [], next_cursor: null }),
    '/api/v1/intake/stats': () => json(200, { days: 7, gatherings: 0, by_state: {}, decided_by: {}, fallback: 0, ai_decisions: 0, ai_corrected: 0 }),
    '/api/v1/agents': () => json(200, { items: [] }),
  });
  open(`/review?channel=${channel}`);
  await waitFor(() => expect(fetchMock.mock.calls.some(([url]) => String(url).startsWith('/api/v1/intake/gatherings?'))).toBe(true));
  const list = fetchMock.mock.calls.map(([url]) => String(url)).find((url) => url.startsWith('/api/v1/intake/gatherings?'));
  expect(list).toContain(`channel_id=${channel}`);
  expect(list).not.toContain('scope=');
});

const openTrial = { plan: 'trial', trial_ends_at: '2026-10-30T09:00:00Z', locked: false, trial_extended: false, agents_over: 0, seat_paused: false };
const narrow = (permissions: string[]) => ({ ...meBody('agent', openTrial), permissions });

// A role that reads only the knowledge base sees only what it opens: the rail offers Knowledge and Settings,
// the inbox address opens Knowledge, and no page asks the server for what the role may not read.
test('a role that reads only the knowledge base lands in Knowledge and is offered nothing else to read', async () => {
  // As in the browser, no session is known until /auth/me answers.
  forgetPermissions();
  serve({
    '/api/v1/auth/me': () => json(200, narrow(['kb_articles:read', 'kb_media:read', 'help_center:read', 'profile:update'])),
    '/api/v1/config': () => json(200, selfHostedConfig),
    '/api/v1/kb/collections': () => json(200, { items: [] }),
    '/api/v1/kb/articles': () => json(200, { items: [], next_cursor: null }),
    '/api/v1/help-center': () => json(200, { enabled: false }),
  });
  const router = open('/');
  await waitFor(() => expect(router.state.location.pathname).toBe('/knowledge'));
  const rail = screen.getByRole('navigation', { name: 'Main' });
  expect(within(rail).queryByRole('link', { name: 'Inbox' })).not.toBeInTheDocument();
  expect(within(rail).queryByRole('link', { name: 'Reports' })).not.toBeInTheDocument();
  expect(within(rail).getByRole('link', { name: 'Knowledge base' })).toBeInTheDocument();
  await act(async () => { await router.navigate({ to: '/results' }); });
  expect(await screen.findByText(/Your roles do not let you open Reports/)).toBeInTheDocument();
  const asked = fetchMock.mock.calls.map(([url]) => String(url).split('?')[0]);
  for (const path of ['/api/v1/conversations', '/api/v1/inbox/activity', '/api/v1/intake/summary', '/api/v1/results', '/api/v1/tags', '/api/v1/agents']) expect(asked, path).not.toContain(path);
});

test('a role that reads only media lands in the media library, and the rail takes it there', async () => {
  forgetPermissions();
  serve({
    '/api/v1/auth/me': () => json(200, narrow(['kb_media:read', 'profile:update'])),
    '/api/v1/config': () => json(200, selfHostedConfig),
    '/api/v1/kb/media': () => json(200, { items: [], next_cursor: null }),
    '/api/v1/kb/media-uploads': () => json(200, { items: [] }),
  });
  const router = open('/');
  await waitFor(() => expect(router.state.location.pathname).toBe('/knowledge/media'));
  const rail = screen.getByRole('navigation', { name: 'Main' });
  expect(within(rail).getByRole('link', { name: 'Knowledge base' })).toHaveAttribute('href', '/knowledge/media');
});

// The help center's settings moved from Settings to Knowledge base → Settings (#528); they still need only
// help_center:read to open, as the section of Settings did, not the articles' permission.
test('a role that runs the help center without reading articles opens its settings from Knowledge and from General', async () => {
  forgetPermissions();
  serve({
    '/api/v1/auth/me': () => json(200, narrow(['help_center:read', 'help_center:update', 'kb_media:read', 'profile:update'])),
    '/api/v1/config': () => json(200, selfHostedConfig),
    '/api/v1/kb/media': () => json(200, { items: [], next_cursor: null }),
    '/api/v1/kb/media-uploads': () => json(200, { items: [] }),
    '/api/v1/help-center': () => json(200, { enabled: false, title: '', locale: 'en', content_version: 1, updated_at: null, url: 'https://desk.example.com/help/beta-desk' }),
    '/api/v1/auth/sessions': () => json(200, { items: [] }),
  });
  const router = open('/');
  await waitFor(() => expect(router.state.location.pathname).toBe('/knowledge/media'));
  const tabs = await screen.findByRole('navigation', { name: 'Knowledge base sections' });
  fireEvent.click(within(tabs).getByRole('link', { name: 'Settings' }));
  await waitFor(() => expect(router.state.location.pathname).toBe('/knowledge/settings'));
  const card = await screen.findByRole('region', { name: 'Help center' });
  expect(await within(card).findByRole('checkbox', { name: 'Publish the help center' })).toBeEnabled();
  expect(screen.queryByText(/Your roles do not let you open/)).not.toBeInTheDocument();

  // Settings → General points there, and is in the menu for this role.
  await act(async () => { await router.navigate({ to: '/settings/account' }); });
  fireEvent.click(within(await screen.findByRole('navigation', { name: 'Settings sections' })).getByRole('link', { name: 'General' }));
  await waitFor(() => expect(router.state.location.pathname).toBe('/settings/general'));
  fireEvent.click(await screen.findByRole('link', { name: 'Open Help center settings' }));
  await waitFor(() => expect(router.state.location.pathname).toBe('/knowledge/settings'));
  expect(await screen.findByRole('region', { name: 'Help center' })).toBeInTheDocument();
});

// A role that reads only the help center still reaches its settings from the rail.
test('a role that reads only the help center finds Knowledge on the rail, at its settings', async () => {
  forgetPermissions();
  serve({
    '/api/v1/auth/me': () => json(200, narrow(['help_center:read', 'profile:update'])),
    '/api/v1/config': () => json(200, selfHostedConfig),
    '/api/v1/help-center': () => json(200, { enabled: false, title: '', locale: 'en', content_version: 1, updated_at: null, url: 'https://desk.example.com/help/beta-desk' }),
  });
  const router = open('/');
  await waitFor(() => expect(router.state.location.pathname).toBe('/knowledge/settings'));
  expect(within(screen.getByRole('navigation', { name: 'Main' })).getByRole('link', { name: 'Knowledge base' })).toHaveAttribute('href', '/knowledge/settings');
  expect(await screen.findByRole('region', { name: 'Help center' })).toBeInTheDocument();
});

// Revoking a teammate's agent token needs team:revoke alone; the tokens and their sessions are on AI & agents.
test('a person who may revoke but not create tokens finds the teammates\' agent tokens in AI & agents', async () => {
  forgetPermissions();
  const token = { id: '77777777-7777-4777-8777-777777777777', agent_id: '33333333-3333-4333-8333-333333333333', agent_email: 'ann@example.com', agent_name: 'Ann', label: 'Claude Code',
    token: { kind: 'custom', role: null, role_held: true, permissions: ['conversations:read'], legacy_tier: null }, role: null, permissions: ['conversations:read'], current: false,
    created_at: '2026-10-23T00:00:00Z', last_seen_at: '2026-10-23T00:00:00Z', expires_at: '2099-01-01T00:00:00Z', revoked_at: null };
  serve({
    '/api/v1/auth/me': () => json(200, narrow(['team:read', 'team:revoke', 'profile:update'])),
    '/api/v1/config': () => json(200, selfHostedConfig),
    '/api/v1/auth/sessions': () => json(200, { items: [token] }),
  });
  const router = open('/settings/account');
  fireEvent.click(within(await screen.findByRole('navigation', { name: 'Settings sections' })).getByRole('link', { name: 'AI & agents' }));
  await waitFor(() => expect(router.state.location.pathname).toBe('/settings/ai'));
  // The list is there without Create token: this person may revoke, not create.
  const list = await screen.findByRole('table', { name: 'Agent tokens' });
  expect(await within(list).findByRole('row', { name: /Claude Code/ })).toBeInTheDocument();
  expect(within(list).getByRole('button', { name: 'Actions for Claude Code' })).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Create token' })).toBeNull();
});

// The pages scroll inside the app's content pane on a wide screen, not the window.
test('a settings page opened from the menu starts at its top, and Back returns to where the last one was scrolled', async () => {
  settingsServer();
  const router = open('/settings/account');
  const menu = await screen.findByRole('navigation', { name: 'Settings sections' });
  const content = document.querySelector('.app-content') as HTMLElement;
  content.scrollTop = 300;
  fireEvent.click(within(menu).getByRole('link', { name: 'General' }));
  await screen.findByRole('heading', { level: 1, name: 'General' });
  await waitFor(() => expect(router.state.location.pathname).toBe('/settings/general'));
  await waitFor(() => expect(content.scrollTop).toBe(0));
  await act(() => { router.history.back(); });
  await screen.findByRole('heading', { level: 1, name: 'Account' });
  await waitFor(() => expect(content.scrollTop).toBe(300));
});

// Each page draws its own menu: the item pressed keeps keyboard focus on the page it opened.
test('a page opened from the settings menu keeps keyboard focus on its menu item', async () => {
  settingsServer();
  open('/settings/account');
  const general = within(await screen.findByRole('navigation', { name: 'Settings sections' })).getByRole('link', { name: 'General' });
  general.focus();
  fireEvent.click(general);
  await screen.findByRole('heading', { level: 1, name: 'General' });
  await waitFor(() => expect(document.activeElement).toHaveAttribute('aria-current', 'page'));
  expect(document.activeElement).toHaveTextContent('General');
  expect(document.activeElement?.closest('nav')).toHaveAccessibleName('Settings sections');
});

// On a phone /settings is the menu alone; turned on its side the screen is wide, where Settings opens Account.
test('the settings menu of a phone turned on its side opens Account', async () => {
  const listeners = new Set<() => void>();
  const query = { matches: true, addEventListener: (_: string, fn: () => void) => listeners.add(fn), removeEventListener: (_: string, fn: () => void) => listeners.delete(fn) };
  vi.stubGlobal('matchMedia', (text: string) => (text === '(max-width: 44rem)' ? query : { matches: false, addEventListener: () => {}, removeEventListener: () => {} }));
  try {
    settingsServer();
    const router = open('/settings');
    expect(await screen.findByRole('heading', { level: 1, name: 'Settings' })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe('/settings');
    query.matches = false;
    act(() => { for (const fn of listeners) fn(); });
    await waitFor(() => expect(router.state.location.pathname).toBe('/settings/account'));
    expect(await screen.findByRole('heading', { level: 1, name: 'Account' })).toBeInTheDocument();
  } finally {
    vi.unstubAllGlobals();
    vi.stubGlobal('fetch', fetchMock);
  }
});

const savedView = { id: '33333333-3333-4333-8333-333333333333', name: 'Mine, open', scope: 'personal', filters: { status: ['open'], assignee: ['me'] },
  owner_agent_id: '11111111-1111-4111-8111-111111111111', created_at: '2026-11-01T00:00:00Z', updated_at: '2026-11-01T00:00:00Z', deleted_at: null, purges_at: null };
const channelRow = (id: string, name: string, type: string, state: string) => ({ id, type, name, enabled: true, status: { state, reason_code: null, message: null }, summary: '',
  capabilities: [], held_count: 0, unsent_count: 0, last_inbound_at: null, created_at: '2026-11-01T00:00:00Z', updated_at: '2026-11-01T00:00:00Z' });

test('on every page the sidebar counts the open conversations and lists the saved views and the channels, each opening its inbox list', async () => {
  serve({
    '/api/v1/auth/me': () => json(200, meBody('admin', null)),
    '/api/v1/config': () => json(200, config),
    '/api/v1/kb/collections': () => json(200, { items: [] }),
    '/api/v1/kb/articles': () => json(200, { items: [], next_cursor: null }),
    '/api/v1/conversations': () => json(200, { items: [], page: 1, page_size: 50, status_counts: { open: 23, pending: 4, resolved: 9, closed: 0, spam: 0 } }),
    '/api/v1/saved-views': () => json(200, { items: [savedView] }),
    '/api/v1/channels': () => json(200, { items: [channelRow('44444444-4444-4444-8444-444444444444', 'support@northwind.help', 'email', 'working'),
      channelRow('55555555-5555-4555-8555-555555555555', 'Chat widget', 'help_chat', 'needs_attention')], types: [{ type: 'email', available: true, count: 1, default_channel_id: null }] }),
  });
  const router = open('/knowledge');
  const rail = await screen.findByRole('navigation', { name: 'Main' });
  expect(await within(rail).findByRole('link', { name: 'Inbox, 23 open' })).toHaveAttribute('href', '/');
  expect(within(rail).getByRole('link', { name: 'Reports' })).toHaveAttribute('href', '/results');
  expect(within(rail).getByRole('link', { name: 'Invite teammates' })).toHaveAttribute('href', '/settings/members');
  // The open view is in the address, so Back and a shared link open the same list.
  fireEvent.click(await within(rail).findByRole('link', { name: 'Mine, open' }));
  await waitFor(() => expect(router.state.location.pathname).toBe('/'));
  expect(router.state.location.search).toEqual({ status: 'open', assignee: 'me' });
  expect(within(rail).getByRole('link', { name: 'Mine, open' })).toHaveAttribute('aria-current', 'page');
  fireEvent.click(within(rail).getByRole('link', { name: 'Chat widget, needs attention' }));
  await waitFor(() => expect(router.state.location.search).toEqual({ channel_id: '55555555-5555-4555-8555-555555555555' }));
  expect(within(rail).getByRole('link', { name: 'support@northwind.help' })).not.toHaveAttribute('aria-current');
});

test('the top bar names the page the person is on', async () => {
  serve({
    '/api/v1/auth/me': () => json(200, meBody('agent', null)),
    '/api/v1/config': () => json(200, config),
    '/api/v1/kb/collections': () => json(200, { items: [] }),
    '/api/v1/kb/articles': () => json(200, { items: [], next_cursor: null }),
  });
  open('/knowledge');
  expect(await screen.findByRole('heading', { level: 1, name: 'Knowledge base' })).toBeInTheDocument();
  // The page has a header of its own; the top bar is the app's, above it.
  await waitFor(() => expect(document.querySelector('.app-topbar .topbar-title')).toHaveTextContent('Knowledge base'));
});
