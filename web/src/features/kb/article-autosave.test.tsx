import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { ArticleRoute } from './article-editor';
import { article, collections, ids, json, me, refusal, serve } from './kb.test.fixture';

const navigate = vi.hoisted(() => vi.fn());
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => navigate, useBlocker: vi.fn() }));
vi.mock('./editor/codemirror-editor', () => import('./editor/fake-editor.test.fixture'));
afterEach(() => { navigate.mockReset(); vi.unstubAllGlobals(); localStorage.clear(); });

// The editor tells the page of typing 120 ms after it stops; autosave waits 3 seconds more.
const settle = () => act(() => new Promise((resolve) => { setTimeout(resolve, 150); }));
const wait = (ms: number) => act(() => new Promise((resolve) => { setTimeout(resolve, ms); }));
const slow = { timeout: 4500 };
const articlePath = `/api/v1/kb/articles/${ids.article}`;
const mine = `colliedesk:kb-unsaved:${me.workspace.id}:${me.agent.id}:${ids.article}`;
const noVotes = { article_id: ids.article, helpful: 0, not_helpful: 0, imported: { helpful: 0, not_helpful: 0 }, votes: [], resets: [] };
const base = { 'GET /api/v1/auth/me': () => json(200, me), 'GET /api/v1/kb/collections': () => json(200, { items: collections }),
  [`GET ${articlePath}/votes`]: () => json(200, noVotes) };

function open(props: { articleId?: string; collectionId?: string }) {
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><ArticleRoute {...props} /></QueryClientProvider>);
}
const patches = <T extends { method: string }>(calls: T[]) => calls.filter((call) => call.method === 'PATCH');

test('3 seconds after typing stops the article is autosaved without a version, and Save still adds one', async () => {
  const calls = serve({ ...base, [`GET ${articlePath}`]: () => json(200, article()),
    [`PATCH ${articlePath}`]: (call) => json(200, article({ body_md: (call.body as { body_md: string }).body_md,
      updated_at: patches(calls).length === 1 ? '2026-10-03T00:04:00Z' : '2026-10-03T00:09:00Z' })) });
  open({ articleId: ids.article });
  fireEvent.change(await screen.findByRole('textbox', { name: 'Text (Markdown)' }), { target: { value: 'Open **Settings**, then Security.' } });
  await settle();
  expect(patches(calls)).toHaveLength(0);
  await waitFor(() => expect(patches(calls)).toHaveLength(1), slow);
  expect(patches(calls)[0].body).toEqual({ updated_at: '2026-10-02T00:00:00Z', autosave: true, body_md: 'Open **Settings**, then Security.' });
  expect(await screen.findByText('· Saved Oct 3, 12:04 AM')).toBeInTheDocument();
  // The text is on the server, so leaving loses nothing; Save makes it a version of the history.
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  await waitFor(() => expect(patches(calls)).toHaveLength(2));
  expect(patches(calls)[1].body).toEqual({ updated_at: '2026-10-03T00:04:00Z', body_md: 'Open **Settings**, then Security.' });
}, 10_000);

test('leaving the window or hiding the tab saves at once', async () => {
  let at = 3;
  const calls = serve({ ...base, [`GET ${articlePath}`]: () => json(200, article()),
    [`PATCH ${articlePath}`]: (call) => json(200, article({ body_md: (call.body as { body_md: string }).body_md, updated_at: `2026-10-0${at++}T00:00:00Z` })) });
  open({ articleId: ids.article });
  const text = await screen.findByRole('textbox', { name: 'Text (Markdown)' });
  fireEvent.change(text, { target: { value: 'First' } });
  await settle();
  act(() => { window.dispatchEvent(new Event('blur')); });
  await waitFor(() => expect(patches(calls)).toHaveLength(1), { timeout: 1000 });
  expect(patches(calls)[0].body).toEqual({ updated_at: '2026-10-02T00:00:00Z', autosave: true, body_md: 'First' });
  fireEvent.change(text, { target: { value: 'Second' } });
  await settle();
  Object.defineProperty(document, 'visibilityState', { value: 'hidden', configurable: true });
  try {
    act(() => { document.dispatchEvent(new Event('visibilitychange')); });
    await waitFor(() => expect(patches(calls)).toHaveLength(2), { timeout: 1000 });
  } finally {
    Object.defineProperty(document, 'visibilityState', { value: 'visible', configurable: true });
  }
  expect(patches(calls)[1].body).toEqual({ updated_at: '2026-10-03T00:00:00Z', autosave: true, body_md: 'Second' });
});

test('typed text is kept on this device until the server has it; offline says so and saves when back online', async () => {
  let online = false;
  serve({ ...base, [`GET ${articlePath}`]: () => json(200, article()),
    [`PATCH ${articlePath}`]: (call) => {
      if (!online) throw new TypeError('Failed to fetch');
      return json(200, article({ body_md: (call.body as { body_md: string }).body_md, updated_at: '2026-10-03T00:00:00Z' }));
    } });
  open({ articleId: ids.article });
  fireEvent.change(await screen.findByRole('textbox', { name: 'Text (Markdown)' }), { target: { value: 'My text' } });
  await settle();
  expect(JSON.parse(localStorage.getItem(mine) ?? '{}')).toMatchObject({ title: 'Reset your password', body: 'My text' });
  expect(await screen.findByText('· Offline, kept on this device', undefined, slow)).toBeInTheDocument();
  expect(localStorage.getItem(mine)).not.toBeNull();
  online = true;
  act(() => { window.dispatchEvent(new Event('online')); });
  await waitFor(() => expect(localStorage.getItem(mine)).toBeNull());
}, 10_000);

test('a newer copy kept on this device is offered when the article opens, and Restore puts it back', async () => {
  localStorage.setItem(mine, JSON.stringify({ title: 'Reset it', body: 'Unsaved words', at: '2026-10-02T00:03:00Z' }));
  serve({ ...base, [`GET ${articlePath}`]: () => json(200, article()) });
  open({ articleId: ids.article });
  expect(await screen.findByText('Restore unsaved text from Oct 2, 12:03 AM?')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Restore' }));
  await waitFor(() => expect(screen.getByRole('textbox', { name: 'Text (Markdown)' })).toHaveValue('Unsaved words'));
  expect(screen.getByRole('textbox', { name: 'Title' })).toHaveValue('Reset it');
  expect(screen.queryByText(/Restore unsaved text/)).not.toBeInTheDocument();
});

test('a copy older than the server\'s is dropped, and one kept by another person or workspace is never offered', async () => {
  const other = `colliedesk:kb-unsaved:${me.workspace.id}:99999999-9999-4999-8999-999999999999:${ids.article}`;
  const elsewhere = `colliedesk:kb-unsaved:00000000-0000-4000-8000-000000000000:${me.agent.id}:${ids.article}`;
  localStorage.setItem(mine, JSON.stringify({ title: 'Old', body: 'Old words', at: '2026-10-01T00:00:00Z' }));
  localStorage.setItem(other, JSON.stringify({ title: 'Theirs', body: 'Their words', at: '2026-10-09T00:00:00Z' }));
  localStorage.setItem(elsewhere, JSON.stringify({ title: 'Theirs', body: 'Their words', at: '2026-10-09T00:00:00Z' }));
  serve({ ...base, [`GET ${articlePath}`]: () => json(200, article()) });
  open({ articleId: ids.article });
  await screen.findByRole('textbox', { name: 'Text (Markdown)' });
  await waitFor(() => expect(localStorage.getItem(mine)).toBeNull());
  expect(screen.queryByText(/Restore unsaved text/)).not.toBeInTheDocument();
  expect(localStorage.getItem(other)).not.toBeNull();
  expect(localStorage.getItem(elsewhere)).not.toBeNull();
});

test('an autosave over a newer server copy shows both versions to choose from and autosaves no more', async () => {
  let current = article();
  const calls = serve({ ...base, [`GET ${articlePath}`]: () => json(200, current),
    [`PATCH ${articlePath}`]: () => refusal(409, 'changed_elsewhere', 'This article was saved by someone else.') });
  open({ articleId: ids.article });
  const text = await screen.findByRole('textbox', { name: 'Text (Markdown)' });
  current = article({ body_md: 'Their text', updated_at: '2026-10-03T00:00:00Z' });
  fireEvent.change(text, { target: { value: 'My text' } });
  const alert = await screen.findByRole('alert', undefined, slow);
  expect(alert).toHaveTextContent('Someone saved this article after you opened it.');
  await waitFor(() => expect(within(alert).getByText(/Their text/)).toBeInTheDocument());
  expect(within(alert).getByText(/My text/)).toBeInTheDocument();
  expect(text).toHaveValue('My text');
  fireEvent.change(text, { target: { value: 'My text, longer' } });
  await wait(3500);
  expect(patches(calls)).toHaveLength(1);
}, 12_000);

test('a new article is never autosaved', async () => {
  const calls = serve({ ...base });
  open({ collectionId: ids.setup });
  fireEvent.change(await screen.findByRole('textbox', { name: 'Title' }), { target: { value: 'Reset your password' } });
  fireEvent.change(screen.getByRole('textbox', { name: 'Text (Markdown)' }), { target: { value: '# Steps' } });
  await wait(3500);
  expect(calls.filter((call) => call.method !== 'GET')).toHaveLength(0);
}, 10_000);
