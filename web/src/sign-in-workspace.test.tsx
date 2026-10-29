import { QueryClientProvider } from '@tanstack/react-query';
import { RouterProvider, createMemoryHistory, createRouter } from '@tanstack/react-router';
import { render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeAll, expect, test, vi } from 'vitest';
import { routeTree } from './app';
import { createQueryClient } from './query-client';
import { presenceViewPart } from './features/inbox/presence-part';
import { autofixPart } from './features/autofix/autofix-part';

// The root of a workspace's own host name leads to /sign-in?workspace=<slug>. Clerk is the identity
// provider, the external boundary: its session state and token are faked here.
const clerk = vi.hoisted(() => ({ signedIn: false }));
vi.mock('@clerk/clerk-react', () => ({
  ClerkProvider: ({ children }: { children: React.ReactNode }) => children,
  SignIn: () => <div>Clerk widget</div>,
  useAuth: () => ({ getToken: () => Promise.resolve('clerk-session-token'), isLoaded: true, isSignedIn: clerk.signedIn }),
  useClerk: () => ({ signOut: vi.fn() }),
}));

const fetchMock = vi.fn();
vi.stubGlobal('fetch', fetchMock);
afterEach(() => { fetchMock.mockReset(); clerk.signedIn = false; });

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
const config = { cloud_sign_in: { enabled: true, publishable_key: 'pk' }, billing: { enabled: false }, signup: { enabled: false },
  limits: { outgoing_attachments_bytes: 10485760, outgoing_attachments_files: 10 }, product_analytics: { enabled: false, key: null, host: null, realm: null }, ai: { server_provider: false } };

// signedIn is whether the browser holds a Colliedesk session (for Beta Desk) before the exchange.
function serve(signedIn: boolean) {
  fetchMock.mockImplementation((url: string, init?: RequestInit) => {
    const path = url.split('?')[0];
    if (path === '/api/v1/auth/cloud/exchange' && init?.method === 'POST') { signedIn = true; return Promise.resolve(json(200, { agent: me.agent, workspace: me.workspace })); }
    if (path === '/api/v1/auth/me') return Promise.resolve(signedIn ? json(200, me) : json(401, { error: { code: 'unauthorized', message: 'Sign in.' } }));
    if (path === '/api/v1/config') return Promise.resolve(json(200, config));
    if (path === '/api/v1/inbox/activity') return Promise.resolve(json(200, { now: '2026-10-15T10:00:00Z', new_conversations: [], assigned_to_me: [], updated: [] }));
    return Promise.resolve(json(200, { items: [], page: 1, page_size: 50 }));
  });
}

function open(path: string) {
  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: [path] }) });
  render(<QueryClientProvider client={createQueryClient()}><RouterProvider router={router} /></QueryClientProvider>);
  return router;
}

const exchanges = () => fetchMock.mock.calls.filter(([url]) => String(url) === '/api/v1/auth/cloud/exchange');

test('a sign-in from a workspace host signs in to that workspace without asking which one', async () => {
  clerk.signedIn = true;
  serve(false);
  const router = open('/sign-in?workspace=beta-desk');
  expect(await screen.findByRole('region', { name: 'Signed in as' })).toHaveTextContent('Beta Desk');
  expect(router.state.location.pathname).toBe('/');
  expect(exchanges()).toHaveLength(1);
  expect(JSON.parse(String(exchanges()[0][1].body))).toMatchObject({ workspace_slug: 'beta-desk' });
});

test('a browser already signed in to the workspace of the host lands in its inbox', async () => {
  serve(true);
  const router = open('/sign-in?workspace=beta-desk');
  expect(await screen.findByRole('region', { name: 'Signed in as' })).toHaveTextContent('Beta Desk');
  expect(router.state.location.pathname).toBe('/');
  expect(exchanges()).toHaveLength(0);
});

test('a browser signed in to another workspace stays in it, named on screen, and is never switched', async () => {
  clerk.signedIn = true;
  serve(true);
  const router = open('/sign-in?workspace=acme');
  expect(await screen.findByRole('region', { name: 'Signed in as' })).toHaveTextContent('Beta Desk');
  expect(router.state.location.pathname).toBe('/');
  await waitFor(() => expect(fetchMock.mock.calls.some(([url]) => String(url).startsWith('/api/v1/inbox/activity'))).toBe(true));
  expect(exchanges()).toHaveLength(0);
});

test('a workspace hint that cannot be a slug is ignored', async () => {
  clerk.signedIn = true;
  serve(false);
  open('/sign-in?workspace=Not%20a%20slug');
  expect(await screen.findByRole('region', { name: 'Signed in as' })).toHaveTextContent('Beta Desk');
  expect(exchanges().length).toBeGreaterThan(0);
  expect(JSON.parse(String(exchanges()[0][1].body))).not.toHaveProperty('workspace_slug');
});
