import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { TranslationRoute } from './translation-editor';
import { article, change, collection, ids, json, me, serve } from './kb.test.fixture';

vi.mock('@tanstack/react-router', () => ({ useNavigate: () => vi.fn() }));
afterEach(() => { vi.unstubAllGlobals(); });

const path = `/api/v1/kb/articles/${ids.article}/translations/fr`;
// live is a published French translation; its help center page shows "Choisissez Sécurité.".
const live = (fields: Record<string, unknown> = {}) => ({
  article_id: ids.article, locale: 'fr', title: 'Réinitialiser', slug: 'reinitialiser', status: 'published', published_at: '2026-10-02T00:00:00Z', outdated: false, outdated_at: null,
  created_at: '2026-10-02T00:00:00Z', updated_at: '2026-10-02T00:00:00Z', in_trash: false, has_draft: false, last_change: change, url: 'https://desk.example.com/help/acme/fr/a/7-r',
  body_md: 'Ouvrez **Réglages**.\nChoisissez Sécurité.', seo_title: '', seo_description: '', has_source_html: false, deleted_at: null, language_visible: true, attachments: [],
  original: { id: ids.article, number: 7, locale: 'en', title: 'Reset your password', body_md: 'Open **Settings**.', seo_title: '', seo_description: '',
    status: 'published', collection_id: ids.setup, collection_name: 'Setup', version: 3, updated_at: '2026-10-01T00:00:00Z' },
  draft: null, ...fields,
});
// changed is the same translation with unpublished changes saved on 3 October.
const changed = (fields: Record<string, unknown> = {}) => live({ has_draft: true, updated_at: '2026-10-03T00:00:00Z', draft: {
  title: 'Réinitialiser le mot de passe', slug: 'reinitialiser', body_md: 'Ouvrez **Réglages**.\nChoisissez Mot de passe.', seo_title: '', seo_description: '', attachments: [],
  created_at: '2026-10-03T00:00:00Z', updated_at: '2026-10-03T00:00:00Z', updated_by: { agent_id: null, name: 'Ada' }, ...fields } });
const center = { enabled: true, title: 'Help', locale: 'en', require_approval: false, content_version: 1, updated_at: null, url: 'https://desk.example.com/help/acme' };
const base = (translation: unknown, approval: 'on' | 'inherit' = 'inherit') => ({
  'GET /api/v1/auth/me': () => json(200, me), 'GET /api/v1/help-center': () => json(200, center),
  'GET /api/v1/kb/collections': () => json(200, { items: [{ ...collection(ids.setup, 'Setup', null, 1), require_approval: approval }] }),
  'GET /api/v1/help-center/languages': () => json(200, { primary: 'en', unlisted: [], languages: [{ locale: 'fr', visible: true, position: 1, published: 1, drafts: 0, outdated: 0, missing: 0 }] }),
  [`GET /api/v1/kb/articles/${ids.article}`]: () => json(200, article({ translations: [live()] })),
  [`GET ${path}`]: () => json(200, translation), [`GET ${path}/reviews`]: () => json(200, { reviews: [] }),
});

function open() {
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><TranslationRoute articleId={ids.article} locale="fr" /></QueryClientProvider>);
}

test('a published translation with unpublished changes opens on them, compares them, and Publish changes puts them live under one key', async () => {
  const calls = serve({ ...base(changed()),
    [`POST ${path}/draft/publish`]: () => json(200, live({ title: 'Réinitialiser le mot de passe', body_md: 'Ouvrez **Réglages**.\nChoisissez Mot de passe.', updated_at: '2026-10-05T00:00:00Z' })) });
  open();
  const text = await screen.findByRole('textbox', { name: 'Text' });
  expect(text).toHaveValue('Ouvrez **Réglages**.\nChoisissez Mot de passe.');
  expect(screen.getByRole('textbox', { name: 'Title' })).toHaveValue('Réinitialiser le mot de passe');
  expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();
  const bar = screen.getByRole('group', { name: 'Unpublished changes' });
  fireEvent.click(within(bar).getByRole('button', { name: 'Compare with published' }));
  const lines = within(screen.getByRole('dialog', { name: 'Unpublished changes' })).getAllByRole('listitem').map((line) => line.textContent);
  expect(lines).toEqual(['Title removed: Réinitialiser', 'Title added: Réinitialiser le mot de passe', 'Ouvrez **Réglages**.', 'Removed: Choisissez Sécurité.', 'Added: Choisissez Mot de passe.']);
  fireEvent.click(screen.getByRole('button', { name: 'Close' }));
  fireEvent.click(within(bar).getByRole('button', { name: 'Publish changes' }));
  await waitFor(() => expect(calls.filter((call) => call.method === 'POST')).toHaveLength(1));
  const sent = calls.find((call) => call.method === 'POST')!;
  expect(sent.path).toBe(`${path}/draft/publish`);
  expect(sent.body).toEqual({ updated_at: '2026-10-03T00:00:00Z' });
  expect(sent.headers['Idempotency-Key']).toMatch(/^.{8,}$/);
  await waitFor(() => expect(screen.queryByRole('group', { name: 'Unpublished changes' })).not.toBeInTheDocument());
  expect(screen.getByRole('textbox', { name: 'Text' })).toHaveValue('Ouvrez **Réglages**.\nChoisissez Mot de passe.');
});

test('saving a published translation keeps the saved changes on screen and offers Publish changes', async () => {
  // The server answers with the draft as saved: only the text changed, the title is the published one.
  const calls = serve({ ...base(live()), [`PATCH ${path}`]: () => json(200, changed({ title: 'Réinitialiser' })) });
  open();
  fireEvent.change(await screen.findByRole('textbox', { name: 'Text' }), { target: { value: 'Ouvrez **Réglages**.\nChoisissez Mot de passe.' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  const publish = await screen.findByRole('button', { name: 'Publish changes' });
  expect(publish).toBeEnabled();
  expect(calls.find((call) => call.method === 'PATCH')?.body).toEqual({ updated_at: '2026-10-02T00:00:00Z', body_md: 'Ouvrez **Réglages**.\nChoisissez Mot de passe.' });
  expect(screen.getByRole('textbox', { name: 'Text' })).toHaveValue('Ouvrez **Réglages**.\nChoisissez Mot de passe.');
  expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();
});

test('Discard draft asks first, then shows the published translation again', async () => {
  const calls = serve({ ...base(changed()), [`DELETE ${path}/draft`]: () => json(200, live({ updated_at: '2026-10-05T00:00:00Z' })) });
  open();
  fireEvent.click(await screen.findByRole('button', { name: 'Discard draft' }));
  fireEvent.click(within(screen.getByRole('alertdialog', { name: 'Discard the unpublished changes?' })).getByRole('button', { name: 'Discard changes' }));
  await waitFor(() => expect(screen.getByRole('textbox', { name: 'Text' })).toHaveValue('Ouvrez **Réglages**.\nChoisissez Sécurité.'));
  expect(calls.find((call) => call.method === 'DELETE')?.search.get('updated_at')).toBe('2026-10-03T00:00:00Z');
  expect(screen.getByRole('textbox', { name: 'Title' })).toHaveValue('Réinitialiser');
  expect(screen.queryByRole('group', { name: 'Unpublished changes' })).not.toBeInTheDocument();
});

test('where approval is required, Publish changes waits for a teammate and says why', async () => {
  serve(base(changed(), 'on'));
  open();
  const why = await screen.findByText('Publishing needs a teammate’s approval: send it for review.', { selector: '.kb-draft-bar small' });
  const publish = within(screen.getByRole('group', { name: 'Unpublished changes' })).getByRole('button', { name: 'Publish changes' });
  expect(publish).toBeDisabled();
  expect(publish).toHaveAttribute('aria-describedby', why.id);
  expect(await screen.findByRole('button', { name: 'Send for review' })).toBeEnabled();
});
