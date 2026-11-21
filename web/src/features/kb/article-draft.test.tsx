import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { ArticleRoute } from './article-editor';
import { article, collections, ids, json, me, refusal, serve } from './kb.test.fixture';

const navigate = vi.hoisted(() => vi.fn());
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => navigate, useBlocker: vi.fn() }));
vi.mock('./editor/codemirror-editor', () => import('./editor/fake-editor.test.fixture'));
afterEach(() => { navigate.mockReset(); vi.unstubAllGlobals(); localStorage.clear(); });

const settle = () => act(() => new Promise((resolve) => { setTimeout(resolve, 150); }));
const path = `/api/v1/kb/articles/${ids.article}`;
const base = { 'GET /api/v1/auth/me': () => json(200, me), 'GET /api/v1/kb/collections': () => json(200, { items: collections }) };

// live is a published article; its help center page shows "Choose Security.".
const live = (fields: Record<string, unknown> = {}) => article({ status: 'published', published_at: '2026-10-02T00:00:00Z', body_md: 'Open **Settings**.\nChoose Security.',
  updated_at: '2026-10-02T00:00:00Z', has_draft: false, draft: null, ...fields });
// changed is the same article with unpublished changes saved on 3 October; the article's updated_at,
// the token every write sends, moves with them.
const changed = (fields: Record<string, unknown> = {}, articleFields: Record<string, unknown> = {}) => live({ has_draft: true, updated_at: '2026-10-03T00:00:00Z', ...articleFields, draft: {
  title: 'Reset a password', slug: 'reset-your-password', body_md: 'Open **Settings**.\nChoose Password.', seo_title: '', seo_description: '', collection_id: ids.setup,
  collection_ids: [ids.setup], show_contents: true, attachments: [], created_at: '2026-10-03T00:00:00Z', updated_at: '2026-10-03T00:00:00Z', updated_by: { agent_id: null, name: 'Ada' }, ...fields } });

function open() {
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><ArticleRoute articleId={ids.article} /></QueryClientProvider>);
}

test('a published article with unpublished changes opens on them, says so, and a save sends the version of the changes', async () => {
  const calls = serve({ ...base, [`GET ${path}`]: () => json(200, changed()),
    [`PATCH ${path}`]: () => json(200, changed({ body_md: 'Open **Settings**.\nChoose Password now.', updated_at: '2026-10-04T00:00:00Z' }, { updated_at: '2026-10-04T00:00:00Z' })) });
  open();
  const text = await screen.findByRole('textbox', { name: 'Text (Markdown)' });
  expect(text).toHaveValue('Open **Settings**.\nChoose Password.');
  expect(screen.getByRole('textbox', { name: 'Title' })).toHaveValue('Reset a password');
  expect(screen.getByText('· Unpublished changes')).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();
  fireEvent.change(text, { target: { value: 'Open **Settings**.\nChoose Password now.' } });
  await settle();
  // Publish changes puts what is saved live; edits on screen are saved first.
  expect(screen.getByRole('button', { name: 'Publish changes' })).toBeDisabled();
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  await waitFor(() => expect(calls.filter((call) => call.method === 'PATCH')).toHaveLength(1));
  expect(calls.find((call) => call.method === 'PATCH')?.body).toEqual({ updated_at: '2026-10-03T00:00:00Z', body_md: 'Open **Settings**.\nChoose Password now.' });
  await waitFor(() => expect(screen.getByRole('button', { name: 'Publish changes' })).toBeEnabled());
});

test('the On this page switch shows the unpublished changes, and turning it back on saves against them', async () => {
  const calls = serve({ ...base, [`GET ${path}`]: () => json(200, changed({ show_contents: false })),
    [`PATCH ${path}`]: () => json(200, changed({ show_contents: true, updated_at: '2026-10-04T00:00:00Z' }, { updated_at: '2026-10-04T00:00:00Z' })) });
  open();
  const box = await screen.findByRole('checkbox', { name: 'Show On this page' });
  expect(box).not.toBeChecked();
  fireEvent.click(box);
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  await waitFor(() => expect(calls.filter((call) => call.method === 'PATCH')).toHaveLength(1));
  expect(calls.find((call) => call.method === 'PATCH')?.body).toEqual({ updated_at: '2026-10-03T00:00:00Z', show_contents: true });
});

test('an article taken out of the trash with its changes saves with the article\'s updated_at, not the older one of the changes', async () => {
  const calls = serve({ ...base, [`GET ${path}`]: () => json(200, changed({}, { updated_at: '2026-10-04T00:00:00Z' })),
    [`PATCH ${path}`]: () => json(200, changed({ body_md: 'Open **Settings**.\nChoose Password now.', updated_at: '2026-10-05T00:00:00Z' }, { updated_at: '2026-10-05T00:00:00Z' })) });
  open();
  fireEvent.change(await screen.findByRole('textbox', { name: 'Text (Markdown)' }), { target: { value: 'Open **Settings**.\nChoose Password now.' } });
  await settle();
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  await waitFor(() => expect(calls.filter((call) => call.method === 'PATCH')).toHaveLength(1));
  expect(calls.find((call) => call.method === 'PATCH')?.body).toEqual({ updated_at: '2026-10-04T00:00:00Z', body_md: 'Open **Settings**.\nChoose Password now.' });
});

test('saving a published article without changes yet starts them: the state says so and Publish changes appears', async () => {
  serve({ ...base, [`GET ${path}`]: () => json(200, live()),
    [`PATCH ${path}`]: () => json(200, changed({ title: 'Reset your password', body_md: 'Open **Settings**.\nChoose Password.' })) });
  open();
  const text = await screen.findByRole('textbox', { name: 'Text (Markdown)' });
  expect(screen.queryByRole('button', { name: 'Publish changes' })).not.toBeInTheDocument();
  fireEvent.change(text, { target: { value: 'Open **Settings**.\nChoose Password.' } });
  await settle();
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  expect(await screen.findByRole('button', { name: 'Publish changes' })).toBeEnabled();
  expect(screen.getByText('· Unpublished changes')).toBeInTheDocument();
});

test('Publish changes puts them live under one key with the version of the changes', async () => {
  const calls = serve({ ...base, [`GET ${path}`]: () => json(200, changed()),
    [`POST ${path}/draft/publish`]: () => json(200, live({ title: 'Reset a password', body_md: 'Open **Settings**.\nChoose Password.', updated_at: '2026-10-05T00:00:00Z' })) });
  open();
  fireEvent.click(await screen.findByRole('button', { name: 'Publish changes' }));
  await waitFor(() => expect(calls.filter((call) => call.method === 'POST')).toHaveLength(1));
  const sent = calls.find((call) => call.method === 'POST')!;
  expect(sent.body).toEqual({ updated_at: '2026-10-03T00:00:00Z' });
  expect(sent.headers['Idempotency-Key']).toMatch(/^.{8,}$/);
  await waitFor(() => expect(screen.queryByRole('button', { name: 'Publish changes' })).not.toBeInTheDocument());
  expect(screen.queryByText('· Unpublished changes')).not.toBeInTheDocument();
  expect(screen.getByRole('textbox', { name: 'Text (Markdown)' })).toHaveValue('Open **Settings**.\nChoose Password.');
});

test('Discard draft asks first, then drops the changes and shows the published text again', async () => {
  const calls = serve({ ...base, [`GET ${path}`]: () => json(200, changed()), [`DELETE ${path}/draft`]: () => json(200, live()) });
  open();
  fireEvent.click(await screen.findByRole('button', { name: 'Discard draft' }));
  const asking = screen.getByRole('alertdialog', { name: 'Discard the unpublished changes?' });
  expect(calls.filter((call) => call.method === 'DELETE')).toHaveLength(0);
  fireEvent.click(within(asking).getByRole('button', { name: 'Discard changes' }));
  await waitFor(() => expect(screen.getByRole('textbox', { name: 'Text (Markdown)' })).toHaveValue('Open **Settings**.\nChoose Security.'));
  const sent = calls.find((call) => call.method === 'DELETE')!;
  expect(sent.path).toBe(`${path}/draft`);
  expect(sent.search.get('updated_at')).toBe('2026-10-03T00:00:00Z');
  expect(screen.getByRole('textbox', { name: 'Title' })).toHaveValue('Reset your password');
  expect(screen.queryByRole('button', { name: 'Discard draft' })).not.toBeInTheDocument();
});

test('Compare with published shows the lines the changes remove and add, and a changed title', async () => {
  serve({ ...base, [`GET ${path}`]: () => json(200, changed()) });
  open();
  fireEvent.click(await screen.findByRole('button', { name: 'Compare with published' }));
  const compare = screen.getByRole('dialog', { name: 'Unpublished changes' });
  const lines = within(compare).getAllByRole('listitem').map((line) => line.textContent);
  expect(lines).toEqual(['Title removed: Reset your password', 'Title added: Reset a password', 'Open **Settings**.', 'Removed: Choose Security.', 'Added: Choose Password.']);
});

test('someone else changing the article first stops Publish changes and offers their version', async () => {
  serve({ ...base, [`GET ${path}`]: () => json(200, changed()),
    [`POST ${path}/draft/publish`]: () => refusal(409, 'changed_elsewhere', 'This article was saved by someone else.') });
  open();
  fireEvent.click(await screen.findByRole('button', { name: 'Publish changes' }));
  expect(await screen.findByText(/Someone saved this article after you opened it/)).toBeInTheDocument();
});

test('Publish changes asks about an image without alt text the changes bring in; a save no longer does', async () => {
  const old = '![](https://example.com/old.png)';
  const fresh = `${old}\n\n![](https://example.com/new.png)`;
  const calls = serve({ ...base, [`GET ${path}`]: () => json(200, live({ body_md: old })),
    [`PATCH ${path}`]: () => json(200, changed({ title: 'Reset your password', body_md: fresh })),
    [`POST ${path}/draft/publish`]: () => json(200, live({ body_md: fresh, updated_at: '2026-10-05T00:00:00Z' })) });
  open();
  fireEvent.change(await screen.findByRole('textbox', { name: 'Text (Markdown)' }), { target: { value: fresh } });
  await settle();
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  await waitFor(() => expect(calls.filter((call) => call.method === 'PATCH')).toHaveLength(1));
  expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
  fireEvent.click(await screen.findByRole('button', { name: 'Publish changes' }));
  const asking = screen.getByRole('alertdialog', { name: '2 images have no alt text' });
  expect(asking).toHaveTextContent('Published: an image without alt text goes live at once.');
  expect(calls.filter((call) => call.method === 'POST')).toHaveLength(0);
  fireEvent.click(within(asking).getByRole('button', { name: 'Publish anyway' }));
  await waitFor(() => expect(calls.filter((call) => call.method === 'POST')).toHaveLength(1));
});
