import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { ArticleRoute } from './article-editor';
import { KnowledgeRoute } from './knowledge-page';
import { article, change, collection, collections, ids, json, me, refusal, serve, summary } from './kb.test.fixture';

const navigate = vi.hoisted(() => vi.fn());
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => navigate, useBlocker: vi.fn() }));
vi.mock('./editor/codemirror-editor', () => import('./editor/fake-editor.test.fixture'));
afterEach(() => { navigate.mockReset(); vi.unstubAllGlobals(); });

const howTo = {
  id: '99999999-9999-4999-8999-000000000001', name: 'How-to', description: 'Steps to get one thing done', body: '## Before you start\n\n## Steps\n',
  created_at: '2026-10-02T00:00:00Z', updated_at: '2026-10-02T00:00:00Z', last_change: change,
};
const faq = { ...howTo, id: '99999999-9999-4999-8999-000000000002', name: 'FAQ', description: 'Short answers', body: '## Question\n' };
// Setup starts its new articles from the FAQ template.
const withDefault = collections.map((item) => (item.id === ids.setup ? { ...collection(ids.setup, 'Setup', ids.guides, 2), default_template_id: faq.id } : item));

const base = {
  'GET /api/v1/auth/me': () => json(200, me),
  'GET /api/v1/kb/collections': () => json(200, { items: withDefault }),
  'GET /api/v1/kb/articles': () => json(200, { items: [summary(1, 'Reset your password')], next_cursor: null }),
  'GET /api/v1/kb/templates': () => json(200, { items: [howTo, faq] }),
  [`GET /api/v1/kb/templates/${howTo.id}`]: () => json(200, howTo),
  [`GET /api/v1/kb/templates/${faq.id}`]: () => json(200, faq),
  [`GET /api/v1/kb/articles/${ids.article}/votes`]: () => json(200, { article_id: ids.article, helpful: 0, not_helpful: 0, imported: { helpful: 0, not_helpful: 0 }, votes: [], resets: [] }),
};
const client = () => new QueryClient({ defaultOptions: { queries: { retry: false } } });
const editor = (props: { articleId?: string; collectionId?: string; templateId?: string }) =>
  render(<QueryClientProvider client={client()}><ArticleRoute {...props} /></QueryClientProvider>);

test('New article opens a menu: Blank, the collection default, then the other templates; a pick opens the editor from it', async () => {
  serve(base);
  render(<QueryClientProvider client={client()}><KnowledgeRoute search={{ collection: ids.setup }} /></QueryClientProvider>);
  fireEvent.click(await screen.findByRole('button', { name: 'New article' }));
  const menu = screen.getByRole('menu', { name: 'New article' });
  const items = within(menu).getAllByRole('menuitem').map((item) => item.textContent);
  expect(items).toEqual(['BlankAn empty article', 'FAQDefault for this collection', 'How-toSteps to get one thing done', 'Manage templates']);
  fireEvent.click(within(menu).getByRole('menuitem', { name: /^FAQ/ }));
  expect(navigate).toHaveBeenLastCalledWith({ to: '/knowledge/new', search: { collection: ids.setup, template: faq.id } });
  fireEvent.click(screen.getByRole('button', { name: 'New article' }));
  fireEvent.click(within(screen.getByRole('menu', { name: 'New article' })).getByRole('menuitem', { name: /^Blank/ }));
  expect(navigate).toHaveBeenLastCalledWith({ to: '/knowledge/new', search: { collection: ids.setup, template: 'none' } });
});

test('an article started from a template has its text and no title, and is saved with that text', async () => {
  const calls = serve({ ...base, 'POST /api/v1/kb/articles': () => json(201, article({ title: 'Install', body_md: howTo.body })) });
  editor({ collectionId: ids.setup, templateId: howTo.id });
  expect(await screen.findByRole('textbox', { name: 'Text (Markdown)' })).toHaveValue(howTo.body);
  const title = screen.getByRole('textbox', { name: 'Title' });
  expect(title).toHaveValue('');
  fireEvent.change(title, { target: { value: 'Install' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save draft' }));
  await waitFor(() => expect(calls.some((call) => call.method === 'POST')).toBe(true));
  expect(calls.find((call) => call.method === 'POST')?.body).toEqual({ collection_id: ids.setup, title: 'Install', body_md: howTo.body, status: 'draft' });
});

test('a collection\'s default template starts a new article in it, and Blank starts it empty', async () => {
  const calls = serve(base);
  const first = editor({ collectionId: ids.setup });
  expect(await screen.findByRole('textbox', { name: 'Text (Markdown)' })).toHaveValue(faq.body);
  first.unmount();
  editor({ collectionId: ids.setup, templateId: 'none' });
  expect(await screen.findByRole('textbox', { name: 'Text (Markdown)' })).toHaveValue('');
  expect(calls.filter((call) => call.path.startsWith('/api/v1/kb/templates/'))).toHaveLength(1);
});

test('a template moved to the trash meanwhile says so, and the article starts blank', async () => {
  serve({ ...base, [`GET /api/v1/kb/templates/${howTo.id}`]: () => refusal(404, 'not_found', 'Template not found.') });
  editor({ collectionId: ids.setup, templateId: howTo.id });
  expect(await screen.findByText('That template is in the trash; undelete it or pick another. The article starts blank.')).toBeInTheDocument();
  expect(screen.getByRole('textbox', { name: 'Text (Markdown)' })).toHaveValue('');
});

test('Insert template adds a template\'s text to the article, and is not offered in the preview', async () => {
  const calls = serve({ ...base, [`GET /api/v1/kb/articles/${ids.article}`]: () => json(200, article()),
    [`PATCH /api/v1/kb/articles/${ids.article}`]: () => json(200, article({ body_md: `Open **Settings**.\n\n${faq.body}` })) });
  editor({ articleId: ids.article });
  fireEvent.click(await screen.findByRole('button', { name: 'Insert template' }));
  fireEvent.click(within(screen.getByRole('menu', { name: 'Insert template' })).getByRole('menuitem', { name: /^FAQ/ }));
  expect(screen.getByRole('textbox', { name: 'Text (Markdown)' })).toHaveValue(`Open **Settings**.\n\n${faq.body}`);
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  await waitFor(() => expect(calls.some((call) => call.method === 'PATCH')).toBe(true));
  expect(calls.find((call) => call.method === 'PATCH')?.body).toMatchObject({ body_md: `Open **Settings**.\n\n${faq.body}` });
  fireEvent.click(screen.getByRole('tab', { name: 'Preview' }));
  expect(screen.queryByRole('button', { name: 'Insert template' })).not.toBeInTheDocument();
});

test('a collection\'s Default template is set in its form, says what it does, and is sent only when changed', async () => {
  const setup = withDefault.find((item) => item.id === ids.setup);
  const calls = serve({ ...base, [`PATCH /api/v1/kb/collections/${ids.setup}`]: () => json(200, { ...setup, default_template_id: null }) });
  render(<QueryClientProvider client={client()}><KnowledgeRoute search={{ collection: ids.setup }} /></QueryClientProvider>);
  fireEvent.click(await screen.findByRole('button', { name: 'Edit Setup' }));
  const form = screen.getByRole('form', { name: 'Edit Setup' });
  const select = await within(form).findByRole('combobox', { name: 'Default template' });
  expect(select).toHaveValue(faq.id);
  expect(within(select).getAllByRole('option').map((option) => option.textContent)).toEqual(['None (blank)', 'How-to', 'FAQ']);
  expect(form).toHaveTextContent('New articles in this collection start from it.');
  expect(within(form).getByRole('button', { name: 'About Default template' })).toHaveAccessibleDescription('Deleting the template sets this back to none.');
  fireEvent.change(select, { target: { value: '' } });
  fireEvent.click(within(form).getByRole('button', { name: 'Save collection' }));
  await waitFor(() => expect(calls.find((call) => call.method === 'PATCH')?.body).toEqual({ updated_at: setup?.updated_at, default_template_id: null }));
});
