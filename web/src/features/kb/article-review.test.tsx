import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { roleOf } from '../../test-roles';
import { ArticleRoute } from './article-editor';
import { article, collection, ids, json, me, serve } from './kb.test.fixture';

const navigate = vi.hoisted(() => vi.fn());
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => navigate, useBlocker: vi.fn() }));
vi.mock('./editor/codemirror-editor', () => import('./editor/fake-editor.test.fixture'));
afterEach(() => { navigate.mockReset(); vi.unstubAllGlobals(); });

const path = `/api/v1/kb/articles/${ids.article}`;
const bo = '99999999-9999-4999-8999-999999999991';
const cy = '99999999-9999-4999-8999-999999999992';
const dee = '99999999-9999-4999-8999-999999999993';
// Setup requires approval; Bo and Dee (deactivated) may publish, Cy may not; Ada is the person signed in.
const approving = [collection(ids.guides, 'Guides', null, 1), { ...collection(ids.setup, 'Setup', ids.guides, 2), require_approval: 'on' as const }];
const publisher = roleOf({ id: '00000000-0000-4000-8000-0000000000b1', name: 'Editor', system: null }, ['kb_articles:read', 'kb_articles:publish'],
  { members: [{ id: me.agent.id, name: 'Ada', active: true }, { id: bo, name: 'Bo', active: true }, { id: dee, name: 'Dee', active: false }] });
const writer = roleOf({ id: '00000000-0000-4000-8000-0000000000b2', name: 'Writer', system: null }, ['kb_articles:read', 'kb_articles:update'],
  { members: [{ id: cy, name: 'Cy', active: true }] });
const center = { enabled: true, title: 'Help', locale: 'en', require_approval: false, content_version: 1, updated_at: null, show_tags: false, url: 'https://desk.example.com/help/acme' };
const review = (state: 'open' | 'approved' | 'changes_requested', fields: Record<string, unknown> = {}) => ({
  id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', article_id: ids.article, article_title: 'Reset your password', locale: null, state, note: 'Check the steps', content_hash: 'a'.repeat(64), current: true,
  reviewers: [{ agent_id: bo, name: 'Bo', seen_at: null }], requested_by: { agent_id: me.agent.id, name: 'Ada' }, requested_by_token: null,
  decided_by: state === 'open' ? null : { agent_id: bo, name: 'Bo' }, decided_by_token: null, decided_at: state === 'open' ? null : '2026-10-03T00:00:00Z',
  decision_note: state === 'changes_requested' ? 'Shorter intro' : '', supersedes_review_id: null, created_at: '2026-10-02T00:00:00Z', ...fields,
});
const base = (reviews: unknown[], of = article()) => ({
  'GET /api/v1/auth/me': () => json(200, me), 'GET /api/v1/kb/collections': () => json(200, { items: approving }), 'GET /api/v1/help-center': () => json(200, center),
  'GET /api/v1/roles': () => json(200, { items: [publisher, writer] }), [`GET ${path}`]: () => json(200, of), [`GET ${path}/reviews`]: () => json(200, { reviews }),
});

function open() {
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><ArticleRoute articleId={ids.article} /></QueryClientProvider>);
}

test('in a collection that requires approval, Publish is off and says why until a teammate approves', async () => {
  serve(base([]));
  open();
  expect(await screen.findByText('Publishing needs a teammate’s approval: send it for review.')).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Publish' })).toBeDisabled();
});

test('Send for review offers only active teammates who may publish, not the author, and sends the chosen ones with the note', async () => {
  // The server lists the review once it was sent.
  const calls = serve({ ...base([]), [`POST ${path}/reviews`]: () => json(201, review('open')),
    [`GET ${path}/reviews`]: () => json(200, { reviews: calls.some((call) => call.method === 'POST') ? [review('open')] : [] }) });
  open();
  fireEvent.click(await screen.findByRole('button', { name: 'Send for review' }));
  const dialog = await screen.findByRole('dialog', { name: 'Send for review' });
  const choices = await within(dialog).findAllByRole('checkbox');
  expect(choices.map((box) => box.closest('label')?.textContent)).toEqual(['Bo']);
  fireEvent.click(within(dialog).getByRole('checkbox', { name: 'Bo' }));
  fireEvent.change(within(dialog).getByRole('textbox', { name: 'Note to the reviewers' }), { target: { value: 'Check the steps' } });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Send for review' }));
  await waitFor(() => expect(calls.find((call) => call.method === 'POST')?.body).toEqual({ reviewer_ids: [bo], note: 'Check the steps' }));
  expect(calls.find((call) => call.method === 'POST')?.headers['Idempotency-Key']).toBeTruthy();
  expect(await screen.findByText('Waiting for approval from Bo.')).toBeInTheDocument();
});

test('an approval of the saved text turns Publish on, and Publish puts it live', async () => {
  const calls = serve({ ...base([review('approved')]), [`PATCH ${path}`]: () => json(200, article({ status: 'published', published_at: '2026-10-04T00:00:00Z' })) });
  open();
  expect(await screen.findByText('Approved by Bo. Publish it now.')).toBeInTheDocument();
  const publish = screen.getByRole('button', { name: 'Publish' });
  await waitFor(() => expect(publish).toBeEnabled());
  fireEvent.click(publish);
  await waitFor(() => expect(calls.find((call) => call.method === 'PATCH')?.body).toEqual({ updated_at: '2026-10-02T00:00:00Z', status: 'published' }));
});

test('a reviewer asking for changes shows their line; an open review can be withdrawn', async () => {
  serve(base([review('changes_requested')]));
  open();
  expect(await screen.findByText('Bo asked for changes: Shorter intro')).toBeInTheDocument();
  vi.unstubAllGlobals();
  const calls = serve({ ...base([review('open')]), [`POST /api/v1/kb/reviews/${review('open').id}/withdraw`]: () => json(200, review('open', { state: 'withdrawn' })) });
  open();
  fireEvent.click(await screen.findByRole('button', { name: 'Withdraw' }));
  await waitFor(() => expect(calls.find((call) => call.method === 'POST')?.path).toBe(`/api/v1/kb/reviews/${review('open').id}/withdraw`));
});

test('unpublished changes waiting for a reviewer cannot be published from the changes bar', async () => {
  const live = article({ status: 'published', published_at: '2026-10-02T00:00:00Z', has_draft: true, draft: {
    title: 'Reset a password', slug: 'reset-your-password', body_md: 'Open **Settings**.\nChoose Password.', seo_title: '', seo_description: '', collection_id: ids.setup,
    collection_ids: [ids.setup], show_contents: true, attachments: [], created_at: '2026-10-03T00:00:00Z', updated_at: '2026-10-03T00:00:00Z', updated_by: { agent_id: null, name: 'Ada' }, tags: [] } });
  serve(base([review('open')], live));
  open();
  expect(await screen.findByRole('button', { name: 'Publish changes' })).toBeDisabled();
  expect(screen.getAllByText('Waiting for approval from Bo.').length).toBeGreaterThan(0);
});
