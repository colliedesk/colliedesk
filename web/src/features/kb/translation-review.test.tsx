import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { TranslationRoute } from './translation-editor';
import { article, change, collection, ids, json, me, serve } from './kb.test.fixture';

vi.mock('@tanstack/react-router', () => ({ useNavigate: () => vi.fn() }));
afterEach(() => { vi.unstubAllGlobals(); });

const path = `/api/v1/kb/articles/${ids.article}/translations/fr`;
const bo = '99999999-9999-4999-8999-999999999991';
// Setup, the article's collection, requires approval before anything in it goes live, translations too.
const approving = [{ ...collection(ids.setup, 'Setup', null, 1), require_approval: 'on' as const }];
const center = { enabled: true, title: 'Help', locale: 'en', require_approval: false, content_version: 1, updated_at: null, url: 'https://desk.example.com/help/acme' };
const translation = {
  article_id: ids.article, locale: 'fr', title: 'Réinitialiser', slug: 'reinitialiser', status: 'draft', published_at: null, outdated: false, outdated_at: null,
  created_at: '2026-10-02T00:00:00Z', updated_at: '2026-10-02T00:00:00Z', in_trash: false, has_draft: false, draft: null, last_change: change, url: 'https://desk.example.com/help/acme/fr/a/7-r',
  body_md: 'Ouvrez **Réglages**.', seo_title: '', seo_description: '', has_source_html: false, deleted_at: null, language_visible: true, attachments: [],
  original: { id: ids.article, number: 7, locale: 'en', title: 'Reset your password', body_md: 'Open **Settings**.', seo_title: '', seo_description: '',
    status: 'published', collection_id: ids.setup, collection_name: 'Setup', version: 3, updated_at: '2026-10-03T00:00:00Z' },
};
const approved = {
  id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', article_id: ids.article, article_title: 'Reset your password', locale: 'fr', state: 'approved', note: '', content_hash: 'a'.repeat(64), current: true,
  reviewers: [{ agent_id: bo, name: 'Bo', seen_at: '2026-10-03T00:00:00Z' }], requested_by: { agent_id: me.agent.id, name: 'Ada' }, requested_by_token: null,
  decided_by: { agent_id: bo, name: 'Bo' }, decided_by_token: null, decided_at: '2026-10-03T00:00:00Z', decision_note: '', supersedes_review_id: null, created_at: '2026-10-02T00:00:00Z',
};
const base = (reviews: unknown[]) => ({
  'GET /api/v1/auth/me': () => json(200, me), 'GET /api/v1/kb/collections': () => json(200, { items: approving }), 'GET /api/v1/help-center': () => json(200, center),
  'GET /api/v1/help-center/languages': () => json(200, { primary: 'en', unlisted: [], languages: [{ locale: 'fr', visible: true, position: 1, published: 0, drafts: 1, outdated: 0, missing: 0 }] }),
  [`GET /api/v1/kb/articles/${ids.article}`]: () => json(200, article({ translations: [translation] })),
  [`GET ${path}`]: () => json(200, translation), [`GET ${path}/reviews`]: () => json(200, { reviews }),
});

function open() {
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><TranslationRoute articleId={ids.article} locale="fr" /></QueryClientProvider>);
}

test('a translation in a collection that requires approval cannot be published until a teammate approves it, and says why', async () => {
  serve(base([]));
  open();
  const reasonText = await screen.findByText('Publishing needs a teammate’s approval: send it for review.');
  const publish = screen.getByRole('button', { name: 'Publish' });
  expect(publish).toBeDisabled();
  expect(publish).toHaveAttribute('aria-describedby', reasonText.id);
  expect(await screen.findByRole('button', { name: 'Send for review' })).toBeEnabled();
});

test('an approved translation is published with one click', async () => {
  const calls = serve({ ...base([approved]), [`PATCH ${path}`]: () => json(200, { ...translation, status: 'published', updated_at: '2026-10-04T00:00:00Z' }) });
  open();
  expect(await screen.findByText('Approved by Bo. Publish it now.')).toBeInTheDocument();
  const publish = screen.getByRole('button', { name: 'Publish' });
  await waitFor(() => expect(publish).toBeEnabled());
  fireEvent.click(publish);
  await waitFor(() => expect(calls.find((call) => call.method === 'PATCH')?.body).toEqual({ updated_at: '2026-10-02T00:00:00Z', status: 'published' }));
});
