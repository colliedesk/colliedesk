import { QueryClient, QueryClientProvider, focusManager } from '@tanstack/react-query';
import { createQueryClient } from '../../query-client';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, expect, test, vi } from 'vitest';
import { PermissionsContext } from '../../lib/permissions-context';
import { permissionsOf } from '../../test-roles';
import { LanguagesRoute } from './languages-page';
import { TranslationRoute } from './translation-editor';
import { article, change, ids, json, me, refusal, serve } from './kb.test.fixture';

const navigate = vi.hoisted(() => vi.fn());
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => navigate }));
afterEach(() => { navigate.mockReset(); vi.unstubAllGlobals(); });

function show(node: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><PermissionsContext.Provider value={permissionsOf('admin')}>{node}</PermissionsContext.Provider></QueryClientProvider>);
}

const languages = (visible = false) => ({
  primary: 'en', unlisted: [],
  languages: [{ locale: 'fr', visible, position: 1, published: 1, drafts: 0, outdated: 1, missing: 2 }],
});

const translation = (fields: Record<string, unknown> = {}) => ({
  article_id: ids.article, locale: 'fr', title: 'Réinitialiser', slug: 'réinitialiser', status: 'draft', published_at: null, outdated: true, outdated_at: '2026-10-03T00:00:00Z',
  created_at: '2026-10-02T00:00:00Z', updated_at: '2026-10-02T00:00:00Z', in_trash: false, last_change: change, url: 'https://desk.example.com/help/acme/fr/a/7-r',
  body_md: 'Ouvrez **Réglages**.', seo_title: '', seo_description: '', has_source_html: false, deleted_at: null, language_visible: true,
  original: { id: ids.article, number: 7, locale: 'en', title: 'Reset your password', body_md: 'Open **Settings**, then Security.', seo_title: '', seo_description: '',
    status: 'published', collection_id: ids.setup, collection_name: 'Setup', version: 3, updated_at: '2026-10-03T00:00:00Z' },
  ...fields,
});

test('a language starts hidden, is shown with one click, and lists what is missing in it', async () => {
  const calls = serve({
    'GET /api/v1/help-center/languages': () => json(200, languages()),
    'PUT /api/v1/help-center/languages': (call) => json(200, { ...languages(true), languages: (call.body as { languages: { locale: string; visible: boolean }[] }).languages
      .map((each, at) => ({ ...each, position: at + 1, published: 1, drafts: 0, outdated: 1, missing: 2 })) }),
    'GET /api/v1/kb/translations': () => json(200, { items: [{ article_id: ids.article, number: 7, title: 'Reset your password', status: 'published', collection_id: ids.setup,
      collection_name: 'Setup', updated_at: '2026-10-02T00:00:00Z', translation: null }], next_cursor: null }),
  });
  show(<LanguagesRoute search={{ locale: 'fr', state: 'missing' }} />);
  const shown = await screen.findByRole('checkbox', { name: 'Show French (fr) to customers' });
  expect(shown).not.toBeChecked();
  fireEvent.click(shown);
  await waitFor(() => expect(calls.find((call) => call.method === 'PUT')?.body).toEqual({ languages: [{ locale: 'fr', visible: true }] }));
  const missing = await screen.findByRole('region', { name: 'Not translated in French (fr)' });
  expect(within(missing).getByRole('link', { name: /Reset your password/ })).toHaveAttribute('href', `/knowledge/articles/${ids.article}/translations/fr`);
  fireEvent.click(screen.getByRole('button', { name: '1 outdated' }));
  expect(navigate).toHaveBeenCalledWith({ to: '/knowledge/languages', search: { locale: 'fr', state: 'outdated' } });
  // A language that still holds translations cannot be removed; hiding it is the way.
  expect(screen.getByRole('button', { name: 'Remove French (fr)' })).toBeDisabled();
});

test('a translation is edited beside its original and marked up to date in the same save', async () => {
  const calls = serve({
    'GET /api/v1/auth/me': () => json(200, me),
    'GET /api/v1/help-center/languages': () => json(200, languages(true)),
    [`GET /api/v1/kb/articles/${ids.article}`]: () => json(200, article({ translations: [translation()] })),
    [`GET /api/v1/kb/articles/${ids.article}/translations/fr`]: () => json(200, translation()),
    [`PATCH /api/v1/kb/articles/${ids.article}/translations/fr`]: () => json(200, translation({ outdated: false, outdated_at: null, updated_at: '2026-10-04T00:00:00Z' })),
  });
  show(<TranslationRoute articleId={ids.article} locale="fr" />);
  const original = await screen.findByRole('region', { name: 'Original, English (en)' });
  expect(within(original).getByText('Reset your password')).toBeInTheDocument();
  expect(screen.getByText('The original changed after this translation was last brought up to date.')).toBeInTheDocument();
  fireEvent.change(screen.getByRole('textbox', { name: 'Text' }), { target: { value: 'Ouvrez **Réglages**, puis Sécurité.' } });
  fireEvent.click(screen.getByRole('checkbox', { name: 'Mark as up to date with the original' }));
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  await waitFor(() => expect(calls.find((call) => call.method === 'PATCH')?.body).toEqual({ updated_at: '2026-10-02T00:00:00Z', body_md: 'Ouvrez **Réglages**, puis Sécurité.', outdated: false }));
});

test('a missing translation starts from the article, and a refusal says why', async () => {
  const calls = serve({
    'GET /api/v1/auth/me': () => json(200, me),
    'GET /api/v1/help-center/languages': () => json(200, languages()),
    [`GET /api/v1/kb/articles/${ids.article}`]: () => json(200, article()),
    [`POST /api/v1/kb/articles/${ids.article}/translations`]: () => refusal(400, 'language_not_listed', 'This language is not one of the help center\'s languages.'),
  });
  show(<TranslationRoute articleId={ids.article} locale="fr" />);
  fireEvent.click(await screen.findByRole('button', { name: 'Start from the original text' }));
  expect(screen.getByRole('textbox', { name: 'Text' })).toHaveValue('Open **Settings**.');
  expect(screen.getByText(/Customers do not see this language yet/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Save translation' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('This language is not one of the help center\'s languages.');
  expect(calls.find((call) => call.method === 'POST')?.body).toEqual({ locale: 'fr', title: 'Reset your password', body_md: 'Open **Settings**.' });
});

test('a translation saved elsewhere while one is typing keeps the typed text and says so', async () => {
  let saved = translation();
  const calls = serve({
    'GET /api/v1/auth/me': () => json(200, me),
    'GET /api/v1/help-center/languages': () => json(200, languages(true)),
    [`GET /api/v1/kb/articles/${ids.article}`]: () => json(200, article({ translations: [saved] })),
    [`GET /api/v1/kb/articles/${ids.article}/translations/fr`]: () => json(200, saved),
    [`PATCH /api/v1/kb/articles/${ids.article}/translations/fr`]: () => json(200, translation({ body_md: 'Mon texte pas encore enregistré.', updated_at: '2026-10-06T00:00:00Z' })),
  });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><PermissionsContext.Provider value={permissionsOf('admin')}><TranslationRoute articleId={ids.article} locale="fr" /></PermissionsContext.Provider></QueryClientProvider>);
  const text = await screen.findByRole('textbox', { name: 'Text' });
  fireEvent.change(text, { target: { value: 'Mon texte pas encore enregistré.' } });
  // Someone else saves; the tab gets the focus back and the query reads it again.
  saved = translation({ body_md: 'Leur version.', updated_at: '2026-10-05T00:00:00Z' });
  await client.refetchQueries();
  await waitFor(() => expect(client.getQueryData(['kb-translation', ids.article, 'fr'])).toMatchObject({ updated_at: '2026-10-05T00:00:00Z' }));
  expect(screen.getByRole('textbox', { name: 'Text' })).toHaveValue('Mon texte pas encore enregistré.');
  expect(await screen.findByRole('alert')).toHaveTextContent('Someone saved this translation after you opened it.');
  // Save mine over theirs: over the version they saved, so nothing of theirs is lost (it is in the history).
  fireEvent.click(screen.getByRole('button', { name: 'Save mine over theirs' }));
  await waitFor(() => expect(calls.find((call) => call.method === 'PATCH')?.body).toEqual({ updated_at: '2026-10-05T00:00:00Z', body_md: 'Mon texte pas encore enregistré.' }));
});

test('a translation saved elsewhere while nothing is typed is shown as saved', async () => {
  let saved = translation();
  serve({
    'GET /api/v1/auth/me': () => json(200, me),
    'GET /api/v1/help-center/languages': () => json(200, languages(true)),
    [`GET /api/v1/kb/articles/${ids.article}`]: () => json(200, article({ translations: [saved] })),
    [`GET /api/v1/kb/articles/${ids.article}/translations/fr`]: () => json(200, saved),
  });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><PermissionsContext.Provider value={permissionsOf('admin')}><TranslationRoute articleId={ids.article} locale="fr" /></PermissionsContext.Provider></QueryClientProvider>);
  await screen.findByRole('textbox', { name: 'Text' });
  saved = translation({ body_md: 'Leur version.', updated_at: '2026-10-05T00:00:00Z' });
  await client.refetchQueries();
  await waitFor(() => expect(screen.getByRole('textbox', { name: 'Text' })).toHaveValue('Leur version.'));
  expect(screen.queryByText(/Someone saved this translation/)).not.toBeInTheDocument();
});

test('a language the server refuses stays in the box with the reason', async () => {
  serve({
    'GET /api/v1/help-center/languages': () => json(200, languages()),
    'PUT /api/v1/help-center/languages': () => refusal(400, 'invalid_request', 'en is the primary language of the help center (the articles\' own language); list only the other languages.'),
  });
  show(<LanguagesRoute search={{}} />);
  const code = await screen.findByRole('textbox', { name: 'Language code' });
  fireEvent.change(code, { target: { value: 'en' } });
  fireEvent.click(screen.getByRole('button', { name: 'Add language' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('en is the primary language');
  expect(code).toHaveValue('en');
});

test('Write and Preview are tabs of one panel, moved between with the arrow keys', async () => {
  serve({
    'GET /api/v1/auth/me': () => json(200, me),
    'GET /api/v1/help-center/languages': () => json(200, languages(true)),
    [`GET /api/v1/kb/articles/${ids.article}`]: () => json(200, article({ translations: [translation()] })),
    [`GET /api/v1/kb/articles/${ids.article}/translations/fr`]: () => json(200, translation()),
  });
  show(<TranslationRoute articleId={ids.article} locale="fr" />);
  const write = await screen.findByRole('tab', { name: 'Write' });
  const panel = screen.getByRole('tabpanel', { name: 'Write' });
  expect(write).toHaveAttribute('aria-controls', panel.id);
  expect(write).toHaveAttribute('tabindex', '0');
  expect(screen.getByRole('tab', { name: 'Preview' })).toHaveAttribute('tabindex', '-1');
  fireEvent.keyDown(write, { key: 'ArrowRight' });
  const preview = screen.getByRole('tab', { name: 'Preview' });
  expect(preview).toHaveAttribute('aria-selected', 'true');
  expect(preview).toHaveFocus();
  expect(screen.getByRole('tabpanel', { name: 'Preview' })).toBeInTheDocument();
});

test('a save shows what was saved even before the page reads the translation again', async () => {
  // The read after the save still answers the version before it (a slow replica, a read in flight).
  serve({
    'GET /api/v1/auth/me': () => json(200, me),
    'GET /api/v1/help-center/languages': () => json(200, languages(true)),
    [`GET /api/v1/kb/articles/${ids.article}`]: () => json(200, article({ translations: [translation()] })),
    [`GET /api/v1/kb/articles/${ids.article}/translations/fr`]: () => json(200, translation()),
    [`PATCH /api/v1/kb/articles/${ids.article}/translations/fr`]: () => json(200, translation({ body_md: 'Enregistré.', updated_at: '2026-10-06T00:00:00Z' })),
  });
  show(<TranslationRoute articleId={ids.article} locale="fr" />);
  const text = await screen.findByRole('textbox', { name: 'Text' });
  fireEvent.change(text, { target: { value: 'Enregistré.' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  await screen.findByText('Saved');
  await new Promise((resolve) => setTimeout(resolve, 50));
  expect(screen.getByRole('textbox', { name: 'Text' })).toHaveValue('Enregistré.');
  expect(screen.queryByText(/Someone saved this translation/)).not.toBeInTheDocument();
});

test('a published translation keeps Save in reach: history, unpublish and delete are in its menu, as on an article', async () => {
  serve({
    'GET /api/v1/auth/me': () => json(200, me),
    'GET /api/v1/help-center/languages': () => json(200, languages(true)),
    [`GET /api/v1/kb/articles/${ids.article}`]: () => json(200, article({ translations: [translation({ status: 'published' })] })),
    [`GET /api/v1/kb/articles/${ids.article}/translations/fr`]: () => json(200, translation({ status: 'published', published_at: '2026-10-02T00:00:00Z' })),
  });
  show(<TranslationRoute articleId={ids.article} locale="fr" />);
  await screen.findByRole('textbox', { name: 'Text' });
  const bar = screen.getByRole('button', { name: 'Save' }).parentElement!;
  expect(within(bar).queryByRole('button', { name: 'Version history' })).not.toBeInTheDocument();
  expect(within(bar).queryByRole('button', { name: 'Delete translation' })).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'More translation actions' }));
  for (const name of ['Version history', 'Unpublish', 'Delete translation']) expect(screen.getByRole('menuitem', { name })).toBeInTheDocument();
});

test('coming back to the tab reads the translation again and says when someone saved it meanwhile', async () => {
  // The app's own cache (15 s fresh): a person back after a few seconds is told too.
  let saved = translation();
  serve({
    'GET /api/v1/auth/me': () => json(200, me),
    'GET /api/v1/help-center/languages': () => json(200, languages(true)),
    [`GET /api/v1/kb/articles/${ids.article}`]: () => json(200, article({ translations: [saved] })),
    [`GET /api/v1/kb/articles/${ids.article}/translations/fr`]: () => json(200, saved),
  });
  const client = createQueryClient();
  render(<QueryClientProvider client={client}><PermissionsContext.Provider value={permissionsOf('admin')}><TranslationRoute articleId={ids.article} locale="fr" /></PermissionsContext.Provider></QueryClientProvider>);
  const text = await screen.findByRole('textbox', { name: 'Text' });
  fireEvent.change(text, { target: { value: 'Mon texte.' } });
  saved = translation({ body_md: 'Leur version.', updated_at: '2026-10-05T00:00:00Z' });
  focusManager.setFocused(false);
  focusManager.setFocused(true);
  expect(await screen.findByRole('alert')).toHaveTextContent('Someone saved this translation after you opened it.');
  expect(screen.getByRole('textbox', { name: 'Text' })).toHaveValue('Mon texte.');
  focusManager.setFocused(undefined);
});

test('languages count one draft as one, and move up and down in the order the help center lists them', async () => {
  const two = { primary: 'en', unlisted: [], languages: [
    { locale: 'fr', visible: true, position: 1, published: 2, drafts: 1, outdated: 0, missing: 0 },
    { locale: 'es', visible: false, position: 2, published: 0, drafts: 2, outdated: 0, missing: 2 }] };
  const calls = serve({
    'GET /api/v1/help-center/languages': () => json(200, two),
    'PUT /api/v1/help-center/languages': () => json(200, { ...two, languages: [two.languages[1], two.languages[0]] }),
  });
  show(<LanguagesRoute search={{}} />);
  expect(await screen.findByText('2 published · 1 draft')).toBeInTheDocument();
  expect(screen.getByText('0 published · 2 drafts')).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Move French (fr) up' })).toBeDisabled();
  fireEvent.click(screen.getByRole('button', { name: 'Move Spanish (es) up' }));
  await waitFor(() => expect(calls.find((call) => call.method === 'PUT')?.body).toEqual({ languages: [{ locale: 'es', visible: false }, { locale: 'fr', visible: true }] }));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Move Spanish (es) up' })).toBeDisabled());
});

// Each row keeps one short line; the how and why sit in the tip next to it.
test('the languages page says what a translation is in one line, and who sees a language in a tip', async () => {
  serve({ 'GET /api/v1/help-center/languages': () => json(200, languages()) });
  show(<LanguagesRoute search={{}} />);
  const section = (await screen.findByRole('heading', { name: 'Languages' })).closest('section') as HTMLElement;
  expect(section).toHaveTextContent('Translations are other languages of the same article.');
  expect(within(section).getByRole('button', { name: 'About Languages' })).toHaveAccessibleDescription('Customers see a language only while you show it, and only its published translations.');
});

test('the primary language says what is written in it in one line, and where to change it in a tip', async () => {
  serve({ 'GET /api/v1/help-center/languages': () => json(200, languages()) });
  show(<LanguagesRoute search={{}} />);
  const primary = (await screen.findByText('Primary language')).closest('div') as HTMLElement;
  expect(primary).toHaveTextContent('The articles are written in it.');
  expect(within(primary).getByRole('button', { name: 'About Primary language' })).toHaveAccessibleDescription('Change it in Settings, Help center.');
});

test('translations in an unlisted language say who sees them in one line, and that they are kept in a tip', async () => {
  serve({ 'GET /api/v1/help-center/languages': () => json(200, { ...languages(), unlisted: ['de'] }) });
  show(<LanguagesRoute search={{}} />);
  const note = (await screen.findByText('Translations in languages the help center does not list')).closest('div') as HTMLElement;
  expect(note).toHaveTextContent('Customers never see them. Add the language to show them.');
  expect(within(note).getByRole('button', { name: 'About Translations in languages the help center does not list' })).toHaveAccessibleDescription('They are kept and can be edited or deleted.');
});

test('a translation of a draft article says it is not public yet in one line', async () => {
  serve({
    'GET /api/v1/auth/me': () => json(200, me),
    'GET /api/v1/help-center/languages': () => json(200, languages(true)),
    [`GET /api/v1/kb/articles/${ids.article}`]: () => json(200, article({ translations: [translation()] })),
    [`GET /api/v1/kb/articles/${ids.article}/translations/fr`]: () => json(200, translation({ original: { ...translation().original, status: 'draft' } })),
  });
  show(<TranslationRoute articleId={ids.article} locale="fr" />);
  expect(await screen.findByText('The article is a draft: not public until it is published.')).toBeInTheDocument();
});

test('deleting a translation says it leaves the help center and the article stays in one line, and that it can be restored in a tip', async () => {
  serve({
    'GET /api/v1/auth/me': () => json(200, me),
    'GET /api/v1/help-center/languages': () => json(200, languages(true)),
    [`GET /api/v1/kb/articles/${ids.article}`]: () => json(200, article({ translations: [translation()] })),
    [`GET /api/v1/kb/articles/${ids.article}/translations/fr`]: () => json(200, translation()),
  });
  show(<TranslationRoute articleId={ids.article} locale="fr" />);
  await screen.findByRole('textbox', { name: 'Text' });
  fireEvent.click(screen.getByRole('button', { name: 'More translation actions' }));
  fireEvent.click(screen.getByRole('menuitem', { name: 'Delete translation' }));
  const dialog = screen.getByRole('alertdialog');
  expect(dialog).toHaveTextContent('It leaves the help center; the article stays.');
  expect(within(dialog).getByRole('button', { name: 'About the trash' })).toHaveAccessibleDescription('It goes to the trash and can be restored for 30 days.');
});
