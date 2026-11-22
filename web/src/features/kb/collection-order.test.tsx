import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { KnowledgeRoute } from './knowledge-page';
import { collections, ids, json, me, serve, summary } from './kb.test.fixture';

vi.mock('@tanstack/react-router', () => ({ useNavigate: () => vi.fn() }));
afterEach(() => { vi.unstubAllGlobals(); });

const setup = collections.find((item) => item.id === ids.setup);
const routes = (order: 'manual' | 'title') => ({
  'GET /api/v1/auth/me': () => json(200, me),
  'GET /api/v1/kb/collections': () => json(200, { items: collections.map((item) => item.id === ids.setup ? { ...item, article_order: order } : item) }),
  'GET /api/v1/kb/articles': () => json(200, { items: [summary(1, 'Alpha accounts'), summary(2, 'Bravo billing')], next_cursor: null }),
});

function open() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><KnowledgeRoute search={{ collection: ids.setup }} /></QueryClientProvider>);
}

test('a collection picks how it lists its articles in its form, and sends only that change', async () => {
  const calls = serve({ ...routes('manual'), [`PATCH /api/v1/kb/collections/${ids.setup}`]: () => json(200, { ...setup, article_order: 'title' }) });
  open();
  fireEvent.click(await screen.findByRole('button', { name: 'Edit Setup' }));
  const form = screen.getByRole('form', { name: 'Edit Setup' });
  const select = within(form).getByRole('combobox', { name: 'Article order' });
  expect(select).toHaveValue('manual');
  expect(within(select).getAllByRole('option').map((option) => option.textContent)).toEqual(['By hand', 'Title, A to Z', 'Newest first', 'Recently updated first']);
  fireEvent.change(select, { target: { value: 'title' } });
  fireEvent.click(within(form).getByRole('button', { name: 'Save collection' }));
  await waitFor(() => expect(calls.find((call) => call.method === 'PATCH')?.body).toEqual({ updated_at: setup?.updated_at, article_order: 'title' }));
});

test('a collection listed by title has no buttons to move its articles, and says how it is sorted', async () => {
  serve(routes('title'));
  open();
  expect(await screen.findByText('Alpha accounts')).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Move Alpha accounts down' })).not.toBeInTheDocument();
  expect(screen.getByText('Sorted by title, A to Z. Edit the collection to order it by hand.')).toBeInTheDocument();
});

test('a collection ordered by hand keeps the buttons to move its articles', async () => {
  serve(routes('manual'));
  open();
  expect(await screen.findByRole('button', { name: 'Move Alpha accounts down' })).toBeInTheDocument();
});
