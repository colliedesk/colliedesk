import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { PermissionsContext } from '../../lib/permissions-context';
import { permissionsOf } from '../../test-roles';
import { ArticleRoute } from './article-editor';
import { ArticleSourcesBox } from './article-sources';
import { article, collections, ids, json, me, noStats, serve } from './kb.test.fixture';

vi.mock('@tanstack/react-router', () => ({ useNavigate: () => vi.fn(), useBlocker: vi.fn() }));
vi.mock('./editor/codemirror-editor', () => import('./editor/fake-editor.test.fixture'));
afterEach(() => { vi.unstubAllGlobals(); });

const jam = '00000000-0000-4000-8000-000000000201';
const again = '00000000-0000-4000-8000-000000000202';
const source = {
  gap_id: '00000000-0000-4000-8000-000000000101',
  conversations: [
    { conversation_id: jam, display_ref: '#41', subject: 'Printer jam in tray two' },
    { conversation_id: again, display_ref: '#57', subject: 'Paper jams in tray two again' },
  ],
};

test('an article drafted from a gap lists the conversations it was written from in the editor, each a link to it', async () => {
  const noVotes = { article_id: ids.article, helpful: 0, not_helpful: 0, imported: { helpful: 0, not_helpful: 0 }, votes: [], resets: [] };
  serve({
    'GET /api/v1/auth/me': () => json(200, me), 'GET /api/v1/kb/collections': () => json(200, { items: collections }),
    [`GET /api/v1/kb/articles/${ids.article}/votes`]: () => json(200, noVotes), [`GET /api/v1/kb/articles/${ids.article}/stats`]: () => json(200, noStats),
    [`GET /api/v1/kb/articles/${ids.article}`]: () => json(200, article({ title: 'Clear a paper jam in tray two', gap_source: source })),
  });
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><ArticleRoute articleId={ids.article} /></QueryClientProvider>);
  const box = await screen.findByRole('region', { name: 'Sources' });
  expect(within(box).getByText('AI drafted it from these resolved conversations.')).toBeInTheDocument();
  expect(within(box).getAllByRole('link').map((link) => [link.textContent, link.getAttribute('href')])).toEqual([
    ['#41 Printer jam in tray two', `/conversations/${jam}`],
    ['#57 Paper jams in tray two again', `/conversations/${again}`],
  ]);
});

test('an article written by a person has no Sources', async () => {
  render(<ArticleSourcesBox source={undefined} headingId="sources" />);
  expect(screen.queryByRole('region', { name: 'Sources' })).not.toBeInTheDocument();
});

test('a team member who may not read conversations sees that the draft came from conversations, never which ones', () => {
  const held = permissionsOf('admin').filter((permission) => permission !== 'conversations:read');
  render(<PermissionsContext.Provider value={held}><ArticleSourcesBox source={source} headingId="sources" /></PermissionsContext.Provider>);
  const box = screen.getByRole('region', { name: 'Sources' });
  expect(within(box).queryByRole('link')).not.toBeInTheDocument();
  expect(within(box).queryByText(/tray two/)).not.toBeInTheDocument();
  expect(within(box).getByText('AI drafted it from conversations you may not open.')).toBeInTheDocument();
});

test('a draft no resolved conversation matched says it was written from what readers asked alone', () => {
  render(<ArticleSourcesBox source={{ ...source, conversations: [] }} headingId="sources" />);
  const box = screen.getByRole('region', { name: 'Sources' });
  expect(within(box).queryByRole('link')).not.toBeInTheDocument();
  expect(within(box).getByText('AI drafted it from readers\' questions only.')).toBeInTheDocument();
});
