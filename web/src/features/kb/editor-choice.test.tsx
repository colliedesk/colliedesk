import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { Editor } from '@tiptap/core';
import { afterEach, expect, test, vi } from 'vitest';
import { ArticleRoute } from './article-editor';
import { article, collections, ids, json, me, serve } from './kb.test.fixture';

vi.mock('@tanstack/react-router', () => ({ useNavigate: () => vi.fn(), useBlocker: vi.fn() }));
// The Markdown editor is the field of fake-editor.test.fixture.tsx; the visual editor is the real one.
vi.mock('./editor/codemirror-editor', () => import('./editor/fake-editor.test.fixture'));
afterEach(() => { vi.unstubAllGlobals(); localStorage.clear(); });

// jsdom lays nothing out; ProseMirror measures text ranges to scroll the cursor into view.
if (!('getClientRects' in Range.prototype)) Object.assign(Range.prototype, { getClientRects: () => [], getBoundingClientRect: () => new DOMRect() });
if (!('elementFromPoint' in document)) Object.assign(document, { elementFromPoint: () => null });

// The first import of an editor's chunk can take more than a second while the whole suite runs.
const loaded = { timeout: 5000 };
const settle = () => act(() => new Promise((resolve) => { setTimeout(resolve, 150); }));

function open(editor: 'visual' | 'markdown' | null, body: string) {
  const calls = serve({
    'GET /api/v1/auth/me': () => json(200, { ...me, kb_editor: editor }), 'GET /api/v1/kb/collections': () => json(200, { items: collections }),
    [`GET /api/v1/kb/articles/${ids.article}`]: () => json(200, article({ body_md: body })),
    [`GET /api/v1/kb/articles/${ids.article}/votes`]: () => json(200, { article_id: ids.article, helpful: 0, not_helpful: 0, imported: { helpful: 0, not_helpful: 0 }, votes: [], resets: [] }),
    'PUT /api/v1/agents/me/kb-editor': (call) => json(200, call.body),
  });
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><ArticleRoute articleId={ids.article} /></QueryClientProvider>);
  return calls;
}

test('a member who never chose writes in the visual editor, and choosing Markdown keeps the text and is remembered', async () => {
  const body = 'Steps\n=====\n\n__Open__ Settings.\n';
  const calls = open(null, body);
  const visual = await screen.findByRole('textbox', { name: 'Text' }, loaded);
  expect(visual).toHaveClass('kb-visual');
  expect(visual.querySelector('h1')).toHaveTextContent('Steps');
  expect(screen.getByRole('button', { name: 'Visual' })).toHaveAttribute('aria-pressed', 'true');
  expect(screen.queryByRole('tab', { name: 'Side by side' })).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Markdown' }));
  expect(await screen.findByRole('textbox', { name: 'Text (Markdown)' }, loaded)).toHaveValue(body);
  expect(screen.getByRole('button', { name: 'Markdown' })).toHaveAttribute('aria-pressed', 'true');
  await waitFor(() => expect(calls.find((call) => call.method === 'PUT')?.body).toEqual({ editor: 'markdown' }));
  expect(calls.filter((call) => call.method === 'PATCH')).toEqual([]);
});

test('text written in Markdown shows as itself in the visual editor and comes back the same', async () => {
  const calls = open('markdown', 'Old\n');
  fireEvent.change(await screen.findByRole('textbox', { name: 'Text (Markdown)' }, loaded), { target: { value: '# Steps\n\nOpen **Settings**.\n' } });
  await settle();
  fireEvent.click(screen.getByRole('button', { name: 'Visual' }));
  const visual = await screen.findByRole('textbox', { name: 'Text' }, loaded);
  expect(visual.querySelector('h1')).toHaveTextContent('Steps');
  expect(visual.querySelector('strong')).toHaveTextContent('Settings');
  expect((visual as HTMLElement & { editor: Editor }).editor.can().undo()).toBe(false);
  fireEvent.click(screen.getByRole('button', { name: 'Markdown' }));
  expect(await screen.findByRole('textbox', { name: 'Text (Markdown)' }, loaded)).toHaveValue('# Steps\n\nOpen **Settings**.\n');
  await waitFor(() => expect(calls.filter((call) => call.method === 'PUT').map((call) => call.body)).toEqual([{ editor: 'visual' }, { editor: 'markdown' }]));
});
