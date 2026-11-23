import { QueryClientProvider } from '@tanstack/react-query';
import { RouterProvider, createMemoryHistory, createRouter } from '@tanstack/react-router';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { routeTree } from './app';
import { createQueryClient } from './query-client';
import { leaveTo } from './lib/leave-to';
import { forgetSignOut } from './lib/sign-out-intent';
import { permissionsOf, rolesOf } from './test-roles';

// A person signed in to one workspace switches to another of theirs from the account menu. Clerk (the identity
// provider) and leaving the page are external boundaries here.
// Signing out of Clerk ends its session, as the real one does.
const clerk = vi.hoisted(() => {
  const state = { signedIn: true, signOut: vi.fn(() => { state.signedIn = false; return Promise.resolve(); }) };
  return state;
});
vi.mock('@clerk/clerk-react', () => ({
  ClerkProvider: ({ children }: { children: React.ReactNode }) => children,
  SignIn: () => <div>Clerk widget</div>,
  useAuth: () => ({ getToken: () => Promise.resolve('clerk-session-token'), isLoaded: true, isSignedIn: clerk.signedIn }),
  useClerk: () => ({ signOut: clerk.signOut }),
}));
vi.mock('./lib/leave-to', () => ({ leaveTo: vi.fn() }));

const fetchMock = vi.fn();
vi.stubGlobal('fetch', fetchMock);
// A Sign out is remembered in the page until a session answers; each test starts as a fresh page.
afterEach(() => { fetchMock.mockReset(); vi.mocked(leaveTo).mockReset(); clerk.signedIn = true; clerk.signOut.mockClear(); forgetSignOut(); });

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status });
const me = {
  agent: { id: '11111111-1111-4111-8111-111111111111', email: 'sam@example.com', display_name: 'Sam', roles: rolesOf('agent'), timezone_override: null },
  workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'beta-desk', name: 'Beta Desk', timezone: 'UTC' },
  timezone: 'UTC', token: null, permissions: permissionsOf('agent'), plan: null,
};
const appOrigin = 'https://app.example.com';
const continueURL = 'https://alpha-desk.example.com/sign-in#handoff=one-time';
// onAppHost: the page runs on APP_ORIGIN (jsdom's origin); otherwise on a workspace host.
function config(onAppHost: boolean) {
  return { app_origin: onAppHost ? window.location.origin : appOrigin, workspace_hosts: true, cloud_sign_in: { enabled: true, publishable_key: 'pk' }, billing: { enabled: false },
    signup: { enabled: !onAppHost }, limits: { outgoing_attachments_bytes: 10485760, outgoing_attachments_files: 10 }, product_analytics: { enabled: false, key: null, host: null, realm: null },
    ai: { server_provider: false } };
}
const alpha = { slug: 'alpha-desk', name: 'Alpha Desk', host: 'alpha-desk.example.com', roles: ['Admin'] };
const beta = { slug: 'beta-desk', name: 'Beta Desk', host: 'beta-desk.example.com', roles: ['Agent'] };
const zeta = { slug: 'a-zeta', name: 'Zeta Desk', host: 'a-zeta.example.com', roles: ['Agent'] };
type Choice = typeof alpha;

const thisDevice = { id: '44444444-4444-4444-8444-444444444444', agent_id: me.agent.id, agent_email: me.agent.email, agent_name: 'Sam', label: 'Cloud sign-in', current: true,
  token: null, role: null, permissions: [],
  created_at: '2026-11-13T00:00:00Z', last_seen_at: '2026-11-13T00:00:00Z', expires_at: '2099-01-01T00:00:00Z', revoked_at: null };

// ended is why a missing session answers 401: revoked by someone, or expired (plain unauthorized).
// choose makes an exchange without a workspace answer 409 with the chooser.
function serve(options: { signedIn: boolean; onAppHost: boolean; workspaces?: Choice[]; listStatus?: number; choose?: boolean; ended?: 'session_revoked' | 'unauthorized' }) {
  let signedIn = options.signedIn;
  let ended = options.ended ?? 'unauthorized';
  fetchMock.mockImplementation((url: string, init?: RequestInit) => {
    const path = url.split('?')[0];
    if (path === '/api/v1/config') return Promise.resolve(json(200, config(options.onAppHost)));
    if (path === '/api/v1/auth/me') {
      if (signedIn) return Promise.resolve(json(200, me));
      // The server clears the cookie with its first refusal: later calls are plain unauthorized.
      const code = ended;
      ended = 'unauthorized';
      return Promise.resolve(json(401, { error: { code, message: 'Sign in.' } }));
    }
    if (path === '/api/v1/auth/cloud/workspaces') {
      if (options.listStatus) return Promise.resolve(json(options.listStatus, { error: { code: 'unauthorized', message: 'A valid cloud session is required.' } }));
      return Promise.resolve(json(200, { workspaces: options.workspaces ?? [alpha, beta] }));
    }
    if (path === '/api/v1/auth/cloud/exchange' && options.choose && !String(init?.body ?? '').includes('workspace_slug')) {
      return Promise.resolve(json(409, { email: 'sam@example.com', workspaces: [beta, alpha] }));
    }
    if (path === '/api/v1/auth/cloud/exchange') return Promise.resolve(json(200, { agent: me.agent, workspace: { ...me.workspace, slug: 'alpha-desk', name: 'Alpha Desk' }, continue_url: continueURL }));
    if (path === '/api/v1/auth/logout') { signedIn = false; return Promise.resolve(new Response(null, { status: 204 })); }
    if (path === '/api/v1/auth/sessions') return Promise.resolve(json(200, { items: [thisDevice] }));
    if (path === `/api/v1/auth/sessions/${thisDevice.id}`) { signedIn = false; return Promise.resolve(new Response(null, { status: 204 })); }
    if (path === '/api/v1/inbox/activity') return Promise.resolve(json(200, { now: '2026-11-13T10:00:00Z', new_conversations: [], assigned_to_me: [], updated: [] }));
    return Promise.resolve(json(200, { items: [], page: 1, page_size: 50 }));
  });
}

function open(path: string) {
  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: [path] }) });
  render(<QueryClientProvider client={createQueryClient()}><RouterProvider router={router} /></QueryClientProvider>);
  return router;
}

const calls = (path: string) => fetchMock.mock.calls.filter(([url]) => String(url) === path);
const openAccountMenu = async () => fireEvent.click(await screen.findByRole('button', { name: /^Account/ }));

const openSwitch = async () => {
  await openAccountMenu();
  fireEvent.click(await screen.findByRole('button', { name: 'Switch workspace…' }));
  return screen.findByRole('dialog', { name: 'Switch workspace' });
};
// rowNames lists the dialog's workspace rows top to bottom by their names.
const rowNames = (dialog: HTMLElement) => Array.from(dialog.querySelectorAll('.workspace-row-name')).map((name) => name.firstChild?.textContent);

test('the account menu offers Switch workspace before any list is read, also to a person with one workspace', async () => {
  serve({ signedIn: true, onAppHost: false, workspaces: [beta] });
  open('/');
  await openAccountMenu();
  const menu = screen.getByRole('group', { name: 'Account' });
  // Name and role and address first, then the switch, Report a problem and Sign out.
  expect(within(menu).getByText('Agent · sam@example.com')).toBeInTheDocument();
  expect(within(menu).getAllByRole('button').map((button) => button.textContent)).toEqual(['Switch workspace…', 'Report a problem', 'Sign out']);
  // Nothing is read for the switch until the person asks for it.
  expect(calls('/api/v1/auth/cloud/workspaces')).toHaveLength(0);
});

test('Switch workspace opens a dialog that shows the current workspace at once and loads the others', async () => {
  serve({ signedIn: true, onAppHost: false, workspaces: [zeta, beta, alpha] });
  open('/');
  const dialog = await openSwitch();
  expect(within(dialog).getByText('Signed in as sam@example.com')).toBeInTheDocument();
  // Loading: the current workspace and two placeholder rows, never a blank dialog.
  expect(within(dialog).getByRole('status', { name: 'Loading your workspaces' })).toBeInTheDocument();
  expect(rowNames(dialog)).toEqual(['Beta Desk']);
  await waitFor(() => expect(rowNames(dialog)).toEqual(['Beta Desk', 'Alpha Desk', 'Zeta Desk']));
  // The current workspace is marked and opens nothing; the others link to the existing hand-off.
  expect(within(dialog).getByText('· Current')).toBeInTheDocument();
  expect(within(dialog).queryByRole('link', { name: /Beta Desk/ })).not.toBeInTheDocument();
  const link = within(dialog).getByRole('link', { name: 'Alpha Desk' });
  expect(link).toHaveAttribute('href', `${appOrigin}/sign-in?workspace=alpha-desk`);
  expect(link).toHaveAccessibleDescription('alpha-desk.example.com, Admin');
  expect(within(dialog).getByRole('link', { name: 'Create a workspace' })).toHaveAttribute('href', `${appOrigin}/sign-in?intent=trial`);
  // The list is asked with the identity provider's token, never with the workspace's cookie.
  const [, init] = calls('/api/v1/auth/cloud/workspaces')[0];
  expect(new Headers(init.headers).get('Authorization')).toBe('Bearer clerk-session-token');
  expect(init.credentials).toBe('omit');
});

test('a person with one workspace is told it is their only one', async () => {
  serve({ signedIn: true, onAppHost: false, workspaces: [beta] });
  open('/');
  const dialog = await openSwitch();
  expect(await within(dialog).findByText('This is your only workspace')).toBeInTheDocument();
  expect(within(dialog).queryByRole('link', { name: /Desk/ })).not.toBeInTheDocument();
});

test('without an identity provider session the dialog says the list could not be read and offers the chooser on APP_ORIGIN', async () => {
  const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
  clerk.signedIn = false;
  serve({ signedIn: true, onAppHost: false });
  open('/');
  const dialog = await openSwitch();
  expect(await within(dialog).findByText('Could not load your other workspaces')).toBeInTheDocument();
  expect(within(dialog).getByRole('link', { name: 'Choose on app.example.com' })).toHaveAttribute('href', `${appOrigin}/sign-in`);
  expect(warn).toHaveBeenCalledWith('Switch workspace: could not load the workspaces', 'not-signed-in');
  expect(calls('/api/v1/auth/cloud/workspaces')).toHaveLength(0);
  // Try again reads the list once the sign-in is there.
  clerk.signedIn = true;
  fireEvent.click(within(dialog).getByRole('button', { name: 'Try again' }));
  await waitFor(() => expect(rowNames(dialog)).toEqual(['Beta Desk', 'Alpha Desk']));
  warn.mockRestore();
});

test('a list the server refuses is an error the person sees, never an empty list', async () => {
  const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
  serve({ signedIn: true, onAppHost: false, listStatus: 401 });
  open('/');
  const dialog = await openSwitch();
  expect(await within(dialog).findByText('Could not load your other workspaces')).toBeInTheDocument();
  expect(within(dialog).queryByText('This is your only workspace')).not.toBeInTheDocument();
  expect(warn).toHaveBeenCalledWith('Switch workspace: could not load the workspaces', 'request-failed', expect.any(Error));
  warn.mockRestore();
});

test('arrow keys move between the workspaces and Escape closes the dialog back to the account button', async () => {
  serve({ signedIn: true, onAppHost: false, workspaces: [zeta, beta, alpha] });
  open('/');
  const dialog = await openSwitch();
  await waitFor(() => expect(rowNames(dialog)).toHaveLength(3));
  fireEvent.keyDown(dialog, { key: 'ArrowDown' });
  expect(within(dialog).getByRole('link', { name: 'Alpha Desk' })).toHaveFocus();
  fireEvent.keyDown(dialog, { key: 'ArrowDown' });
  expect(within(dialog).getByRole('link', { name: 'Zeta Desk' })).toHaveFocus();
  fireEvent.keyDown(dialog, { key: 'ArrowUp' });
  expect(within(dialog).getByRole('link', { name: 'Alpha Desk' })).toHaveFocus();
  fireEvent.keyDown(dialog, { key: 'Escape' });
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: /^Account/ })).toHaveFocus();
});

test('the switch link signs in to the chosen workspace on APP_ORIGIN without asking again', async () => {
  serve({ signedIn: false, onAppHost: true });
  open('/sign-in?workspace=alpha-desk');
  await waitFor(() => expect(leaveTo).toHaveBeenCalledWith(continueURL));
  expect(JSON.parse(String(calls('/api/v1/auth/cloud/exchange')[0][1].body))).toMatchObject({ workspace_slug: 'alpha-desk' });
  expect(clerk.signOut).not.toHaveBeenCalled();
  expect(screen.queryByText('Choose a workspace')).not.toBeInTheDocument();
});

test('the sign-in chooser opens the chosen workspace from the live sign-in, or signs out for another email', async () => {
  serve({ signedIn: false, onAppHost: true, choose: true });
  open('/sign-in');
  expect(await screen.findByRole('heading', { level: 1, name: 'Choose a workspace' })).toBeInTheDocument();
  expect(screen.getByText(/Signed in as sam@example\.com\./)).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Use another email' }));
  await waitFor(() => expect(clerk.signOut).toHaveBeenCalled());
  expect(screen.queryByRole('heading', { name: 'Choose a workspace' })).not.toBeInTheDocument();
});

test('choosing a workspace in the sign-in chooser shows it opening and hands the browser to it', async () => {
  serve({ signedIn: false, onAppHost: true, choose: true });
  open('/sign-in');
  fireEvent.click(await screen.findByRole('button', { name: 'Alpha Desk' }));
  expect(screen.getByRole('button', { name: 'Alpha Desk' })).toHaveTextContent('Opening…');
  await waitFor(() => expect(leaveTo).toHaveBeenCalledWith(continueURL));
  expect(JSON.parse(String(calls('/api/v1/auth/cloud/exchange').at(-1)?.[1].body))).toMatchObject({ workspace_slug: 'alpha-desk' });
});

test('a page without a session opens the sign-in page and keeps the identity provider session', async () => {
  serve({ signedIn: false, onAppHost: true });
  const router = open('/');
  await waitFor(() => expect(leaveTo).toHaveBeenCalledWith(continueURL));
  expect(router.state.location.pathname).toBe('/sign-in');
  expect(router.state.location.search).not.toHaveProperty('signed_out');
  expect(clerk.signOut).not.toHaveBeenCalled();
  expect(screen.queryByText('You are signed out.')).not.toBeInTheDocument();
});

test('Sign out ends the identity provider session too, so the next sign-in asks for a code', async () => {
  serve({ signedIn: true, onAppHost: false });
  const router = open('/');
  await openAccountMenu();
  fireEvent.click(await screen.findByRole('button', { name: 'Sign out' }));
  await waitFor(() => expect(router.state.location.pathname).toBe('/sign-in'));
  expect(router.state.location.search).toMatchObject({ signed_out: true });
  await waitFor(() => expect(clerk.signOut).toHaveBeenCalled());
  expect(await screen.findByText('You are signed out.')).toBeInTheDocument();
});

test('Sign out of this device in Sessions ends the identity provider session too', async () => {
  serve({ signedIn: true, onAppHost: false });
  const router = open('/settings/account');
  // The settings page loads from its own chunk.
  const sessions = await screen.findByRole('region', { name: 'Sessions' }, { timeout: 5000 });
  fireEvent.click(await within(sessions).findByRole('button', { name: 'Sign out' }));
  await waitFor(() => expect(router.state.location.pathname).toBe('/sign-in'));
  expect(router.state.location.search).toMatchObject({ signed_out: true });
  await waitFor(() => expect(clerk.signOut).toHaveBeenCalled());
});

test('a session someone revoked ends the identity provider session too, so the next sign-in asks for a code', async () => {
  serve({ signedIn: false, onAppHost: false, ended: 'session_revoked' });
  const router = open('/');
  await waitFor(() => expect(router.state.location.pathname).toBe('/sign-in'));
  expect(router.state.location.search).toMatchObject({ signed_out: true });
  await waitFor(() => expect(clerk.signOut).toHaveBeenCalled());
  expect(calls('/api/v1/auth/cloud/exchange')).toHaveLength(0);
});

test('an expired session keeps the identity provider session and signs straight back in', async () => {
  serve({ signedIn: false, onAppHost: false, ended: 'unauthorized' });
  const router = open('/');
  await waitFor(() => expect(calls('/api/v1/auth/cloud/exchange')).toHaveLength(1));
  expect(router.state.location.search).not.toHaveProperty('signed_out');
  expect(clerk.signOut).not.toHaveBeenCalled();
});
