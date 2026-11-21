import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { KnowledgeRoute } from './knowledge-page';
import type { KnowledgeSearch } from './knowledge-search';
import { collections, ids, json, me, serve, summary } from './kb.test.fixture';
import { permissionsOf } from '../../test-roles';
import { PermissionsContext } from '../../lib/permissions-context';

vi.mock('@tanstack/react-router', () => ({ useNavigate: () => vi.fn() }));
afterEach(() => vi.unstubAllGlobals());

const operation = '12345678-1234-4234-8234-123456789012';
const refund = summary(1, 'Refund rules');
const invoices = summary(2, 'Invoices');
const declined = summary(3, 'Card declined');
const base = {
  'GET /api/v1/auth/me': () => json(200, { ...me, kb_list_columns: { columns: ['status', 'tags', 'updated'], default: true } }),
  'GET /api/v1/kb/collections': () => json(200, { items: collections }),
  'GET /api/v1/kb/articles': () => json(200, { items: [refund, invoices, declined], next_cursor: null }),
  'GET /api/v1/kb/tags': () => json(200, { items: [{ tag: 'billing', article_count: 2 }] }),
};

function open(search: KnowledgeSearch = {}, permissions: readonly string[] = permissionsOf('admin')) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><PermissionsContext.Provider value={permissions}>
    <KnowledgeRoute search={search} /></PermissionsContext.Provider></QueryClientProvider>);
}

test('the person ticks two articles, adds a tag to both, and Undo in the toast takes it off again', async () => {
  const calls = serve({ ...base,
    'POST /api/v1/kb/articles/bulk': () => json(200, {
      operation: { id: operation, action: 'tags_add', undo_until: '2026-11-22T10:10:00Z' }, counts: { done: 2, unchanged: 0, refused: 0 },
      results: [{ id: refund.id, number: 1, title: 'Refund rules', outcome: 'done', undoable: true }, { id: declined.id, number: 3, title: 'Card declined', outcome: 'done', undoable: true }],
    }),
    [`POST /api/v1/kb/bulk-operations/${operation}/undo`]: () => json(200, {
      operation: { id: operation, action: 'tags_add', undone_at: '2026-11-22T10:01:00Z' }, counts: { undone: 2, not_undone: 0 },
      results: [{ id: refund.id, number: 1, title: 'Refund rules', outcome: 'undone' }, { id: declined.id, number: 3, title: 'Card declined', outcome: 'undone' }],
    }) });
  open();
  fireEvent.click(await screen.findByRole('checkbox', { name: 'Select Refund rules' }));
  fireEvent.click(screen.getByRole('checkbox', { name: 'Select Card declined' }));
  const bar = screen.getByRole('group', { name: 'Selected articles' });
  expect(bar).toHaveTextContent('2 selected');
  fireEvent.click(within(bar).getByLabelText('Add tag', { selector: 'summary' }));
  const adding = within(bar).getByRole('group', { name: 'Add tag' });
  fireEvent.change(within(adding).getByRole('textbox', { name: 'New tag' }), { target: { value: 'Refunds ' } });
  fireEvent.click(within(adding).getByRole('button', { name: 'Add' }));
  await waitFor(() => expect(calls.find((call) => call.path === '/api/v1/kb/articles/bulk')?.body)
    .toEqual({ action: 'tags_add', tags: ['refunds'], ids: [refund.id, declined.id] }));
  expect(calls.find((call) => call.path === '/api/v1/kb/articles/bulk')?.headers['Idempotency-Key']).toBeTruthy();
  expect(await screen.findByText('2 articles changed.')).toBeInTheDocument();
  expect(screen.queryByRole('group', { name: 'Selected articles' })).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Undo' }));
  expect(await screen.findByText('2 articles put back.')).toBeInTheDocument();
  expect(calls.filter((call) => call.path === `/api/v1/kb/bulk-operations/${operation}/undo`)).toHaveLength(1);
});

test('select all matching sends the filters, not the rows on screen, and each refusal says why', async () => {
  const calls = serve({ ...base,
    'GET /api/v1/kb/articles': () => json(200, { items: [refund, invoices], next_cursor: 'page-2' }),
    'POST /api/v1/kb/articles/bulk': () => json(200, {
      operation: { id: operation, action: 'publish', undo_until: '2026-11-22T10:10:00Z' }, counts: { done: 1, unchanged: 1, refused: 1 },
      results: [{ id: refund.id, number: 1, title: 'Refund rules', outcome: 'done', undoable: true },
        { id: declined.id, number: 3, title: 'Card declined', outcome: 'unchanged', undoable: false },
        { id: invoices.id, number: 2, title: 'Invoices', outcome: 'refused', undoable: false,
          error: { code: 'approval_required', message: 'This article needs an approved review before it is published.' } }],
    }) });
  open({ collection: ids.setup, tag: ['faq'] });
  fireEvent.click(await screen.findByRole('checkbox', { name: 'Select Refund rules' }));
  const bar = screen.getByRole('group', { name: 'Selected articles' });
  fireEvent.click(within(bar).getByRole('button', { name: 'Select all matching articles' }));
  expect(bar).toHaveTextContent('All matching articles selected');
  fireEvent.click(within(bar).getByRole('button', { name: 'Publish' }));
  await waitFor(() => expect(calls.find((call) => call.path === '/api/v1/kb/articles/bulk')?.body)
    .toEqual({ action: 'publish', filter: { collection_id: ids.setup, tag: ['faq'] } }));
  expect(await screen.findByText('1 article changed, 1 already was, 1 refused. Invoices: This article needs an approved review before it is published.')).toBeInTheDocument();
});

test('the actions follow the permissions: no publish without kb_articles:publish, no ticks without kb_articles:update', async () => {
  const editor = permissionsOf('admin').filter((name) => name !== 'kb_articles:publish' && name !== 'kb_articles:delete');
  serve(base);
  open({}, editor);
  fireEvent.click(await screen.findByRole('checkbox', { name: 'Select Invoices' }));
  const bar = screen.getByRole('group', { name: 'Selected articles' });
  expect(within(bar).getByLabelText('Move to collection', { selector: 'summary' })).toBeInTheDocument();
  expect(within(bar).queryByRole('button', { name: 'Publish' })).not.toBeInTheDocument();
  expect(within(bar).queryByRole('button', { name: 'Approve' })).not.toBeInTheDocument();
  expect(within(bar).queryByRole('button', { name: 'Move to trash' })).not.toBeInTheDocument();
  cleanup();
  const reader = permissionsOf('admin').filter((name) => !name.startsWith('kb_articles:') || name === 'kb_articles:read');
  open({}, reader);
  await screen.findByRole('link', { name: /Invoices/ });
  expect(screen.queryByRole('checkbox', { name: 'Select Invoices' })).not.toBeInTheDocument();
});
