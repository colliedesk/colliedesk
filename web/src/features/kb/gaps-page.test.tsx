import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { GapsRoute } from './gaps-page';
import { json, me, serve, summary } from './kb.test.fixture';

vi.mock('@tanstack/react-router', () => ({ useNavigate: () => vi.fn() }));
afterEach(() => { vi.unstubAllGlobals(); });

const invoicesID = '00000000-0000-4000-8000-000000000101';
const shippingID = '00000000-0000-4000-8000-000000000102';
const gap = (id: string, title: string, recent: number, kinds: string[], status: 'open' | 'fixed' | 'dismissed' = 'open') => ({
  id, title, status, kinds, askers: recent + 1, recent_askers: recent, first_seen: '2026-11-01T09:00:00Z', last_seen: '2026-11-21T09:00:00Z',
  fixed_by_article: null as { id: string; title: string } | null, fixed_automatically: false, status_changed_by: null, status_changed_at: null,
});
const invoices = gap(invoicesID, 'Where are my invoices', 2, ['search_no_result', 'vote_no']);
const shipping = gap(shippingID, 'shipping to canada', 1, ['chat_handoff']);
const signals = [
  { kind: 'vote_no', text: 'I could not find the invoices', created_at: '2026-11-21T09:00:00Z' },
  { kind: 'search_no_result', text: 'where are my invoices', created_at: '2026-11-20T09:00:00Z' },
];
const center = { enabled: true, title: '', locale: 'en', require_approval: false, content_version: 3, updated_at: null, show_tags: false, feedback_conversations: false, url: 'https://desk.example.com/help/acme' };
const answer = summary(9, 'Download your invoices', 'published');
// rowsOf is the gaps of the list, not the kind labels and signals listed inside each one.
const rowsOf = (list: HTMLElement) => within(list).getAllByRole('listitem').filter((row) => row.parentElement === list);

function open() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><GapsRoute /></QueryClientProvider>);
}

test('the gaps page ranks what readers did not find, and a row opens its signals and links it to an article', async () => {
  let open_ = [invoices, shipping];
  const calls = serve({
    'GET /api/v1/auth/me': () => json(200, me),
    'GET /api/v1/kb/gaps': (call) => json(200, { items: call.search.get('kind') === 'chat_handoff' ? [shipping] : open_ }),
    [`GET /api/v1/kb/gaps/${invoicesID}`]: () => json(200, { ...invoices, signal_count: 2, signals }),
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
    [`GET /api/v1/kb/gaps/${shippingID}`]: () => json(200, { ...shipping, signal_count: 1, signals: [{ kind: 'chat_handoff', text: 'do you ship to canada', created_at: '2026-11-21T09:00:00Z' }] }),
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
