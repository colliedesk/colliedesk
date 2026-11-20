import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { ReviewRoute } from './review-page';
import { ids, json, me, refusal, serve } from './kb.test.fixture';

vi.mock('@tanstack/react-router', () => ({ useNavigate: () => vi.fn() }));
afterEach(() => { vi.unstubAllGlobals(); });

const id = 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa';
const path = `/api/v1/kb/reviews/${id}`;
const content = (body: string) => ({ title: 'Reset your password', slug: 'reset-your-password', body_md: body, seo_title: '', seo_description: '', collection_ids: [ids.setup] });
const detail = (fields: Record<string, unknown> = {}) => ({
  id, article_id: ids.article, article_title: 'Reset your password', locale: null, state: 'open', note: 'Check the steps', content_hash: 'a'.repeat(64), current: true,
  reviewers: [{ agent_id: me.agent.id, name: 'Ada', seen_at: null }], requested_by: { agent_id: '99999999-9999-4999-8999-999999999991', name: 'Bo' },
  requested_by_token: { id: 'bbbbbbbb-bbbb-4bbb-8bbb-000000000001', label: 'Claude laptop' }, decided_by: null, decided_by_token: null, decided_at: null,
  decision_note: '', supersedes_review_id: null, created_at: '2026-10-02T00:00:00Z',
  live: content('Open Settings.\nChoose Password.'), proposed: content('Open Settings.\nChoose Security.'),
  diff: [{ change: 'same', text: 'Open Settings.' }, { change: 'removed', text: 'Choose Password.' }, { change: 'added', text: 'Choose Security.' }], ...fields,
});

function open() {
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><ReviewRoute reviewId={id} /></QueryClientProvider>);
}

test('a review shows the line changes against the live text, who wrote it, and Approve approves it', async () => {
  const calls = serve({ 'GET /api/v1/auth/me': () => json(200, me), [`GET ${path}`]: () => json(200, detail()),
    [`POST ${path}/approve`]: () => json(200, { ...detail(), state: 'approved', decided_by: { agent_id: me.agent.id, name: 'Ada' }, decided_at: '2026-10-03T00:00:00Z' }) });
  open();
  expect(await screen.findByRole('heading', { name: 'Reset your password' })).toBeInTheDocument();
  expect(screen.getByText('Written by an AI agent through Claude laptop')).toBeInTheDocument();
  const diff = screen.getByRole('list', { name: 'Changes' });
  expect(within(diff).getAllByRole('listitem').map((line) => [line.dataset.change, line.textContent])).toEqual([
    ['same', 'Open Settings.'], ['removed', 'removed: Choose Password.'], ['added', 'added: Choose Security.']]);
  expect(screen.getByRole('link', { name: 'Edit' })).toHaveAttribute('href', `/knowledge/articles/${ids.article}`);
  fireEvent.click(await screen.findByRole('button', { name: 'Approve' }));
  await waitFor(() => expect(calls.find((call) => call.method === 'POST')?.path).toBe(`${path}/approve`));
  expect(calls.find((call) => call.method === 'POST')?.headers['Idempotency-Key']).toBeTruthy();
  expect(await screen.findByText('Approved')).toBeInTheDocument();
});

test('Request changes sends one line back to the author', async () => {
  const calls = serve({ 'GET /api/v1/auth/me': () => json(200, me), [`GET ${path}`]: () => json(200, detail({ locale: 'fr' })),
    [`POST ${path}/request-changes`]: () => json(200, { ...detail(), state: 'changes_requested', decision_note: 'Name the menu' }) });
  open();
  expect(await screen.findByRole('link', { name: 'Edit' })).toHaveAttribute('href', `/knowledge/articles/${ids.article}/translations/fr`);
  fireEvent.click(await screen.findByRole('button', { name: 'Request changes' }));
  fireEvent.change(screen.getByRole('textbox', { name: 'What to change' }), { target: { value: 'Name the menu' } });
  fireEvent.click(screen.getByRole('button', { name: 'Send back' }));
  await waitFor(() => expect(calls.find((call) => call.method === 'POST')?.body).toEqual({ note: 'Name the menu' }));
  expect(await screen.findByText('Sent back for changes')).toBeInTheDocument();
});

test('a review of text that changed since says it no longer counts and offers no approval', async () => {
  serve({ 'GET /api/v1/auth/me': () => json(200, me), [`GET ${path}`]: () => json(200, detail({ current: false, state: 'superseded' })) });
  open();
  expect(await screen.findByText('The text changed since: this review no longer counts.')).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Approve' })).not.toBeInTheDocument();
});

test('a review id from another workspace says it is not here', async () => {
  serve({ 'GET /api/v1/auth/me': () => json(200, me), [`GET ${path}`]: () => refusal(404, 'not_found', 'Not found') });
  open();
  expect(await screen.findByText('This review is not in this workspace.')).toBeInTheDocument();
});
