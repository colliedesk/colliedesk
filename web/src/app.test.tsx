import { QueryClientProvider } from '@tanstack/react-query';
import { RouterProvider, createMemoryHistory, createRouter } from '@tanstack/react-router';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeAll, expect, test, vi } from 'vitest';
import { routeTree } from './app';
import { readCheckoutIntent, rememberCheckoutIntent } from './features/settings/checkout-intent';
import { readPlanIntent, rememberPlanIntent } from './features/settings/plan-intent';
import { createQueryClient } from './query-client';
import { resetProductAnalytics } from './lib/product-analytics';
import { presenceViewPart } from './features/inbox/presence-part';
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
  agent: { id: '11111111-1111-4111-8111-111111111111', email: 'sam@example.com', display_name: '', role, timezone_override: null },
  workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'beta-desk', name: 'Beta Desk', timezone: 'UTC' },
  timezone: 'UTC', scope: null, plan,
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
  expect(within(sections).getByRole('link', { name: 'Team' })).toHaveAttribute('href', '/settings#team-heading');
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

test('the rail opens the knowledge base, which marks Knowledge as the current destination', async () => {
  serve({
    '/api/v1/auth/me': () => json(200, meBody('agent', null)),
    '/api/v1/config': () => json(200, config),
    '/api/v1/kb/collections': () => json(200, { items: [] }),
    '/api/v1/kb/articles': () => json(200, { items: [], next_cursor: null }),
  });
  open('/knowledge');
  expect(await screen.findByRole('heading', { level: 1, name: 'Knowledge base' })).toBeInTheDocument();
  const rail = screen.getByRole('navigation', { name: 'Main' });
  expect(within(rail).getByRole('link', { name: 'Knowledge' })).toHaveAttribute('aria-current', 'page');
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

test('Settings shows its title at once while its own chunk loads, then the sections', async () => {
  serve({
    '/api/v1/auth/me': () => json(200, meBody('agent', trialPlan)),
    '/api/v1/config': () => json(200, config),
    '/api/v1/inbox/activity': () => json(200, { now: '2026-10-15T10:00:00Z', new_conversations: [], assigned_to_me: [], updated: [] }),
  });
  open('/settings');
  const title = await screen.findByRole('heading', { level: 1, name: 'Settings' });
  expect(title).toBeInTheDocument();
  // The sections arrive with the chunk; the title stays where it was.
  await waitFor(() => expect(document.querySelector('.settings-page[aria-busy="true"]')).toBeNull());
  expect(screen.getByRole('heading', { level: 1, name: 'Settings' })).toBeInTheDocument();
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
  fireEvent.click(screen.getByRole('button', { name: 'Account' }));
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
