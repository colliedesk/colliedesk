import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { MediaRoute } from './media-page';
import { json, me, serve } from './kb.test.fixture';

vi.mock('@tanstack/react-router', () => ({ useNavigate: () => vi.fn() }));

const copyId = 'aaaaaaaa-aaaa-4aaa-8aaa-000000000021';
const originalId = 'aaaaaaaa-aaaa-4aaa-8aaa-000000000022';
const replacementId = 'aaaaaaaa-aaaa-4aaa-8aaa-000000000023';
const articleId = 'aaaaaaaa-aaaa-4aaa-8aaa-000000000024';
const annotated = (fields: Record<string, unknown> = {}) => ({
  id: copyId, kind: 'image', content_type: 'image/png', size: 2_048, sha256: 'b'.repeat(64), url: `/api/v1/kb/media/${copyId}`, name: 'Settings (annotated)', alt: '',
  width: 640, height: 480, duration_ms: null, codecs: [], thumbnail_url: null, poster_url: null, kept: false, deleted_at: null,
  created_at: '2026-11-01T00:00:00Z', updated_at: '2026-11-01T00:00:00Z', usage: { articles: 1, trashed_articles: 0, older_versions: 0, help_center: [] },
  articles: [{ id: articleId, number: 4, title: 'Billing', status: 'published', in_trash: false, current: true }],
  earlier_versions: [{ replacement_id: replacementId, media_id: originalId, name: 'Settings', url: `/api/v1/kb/media/${originalId}`, article_ids: [articleId], replaced_at: '2026-11-01T00:00:05Z' }],
  ...fields,
});

test('an annotated copy lists the image it replaced as its earlier version, and Undo puts the original back', async () => {
  let undone = false;
  const calls = serve({ 'GET /api/v1/auth/me': () => json(200, me), 'GET /api/v1/kb/media': () => json(200, { items: [annotated()], next_cursor: null }),
    [`GET /api/v1/kb/media/${copyId}/details`]: () => json(200, annotated(undone ? { earlier_versions: [] } : {})),
    [`POST /api/v1/kb/media-replacements/${replacementId}/undo`]: () => {
      undone = true;
      return json(200, { id: replacementId, from_media_id: originalId, to_media_id: copyId, article_ids: [articleId], translations: [], skipped_article_ids: [], in_place: false,
        created_at: '2026-11-01T00:00:05Z', undone_at: '2026-11-01T00:01:00Z' });
    } });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><MediaRoute view="library" search={{ item: copyId }} /></QueryClientProvider>);
  const panel = await screen.findByRole('complementary', { name: 'Settings (annotated)' });
  const earlier = within(panel).getByRole('region', { name: 'Earlier version' });
  expect(within(earlier).getByText('Settings')).toBeInTheDocument();
  expect(within(earlier).getByText('Replaced this in 1 article.')).toBeInTheDocument();
  expect(earlier.querySelector('img')).toHaveAttribute('src', `/api/v1/kb/media/${originalId}`);
  fireEvent.click(within(earlier).getByRole('button', { name: 'Undo' }));
  await waitFor(() => expect(calls.some((call) => call.method === 'POST' && call.path === `/api/v1/kb/media-replacements/${replacementId}/undo`)).toBe(true));
  expect(await within(panel).findByText('Put back.')).toBeInTheDocument();
  await waitFor(() => expect(within(panel).queryByRole('region', { name: 'Earlier version' })).toBeNull());
});

test('an item no replacement put in place of another shows no earlier version', async () => {
  serve({ 'GET /api/v1/auth/me': () => json(200, me), 'GET /api/v1/kb/media': () => json(200, { items: [annotated()], next_cursor: null }),
    [`GET /api/v1/kb/media/${copyId}/details`]: () => json(200, annotated({ earlier_versions: [] })) });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><MediaRoute view="library" search={{ item: copyId }} /></QueryClientProvider>);
  const panel = await screen.findByRole('complementary', { name: 'Settings (annotated)' });
  expect(within(panel).queryByRole('region', { name: 'Earlier version' })).toBeNull();
});
