import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { ArticleRoute } from './article-editor';
import { article, collections, ids, json, me, serve } from './kb.test.fixture';

const navigate = vi.hoisted(() => vi.fn());
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => navigate, useBlocker: vi.fn() }));
vi.mock('./editor/codemirror-editor', () => import('./editor/fake-editor.test.fixture'));
afterEach(() => { navigate.mockReset(); vi.unstubAllGlobals(); });

const path = `/api/v1/kb/articles/${ids.article}`;

function open(listed: string[]) {
  const first = article({ collection_ids: listed, collection_id: listed[0] });
  const calls = serve({
    'GET /api/v1/auth/me': () => json(200, me), 'GET /api/v1/kb/collections': () => json(200, { items: collections }),
    [`GET ${path}`]: () => json(200, first),
    [`PATCH ${path}`]: (call) => {
      const sent = call.body as { collection_id?: string; collection_ids?: string[] };
      const ids = sent.collection_ids ?? [sent.collection_id ?? listed[0], ...listed.slice(1).filter((id) => id !== sent.collection_id)];
      return json(200, article({ collection_id: ids[0], collection_ids: ids, updated_at: '2026-10-03T00:00:00Z' }));
    },
  });
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><ArticleRoute articleId={ids.article} /></QueryClientProvider>);
  return calls;
}

const patches = (calls: ReturnType<typeof serve>) => calls.filter((call) => call.method === 'PATCH').map((call) => call.body);

test('an article is also listed in another collection; the save sends every collection, the home first', async () => {
  const calls = open([ids.setup]);
  const add = await screen.findByRole('combobox', { name: 'Also list in' });
  expect(screen.getByRole('combobox', { name: 'Collections' })).toHaveValue(ids.setup);
  // The home is not offered again.
  expect(within(add).queryByRole('option', { name: /Setup/ })).not.toBeInTheDocument();
  fireEvent.change(add, { target: { value: ids.billing } });
  expect(screen.getByRole('button', { name: 'Remove Billing' })).toBeInTheDocument();
  expect(within(add).queryByRole('option', { name: 'Billing' })).not.toBeInTheDocument();
  expect(add).toHaveValue('');
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  await waitFor(() => expect(patches(calls)).toHaveLength(1));
  expect(patches(calls)[0]).toEqual({ updated_at: '2026-10-02T00:00:00Z', collection_ids: [ids.setup, ids.billing] });
  await waitFor(() => expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled());
});

test('removing another collection sends the list without it; the home stays and says it is the home', async () => {
  const calls = open([ids.setup, ids.billing]);
  fireEvent.click(await screen.findByRole('button', { name: 'Remove Billing' }));
  expect(screen.queryByRole('button', { name: 'Remove Billing' })).not.toBeInTheDocument();
  expect(screen.getByRole('combobox', { name: 'Collections' })).toHaveAccessibleDescription(/first one is the home/);
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  await waitFor(() => expect(patches(calls)).toHaveLength(1));
  expect(patches(calls)[0]).toEqual({ updated_at: '2026-10-02T00:00:00Z', collection_ids: [ids.setup] });
});

test('making a listed collection the home takes it out of the other rows and moves the article', async () => {
  const calls = open([ids.setup, ids.billing]);
  fireEvent.change(await screen.findByRole('combobox', { name: 'Collections' }), { target: { value: ids.billing } });
  expect(screen.queryByRole('button', { name: 'Remove Billing' })).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  await waitFor(() => expect(patches(calls)).toHaveLength(1));
  expect(patches(calls)[0]).toEqual({ updated_at: '2026-10-02T00:00:00Z', collection_id: ids.billing });
});
