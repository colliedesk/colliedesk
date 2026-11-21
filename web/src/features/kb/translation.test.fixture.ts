import { article, change, collection, ids, json, me } from './kb.test.fixture';

export const translationPath = `/api/v1/kb/articles/${ids.article}/translations/fr`;
// live is a published French translation; its help center page shows "Choisissez Sécurité.".
export const live = (fields: Record<string, unknown> = {}) => ({
  article_id: ids.article, locale: 'fr', title: 'Réinitialiser', slug: 'reinitialiser', status: 'published', published_at: '2026-10-02T00:00:00Z', outdated: false, outdated_at: null,
  created_at: '2026-10-02T00:00:00Z', updated_at: '2026-10-02T00:00:00Z', in_trash: false, has_draft: false, last_change: change, url: 'https://desk.example.com/help/acme/fr/a/7-r',
  body_md: 'Ouvrez **Réglages**.\nChoisissez Sécurité.', seo_title: '', seo_description: '', has_source_html: false, deleted_at: null, language_visible: true, attachments: [],
  original: { id: ids.article, number: 7, locale: 'en', title: 'Reset your password', body_md: 'Open **Settings**.', seo_title: '', seo_description: '',
    status: 'published', collection_id: ids.setup, collection_name: 'Setup', version: 3, updated_at: '2026-10-01T00:00:00Z' },
  draft: null, ...fields,
});
// changed is the same translation with unpublished changes saved on 3 October.
export const changed = (fields: Record<string, unknown> = {}, at = '2026-10-03T00:00:00Z') => live({ has_draft: true, updated_at: at, draft: {
  title: 'Réinitialiser le mot de passe', slug: 'reinitialiser', body_md: 'Ouvrez **Réglages**.\nChoisissez Mot de passe.', seo_title: '', seo_description: '', attachments: [],
  created_at: at, updated_at: at, updated_by: { agent_id: null, name: 'Ada' }, ...fields } });
const center = { enabled: true, title: 'Help', locale: 'en', require_approval: false, content_version: 1, updated_at: null, url: 'https://desk.example.com/help/acme' };
// translationRoutes answers what the translation page reads, with translation as the French one.
export const translationRoutes = (translation: unknown, approval: 'on' | 'inherit' = 'inherit') => ({
  'GET /api/v1/auth/me': () => json(200, me), 'GET /api/v1/help-center': () => json(200, center),
  'GET /api/v1/kb/collections': () => json(200, { items: [{ ...collection(ids.setup, 'Setup', null, 1), require_approval: approval }] }),
  'GET /api/v1/help-center/languages': () => json(200, { primary: 'en', unlisted: [], languages: [{ locale: 'fr', visible: true, position: 1, published: 1, drafts: 0, outdated: 0, missing: 0 }] }),
  [`GET /api/v1/kb/articles/${ids.article}`]: () => json(200, article({ translations: [live()] })),
  [`GET ${translationPath}`]: () => json(200, translation), [`GET ${translationPath}/reviews`]: () => json(200, { reviews: [] }),
});
