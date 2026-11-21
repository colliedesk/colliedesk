import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { ArticleRoute } from './article-editor';
import { article, collections, ids, json, me, serve } from './kb.test.fixture';

const navigate = vi.hoisted(() => vi.fn());
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => navigate, useBlocker: vi.fn() }));
vi.mock('./editor/codemirror-editor', () => import('./editor/fake-editor.test.fixture'));
afterEach(() => { navigate.mockReset(); vi.unstubAllGlobals(); localStorage.clear(); });

const settle = () => act(() => new Promise((resolve) => { setTimeout(resolve, 150); }));
const bea = '99999999-9999-4999-8999-999999999999';
const cy = '66666666-6666-4666-8666-666666666666';
const member = (id: string, name: string, deactivated_at: string | null = null) => ({ id, email: `${name.toLowerCase()}@example.com`, display_name: name, roles: [],
  manageable: true, timezone_override: null, deactivated_at, last_active_at: null, open_conversations: 0 });
const articlePath = `/api/v1/kb/articles/${ids.article}`;
const base = { 'GET /api/v1/auth/me': () => json(200, me), 'GET /api/v1/kb/collections': () => json(200, { items: collections }),
  'GET /api/v1/kb/tags': () => json(200, { items: [{ tag: 'billing', article_count: 2 }, { tag: 'faq', article_count: 1 }] }),
  'GET /api/v1/agents': () => json(200, { items: [member(me.agent.id, 'Ada'), member(bea, 'Bea'), member(cy, 'Cy', '2026-10-01T00:00:00Z')] }) };

function open(props: { articleId?: string; collectionId?: string }) {
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><ArticleRoute {...props} /></QueryClientProvider>);
}

test('tags are added as the server writes them and removed one by one, and Save sends the whole list', async () => {
  const calls = serve({ ...base, [`GET ${articlePath}`]: () => json(200, article({ tags: ['billing'], updated_at: '2026-10-02T00:00:00Z' })),
    [`PATCH ${articlePath}`]: (call) => json(200, article({ ...(call.body as object), updated_at: '2026-10-03T00:00:00Z' })) });
  open({ articleId: ids.article });
  const tags = await screen.findByRole('list', { name: 'Tags' });
  expect(within(tags).getByText('billing')).toBeInTheDocument();
  const input = screen.getByRole('combobox', { name: 'Add a tag' });
  fireEvent.change(input, { target: { value: '  Refund   Policy ' } });
  fireEvent.keyDown(input, { key: 'Enter' });
  expect(within(tags).getByText('refund policy')).toBeInTheDocument();
  expect(input).toHaveValue('');
  // A tag already there is not added twice.
  fireEvent.change(input, { target: { value: 'BILLING' } });
  fireEvent.keyDown(input, { key: 'Enter' });
  expect(within(tags).getAllByText('billing')).toHaveLength(1);
  fireEvent.click(screen.getByRole('button', { name: 'Remove tag billing' }));
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  await waitFor(() => expect(calls.filter((call) => call.method === 'PATCH')).toHaveLength(1));
  expect(calls.find((call) => call.method === 'PATCH')?.body).toEqual({ updated_at: '2026-10-02T00:00:00Z', tags: ['refund policy'] });
});

test('a tag over 40 characters says why and is not added, and the 20th tag is the last', async () => {
  const twenty = Array.from({ length: 19 }, (_, at) => `tag ${String(at + 1).padStart(2, '0')}`);
  serve({ ...base, [`GET ${articlePath}`]: () => json(200, article({ tags: twenty })) });
  open({ articleId: ids.article });
  const input = await screen.findByRole('combobox', { name: 'Add a tag' });
  fireEvent.change(input, { target: { value: 'x'.repeat(41) } });
  fireEvent.keyDown(input, { key: 'Enter' });
  expect(screen.getByText('A tag is at most 40 characters.')).toHaveAttribute('role', 'alert');
  expect(screen.queryByText('x'.repeat(41))).not.toBeInTheDocument();
  fireEvent.change(input, { target: { value: 'last one' } });
  fireEvent.keyDown(input, { key: 'Enter' });
  expect(within(screen.getByRole('list', { name: 'Tags' })).getAllByRole('listitem')).toHaveLength(20);
  expect(screen.getByRole('combobox', { name: 'Add a tag' })).toBeDisabled();
  expect(screen.getByText('20 tags is the most an article can have.')).toBeInTheDocument();
});

test('a published article shows the tags waiting in its unpublished changes', async () => {
  serve({ ...base, [`GET ${articlePath}`]: () => json(200, article({ status: 'published', published_at: '2026-10-02T00:00:00Z', has_draft: true, tags: ['old'],
    draft: { title: 'Reset your password', slug: 'reset-your-password', body_md: 'Open **Settings**.', seo_title: '', seo_description: '', collection_id: ids.setup,
      collection_ids: [ids.setup], show_contents: true, attachments: [], tags: ['faq'], created_at: '2026-10-02T00:00:00Z', updated_at: '2026-10-02T00:00:00Z', updated_by: null } })) });
  open({ articleId: ids.article });
  const tags = await screen.findByRole('list', { name: 'Tags' });
  expect(within(tags).getByText('faq')).toBeInTheDocument();
  expect(within(tags).queryByText('old')).not.toBeInTheDocument();
});

test('a new article is created with its tags', async () => {
  const calls = serve({ ...base, 'POST /api/v1/kb/articles': () => json(201, article({ title: 'Refunds', tags: ['billing'] })) });
  open({ collectionId: ids.setup });
  fireEvent.change(await screen.findByRole('textbox', { name: 'Title' }), { target: { value: 'Refunds' } });
  const input = screen.getByRole('combobox', { name: 'Add a tag' });
  fireEvent.change(input, { target: { value: 'Billing' } });
  fireEvent.keyDown(input, { key: 'Enter' });
  fireEvent.click(screen.getByRole('button', { name: 'Save draft' }));
  await waitFor(() => expect(calls.find((call) => call.method === 'POST')?.body).toEqual({ collection_id: ids.setup, title: 'Refunds', body_md: '', status: 'draft', tags: ['billing'] }));
});

test('the author changes at once to another active member, and an edit in progress stays to be saved', async () => {
  const calls = serve({ ...base,
    [`GET ${articlePath}`]: () => json(200, article({ author_agent_id: me.agent.id, author: { agent_id: me.agent.id, name: 'Ada', left: false }, updated_at: '2026-10-02T00:00:00Z' })),
    [`PATCH ${articlePath}`]: (call) => {
      const sent = call.body as { author_agent_id?: string; body_md?: string };
      return json(200, sent.author_agent_id
        ? article({ author_agent_id: bea, author: { agent_id: bea, name: 'Bea', left: false }, updated_at: '2026-10-03T00:00:00Z' })
        : article({ author_agent_id: bea, author: { agent_id: bea, name: 'Bea', left: false }, body_md: sent.body_md, updated_at: '2026-10-04T00:00:00Z' }));
    } });
  open({ articleId: ids.article });
  const author = await screen.findByRole('combobox', { name: 'Author' });
  await waitFor(() => expect(within(author).getAllByRole('option').map((option) => option.textContent)).toEqual(['Ada', 'Bea']));
  expect(author).toHaveValue(me.agent.id);
  fireEvent.change(screen.getByRole('textbox', { name: 'Text (Markdown)' }), { target: { value: 'Open **Security**.' } });
  await settle();
  fireEvent.change(author, { target: { value: bea } });
  await waitFor(() => expect(calls.filter((call) => call.method === 'PATCH')).toHaveLength(1));
  expect(calls.find((call) => call.method === 'PATCH')?.body).toEqual({ updated_at: '2026-10-02T00:00:00Z', author_agent_id: bea });
  await waitFor(() => expect(screen.getByRole('combobox', { name: 'Author' })).toHaveValue(bea));
  expect(screen.getByRole('textbox', { name: 'Text (Markdown)' })).toHaveValue('Open **Security**.');
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  await waitFor(() => expect(calls.filter((call) => call.method === 'PATCH')).toHaveLength(2));
  expect(calls.filter((call) => call.method === 'PATCH')[1].body).toEqual({ updated_at: '2026-10-03T00:00:00Z', body_md: 'Open **Security**.' });
});

test('an author who left the team is shown as such until someone else is chosen', async () => {
  serve({ ...base, [`GET ${articlePath}`]: () => json(200, article({ author_agent_id: cy, author: { agent_id: cy, name: 'Cy', left: true } })) });
  open({ articleId: ids.article });
  const author = await screen.findByRole('combobox', { name: 'Author' });
  await waitFor(() => expect(within(author).getAllByRole('option').map((option) => option.textContent)).toEqual(['Cy (left)', 'Ada', 'Bea']));
  expect(author).toHaveValue(cy);
});
