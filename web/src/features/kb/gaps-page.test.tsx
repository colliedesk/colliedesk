import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, expect, test, vi } from 'vitest';
import { PermissionsContext } from '../../lib/permissions-context';
import { permissionsOf } from '../../test-roles';
import { GapsRoute } from './gaps-page';
import { article, ids, json, me, refusal, serve, summary } from './kb.test.fixture';

const navigate = vi.hoisted(() => vi.fn());
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => navigate }));
afterEach(() => { navigate.mockReset(); vi.unstubAllGlobals(); });

const invoicesID = '00000000-0000-4000-8000-000000000101';
const shippingID = '00000000-0000-4000-8000-000000000102';
const gap = (id: string, title: string, recent: number, kinds: string[], status: 'open' | 'fixed' | 'dismissed' = 'open') => ({
  id, title, status, kinds, askers: recent + 1, recent_askers: recent, conversation_count: 0, first_seen: '2026-11-01T09:00:00Z', last_seen: '2026-11-21T09:00:00Z',
  fixed_by_article: null as { id: string; title: string } | null, fixed_automatically: false, status_changed_by: null, status_changed_at: null,
});
const invoices = gap(invoicesID, 'Where are my invoices', 2, ['search_no_result', 'vote_no']);
const shipping = gap(shippingID, 'shipping to canada', 1, ['chat_handoff']);
const signals = [
  { kind: 'vote_no', text: 'I could not find the invoices', note: null, created_at: '2026-11-21T09:00:00Z' },
  { kind: 'search_no_result', text: 'where are my invoices', note: null, created_at: '2026-11-20T09:00:00Z' },
];
const center = { enabled: true, title: '', locale: 'en', require_approval: false, content_version: 3, updated_at: null, show_tags: false, feedback_conversations: false, url: 'https://desk.example.com/help/acme' };
const answer = summary(9, 'Download your invoices', 'published');
// rowsOf is the gaps of the list, not the kind labels and signals listed inside each one.
const rowsOf = (list: HTMLElement) => within(list).getAllByRole('listitem').filter((row) => row.parentElement === list);

function open(permissions?: string[]) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const wrap = (children: ReactNode) => permissions ? <PermissionsContext.Provider value={permissions}>{children}</PermissionsContext.Provider> : children;
  render(<QueryClientProvider client={client}>{wrap(<GapsRoute />)}</QueryClientProvider>);
}

const shippingRoutes = {
  'GET /api/v1/auth/me': () => json(200, me),
  'GET /api/v1/kb/gaps': () => json(200, { items: [shipping] }),
  [`GET /api/v1/kb/gaps/${shippingID}`]: () => json(200, { ...shipping, signal_count: 1, conversations: [], signals: [{ kind: 'chat_handoff', text: 'do you ship to canada', note: null, created_at: '2026-11-21T09:00:00Z' }] }),
};
const drafted = article({
  title: 'Shipping to Canada', status: 'draft',
  gap_source: { gap_id: shippingID, requested: false, conversations: [{ conversation_id: '00000000-0000-4000-8000-000000000201', display_ref: '#41', subject: 'Do you ship to Canada?' }] },
});

test('"Draft an article" has the AI write a draft from the gap and opens it in the editor', async () => {
  const calls = serve({ ...shippingRoutes, [`POST /api/v1/kb/gaps/${shippingID}/draft`]: () => json(201, drafted) });
  open();
  fireEvent.click(await screen.findByRole('button', { name: 'shipping to canada' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Draft an article' }));
  await waitFor(() => expect(navigate).toHaveBeenCalledWith({ to: '/knowledge/articles/$articleId', params: { articleId: ids.article } }));
  const posts = calls.filter((call) => call.method === 'POST');
  expect(posts.map((call) => call.path)).toEqual([`/api/v1/kb/gaps/${shippingID}/draft`]);
  expect(posts[0].headers['Idempotency-Key']).toMatch(/.{16,}/);
});

test('a draft the AI may not write says why in the server\'s words, and the button works again', async () => {
  const answers = [
    refusal(409, 'ai_feature_off', 'This AI feature is off in this workspace, or its provider is gone. An admin turns it on in Settings → Built-in AI.'),
    refusal(402, 'ai_allowance_reached', 'This month\'s included AI is used up. Add your own provider in Settings → Built-in AI, or move to a larger plan.'),
    json(502, { error: { code: 'ai_failed', message: 'The model gave no usable answer.' } }),
  ];
  serve({ ...shippingRoutes, [`POST /api/v1/kb/gaps/${shippingID}/draft`]: () => answers.shift() ?? json(500) });
  open();
  fireEvent.click(await screen.findByRole('button', { name: 'shipping to canada' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Draft an article' }));
  expect(await screen.findByText('This AI feature is off in this workspace, or its provider is gone. An admin turns it on in Settings → Built-in AI.')).toBeInTheDocument();
  await waitFor(() => expect(screen.getByRole('button', { name: 'Draft an article' })).toBeEnabled());
  fireEvent.click(screen.getByRole('button', { name: 'Draft an article' }));
  expect(await screen.findByText('This month\'s included AI is used up. Add your own provider in Settings → Built-in AI, or move to a larger plan.')).toBeInTheDocument();
  await waitFor(() => expect(screen.getByRole('button', { name: 'Draft an article' })).toBeEnabled());
  fireEvent.click(screen.getByRole('button', { name: 'Draft an article' }));
  expect(await screen.findByText('The article could not be drafted. Try again in a moment.')).toBeInTheDocument();
  expect(navigate).not.toHaveBeenCalled();
});

test('a role that may not create articles is not offered a draft, and still dismisses a gap', async () => {
  serve(shippingRoutes);
  open(permissionsOf('admin').filter((permission) => permission !== 'kb_articles:create'));
  fireEvent.click(await screen.findByRole('button', { name: 'shipping to canada' }));
  expect(await screen.findByRole('button', { name: 'Dismiss' })).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Draft an article' })).not.toBeInTheDocument();
});

test('the gaps page ranks what readers did not find, and a row opens its signals and links it to an article', async () => {
  let open_ = [invoices, shipping];
  const calls = serve({
    'GET /api/v1/auth/me': () => json(200, me),
    'GET /api/v1/kb/gaps': (call) => json(200, { items: call.search.get('kind') === 'chat_handoff' ? [shipping] : open_ }),
    [`GET /api/v1/kb/gaps/${invoicesID}`]: () => json(200, { ...invoices, signal_count: 2, conversations: [], signals }),
    [`PATCH /api/v1/kb/gaps/${invoicesID}`]: () => {
      open_ = [shipping];
      return json(200, { ...invoices, status: 'fixed', fixed_by_article: { id: answer.id, title: answer.title }, status_changed_by: { agent_id: me.agent.id, name: 'Ada' }, status_changed_at: '2026-11-22T09:00:00Z' });
    },
    'GET /api/v1/help-center': () => json(200, center),
    'GET /api/v1/kb/articles': () => json(200, { items: [answer], next_cursor: null }),
  });
  open();
  const list = await screen.findByRole('list', { name: 'Open gaps' });
  const rows = rowsOf(list);
  expect(rows.map((row) => within(row).getByRole('button', { expanded: false }).textContent)).toEqual(['Where are my invoices', 'shipping to canada']);
  expect(within(rows[0]).getByText('2 askers in 30 days · 3 in all · last seen Nov 21, 9:00 AM')).toBeInTheDocument();
  expect(within(within(rows[0]).getByRole('list', { name: 'Kinds' })).getAllByRole('listitem').map((label) => label.textContent)).toEqual(['Searched, no result', 'Voted No']);
  expect(screen.getByRole('link', { name: 'Gaps' })).toHaveAttribute('aria-current', 'page');

  fireEvent.change(screen.getByRole('combobox', { name: 'Kind' }), { target: { value: 'chat_handoff' } });
  await waitFor(() => expect(rowsOf(screen.getByRole('list', { name: 'Open gaps' })).length).toBe(1));
  expect(calls.filter((call) => call.path === '/api/v1/kb/gaps').map((call) => [call.search.get('status'), call.search.get('kind')])).toEqual([['open', null], ['open', 'chat_handoff']]);
  fireEvent.change(screen.getByRole('combobox', { name: 'Kind' }), { target: { value: '' } });

  fireEvent.click(await screen.findByRole('button', { name: 'Where are my invoices' }));
  const asked = await screen.findByRole('list', { name: 'What readers asked: Where are my invoices' });
  expect(within(asked).getAllByRole('listitem').map((row) => row.textContent)).toEqual([
    'Voted NoI could not find the invoicesNov 21, 9:00 AM', 'Searched, no resultwhere are my invoicesNov 20, 9:00 AM',
  ]);
  fireEvent.click(screen.getByRole('button', { name: 'Link to an article' }));
  fireEvent.click(await screen.findByRole('option', { name: 'Download your invoices' }));
  await waitFor(() => expect(calls.find((call) => call.method === 'PATCH')?.body).toEqual({ fixed_by_article_id: answer.id }));
  expect(await screen.findByText('Where are my invoices is fixed by Download your invoices.')).toBeInTheDocument();
  await waitFor(() => expect(rowsOf(screen.getByRole('list', { name: 'Open gaps' })).length).toBe(1));
});

test('dismissing a gap sends its status and can be undone', async () => {
  const bodies: unknown[] = [];
  serve({
    'GET /api/v1/auth/me': () => json(200, me),
    'GET /api/v1/kb/gaps': () => json(200, { items: [shipping] }),
    [`GET /api/v1/kb/gaps/${shippingID}`]: () => json(200, { ...shipping, signal_count: 1, conversations: [], signals: [{ kind: 'chat_handoff', text: 'do you ship to canada', note: null, created_at: '2026-11-21T09:00:00Z' }] }),
    [`PATCH /api/v1/kb/gaps/${shippingID}`]: (call) => {
      bodies.push(call.body);
      return json(200, { ...shipping, status: (call.body as { status: 'open' | 'dismissed' }).status });
    },
  });
  open();
  fireEvent.click(await screen.findByRole('button', { name: 'shipping to canada' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Dismiss' }));
  expect(await screen.findByText('shipping to canada is dismissed.')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Undo' }));
  await waitFor(() => expect(bodies).toEqual([{ status: 'dismissed' }, { status: 'open' }]));
});

test('a workspace with no open gap says where gaps come from', async () => {
  serve({ 'GET /api/v1/auth/me': () => json(200, me), 'GET /api/v1/kb/gaps': () => json(200, { items: [] }) });
  open();
  expect(await screen.findByText('No open gaps. A help center search with no result, a "No" vote or a help chat handed to a person shows here within a minute.')).toBeInTheDocument();
});

const passwordID = '00000000-0000-4000-8000-000000000103';
const locked = '00000000-0000-4000-8000-000000000211';
const forgot = '00000000-0000-4000-8000-000000000212';
const password = { ...gap(passwordID, 'Reset a password', 2, ['requested']), conversation_count: 2 };
const passwordRoutes = {
  'GET /api/v1/auth/me': () => json(200, me),
  'GET /api/v1/kb/gaps': () => json(200, { items: [password] }),
  [`GET /api/v1/kb/gaps/${passwordID}`]: () => json(200, {
    ...password, signal_count: 2,
    conversations: [
      { conversation_id: locked, display_ref: '#58', subject: 'Locked out' },
      { conversation_id: forgot, display_ref: '#41', subject: 'Forgot my password' },
    ],
    signals: [
      { kind: 'requested', text: 'Reset a password', note: 'Asked twice this week', created_at: '2026-11-21T09:00:00Z' },
      { kind: 'requested', text: 'Reset a password', note: null, created_at: '2026-11-20T09:00:00Z' },
    ],
  }),
};

test('a request from conversations counts them, links back to each, and shows the line its asker wrote', async () => {
  serve(passwordRoutes);
  open();
  const row = rowsOf(await screen.findByRole('list', { name: 'Open gaps' }))[0];
  expect(within(row).getByText('Asked from 2 conversations')).toBeInTheDocument();
  expect(within(within(row).getByRole('list', { name: 'Kinds' })).getByRole('listitem')).toHaveTextContent('Requested from a conversation');
  fireEvent.click(within(row).getByRole('button', { name: 'Reset a password' }));
  const asked = await screen.findByRole('list', { name: 'Conversations that asked for it' });
  expect(within(asked).getAllByRole('link').map((link) => [link.textContent, link.getAttribute('href')])).toEqual([
    ['#58 Locked out', `/conversations/${locked}`],
    ['#41 Forgot my password', `/conversations/${forgot}`],
  ]);
  expect(screen.getByText('Asked twice this week')).toBeInTheDocument();
});

test('"Start a draft" opens an empty draft of the request in the editor, beside the conversations that asked, without AI', async () => {
  const started = article({ title: 'Reset a password', status: 'draft', gap_source: { gap_id: passwordID, requested: true, conversations: [] } });
  const calls = serve({ ...passwordRoutes, [`POST /api/v1/kb/gaps/${passwordID}/article`]: () => json(201, started) });
  open();
  fireEvent.click(await screen.findByRole('button', { name: 'Reset a password' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Start a draft' }));
  await waitFor(() => expect(navigate).toHaveBeenCalledWith({ to: '/knowledge/articles/$articleId', params: { articleId: ids.article } }));
  const posts = calls.filter((call) => call.method === 'POST');
  expect(posts.map((call) => call.path)).toEqual([`/api/v1/kb/gaps/${passwordID}/article`]);
  expect(posts[0].headers['Idempotency-Key']).toMatch(/.{16,}/);
});

test('a role that may not read conversations sees how many asked, never which ones', async () => {
  serve({ ...passwordRoutes, [`GET /api/v1/kb/gaps/${passwordID}`]: () => json(200, { ...password, signal_count: 0, conversations: [], signals: [] }) });
  open(permissionsOf('admin').filter((permission) => permission !== 'conversations:read'));
  fireEvent.click(await screen.findByRole('button', { name: 'Reset a password' }));
  expect(await screen.findByText('Asked from 2 conversations')).toBeInTheDocument();
  expect(screen.queryByRole('list', { name: 'Conversations that asked for it' })).not.toBeInTheDocument();
});
