import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { KnowledgeRoute } from './knowledge-page';
import type { KnowledgeSearch } from './knowledge-page';
import { collection, collections, ids, json, me, refusal, serve, summary } from './kb.test.fixture';

const navigate = vi.hoisted(() => vi.fn());
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => navigate }));
afterEach(() => { navigate.mockReset(); vi.unstubAllGlobals(); vi.useRealTimers(); });

function open(search: KnowledgeSearch = {}) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const view = render(<QueryClientProvider client={client}><KnowledgeRoute search={search} /></QueryClientProvider>);
  return (next: KnowledgeSearch) => view.rerender(<QueryClientProvider client={client}><KnowledgeRoute search={next} /></QueryClientProvider>);
}

const base = {
  'GET /api/v1/auth/me': () => json(200, me),
  'GET /api/v1/kb/collections': () => json(200, { items: collections }),
  'GET /api/v1/kb/articles': () => json(200, { items: [summary(1, 'Reset your password', 'published'), summary(2, 'Add a teammate')], next_cursor: null }),
};

test('collections nest three levels, and choosing one lists only its articles', async () => {
  const calls = serve(base);
  const show = open();
  const tree = await screen.findByRole('navigation', { name: 'Collections' });
  const guides = (await within(tree).findByRole('link', { name: /^Guides/ })).closest('li')!;
  const setup = within(guides).getByRole('link', { name: /^Setup/ }).closest('li')!;
  expect(within(setup).getByRole('link', { name: 'Email, 0 articles' })).toBeInTheDocument();
  expect(within(tree).getByRole('link', { name: 'All articles' })).toHaveAttribute('aria-current', 'page');
  expect(await screen.findByRole('link', { name: /Reset your password/ })).toHaveTextContent('Published');
  fireEvent.click(within(tree).getByRole('link', { name: 'Setup, 2 articles' }));
  expect(navigate).toHaveBeenLastCalledWith({ to: '/knowledge', search: { collection: ids.setup }, replace: false });
  show({ collection: ids.setup });
  await waitFor(() => expect(calls.some((call) => call.path === '/api/v1/kb/articles' && call.search.get('collection_id') === ids.setup)).toBe(true));
  expect(within(tree).getByRole('link', { name: 'Setup, 2 articles' })).toHaveAttribute('aria-current', 'page');
  expect(screen.getByRole('button', { name: 'Add a collection inside Setup' })).toBeInTheDocument();
  show({ collection: ids.deep });
  expect(await screen.findByText('Collections go three levels deep.')).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Add a collection inside Email' })).not.toBeInTheDocument();
});

test('the search goes to the address a moment after typing stops, and asks the server with it', async () => {
  const calls = serve(base);
  const show = open();
  const box = await screen.findByRole('searchbox', { name: 'Search articles' });
  fireEvent.change(box, { target: { value: 'pass' } });
  fireEvent.change(box, { target: { value: 'password' } });
  await waitFor(() => expect(navigate).toHaveBeenCalledWith({ to: '/knowledge', search: { q: 'password' }, replace: true }));
  expect(navigate).toHaveBeenCalledTimes(1);
  // A status picked while the search waited keeps it.
  fireEvent.change(box, { target: { value: 'reset' } });
  show({ q: 'password', status: 'draft' });
  await waitFor(() => expect(navigate).toHaveBeenLastCalledWith({ to: '/knowledge', search: { q: 'reset', status: 'draft' }, replace: true }));
  await waitFor(() => expect(calls.some((call) => call.search.get('q') === 'password' && call.search.get('status') === 'draft')).toBe(true));
});

test('a workspace without collections is asked for one first, and the new one opens', async () => {
  const created = collection(ids.billing, 'Billing', null);
  const calls = serve({ ...base, 'GET /api/v1/kb/collections': () => json(200, { items: [] }), 'POST /api/v1/kb/collections': () => json(201, created) });
  open();
  expect(await screen.findByText(/Start with a collection/)).toBeInTheDocument();
  expect(screen.queryByRole('link', { name: 'New article' })).not.toBeInTheDocument();
  const form = screen.getByRole('form', { name: 'New collection' });
  fireEvent.change(within(form).getByRole('textbox', { name: 'Name' }), { target: { value: 'Billing' } });
  fireEvent.click(within(form).getByRole('button', { name: 'Create collection' }));
  await waitFor(() => expect(navigate).toHaveBeenCalledWith({ to: '/knowledge', search: { collection: ids.billing }, replace: false }));
  expect(calls.find((call) => call.method === 'POST')?.body).toEqual({ name: 'Billing', parent_id: null });
});

test('changing a collection\'s address name warns that its old help center addresses stop working', async () => {
  const calls = serve({ ...base, 'PATCH /api/v1/kb/collections/22222222-2222-4222-8222-222222222222': () => json(200, collections[1]) });
  open({ collection: ids.setup });
  fireEvent.click(await screen.findByRole('button', { name: 'Edit Setup' }));
  const form = screen.getByRole('form', { name: 'Edit Setup' });
  expect(within(form).queryByRole('note')).not.toBeInTheDocument();
  fireEvent.change(within(form).getByRole('textbox', { name: 'Address name' }), { target: { value: 'getting-started' } });
  expect(within(form).getByRole('note')).toHaveTextContent('Old addresses are not redirected');
  // Setup holds a collection, so it can only move to the top or stay where it is: not into itself or a third level.
  expect(within(within(form).getByRole('combobox', { name: 'Inside' })).getAllByRole('option').map((option) => option.textContent)).toEqual(['Top level', 'Guides', 'Billing']);
  fireEvent.click(within(form).getByRole('button', { name: 'Save collection' }));
  await waitFor(() => expect(calls.find((call) => call.method === 'PATCH')?.body).toEqual({ updated_at: collections[1].updated_at, slug: 'getting-started' }));
});

test('a collection that still holds articles is not deleted, and the reason is shown', async () => {
  serve({ ...base, 'DELETE /api/v1/kb/collections/22222222-2222-4222-8222-222222222222': () => refusal(409, 'collection_not_empty', 'Move or delete its articles first.') });
  open({ collection: ids.setup });
  fireEvent.click(await screen.findByRole('button', { name: 'Delete Setup' }));
  fireEvent.click(screen.getByRole('button', { name: 'Move to trash' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Move or delete its articles first.');
  expect(navigate).not.toHaveBeenCalled();
});

test('the trash restores an article with the version it shows', async () => {
  const trashed = { ...summary(9, 'Old pricing'), deleted_at: '2026-10-10T00:00:00Z', deleted_by: { agent_id: null, name: 'Ada' }, purges_at: '2026-11-09T00:00:00Z' };
  const calls = serve({ ...base,
    'GET /api/v1/kb/articles/trash': () => json(200, { items: [trashed], next_cursor: null }),
    'GET /api/v1/kb/collections/trash': () => json(200, { items: [] }),
    'POST /api/v1/kb/articles/00000000-0000-4000-8000-000000000009/restore': () => json(200, { ...trashed, body_md: '', has_source_html: false, translations: [], deleted_at: null, ai_exclusion: null }) });
  open();
  fireEvent.click(await screen.findByRole('button', { name: 'Trash' }));
  const panel = await screen.findByRole('region', { name: 'Deleted articles' });
  expect(await screen.findByText('No deleted collections.')).toBeInTheDocument();
  fireEvent.click(await within(panel).findByRole('button', { name: 'Restore Old pricing' }));
  await waitFor(() => expect(calls.find((call) => call.path.endsWith('/restore'))?.body).toEqual({ updated_at: trashed.updated_at }));
});

test('while the help center is off, the page says published articles are not public, and how to turn it on', async () => {
  serve({ ...base, 'GET /api/v1/help-center': () => json(200, { enabled: false, title: '', locale: 'en', content_version: 1, updated_at: null, url: 'https://desk.example.com/help/acme' }) });
  open();
  expect((await screen.findByText('The help center is off.')).closest('[role="status"]')).toHaveTextContent('only agents can read them');
  expect(screen.getByRole('link', { name: 'Turn on in Settings' })).toHaveAttribute('href', '/settings#help-center-heading');
  const row = (await screen.findByRole('link', { name: /Reset your password/ })).closest('li')!;
  expect(row).toHaveTextContent('not public: help center off');
  expect(within(row).queryByRole('link', { name: 'Open Reset your password on the help center' })).not.toBeInTheDocument();
  expect((await screen.findByRole('link', { name: /Add a teammate/ })).closest('li')).toHaveTextContent('agents only');
});

test('while the help center is on, a published article opens on its public page', async () => {
  serve({ ...base, 'GET /api/v1/help-center': () => json(200, { enabled: true, title: '', locale: 'en', content_version: 1, updated_at: null, url: 'https://desk.example.com/help/acme' }) });
  open();
  const link = await screen.findByRole('link', { name: 'Open Reset your password on the help center' });
  expect(link).toHaveAttribute('href', summary(1, 'Reset your password', 'published').url);
  expect(screen.queryByText('The help center is off.')).not.toBeInTheDocument();
});

// Every page shows its title (design.md: IBM Plex Serif page titles); the Knowledge page hid its h1.
test('the Knowledge page shows its title, not only to screen readers', async () => {
  serve(base);
  open();
  const title = await screen.findByRole('heading', { level: 1, name: 'Knowledge base' });
  expect(title).not.toHaveClass('sr-only');
  expect(title).toHaveClass('kb-page-title');
});
