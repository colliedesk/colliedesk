import { QueryClientProvider } from '@tanstack/react-query';
import { RouterProvider, createMemoryHistory, createRouter } from '@tanstack/react-router';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeAll, expect, test, vi } from 'vitest';
import { routeTree } from '../../app';
import { createQueryClient } from '../../query-client';
import { presenceViewPart } from '../inbox/presence-part';
import { autofixPart } from '../autofix/autofix-part';
import { permissionsOf, rolesOf } from '../../test-roles';

// The setup flow's AI drafts and reply targets steps, through the app's own routes: the server is the only
// stand-in (fetch). The numbers on screen come from what the server answers, never from the page.
const fetchMock = vi.fn();
vi.stubGlobal('fetch', fetchMock);
afterEach(() => { fetchMock.mockReset(); });

beforeAll(async () => {
  presenceViewPart.preload();
  autofixPart.preload();
  await Promise.all([import('./setup-flow'), import('../inbox/presence-view'), import('../autofix/autofix-chunk')]);
});

const keys = ['email', 'import', 'ai_drafts', 'reply_targets', 'invite', 'first_reply'] as const;
type Key = (typeof keys)[number];
const target = (first: number, resolve: number) => ({ first_response_minutes: first, resolution_minutes: resolve });
const presets = [
  { key: 'within_the_hour', name: 'Within the hour', targets: { urgent: target(15, 120), high: target(30, 240), normal: target(60, 540), low: target(240, 1080) } },
  { key: 'same_day', name: 'Same day', targets: { urgent: target(60, 240), high: target(120, 540), normal: target(480, 1620), low: target(540, 2700) } },
  { key: 'next_business_day', name: 'Next business day', targets: { urgent: target(240, 540), high: target(540, 1080), normal: target(540, 1620), low: target(1080, 2700) } },
];
type Suggestion = { preset: string; basis: 'history' | 'no_history' | 'import_running'; median_first_reply_minutes: number | null };
function progress(states: Partial<Record<Key, string>> = {}, suggestion: Suggestion = { preset: 'same_day', basis: 'no_history', median_first_reply_minutes: null }) {
  const state: Record<Key, string> = { email: 'working', import: 'done', ai_drafts: 'none', reply_targets: 'none', invite: 'alone', first_reply: 'none', ...states };
  return { steps: keys.map((key) => ({ key, name: key, state: state[key], detail: null })), complete: false, recommended: null,
    reply_target_presets: presets, reply_target_suggestion: suggestion };
}

const json = (status: number, body: unknown) => Promise.resolve(new Response(JSON.stringify(body), { status }));
const config = { cloud_sign_in: { enabled: false, publishable_key: null }, billing: { enabled: false }, signup: { enabled: false },
  limits: { outgoing_attachments_bytes: 10485760, outgoing_attachments_files: 10 }, product_analytics: { enabled: false, key: null, host: null, realm: null }, ai: { server_provider: true } };
const slot = (name: string, extra = {}) => ({ slot: name, source: 'none', provider_id: null, model: null, fallback_model: null, effort: 'low', enabled: false, default_level: 'off',
  automatic: false, auto_parts: [], active: false, off_reason: null, consented_by: null, consented_at: null, updated_by: null, updated_at: null, monthly_cap_micro_usd: null, ...extra });
// The server's estimate as Settings → Built-in AI reads it: an allowance of $10 a month and a draft at 5 cents,
// so half the allowance is 100 drafts; at most 40 automatic drafts a day.
function aiSettings(server: boolean) {
  return { server: server ? { kind: 'anthropic', label: 'Colliedesk AI', models: { draft: ['claude-sonnet-5-5'] } } : null, server_off: null, providers: [],
    slots: ['triage', 'draft', 'chat', 'intake'].map((name) => slot(name)), channels: [], source_consents: [], events: [], provider_limit: 5, can_store_keys: true,
    allow_private_base_urls: false,
    automatic: { per_day: 500, per_minute: 20, used_today: 0, paused_until: null, draft_per_day: 40, drafts_today: 0, drafts_paused_until: null, drafts_reserved_until: null,
      intake_per_day: 100, intake_today: 0, intake_paused_until: null },
    allowance: server ? { limit_micro_usd: 10_000_000, used_micro_usd: 0, resets_at: '2026-12-01T00:00:00Z', drafts_left: null } : null,
    auto_draft_estimate: { customer_messages: 120, draft_cost_micro_usd: 50_000 }, auto_triage: { applied: 0, undone: 0 } };
}
const targetsDoc = { enabled: false, timezone: null, hours: [], holidays: [{ date: '2026-12-25', name: 'Christmas' }], targets: {}, warn_before_minutes: 0,
  channel_ids: null, alert_email: true, my_alert_email: true, enabled_at: null, updated_at: '2026-11-10T08:00:00Z' };

// world is what the server holds; a save moves the step on, as the server does.
function serve(world: { setup: ReturnType<typeof progress>; server?: boolean; zone?: string }) {
  const sent: { path: string; body: unknown }[] = [];
  fetchMock.mockImplementation((url: string, init?: RequestInit) => {
    const path = url.split('?')[0];
    const method = init?.method ?? 'GET';
    if (method !== 'GET') sent.push({ path, body: JSON.parse(String(init?.body ?? 'null')) });
    if (path === '/api/v1/auth/me') return json(200, {
      agent: { id: '11111111-1111-4111-8111-111111111111', email: 'ana@northwind.example', display_name: '', roles: rolesOf('admin'), timezone_override: null },
      workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'northwind', name: 'Northwind Help', timezone: world.zone ?? 'Europe/Berlin' },
      timezone: world.zone ?? 'Europe/Berlin', token: null, permissions: permissionsOf('admin'), plan: null,
    });
    if (path === '/api/v1/config') return json(200, config);
    if (path === '/api/v1/workspace/setup') return json(200, world.setup);
    if (path === '/api/v1/ai') return json(200, aiSettings(world.server ?? true));
    if (path === '/api/v1/ai/slots/draft' && method === 'PUT') {
      world.setup = { ...world.setup, steps: world.setup.steps.map((step) => (step.key === 'ai_drafts' ? { ...step, state: 'on' } : step)) };
      return json(200, slot('draft', { source: 'server', enabled: true, default_level: 'suggest', active: true }));
    }
    if (path === '/api/v1/response-targets' && method === 'PUT') {
      world.setup = { ...world.setup, steps: world.setup.steps.map((step) => (step.key === 'reply_targets' ? { ...step, state: 'on' } : step)) };
      return json(200, { ...targetsDoc, ...(init?.body ? JSON.parse(String(init.body)) as object : {}), enabled_at: '2026-11-11T08:00:00Z', updated_at: '2026-11-11T08:00:00Z' });
    }
    if (path === '/api/v1/response-targets') return json(200, targetsDoc);
    if (path === '/api/v1/auth/invites') return json(200, { items: [] });
    if (path === '/api/v1/roles') return json(200, { items: [] });
    return json(404, { error: { code: 'not_found', message: 'no' } });
  });
  return sent;
}

function open(path: string) {
  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: [path] }) });
  render(<QueryClientProvider client={createQueryClient()}><RouterProvider router={router} /></QueryClientProvider>);
  return router;
}
const title = (name: string) => screen.findByRole('heading', { level: 1, name });
const flow = () => within(screen.getByRole('navigation', { name: 'Setup flow' }));

test('the AI drafts step shows an example, how it works, what is sent and what it costs from the server estimate, then turns drafts on', async () => {
  const sent = serve({ setup: progress() });
  open('/setup/ai_drafts');
  expect(await title('Let AI draft your replies')).toBeInTheDocument();
  expect(screen.getByRole('progressbar', { name: 'Setup progress' })).toHaveTextContent('Step 3 of 6 · AI drafts');
  // The example's buttons are not buttons: nothing on the example can be pressed.
  const example = screen.getByRole('figure', { name: 'Example: what your team sees in Today' });
  expect(example).toHaveTextContent('Example. In Today these buttons open the reply.');
  expect(within(example).queryByRole('button')).not.toBeInTheDocument();
  expect(screen.getByRole('list', { name: 'How AI drafts work' })).toHaveTextContent('You decide');
  expect(await screen.findByText(/about 100 automatic drafts this month, at most 40 a day/)).toBeInTheDocument();
  expect(screen.getByText(/go to Colliedesk AI, which runs Anthropic's Claude models/)).toBeInTheDocument();
  expect(flow().getByRole('link', { name: 'Back' })).toHaveAttribute('href', '/setup/import');
  expect(flow().getByRole('link', { name: "I'll do this later" })).toHaveAttribute('href', '/setup/reply_targets');

  fireEvent.click(flow().getByRole('button', { name: 'Turn on AI drafts' }));
  await waitFor(() => expect(sent).toEqual([{ path: '/api/v1/ai/slots/draft', body: { source: 'server', enabled: true, default_level: 'suggest' } }]));
  expect(await screen.findByText('The first draft comes with the next customer message.')).toBeInTheDocument();
  expect(flow().getByRole('button', { name: 'Next: Set reply targets' })).toBeEnabled();
  expect(flow().queryByRole('button', { name: 'Turn on AI drafts' })).not.toBeInTheDocument();
});

test('with nothing to read yet, the AI drafts step says so and links to the import and the help center; turning on still works', async () => {
  serve({ setup: progress({ import: 'none', ai_drafts: 'nothing_to_read' }) });
  open('/setup/ai_drafts');
  const note = await screen.findByRole('note', { name: 'Drafts have little to read yet' });
  expect(within(note).getByRole('link', { name: 'Import from Freshdesk' })).toHaveAttribute('href', '/setup/import');
  expect(within(note).getByRole('link', { name: 'Write an article' })).toHaveAttribute('href', '/knowledge/new');
  await waitFor(() => expect(flow().getByRole('button', { name: 'Turn on AI drafts' })).toBeEnabled());
});

test('on a server without built-in AI, the AI drafts step offers to add a provider in a new tab, and skipping is safe', async () => {
  serve({ setup: progress({ ai_drafts: 'unavailable' }), server: false });
  open('/setup/ai_drafts');
  const add = await screen.findByRole('link', { name: /Add a provider/ });
  expect(add).toHaveAttribute('href', '/settings/ai');
  expect(add).toHaveAttribute('target', '_blank');
  expect(screen.queryByRole('button', { name: 'Turn on AI drafts' })).not.toBeInTheDocument();
  expect(await screen.findByText(/This server has no built-in AI/)).toBeInTheDocument();
  expect(flow().getByRole('link', { name: "I'll do this later" })).toHaveAttribute('href', '/setup/reply_targets');
});

test('the reply targets step suggests the preset that fits the imported history, names it on the button, and applies another the person picks', async () => {
  const sent = serve({ setup: progress({}, { preset: 'same_day', basis: 'history', median_first_reply_minutes: 190 }) });
  const router = open('/setup/reply_targets');
  expect(await title('Choose when a reply is due')).toBeInTheDocument();
  expect(screen.getByText("From your import: your team's median first reply in Freshdesk was 3 h 10 min over the last 30 days. Same day fits it.")).toBeInTheDocument();
  const sameDay = screen.getByRole('radio', { name: /Same day/ });
  expect(sameDay).toBeChecked();
  expect(screen.getByText('Fits your history')).toBeInTheDocument();
  // Each preset's table: Reply and Resolve per priority, a day being one business day.
  const within1h = screen.getByRole('table', { name: 'Within the hour' });
  expect(within(within1h).getByRole('row', { name: /Urgent/ })).toHaveTextContent('Urgent15 min2 h');
  expect(within(within1h).getByRole('row', { name: /Low/ })).toHaveTextContent('Low4 h2 days');
  expect(within(screen.getByRole('table', { name: 'Same day' })).getByRole('row', { name: /Normal/ })).toHaveTextContent('Normal8 h3 days');
  expect(await screen.findByText(/Monday to Friday, 09:00 to 18:00, Europe\/Berlin/)).toBeInTheDocument();
  expect(screen.getByText('A Normal message that arrives Friday at 16:30 is due Monday at 15:30: 1 h 30 on Friday, 6 h 30 on Monday.')).toBeInTheDocument();
  expect(flow().getByRole('button', { name: 'Use Same day' })).toBeEnabled();

  fireEvent.click(screen.getByRole('radio', { name: /Within the hour/ }));
  expect(screen.getByText('A Normal message that arrives Friday at 16:30 is due Friday at 17:30.')).toBeInTheDocument();
  fireEvent.click(flow().getByRole('button', { name: 'Use Within the hour' }));
  await waitFor(() => expect(sent).toHaveLength(1));
  const open9to6 = (day: string) => ({ day, open: true, from: '09:00', to: '18:00' });
  expect(sent[0]).toEqual({ path: '/api/v1/response-targets', body: {
    enabled: true, timezone: null, warn_before_minutes: 30, channel_ids: null, alert_email: true, holidays: [{ date: '2026-12-25', name: 'Christmas' }],
    updated_at: '2026-11-10T08:00:00Z',
    hours: [open9to6('mon'), open9to6('tue'), open9to6('wed'), open9to6('thu'), open9to6('fri'), { day: 'sat', open: false }, { day: 'sun', open: false }],
    targets: { urgent: target(15, 120), high: target(30, 240), normal: target(60, 540), low: target(240, 1080) },
  } });
  await waitFor(() => expect(router.state.location.pathname).toBe('/setup/invite'));
});

test('with no import, or while it runs, Same day is suggested and the line says why', async () => {
  serve({ setup: progress({ import: 'none' }) });
  open('/setup/reply_targets');
  expect(await screen.findByText('No import to learn from, so we suggest Same day, the choice most email teams make.')).toBeInTheDocument();
  expect(screen.getByText('Suggested')).toBeInTheDocument();
  expect(screen.getByRole('radio', { name: /Same day/ })).toBeChecked();
});

test('an import with nothing answered in the last 30 days says so, and suggests Same day', async () => {
  serve({ setup: progress({ import: 'done' }) });
  open('/setup/reply_targets');
  expect(await screen.findByText('Your import has no answered conversation from the last 30 days to learn from, so we suggest Same day, the choice most email teams make.')).toBeInTheDocument();
  expect(screen.getByText('Suggested')).toBeInTheDocument();
});

test('while the import runs, Same day is suggested until it ends', async () => {
  serve({ setup: progress({ import: 'running' }, { preset: 'same_day', basis: 'import_running', median_first_reply_minutes: null }) });
  open('/setup/reply_targets');
  expect(await screen.findByText('Your import is still running. We suggest Same day for now and check your history when it ends.')).toBeInTheDocument();
  expect(screen.getByText('Suggested')).toBeInTheDocument();
});

test('reply targets that are on already say so and lead to the next step', async () => {
  serve({ setup: progress({ reply_targets: 'on' }) });
  open('/setup/reply_targets');
  expect(await screen.findByText(/Reply targets are on\./)).toBeInTheDocument();
  expect(flow().getByRole('button', { name: 'Next: Invite your team' })).toBeEnabled();
  expect(flow().queryByRole('button', { name: /^Use / })).not.toBeInTheDocument();
});
