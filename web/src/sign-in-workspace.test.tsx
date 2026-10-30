import { QueryClientProvider } from '@tanstack/react-query';
import { RouterProvider, createMemoryHistory, createRouter } from '@tanstack/react-router';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeAll, expect, test, vi } from 'vitest';
import { routeTree } from './app';
import { createQueryClient } from './query-client';
import { presenceViewPart } from './features/inbox/presence-part';
import { autofixPart } from './features/autofix/autofix-part';
import { leaveTo } from './lib/leave-to';
import { readPlanIntent } from './features/settings/plan-intent';

// Each workspace's desk runs on its own host; APP_ORIGIN finds workspaces and signs new teams up. Clerk
// is the identity provider and leaving the page is the browser's: both are external boundaries here.
const clerk = vi.hoisted(() => ({ signedIn: false }));
vi.mock('@clerk/clerk-react', () => ({
  ClerkProvider: ({ children }: { children: React.ReactNode }) => children,
  SignIn: () => <div>Clerk widget</div>,
  useAuth: () => ({ getToken: () => Promise.resolve('clerk-session-token'), isLoaded: true, isSignedIn: clerk.signedIn }),
  useClerk: () => ({ signOut: vi.fn() }),
}));
vi.mock('./lib/leave-to', () => ({ leaveTo: vi.fn() }));

const fetchMock = vi.fn();
vi.stubGlobal('fetch', fetchMock);
afterEach(() => { fetchMock.mockReset(); vi.mocked(leaveTo).mockReset(); clerk.signedIn = false; window.sessionStorage.clear(); });

beforeAll(async () => {
  presenceViewPart.preload();
  await import('./features/inbox/presence-view');
  autofixPart.preload();
  await import('./features/autofix/autofix-chunk');
});

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status });
const me = {
  agent: { id: '11111111-1111-4111-8111-111111111111', email: 'sam@example.com', display_name: '', role: 'agent', timezone_override: null },
  workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'beta-desk', name: 'Beta Desk', timezone: 'UTC' },
  timezone: 'UTC', scope: null, plan: null,
};
const handoffURL = 'https://beta-desk.example.com/sign-in?next=%2Fsettings#handoff=one-time';
// appOrigin is where this page runs (jsdom's origin) on APP_ORIGIN, or another origin on a workspace host.
function config(workspaceHosts: boolean, appOrigin = window.location.origin) {
  return { app_origin: appOrigin, workspace_hosts: workspaceHosts, cloud_sign_in: { enabled: true, publishable_key: 'pk' }, billing: { enabled: true }, signup: { enabled: true },
    limits: { outgoing_attachments_bytes: 10485760, outgoing_attachments_files: 10 }, product_analytics: { enabled: false, key: null, host: null, realm: null }, ai: { server_provider: false } };
}

function serve(options: { signedIn: boolean; config: ReturnType<typeof config>; exchange?: Response }) {
  // On APP_ORIGIN of a server with workspace hosts the sign-in answers where the browser continues.
  const continueURL = options.config.workspace_hosts && options.config.app_origin === window.location.origin ? { continue_url: handoffURL } : {};
  let signedIn = options.signedIn;
  fetchMock.mockImplementation((url: string) => {
    const path = url.split('?')[0];
    if (path === '/api/v1/auth/cloud/exchange') {
      const answer = options.exchange ?? json(200, { agent: me.agent, workspace: me.workspace, ...continueURL });
      if (answer.ok) signedIn = true;
      return Promise.resolve(answer);
    }
    if (path === '/api/v1/auth/redeem') { signedIn = true; return Promise.resolve(json(200, { agent: me.agent, workspace: me.workspace })); }
    if (path === '/api/v1/auth/me') return Promise.resolve(signedIn ? json(200, me) : json(401, { error: { code: 'unauthorized', message: 'Sign in.' } }));
    if (path === '/api/v1/config') return Promise.resolve(json(200, options.config));
    if (path === '/api/v1/inbox/activity') return Promise.resolve(json(200, { now: '2026-10-15T10:00:00Z', new_conversations: [], assigned_to_me: [], updated: [] }));
    return Promise.resolve(json(200, { items: [], page: 1, page_size: 50 }));
  });
}

function open(path: string) {
  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: [path] }) });
  render(<QueryClientProvider client={createQueryClient()}><RouterProvider router={router} /></QueryClientProvider>);
  return router;
}

const calls = (path: string) => fetchMock.mock.calls.filter(([url]) => String(url) === path);

test('on a workspace host, the hand-off in the address signs in once and opens the page it names', async () => {
  serve({ signedIn: false, config: config(true, 'https://app.example.com') });
  const router = open('/sign-in?next=%2Fsettings#handoff=one-time');
  await waitFor(() => expect(router.state.location.pathname).toBe('/settings'));
  expect(calls('/api/v1/auth/redeem')).toHaveLength(1);
  expect(JSON.parse(String(calls('/api/v1/auth/redeem')[0][1].body))).toMatchObject({ token: 'one-time', session_type: 'cookie' });
  expect(await screen.findByRole('region', { name: 'Signed in as' })).toHaveTextContent('Beta Desk');
});

test('a hand-off naming another host opens the inbox instead', async () => {
  serve({ signedIn: false, config: config(true, 'https://app.example.com') });
  const router = open('/sign-in?next=%2F%2Fevil.example#handoff=one-time');
  expect(await screen.findByRole('region', { name: 'Signed in as' })).toHaveTextContent('Beta Desk');
  expect(router.state.location.pathname).toBe('/');
});

test('after signing in on APP_ORIGIN the browser continues on its workspace host', async () => {
  clerk.signedIn = true;
  serve({ signedIn: false, config: config(true) });
  open('/sign-in');
  await waitFor(() => expect(leaveTo).toHaveBeenCalledWith(handoffURL));
  expect(screen.queryByRole('region', { name: 'Signed in as' })).not.toBeInTheDocument();
});

test('a buyer signing in on APP_ORIGIN takes the plan to the workspace host in the address', async () => {
  clerk.signedIn = true;
  serve({ signedIn: false, config: config(true) });
  open('/sign-in?buy=team_monthly');
  await waitFor(() => expect(leaveTo).toHaveBeenCalledWith(handoffURL));
  expect(JSON.parse(String(calls('/api/v1/auth/cloud/exchange')[0][1].body))).toMatchObject({ next: '/settings/billing?buy=team_monthly' });
});

test('a visitor from the Team 25 trial link takes the plan to the workspace host in the address', async () => {
  clerk.signedIn = true;
  serve({ signedIn: false, config: config(true) });
  open('/sign-in?intent=trial&plan=team25');
  await waitFor(() => expect(leaveTo).toHaveBeenCalledWith(handoffURL));
  expect(JSON.parse(String(calls('/api/v1/auth/cloud/exchange')[0][1].body))).toMatchObject({ next: '/?plan=team25' });
  expect(readPlanIntent()).toBe('team25');
});

test('a plan other than team25 in the trial link is ignored', async () => {
  clerk.signedIn = true;
  serve({ signedIn: false, config: config(true) });
  open('/sign-in?intent=trial&plan=solo');
  await waitFor(() => expect(leaveTo).toHaveBeenCalledWith(handoffURL));
  expect(JSON.parse(String(calls('/api/v1/auth/cloud/exchange')[0][1].body)).next).toBeUndefined();
  expect(readPlanIntent()).toBeUndefined();
});

test('a buyer of Founding keeps the checkout page in the address, not the Team 25 plan', async () => {
  clerk.signedIn = true;
  serve({ signedIn: false, config: config(true) });
  open('/sign-in?buy=founding&plan=team25');
  await waitFor(() => expect(leaveTo).toHaveBeenCalledWith(handoffURL));
  expect(JSON.parse(String(calls('/api/v1/auth/cloud/exchange')[0][1].body))).toMatchObject({ next: '/settings/billing?buy=founding' });
});

test('on a workspace host, a hand-off that names Team 25 remembers it and opens the inbox without it in the address', async () => {
  serve({ signedIn: false, config: config(true, 'https://app.example.com') });
  const router = open('/sign-in?next=%2F%3Fplan%3Dteam25#handoff=one-time');
  expect(await screen.findByRole('region', { name: 'Signed in as' })).toHaveTextContent('Beta Desk');
  expect(router.state.location.pathname).toBe('/');
  expect(router.state.location.search).not.toHaveProperty('plan');
  expect(readPlanIntent()).toBe('team25');
  expect(calls('/api/v1/billing')).toHaveLength(0);
});

test('on a workspace host, a hand-off naming any other plan remembers nothing', async () => {
  serve({ signedIn: false, config: config(true, 'https://app.example.com') });
  open('/sign-in?next=%2F%3Fplan%3Dsolo#handoff=one-time');
  expect(await screen.findByRole('region', { name: 'Signed in as' })).toHaveTextContent('Beta Desk');
  expect(readPlanIntent()).toBeUndefined();
});

test('a pairing link with the Team 25 plan signs in to the inbox and remembers the plan', async () => {
  serve({ signedIn: false, config: config(false) });
  const router = open('/sign-in?token=pairing-token&plan=team25');
  fireEvent.click(await screen.findByRole('button', { name: /sign in|continue|open/i }));
  expect(await screen.findByRole('region', { name: 'Signed in as' })).toHaveTextContent('Beta Desk');
  expect(router.state.location.pathname).toBe('/');
  expect(JSON.parse(String(calls('/api/v1/auth/redeem')[0][1].body))).toMatchObject({ token: 'pairing-token', next: '/?plan=team25' });
  expect(readPlanIntent()).toBe('team25');
});

test('on a workspace host, an email that is not an agent there is told so and pointed to find its workspace', async () => {
  clerk.signedIn = true;
  serve({ signedIn: false, config: config(true, 'https://app.example.com'), exchange: json(403, { error: { code: 'forbidden', message: 'no' } }) });
  open('/sign-in');
  const finder = await screen.findByRole('link', { name: /find your workspace/i });
  expect(finder).toHaveAttribute('href', 'https://app.example.com/sign-in');
  expect(screen.queryByRole('textbox', { name: /workspace name|team name/i })).not.toBeInTheDocument();
});

test('a server without workspace hosts keeps the browser where it is', async () => {
  clerk.signedIn = true;
  serve({ signedIn: false, config: config(false) });
  open('/sign-in');
  expect(await screen.findByRole('region', { name: 'Signed in as' })).toHaveTextContent('Beta Desk');
  expect(leaveTo).not.toHaveBeenCalled();
});
