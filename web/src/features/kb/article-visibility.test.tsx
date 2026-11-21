import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { ArticleRoute } from './article-editor';
import { article, collections, ids, json, me, refusal, serve } from './kb.test.fixture';

const navigate = vi.hoisted(() => vi.fn());
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => navigate, useBlocker: vi.fn() }));
vi.mock('./editor/codemirror-editor', () => import('./editor/fake-editor.test.fixture'));
afterEach(() => { navigate.mockReset(); vi.unstubAllGlobals(); });

function open(props: { articleId?: string; collectionId?: string }) {
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><ArticleRoute {...props} /></QueryClientProvider>);
}

const path = `/api/v1/kb/articles/${ids.article}`;
const noVotes = { article_id: ids.article, helpful: 0, not_helpful: 0, imported: { helpful: 0, not_helpful: 0 }, votes: [], resets: [] };
const base = { 'GET /api/v1/auth/me': () => json(200, me), 'GET /api/v1/kb/collections': () => json(200, { items: collections }),
  [`GET ${path}/votes`]: () => json(200, noVotes) };

test('who can see this offers three choices with one line each, and a saved article applies the choice at once', async () => {
  const calls = serve({ ...base, [`GET ${path}`]: () => json(200, article({ status: 'published' })),
    [`PATCH ${path}`]: () => json(200, article({ status: 'published', visibility: 'agents', effective_visibility: 'agents', updated_at: '2026-10-03T00:00:00Z' })) });
  open({ articleId: ids.article });
  const box = await screen.findByRole('radiogroup', { name: 'Who can see this' });
  expect(within(box).getByRole('radio', { name: /^Everyone/ })).toBeChecked();
  expect(within(box).getByText('The help center, help chat and your team.')).toBeInTheDocument();
  expect(within(box).getByText('Help chat quotes it without a link. Never on the help center.')).toBeInTheDocument();
  expect(within(box).getByText('Only your team: reply suggestions and AI drafts, never linked.')).toBeInTheDocument();
  fireEvent.click(within(box).getByRole('radio', { name: /^Agents only/ }));
  await waitFor(() => expect(calls.filter((call) => call.method === 'PATCH')).toHaveLength(1));
  expect(calls.find((call) => call.method === 'PATCH')?.body).toEqual({ updated_at: '2026-10-02T00:00:00Z', visibility: 'agents' });
  await waitFor(() => expect(within(box).getByRole('radio', { name: /^Agents only/ })).toBeChecked());
  expect(await screen.findByText('Only your team can see this article now.')).toBeInTheDocument();
});

test('an article a collection restricts says which collection does', async () => {
  serve({ ...base, [`GET ${path}`]: () => json(200, article({ visibility: 'everyone', effective_visibility: 'agents',
    visibility_from: { id: ids.setup, name: 'Internal', visibility: 'agents' } })) });
  open({ articleId: ids.article });
  const box = await screen.findByRole('radiogroup', { name: 'Who can see this' });
  expect(within(box).getByRole('radio', { name: /^Everyone/ })).toBeChecked();
  expect(within(box).getByText('Agents only because of Internal.')).toBeInTheDocument();
});

test('a raise the server refuses says why and keeps the choice as it was', async () => {
  const reason = '1 article would become public without an approved review: 55555555-5555-4555-8555-555555555555.';
  serve({ ...base, [`GET ${path}`]: () => json(200, article({ status: 'published', visibility: 'agents', effective_visibility: 'agents' })),
    [`PATCH ${path}`]: () => refusal(409, 'approval_required', reason) });
  open({ articleId: ids.article });
  const box = await screen.findByRole('radiogroup', { name: 'Who can see this' });
  fireEvent.click(within(box).getByRole('radio', { name: /^Everyone/ }));
  expect(await screen.findByRole('alert')).toHaveTextContent(reason);
  expect(within(box).getByRole('radio', { name: /^Agents only/ })).toBeChecked();
});

test('a new article is created with the choice it was given', async () => {
  const calls = serve({ ...base, 'POST /api/v1/kb/articles': () => json(201, article({ visibility: 'help_chat', effective_visibility: 'help_chat' })) });
  open({ collectionId: ids.setup });
  fireEvent.change(await screen.findByRole('textbox', { name: 'Title' }), { target: { value: 'Reset your password' } });
  fireEvent.click(within(screen.getByRole('radiogroup', { name: 'Who can see this' })).getByRole('radio', { name: /^Help chat only/ }));
  fireEvent.click(screen.getByRole('button', { name: 'Save draft' }));
  await waitFor(() => expect(calls.find((call) => call.method === 'POST')).toBeDefined());
  expect(calls.find((call) => call.method === 'POST')?.body).toEqual({ collection_id: ids.setup, title: 'Reset your password', body_md: '', status: 'draft', visibility: 'help_chat' });
});
