import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { TranslationRoute } from './translation-editor';
import { ids, json, serve } from './kb.test.fixture';
import { changed, live, translationPath, translationRoutes } from './translation.test.fixture';

vi.mock('@tanstack/react-router', () => ({ useNavigate: () => vi.fn() }));
afterEach(() => { vi.unstubAllGlobals(); });

const path = translationPath;
const base = translationRoutes;

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
