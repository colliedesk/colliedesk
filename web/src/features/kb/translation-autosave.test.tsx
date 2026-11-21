import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { TranslationRoute } from './translation-editor';
import { article, ids, json, refusal, serve } from './kb.test.fixture';
import { changed, live, translationPath as path, translationRoutes as base } from './translation.test.fixture';

vi.mock('@tanstack/react-router', () => ({ useNavigate: () => vi.fn() }));
afterEach(() => { vi.unstubAllGlobals(); });

const wait = (ms: number) => act(() => new Promise((resolve) => { setTimeout(resolve, ms); }));
const slow = { timeout: 4500 };
const typed = 'Ouvrez **Réglages**.\nChoisissez Mot de passe.';
const patches = <T extends { method: string }>(calls: T[]) => calls.filter((call) => call.method === 'PATCH');

function open() {
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><TranslationRoute articleId={ids.article} locale="fr" /></QueryClientProvider>);
}

test('3 seconds after typing stops a published translation is autosaved into its draft without a version, and Save still adds one', async () => {
  const calls = serve({ ...base(live()),
    [`PATCH ${path}`]: () => json(200, changed({ title: 'Réinitialiser' }, patches(calls).length === 1 ? '2026-10-03T00:04:00Z' : '2026-10-03T00:09:00Z')) });
  open();
  fireEvent.change(await screen.findByRole('textbox', { name: 'Text' }), { target: { value: typed } });
  await wait(150);
  expect(patches(calls)).toHaveLength(0);
  await waitFor(() => expect(patches(calls)).toHaveLength(1), slow);
  expect(patches(calls)[0].body).toEqual({ updated_at: '2026-10-02T00:00:00Z', autosave: true, body_md: typed });
  expect(await screen.findByText('· Saved Oct 3, 12:04 AM')).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Publish changes' })).toBeEnabled();
  // The text is on the server; Save makes it a version of the history.
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  await waitFor(() => expect(patches(calls)).toHaveLength(2));
  expect(patches(calls)[1].body).toEqual({ updated_at: '2026-10-03T00:04:00Z', body_md: typed });
}, 10_000);

test('leaving the window saves the typed title and text at once', async () => {
  const calls = serve({ ...base(live()), [`PATCH ${path}`]: () => json(200, changed({}, '2026-10-03T00:04:00Z')) });
  open();
  fireEvent.change(await screen.findByRole('textbox', { name: 'Title' }), { target: { value: 'Réinitialiser le mot de passe' } });
  fireEvent.change(screen.getByRole('textbox', { name: 'Text' }), { target: { value: typed } });
  act(() => { window.dispatchEvent(new Event('blur')); });
  await waitFor(() => expect(patches(calls)).toHaveLength(1), { timeout: 1000 });
  expect(patches(calls)[0].body).toEqual({ updated_at: '2026-10-02T00:00:00Z', autosave: true, title: 'Réinitialiser le mot de passe', body_md: typed });
});

test('an autosave over a newer server copy shows both versions to choose from and autosaves no more', async () => {
  let current = live();
  const calls = serve({ ...base(live()), [`GET ${path}`]: () => json(200, current),
    [`PATCH ${path}`]: () => refusal(409, 'changed_elsewhere', 'This translation was saved by someone else.') });
  open();
  const text = await screen.findByRole('textbox', { name: 'Text' });
  current = live({ body_md: 'Leur texte', updated_at: '2026-10-03T00:00:00Z' });
  fireEvent.change(text, { target: { value: 'Mon texte' } });
  const alert = await screen.findByRole('alert', undefined, slow);
  expect(alert).toHaveTextContent('Someone saved this translation after you opened it.');
  await waitFor(() => expect(within(alert).getByText(/Leur texte/)).toBeInTheDocument());
  expect(text).toHaveValue('Mon texte');
  fireEvent.change(text, { target: { value: 'Mon texte, plus long' } });
  await wait(3500);
  expect(patches(calls)).toHaveLength(1);
}, 12_000);

test('a change of the address alone is never autosaved', async () => {
  const calls = serve({ ...base(live()) });
  open();
  fireEvent.change(await screen.findByRole('textbox', { name: 'Address (slug)' }), { target: { value: 'nouvelle-adresse' } });
  act(() => { window.dispatchEvent(new Event('blur')); });
  await wait(3500);
  expect(calls.filter((call) => call.method !== 'GET')).toHaveLength(0);
}, 10_000);

test('a translation not yet created is never autosaved', async () => {
  const calls = serve({ ...base(live()), [`GET /api/v1/kb/articles/${ids.article}`]: () => json(200, article()) });
  open();
  fireEvent.change(await screen.findByRole('textbox', { name: 'Title' }), { target: { value: 'Réinitialiser' } });
  fireEvent.change(screen.getByRole('textbox', { name: 'Text' }), { target: { value: typed } });
  await wait(3500);
  expect(calls.filter((call) => call.method !== 'GET')).toHaveLength(0);
}, 10_000);
