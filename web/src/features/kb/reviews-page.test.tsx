import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { ReviewsRoute } from './reviews-page';
import { json, me, serve, summary } from './kb.test.fixture';

vi.mock('@tanstack/react-router', () => ({ useNavigate: () => vi.fn() }));
afterEach(() => { vi.unstubAllGlobals(); });

const bo = { agent_id: '99999999-9999-4999-8999-999999999991', name: 'Bo' };
const review = (id: string, title: string, fields: Record<string, unknown> = {}) => ({
  id, article_id: summary(7, title).id, article_title: title, locale: null, state: 'open', note: '', content_hash: 'a'.repeat(64), current: true,
  reviewers: [{ agent_id: me.agent.id, name: 'Ada', seen_at: null }], requested_by: bo, requested_by_token: null, decided_by: null, decided_by_token: null,
  decided_at: null, decision_note: '', supersedes_review_id: null, created_at: '2026-10-02T00:00:00Z', ...fields,
});
const first = review('aaaaaaaa-aaaa-4aaa-8aaa-000000000001', 'Reset your password');
const second = review('aaaaaaaa-aaaa-4aaa-8aaa-000000000002', 'Export your data', { locale: 'fr',
  requested_by: { agent_id: bo.agent_id, name: 'Bo' }, requested_by_token: { id: 'bbbbbbbb-bbbb-4bbb-8bbb-000000000001', label: 'Claude laptop' } });

function open() {
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><ReviewsRoute /></QueryClientProvider>);
}

test('Waiting for you lists what I review, and Approve answers for each one it approved or refused', async () => {
  const calls = serve({
    'GET /api/v1/auth/me': () => json(200, me),
    'GET /api/v1/kb/reviews': () => json(200, { reviews: [first, second] }),
    'POST /api/v1/kb/reviews/approve': () => json(200, { results: [{ id: first.id, review: { ...first, state: 'approved' } },
      { id: second.id, error: { code: 'not_reviewer', message: 'A token of the person who asked for this review cannot approve it.' } }] }),
  });
  open();
  const list = await screen.findByRole('list', { name: 'Waiting for you' });
  expect(within(list).getByRole('link', { name: 'Reset your password' })).toHaveAttribute('href', `/knowledge/reviews/${first.id}`);
  expect(within(list).getByText('Translation · fr')).toBeInTheDocument();
  expect(within(list).getByText('Written by an AI agent through Claude laptop')).toBeInTheDocument();
  const asked = calls.find((call) => call.path === '/api/v1/kb/reviews');
  expect(asked?.search.get('state')).toBe('open');
  expect(asked?.search.get('reviewer')).toBe('me');
  expect(screen.getByRole('link', { name: /Reviews/ })).toHaveAttribute('aria-current', 'page');

  fireEvent.click(screen.getByRole('checkbox', { name: 'Select all' }));
  fireEvent.click(screen.getByRole('button', { name: 'Approve 2' }));
  await waitFor(() => expect(calls.find((call) => call.method === 'POST')?.body).toEqual({ review_ids: [first.id, second.id] }));
  expect(calls.find((call) => call.method === 'POST')?.headers['Idempotency-Key']).toBeTruthy();
  expect(await screen.findByText('1 done, 1 refused')).toBeInTheDocument();
  expect(screen.getByText('Export your data: A token of the person who asked for this review cannot approve it.')).toBeInTheDocument();
});

test('Approved lists the articles approved as they are, and Publish puts the chosen ones live', async () => {
  const ready = { ...summary(9, 'Close your account'), has_draft: false };
  const calls = serve({
    'GET /api/v1/auth/me': () => json(200, me),
    'GET /api/v1/kb/reviews': () => json(200, { reviews: [] }),
    'GET /api/v1/kb/articles': () => json(200, { items: [ready], next_cursor: null }),
    'POST /api/v1/kb/articles/publish': () => json(200, { results: [{ id: ready.id, error: { code: 'approval_required', message: 'The text changed since it was approved.' } }] }),
  });
  open();
  expect(await screen.findByText('Nothing waits for your review.')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('radio', { name: 'Approved' }));
  const list = await screen.findByRole('list', { name: 'Approved' });
  expect(calls.find((call) => call.path === '/api/v1/kb/articles')?.search.get('state')).toBe('approved');
  fireEvent.click(within(list).getByRole('checkbox', { name: 'Select Close your account' }));
  fireEvent.click(screen.getByRole('button', { name: 'Publish 1' }));
  await waitFor(() => expect(calls.find((call) => call.method === 'POST')?.body).toEqual({ article_ids: [ready.id] }));
  expect(await screen.findByText('0 done, 1 refused')).toBeInTheDocument();
  expect(screen.getByText('Close your account: The text changed since it was approved.')).toBeInTheDocument();
});

test('In review lists every open review with who sent it and who reviews it', async () => {
  const calls = serve({ 'GET /api/v1/auth/me': () => json(200, me), 'GET /api/v1/kb/reviews': (call) => json(200, { reviews: call.search.get('reviewer') ? [] : [first] }) });
  open();
  fireEvent.click(await screen.findByRole('radio', { name: 'In review' }));
  const list = await screen.findByRole('list', { name: 'In review' });
  expect(within(list).getByText('Sent by Bo')).toBeInTheDocument();
  expect(within(list).getByText('Reviewers: Ada')).toBeInTheDocument();
  expect(calls.some((call) => call.path === '/api/v1/kb/reviews' && call.search.get('state') === 'open' && !call.search.has('reviewer'))).toBe(true);
});
