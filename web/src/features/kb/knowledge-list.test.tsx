import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { KnowledgeRoute } from './knowledge-page';
import type { KnowledgeSearch } from './knowledge-search';
import { collections, ids, json, me, serve, summary } from './kb.test.fixture';

const navigate = vi.hoisted(() => vi.fn());
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => navigate }));
afterEach(() => { navigate.mockReset(); vi.unstubAllGlobals(); });

const bea = '99999999-9999-4999-8999-999999999999';
const member = (id: string, name: string) => ({ id, email: `${name.toLowerCase()}@example.com`, display_name: name, roles: [], manageable: true,
  timezone_override: null, deactivated_at: null, last_active_at: null, open_conversations: 0 });
const tagged = { ...summary(1, 'Refund rules', 'published'), author_agent_id: bea, author: { agent_id: bea, name: 'Bea', left: true }, tags: ['billing', 'faq'],
  languages: [{ locale: 'de', outdated: true }], votes: { helpful: 3, not_helpful: 1 } };
const base = {
  'GET /api/v1/auth/me': () => json(200, { ...me, kb_list_columns: { columns: ['status', 'author', 'tags', 'updated'], default: true } }),
  'GET /api/v1/kb/collections': () => json(200, { items: collections }),
  'GET /api/v1/kb/articles': () => json(200, { items: [tagged], next_cursor: null }),
  'GET /api/v1/kb/tags': () => json(200, { items: [{ tag: 'billing', article_count: 2 }, { tag: 'faq', article_count: 1 }] }),
  'GET /api/v1/agents': () => json(200, { items: [member(me.agent.id, 'Ada'), member(bea, 'Bea')] }),
};

function open(search: KnowledgeSearch = {}) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const view = render(<QueryClientProvider client={client}><KnowledgeRoute search={search} /></QueryClientProvider>);
  return (next: KnowledgeSearch) => view.rerender(<QueryClientProvider client={client}><KnowledgeRoute search={next} /></QueryClientProvider>);
}

test('the list shows the columns the person chose, and a change is saved for them on every device', async () => {
  const calls = serve({ ...base,
    'PUT /api/v1/agents/me/kb-list-columns': (call) => {
      const columns = (call.body as { columns: string[] | null }).columns;
      return json(200, columns ? { columns, default: false } : { columns: ['status', 'author', 'tags', 'updated'], default: true });
    } });
  open();
  const row = await screen.findByRole('link', { name: /Refund rules/ });
  expect(row).toHaveTextContent('Bea (left)');
  expect(row).toHaveTextContent('billing');
  expect(row).not.toHaveTextContent('3 / 1');
  fireEvent.click(screen.getByText('Columns'));
  const chooser = screen.getByRole('group', { name: 'Columns' });
  expect(within(chooser).getAllByRole('checkbox').map((box) => box.getAttribute('aria-label') ?? box.closest('label')?.textContent))
    .toEqual(['Status', 'Author', 'Tags', 'Updated', 'Collection', 'Languages', 'Created', 'Votes']);
  fireEvent.click(within(chooser).getByRole('checkbox', { name: 'Tags' }));
  await waitFor(() => expect(calls.find((call) => call.method === 'PUT')?.body).toEqual({ columns: ['status', 'author', 'updated'] }));
  await waitFor(() => expect(screen.getByRole('link', { name: /Refund rules/ })).not.toHaveTextContent('billing'));
  fireEvent.click(within(chooser).getByRole('checkbox', { name: 'Votes' }));
  await waitFor(() => expect(calls.filter((call) => call.method === 'PUT').at(-1)?.body).toEqual({ columns: ['status', 'author', 'updated', 'votes'] }));
  await waitFor(() => expect(screen.getByRole('link', { name: /Refund rules/ })).toHaveTextContent('3 / 1'));
  fireEvent.click(within(chooser).getByRole('button', { name: 'Reset columns' }));
  await waitFor(() => expect(calls.filter((call) => call.method === 'PUT').at(-1)?.body).toEqual({ columns: null }));
  await waitFor(() => expect(screen.getByRole('link', { name: /Refund rules/ })).toHaveTextContent('billing'));
});

test('filters in the address go to the list and its CSV export, and a filtered collection cannot be reordered', async () => {
  const calls = serve(base);
  open({ collection: ids.setup, subcollections: 'true', tag: ['faq', 'a, b'], tag_match: 'all', author: [bea], created_after: '2026-10-01', created_before: '2026-10-08',
    translation: 'missing', locale: 'de' });
  await screen.findByRole('link', { name: /Refund rules/ });
  const asked = calls.find((call) => call.path === '/api/v1/kb/articles')!;
  expect(asked.search.toString()).toBe(`collection_id=${ids.setup}&include_subcollections=true&author=${bea}&tag=faq&tag=a%2C+b&tag_match=all`
    + '&created_after=2026-10-01&created_before=2026-10-08&translation=missing&locale=de');
  expect(screen.getByRole('link', { name: 'Export CSV' })).toHaveAttribute('href', `/api/v1/kb/articles/export.csv?collection_id=${ids.setup}&include_subcollections=true`
    + `&author=${bea}&tag=faq&tag=a%2C+b&tag_match=all&created_after=2026-10-01&created_before=2026-10-08&translation=missing&locale=de`);
  expect(screen.queryByRole('button', { name: /^Move Refund rules/ })).not.toBeInTheDocument();
});

test('the filters panel writes the address: tags, any or all, an author, the last day to include, a missing translation', async () => {
  serve(base);
  const show = open({ collection: ids.setup });
  fireEvent.click(await screen.findByText('Filters'));
  const panel = screen.getByRole('group', { name: 'Filters' });
  fireEvent.click(await within(panel).findByRole('checkbox', { name: 'billing (2)' }));
  expect(navigate).toHaveBeenLastCalledWith({ to: '/knowledge', search: { collection: ids.setup, tag: ['billing'] }, replace: false });
  show({ collection: ids.setup, tag: ['billing'] });
  fireEvent.click(within(panel).getByRole('radio', { name: 'All of these tags' }));
  expect(navigate).toHaveBeenLastCalledWith({ to: '/knowledge', search: { collection: ids.setup, tag: ['billing'], tag_match: 'all' }, replace: false });
  fireEvent.click(within(panel).getByRole('checkbox', { name: 'Include sub-collections' }));
  expect(navigate).toHaveBeenLastCalledWith({ to: '/knowledge', search: { collection: ids.setup, tag: ['billing'], subcollections: 'true' }, replace: false });
  fireEvent.click(await within(panel).findByRole('checkbox', { name: 'Bea' }));
  expect(navigate).toHaveBeenLastCalledWith({ to: '/knowledge', search: { collection: ids.setup, tag: ['billing'], author: [bea] }, replace: false });
  fireEvent.change(within(panel).getByLabelText('Updated to'), { target: { value: '2026-10-07' } });
  expect(navigate).toHaveBeenLastCalledWith({ to: '/knowledge', search: { collection: ids.setup, tag: ['billing'], updated_before: '2026-10-08' }, replace: false });
  show({ collection: ids.setup, updated_before: '2026-10-08' });
  expect(within(panel).getByLabelText('Updated to')).toHaveValue('2026-10-07');
  fireEvent.change(within(panel).getByRole('combobox', { name: 'Translation' }), { target: { value: 'missing' } });
  expect(navigate).toHaveBeenLastCalledWith({ to: '/knowledge', search: { collection: ids.setup, updated_before: '2026-10-08', translation: 'missing' }, replace: false });
  fireEvent.click(within(panel).getByRole('button', { name: 'Clear filters' }));
  expect(navigate).toHaveBeenLastCalledWith({ to: '/knowledge', search: { collection: ids.setup }, replace: false });
});

test('a knowledge base view saves the filters on screen and opens them again', async () => {
  const view = { id: '12121212-1212-4212-8212-121212121212', name: 'Published FAQ', area: 'kb', scope: 'shared', owner_agent_id: null,
    filters: { status: 'published', tag: ['faq'] }, created_at: '2026-10-01T00:00:00Z', updated_at: '2026-10-01T00:00:00Z', deleted_at: null, purges_at: null };
  const calls = serve({ ...base,
    'GET /api/v1/saved-views': () => json(200, { items: [view] }),
    'POST /api/v1/saved-views': (call) => json(201, { ...view, id: '13131313-1313-4313-8313-131313131313', scope: 'personal', ...(call.body as object) }) });
  open({ status: 'draft', author: [bea] });
  fireEvent.click(await screen.findByText('Views'));
  const menu = screen.getByRole('group', { name: 'Views' });
  fireEvent.click(await within(menu).findByRole('button', { name: 'Published FAQ' }));
  expect(navigate).toHaveBeenLastCalledWith({ to: '/knowledge', search: { status: 'published', tag: ['faq'] }, replace: false });
  expect(calls.find((call) => call.path === '/api/v1/saved-views')?.search.get('area')).toBe('kb');
  fireEvent.change(within(menu).getByRole('textbox', { name: 'View name' }), { target: { value: 'My drafts' } });
  fireEvent.click(within(menu).getByRole('button', { name: 'Save view' }));
  await waitFor(() => expect(calls.find((call) => call.method === 'POST')?.body).toEqual({ name: 'My drafts', scope: 'personal', area: 'kb', filters: { status: 'draft', author: [bea] } }));
});
