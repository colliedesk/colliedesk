import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { MediaRoute } from './media-page';
import { json, me, serve } from './kb.test.fixture';

vi.mock('@tanstack/react-router', () => ({ useNavigate: () => vi.fn() }));
// A file's parts go through XMLHttpRequest (for the progress); here they land at once.
vi.mock('./media-api', async (original) => ({
  ...(await original<typeof import('./media-api')>()),
  sendPart: async (_id: string, part: Blob, first: number, size: number, onSent: (bytes: number) => void) => {
    onSent(part.size);
    return { status: 200, body: { ...upload, size, offset: first + part.size }, offset: String(first + part.size) };
  },
}));

const fileId = 'aaaaaaaa-aaaa-4aaa-8aaa-000000000006';
const newId = 'aaaaaaaa-aaaa-4aaa-8aaa-000000000007';
const replacementId = 'aaaaaaaa-aaaa-4aaa-8aaa-000000000008';
const upload = { id: 'aaaaaaaa-aaaa-4aaa-8aaa-000000000009', kind: 'file', name: 'Warranty form v2.pdf', size: 16, offset: 0, part_size: 8,
  expires_at: '2026-10-22T00:00:00Z', media_id: null, created_by: null, created_at: '2026-10-21T00:00:00Z' };
const pdf = (fields: Record<string, unknown> = {}) => ({
  id: fileId, kind: 'file', content_type: 'application/pdf', size: 40_960, sha256: 'a'.repeat(64), url: `/api/v1/kb/media/${fileId}`, name: 'Warranty form.pdf', alt: '',
  width: null, height: null, duration_ms: null, codecs: [], thumbnail_url: null, poster_url: null, kept: false, deleted_at: null,
  created_at: '2026-10-01T00:00:00Z', updated_at: '2026-10-02T00:00:00Z', usage: { articles: 1, trashed_articles: 0, older_versions: 0, help_center: [] },
  articles: [{ id: 'aaaaaaaa-aaaa-4aaa-8aaa-000000000010', number: 12, title: 'Warranty', status: 'published', in_trash: false, current: true }], earlier_versions: [], ...fields,
});
const replacement = { id: replacementId, from_media_id: fileId, to_media_id: newId, article_ids: [], translations: [], skipped_article_ids: [], in_place: true,
  created_at: '2026-10-03T00:00:00Z', undone_at: null };

function open() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><MediaRoute view="library" search={{ item: fileId }} /></QueryClientProvider>);
}

test('a file\'s panel shows its type, not a picture, and has no alt text or embed', async () => {
  serve({ 'GET /api/v1/auth/me': () => json(200, me), 'GET /api/v1/kb/media': () => json(200, { items: [pdf()], next_cursor: null }),
    [`GET /api/v1/kb/media/${fileId}/details`]: () => json(200, pdf()) });
  open();
  const panel = await screen.findByRole('complementary', { name: 'Warranty form.pdf' });
  expect(within(panel).getByText('PDF')).toBeInTheDocument();
  expect(panel.querySelector('img, video')).toBeNull();
  expect(within(panel).queryByRole('button', { name: 'Copy embed' })).toBeNull();
  expect(within(panel).queryByRole('textbox', { name: /Alt text|Title for screen readers/ })).toBeNull();
  expect(within(panel).getByRole('link', { name: 'Download' })).toHaveAttribute('href', `/api/v1/kb/media/${fileId}?download=1`);
});

test('replacing a file uploads the new version from disk, keeps its address, shows the new size, and Undo puts the earlier one back', async () => {
  let replaced = false;
  const calls = serve({ 'GET /api/v1/auth/me': () => json(200, me), 'GET /api/v1/kb/media': () => json(200, { items: [pdf()], next_cursor: null }),
    [`GET /api/v1/kb/media/${fileId}/details`]: () => json(200, replaced ? pdf({ size: 81_920, name: 'Warranty form v2.pdf', updated_at: '2026-10-03T00:00:00Z' }) : pdf()),
    'POST /api/v1/kb/media-uploads': () => json(201, upload), [`GET /api/v1/kb/media-uploads/${upload.id}`]: () => json(200, upload),
    [`POST /api/v1/kb/media-uploads/${upload.id}/complete`]: () => json(201, pdf({ id: newId, url: `/api/v1/kb/media/${newId}`, name: 'Warranty form v2.pdf', size: 16, articles: undefined })),
    [`POST /api/v1/kb/media/${fileId}/replace`]: () => { replaced = true; return json(200, replacement); },
    [`POST /api/v1/kb/media-replacements/${replacementId}/undo`]: () => json(200, { ...replacement, undone_at: '2026-10-03T00:01:00Z' }) });
  open();
  const panel = await screen.findByRole('complementary', { name: 'Warranty form.pdf' });
  fireEvent.change(within(panel).getByLabelText('Choose the new version'), { target: { files: [new File([new Uint8Array(16)], 'Warranty form v2.pdf', { type: 'application/pdf' })] } });
  expect(await within(panel).findByText('Replaced. The articles that list it offer the new version at the same address.')).toBeInTheDocument();
  expect(calls.find((call) => call.method === 'POST' && call.path === '/api/v1/kb/media-uploads')?.body).toEqual({ kind: 'file', size: 16, name: 'Warranty form v2.pdf', keep: false });
  expect(calls.find((call) => call.path.endsWith('/replace'))?.body).toEqual({ with_media_id: newId });
  await screen.findByRole('complementary', { name: 'Warranty form v2.pdf' });
  fireEvent.click(within(panel).getByRole('tab', { name: 'File' }));
  expect(within(panel).getByText('80.0 KiB')).toBeVisible();
  expect(within(panel).getByRole('link', { name: 'Download' })).toHaveAttribute('href', `/api/v1/kb/media/${fileId}?download=1`);
  fireEvent.click(within(panel).getByRole('button', { name: 'Undo' }));
  await waitFor(() => expect(calls.some((call) => call.path === `/api/v1/kb/media-replacements/${replacementId}/undo`)).toBe(true));
});

test('a new version of a refused type or over 25 MiB says why, and nothing is sent', async () => {
  const calls = serve({ 'GET /api/v1/auth/me': () => json(200, me), 'GET /api/v1/kb/media': () => json(200, { items: [pdf()], next_cursor: null }),
    [`GET /api/v1/kb/media/${fileId}/details`]: () => json(200, pdf()) });
  open();
  const panel = await screen.findByRole('complementary', { name: 'Warranty form.pdf' });
  const input = within(panel).getByLabelText('Choose the new version');
  fireEvent.change(input, { target: { files: [new File(['MZ'], 'setup.exe')] } });
  expect(await within(panel).findByText('setup.exe is not a type an article can list. Files: PDF, DOC, DOCX, XLS, XLSX, PPT, PPTX, ODT, ODS, ODP, CSV, TXT, RTF, ZIP, 7Z, TAR.GZ.')).toBeInTheDocument();
  const big = new File(['x'], 'manual.pdf');
  Object.defineProperty(big, 'size', { value: 26 << 20 });
  fireEvent.change(input, { target: { files: [big] } });
  expect(await within(panel).findByText('manual.pdf is 26.0 MiB, over the 25.0 MiB limit for one file.')).toBeInTheDocument();
  expect(calls.filter((call) => call.method === 'POST')).toEqual([]);
});
