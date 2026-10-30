import { QueryClientProvider } from '@tanstack/react-query';
import { RouterProvider, createMemoryHistory, createRouter } from '@tanstack/react-router';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeAll, expect, test, vi } from 'vitest';
import { routeTree } from '../../app';
import { createQueryClient } from '../../query-client';
import { presenceViewPart } from '../inbox/presence-part';
import { autofixPart } from '../autofix/autofix-part';

// The setup flow through the app's own routes: the server is the only stand-in (fetch).
const fetchMock = vi.fn();
vi.stubGlobal('fetch', fetchMock);
afterEach(() => { fetchMock.mockReset(); });

// The flow and the parts it embeds load from their own chunks; they are there before the tests start, so
// no test ends while one is still on its way (vitest refuses a module asked for after the environment is gone).
beforeAll(async () => {
  presenceViewPart.preload();
  autofixPart.preload();
  await Promise.all([import('./setup-flow'), import('../../channels/email/panels'), import('../settings/import-route'),
    import('../inbox/presence-view'), import('../autofix/autofix-chunk'), import('./setup-checklist')]);
});

type Key = 'email' | 'import' | 'invite' | 'first_reply';
type Progress = { steps: { key: Key; state: string; detail: null }[]; complete: boolean; recommended: Key | null };
function progress(states: Partial<Record<Key, string>> = {}, recommended: Key | null = null): Progress {
  const state: Record<Key, string> = { email: 'none', import: 'none', invite: 'alone', first_reply: 'none', ...states };
  return { steps: (['email', 'import', 'invite', 'first_reply'] as const).map((key) => ({ key, state: state[key], detail: null })), complete: false, recommended };
}

const json = (status: number, body: unknown) => Promise.resolve(new Response(JSON.stringify(body), { status }));
const config = { cloud_sign_in: { enabled: false, publishable_key: null }, billing: { enabled: false }, signup: { enabled: false },
  limits: { outgoing_attachments_bytes: 10485760, outgoing_attachments_files: 10 }, product_analytics: { enabled: false, key: null, host: null, realm: null }, ai: { server_provider: false } };
const invite = { id: '33333333-3333-4333-8333-333333333333', email: 'kim@example.com', role_ceiling: 'agent', created_by_email: 'sam@example.com',
  created_at: '2026-10-30T04:00:00Z', expires_at: '2026-11-06T04:00:00Z', used_at: null, revoked_at: null };

// world is what the server holds; creating an invite moves the invite step on, as the server does.
function serve(world: { role?: 'admin' | 'agent'; setup: Progress; signedOut?: boolean }) {
  fetchMock.mockImplementation((url: string, init?: RequestInit) => {
    const path = url.split('?')[0];
    const method = init?.method ?? 'GET';
    if (world.signedOut && path !== '/api/v1/config') return json(401, { error: { code: 'unauthorized', message: 'Sign in.' } });
    if (path === '/api/v1/auth/me') return json(200, {
      agent: { id: '11111111-1111-4111-8111-111111111111', email: 'sam@example.com', display_name: '', role: world.role ?? 'admin', timezone_override: null },
      workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'beta-desk', name: 'Beta Desk', timezone: 'UTC' }, timezone: 'UTC', scope: null, plan: null,
    });
    if (path === '/api/v1/config') return json(200, config);
    if (path === '/api/v1/workspace/setup') return json(200, world.setup);
    if (path === '/api/v1/channels') return json(200, { items: [], types: [{ type: 'email', available: true, count: 0, default_channel_id: null }] });
    if (path === '/api/v1/imports') return json(200, { items: [] });
    if (path === '/api/v1/auth/invites' && method === 'POST') {
      world.setup = { ...world.setup, steps: world.setup.steps.map((step) => (step.key === 'invite' ? { ...step, state: 'invited' } : step)) };
      return json(201, { invite, url: 'https://desk.example.com/sign-in?token=one-time-invite' });
    }
    if (path === '/api/v1/auth/invites') return json(200, { items: [] });
    return json(404, { error: { code: 'not_found', message: 'no' } });
  });
}

function open(path: string) {
  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: [path] }) });
  render(<QueryClientProvider client={createQueryClient()}><RouterProvider router={router} /></QueryClientProvider>);
  return router;
}
const title = (name: string) => screen.findByRole('heading', { level: 1, name });
// The moves between steps are one navigation. Its main button names the step it opens, so it never reads like
// the Continue of the email setup inside, which moves between that setup's own parts.
const flow = () => within(screen.getByRole('navigation', { name: 'Setup flow' }));
const continueButton = () => flow().getByRole('button', { name: /^(Next: |Finish setup$)/ });

test('a step opens from its address, full screen, with its title, its place in the flow and the workspace it acts in', async () => {
  serve({ setup: progress() });
  open('/setup/import');
  expect(await title('Import from Freshdesk')).toBeInTheDocument();
  expect(screen.getByRole('progressbar', { name: 'Setup progress' })).toHaveTextContent('Step 2 of 4');
  const identity = screen.getByRole('region', { name: 'Signed in as' });
  expect(identity).toHaveTextContent('Beta Desk');
  expect(identity).toHaveTextContent('sam@example.com');
  expect(screen.queryByRole('navigation', { name: 'Main' })).not.toBeInTheDocument();
  // The step is done here: the import form is on this screen, not behind a link to Settings.
  expect(await screen.findByLabelText('Freshdesk address')).toBeInTheDocument();
  expect(screen.queryByRole('navigation', { name: 'Settings sections' })).not.toBeInTheDocument();
});

test('the step the sign-up goal recommends is marked', async () => {
  serve({ setup: progress({}, 'import') });
  open('/setup/import');
  await title('Import from Freshdesk');
  expect(screen.getByText('Recommended')).toBeInTheDocument();
});

test('Continue waits for the person\'s own part of the step, says what is missing, then opens the next step', async () => {
  serve({ setup: progress() });
  const router = open('/setup/invite');
  await title('Invite your team');
  expect(continueButton()).toBeDisabled();
  expect(screen.getByText('Create an invite above to continue.')).toBeInTheDocument();
  fireEvent.change(await screen.findByLabelText('Invite email'), { target: { value: 'kim@example.com' } });
  fireEvent.click(screen.getByRole('button', { name: 'Create invite' }));
  // The server reports the invite as sent; the link stays on screen while the person is on this step.
  await waitFor(() => expect(continueButton()).toBeEnabled());
  expect(screen.getByRole('textbox', { name: 'One-time invite link' })).toHaveValue('https://desk.example.com/sign-in?token=one-time-invite');
  expect(screen.queryByText('Create an invite above to continue.')).not.toBeInTheDocument();
  expect(continueButton()).toHaveAccessibleName('Next: Answer your first email');
  fireEvent.click(continueButton());
  await waitFor(() => expect(router.state.location.pathname).toBe('/setup/first_reply'));
});

test('"I\'ll do this later" opens the next step, Back the one before; the first step has no Back', async () => {
  serve({ setup: progress() });
  const router = open('/setup/email');
  await title('Receive customer email');
  expect(flow().queryByRole('link', { name: 'Back' })).not.toBeInTheDocument();
  fireEvent.click(flow().getByRole('link', { name: 'I\'ll do this later' }));
  await title('Import from Freshdesk');
  expect(router.state.location.pathname).toBe('/setup/import');
  fireEvent.click(flow().getByRole('link', { name: 'Back' }));
  await title('Receive customer email');
  expect(router.state.location.pathname).toBe('/setup/email');
});

test('on the last step the main button reads Finish setup, and both it and "I\'ll do this later" open the summary', async () => {
  serve({ setup: progress({ first_reply: 'sent' }) });
  const router = open('/setup/first_reply');
  await title('Answer your first email');
  expect(continueButton()).toHaveAccessibleName('Finish setup');
  expect(flow().getByRole('link', { name: 'I\'ll do this later' })).toHaveAttribute('href', '/setup/done');
  fireEvent.click(continueButton());
  await waitFor(() => expect(router.state.location.pathname).toBe('/setup/done'));
});

test('/setup opens the first step whose own part is not done, or the summary when none is left', async () => {
  serve({ setup: progress({ email: 'waiting' }) });
  const router = open('/setup');
  await title('Import from Freshdesk');
  expect(router.state.location.pathname).toBe('/setup/import');
});

test('/setup with every own part done opens the summary', async () => {
  serve({ setup: progress({ email: 'waiting', import: 'running', invite: 'invited', first_reply: 'sent' }) });
  const router = open('/setup');
  await waitFor(() => expect(router.state.location.pathname).toBe('/setup/done'));
});

test('an unknown step goes back to the start of the flow', async () => {
  serve({ setup: progress() });
  const router = open('/setup/nothing-here');
  await waitFor(() => expect(router.state.location.pathname).toBe('/setup/email'));
});

test('an agent who is not an admin sees only the steps they can take; an admin step opens theirs instead', async () => {
  serve({ role: 'agent', setup: progress() });
  const router = open('/setup/email');
  await title('Answer your first email');
  expect(router.state.location.pathname).toBe('/setup/first_reply');
  expect(screen.getByRole('progressbar', { name: 'Setup progress' })).toHaveTextContent('Step 1 of 1');
});

test('the summary lists every step with its state, links back to each unfinished one and opens the inbox', async () => {
  serve({ setup: progress({ email: 'working', import: 'running' }) });
  const router = open('/setup/done');
  await title('You\'re ready to answer customers');
  const rows = within(screen.getByRole('list', { name: 'Setup steps' })).getAllByRole('listitem');
  expect(rows.map((row) => row.textContent)).toEqual([
    expect.stringMatching(/Receive customer email.*Done/), expect.stringMatching(/Import from Freshdesk.*Importing/),
    expect.stringMatching(/Invite your team.*To do/), expect.stringMatching(/Answer your first email.*To do/),
  ]);
  expect(within(rows[0]).queryByRole('link')).not.toBeInTheDocument();
  expect(within(rows[1]).getByRole('link', { name: 'Import from Freshdesk' })).toHaveAttribute('href', '/setup/import');
  expect(within(rows[2]).getByRole('link', { name: 'Invite your team' })).toHaveAttribute('href', '/setup/invite');
  fireEvent.click(screen.getByRole('link', { name: 'Open the inbox' }));
  await waitFor(() => expect(router.state.location.pathname).toBe('/'));
});

test('Exit setup goes to the inbox', async () => {
  serve({ setup: progress() });
  const router = open('/setup/email');
  await title('Receive customer email');
  const exit = screen.getByRole('link', { name: 'Exit setup' });
  expect(exit).toHaveAttribute('data-tip', 'Exit setup');
  fireEvent.click(exit);
  await waitFor(() => expect(router.state.location.pathname).toBe('/'));
});

test('signed out, the setup flow shows the way back in, as every page does, and not a step', async () => {
  serve({ setup: progress(), signedOut: true });
  open('/setup/email');
  expect(await screen.findByText(/You are signed out/)).toBeInTheDocument();
  expect(screen.queryByRole('heading', { level: 1, name: 'Receive customer email' })).not.toBeInTheDocument();
  expect(screen.queryByRole('region', { name: 'Signed in as' })).not.toBeInTheDocument();
});
