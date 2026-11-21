import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { ArticleRoute } from './article-editor';
import { article, collections, ids, json, me, refusal, serve, noStats } from './kb.test.fixture';
import type { Call } from './kb.test.fixture';

vi.mock('@tanstack/react-router', () => ({ useNavigate: () => vi.fn(), useBlocker: vi.fn() }));
vi.mock('./editor/codemirror-editor', () => import('./editor/fake-editor.test.fixture'));
// A file's parts go through XMLHttpRequest (for the progress); here they land at once.
vi.mock('./media-api', async (original) => ({
  ...(await original<typeof import('./media-api')>()),
  sendPart: async (_id: string, part: Blob, first: number, size: number, onSent: (bytes: number) => void) => {
    onSent(part.size);
    return { status: 200, body: { ...upload, size, offset: first + part.size }, offset: String(first + part.size) };
  },
}));
afterEach(() => { vi.unstubAllGlobals(); localStorage.clear(); });

const settle = () => act(() => new Promise((resolve) => { setTimeout(resolve, 150); }));
const upload = { id: '77777777-0000-4000-8000-000000000011', kind: 'file', name: 'Warranty form.pdf', size: 12, offset: 0, part_size: 8,
  expires_at: '2026-10-22T00:00:00Z', media_id: null, created_by: null, created_at: '2026-10-21T00:00:00Z' };
const fileId = '77777777-0000-4000-8000-000000000012';
const media = { id: fileId, kind: 'file', content_type: 'application/pdf', size: 12, sha256: 'a'.repeat(64), url: `/api/v1/kb/media/${fileId}`, name: 'Warranty form.pdf',
  alt: '', width: null, height: null, duration_ms: null, codecs: [], thumbnail_url: null, poster_url: null, kept: true, deleted_at: null,
  created_at: new Date().toISOString(), updated_at: new Date().toISOString() };
const listed = (id: string, name: string, size: number) => ({ id, name, content_type: 'application/pdf', size, url: `/api/v1/kb/media/${id}` });
const warranty = listed(fileId, 'Warranty form.pdf', 12);
const first = listed('77777777-0000-4000-8000-000000000021', 'Manual.pdf', 2048);
const second = listed('77777777-0000-4000-8000-000000000022', 'Price list.xlsx', 3 << 20);

const noVotes = { article_id: ids.article, helpful: 0, not_helpful: 0, imported: { helpful: 0, not_helpful: 0 }, votes: [], resets: [] };
const base = { 'GET /api/v1/auth/me': () => json(200, me), 'GET /api/v1/kb/collections': () => json(200, { items: collections }),
  [`GET /api/v1/kb/articles/${ids.article}/votes`]: () => json(200, noVotes), [`GET /api/v1/kb/articles/${ids.article}/stats`]: () => json(200, noStats),
  'POST /api/v1/kb/media-uploads': () => json(201, upload), [`GET /api/v1/kb/media-uploads/${upload.id}`]: () => json(200, upload),
  [`POST /api/v1/kb/media-uploads/${upload.id}/complete`]: () => json(201, media) };
const attachments = `PUT /api/v1/kb/articles/${ids.article}/attachments`;
const sent = (calls: Call[]) => calls.filter((call) => call.method === 'PUT').map((call) => call.body);

function open(props: { articleId?: string; collectionId?: string }) {
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><ArticleRoute {...props} /></QueryClientProvider>);
}
const sized = (name: string, size: number) => {
  const file = new File(['x'], name);
  Object.defineProperty(file, 'size', { value: size });
  return file;
};

test('a chosen file goes up in parts, kept in the library, is listed under the article with its size, and the next save sends the new version', async () => {
  const calls = serve({ ...base, [`GET /api/v1/kb/articles/${ids.article}`]: () => json(200, article()),
    [attachments]: () => json(200, article({ attachments: [warranty], updated_at: '2026-10-05T00:00:00Z' })),
    [`PATCH /api/v1/kb/articles/${ids.article}`]: () => json(200, article({ attachments: [warranty], body_md: 'New text', updated_at: '2026-10-06T00:00:00Z' })) });
  open({ articleId: ids.article });
  fireEvent.change(await screen.findByLabelText('Choose files'), { target: { files: [new File([new Uint8Array(12)], 'Warranty form.pdf', { type: 'application/pdf' })] } });
  const strip = screen.getByRole('region', { name: 'Attachments' });
  expect(within(strip).getByRole('button', { name: 'About Attachments' }))
    .toHaveAccessibleDescription('Readers download them under the text. Up to 25 MiB each and 100 MiB in all.');
  expect(await within(strip).findByRole('link', { name: 'Warranty form.pdf' })).toHaveAttribute('href', `/api/v1/kb/media/${fileId}`);
  expect(within(strip).getByText('PDF · 12 B')).toBeInTheDocument();
  expect(calls.find((call) => call.path === '/api/v1/kb/media-uploads')?.body).toEqual({ kind: 'file', size: 12, name: 'Warranty form.pdf', keep: true });
  expect(sent(calls)).toEqual([{ media_ids: [fileId], updated_at: '2026-10-02T00:00:00Z' }]);
  fireEvent.change(screen.getByRole('textbox', { name: 'Text (Markdown)' }), { target: { value: 'New text' } });
  await settle();
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  await waitFor(() => expect(calls.find((call) => call.method === 'PATCH')?.body).toEqual({ updated_at: '2026-10-05T00:00:00Z', body_md: 'New text' }));
});

test('on a published article the files go into its unpublished changes, and typed text stays', async () => {
  const published = article({ status: 'published', published_at: '2026-10-02T00:00:00Z' });
  const draft = { title: published.title, slug: published.slug, body_md: published.body_md, seo_title: '', seo_description: '', collection_id: ids.setup,
    collection_ids: [ids.setup], show_contents: true, attachments: [warranty], created_at: '2026-10-05T00:00:00Z', updated_at: '2026-10-05T00:00:00Z', updated_by: null, tags: [] };
  serve({ ...base, [`GET /api/v1/kb/articles/${ids.article}`]: () => json(200, published),
    [attachments]: () => json(200, { ...published, has_draft: true, draft, updated_at: '2026-10-05T00:00:00Z' }) });
  open({ articleId: ids.article });
  const text = await screen.findByRole('textbox', { name: 'Text (Markdown)' });
  fireEvent.change(text, { target: { value: 'Typed, not saved' } });
  fireEvent.change(screen.getByLabelText('Choose files'), { target: { files: [new File([new Uint8Array(12)], 'Warranty form.pdf')] } });
  expect(await screen.findByRole('link', { name: 'Warranty form.pdf' })).toBeInTheDocument();
  expect(screen.getByText('Unpublished changes')).toBeInTheDocument();
  expect(text).toHaveValue('Typed, not saved');
});

test('a refused type, a file over 25 MiB and one past 100 MiB for the article say why, and none is sent', async () => {
  const calls = serve({ ...base, [`GET /api/v1/kb/articles/${ids.article}`]: () => json(200, article({ attachments: [listed(first.id, 'Huge.zip', 80 << 20)] })) });
  open({ articleId: ids.article });
  fireEvent.change(await screen.findByLabelText('Choose files'), { target: { files: [new File(['MZ'], 'setup.exe'), sized('manual.pdf', 26 << 20), sized('backup.zip', 21 << 20)] } });
  expect(await screen.findByText('setup.exe is not a type an article can list. Files: PDF, DOC, DOCX, XLS, XLSX, PPT, PPTX, ODT, ODS, ODP, CSV, TXT, RTF, ZIP, 7Z, TAR.GZ.')).toBeInTheDocument();
  expect(screen.getByText('manual.pdf is 26.0 MiB, over the 25.0 MiB limit for one file.')).toBeInTheDocument();
  expect(screen.getByText('backup.zip would bring the files of this article to 101.0 MiB, over the 100.0 MiB limit.')).toBeInTheDocument();
  expect(calls.filter((call) => call.method === 'POST' || call.method === 'PUT')).toEqual([]);
});

test('the server\'s refusal of a file shows beside the list', async () => {
  serve({ ...base, [`GET /api/v1/kb/articles/${ids.article}`]: () => json(200, article()),
    'POST /api/v1/kb/media-uploads': () => refusal(400, 'file_type_refused', 'renamed.pdf is not a PDF: its first bytes are a program.') });
  open({ articleId: ids.article });
  fireEvent.change(await screen.findByLabelText('Choose files'), { target: { files: [new File(['MZ'], 'renamed.pdf')] } });
  expect(await screen.findByText('renamed.pdf is not a PDF: its first bytes are a program.')).toBeInTheDocument();
});

test('a file moves up, and Remove takes it off the list (the library keeps it)', async () => {
  const calls = serve({ ...base, [`GET /api/v1/kb/articles/${ids.article}`]: () => json(200, article({ attachments: [first, second] })),
    [attachments]: (call) => {
      const order = (call.body as { media_ids: string[] }).media_ids;
      return json(200, article({ attachments: order.map((id) => [first, second].find((item) => item.id === id)), updated_at: `2026-10-0${3 + sent(calls).length}T00:00:00Z` }));
    } });
  open({ articleId: ids.article });
  const strip = await screen.findByRole('region', { name: 'Attachments' });
  expect(await within(strip).findByText('XLSX · 3.0 MiB')).toBeInTheDocument();
  fireEvent.click(within(strip).getByRole('button', { name: 'Move Price list.xlsx up' }));
  await waitFor(() => expect(within(strip).getAllByRole('link').map((link) => link.textContent)).toEqual(['Price list.xlsx', 'Manual.pdf']));
  fireEvent.click(within(strip).getByRole('button', { name: 'Remove Manual.pdf' }));
  await waitFor(() => expect(within(strip).getAllByRole('link').map((link) => link.textContent)).toEqual(['Price list.xlsx']));
  expect(sent(calls)).toEqual([{ media_ids: [second.id, first.id], updated_at: '2026-10-02T00:00:00Z' }, { media_ids: [second.id], updated_at: '2026-10-04T00:00:00Z' }]);
});

test('a file pasted into the text is attached, and the text stays as it was', async () => {
  const calls = serve({ ...base, [`GET /api/v1/kb/articles/${ids.article}`]: () => json(200, article({ body_md: 'Intro' })),
    [attachments]: () => json(200, article({ body_md: 'Intro', attachments: [warranty], updated_at: '2026-10-05T00:00:00Z' })) });
  open({ articleId: ids.article });
  const text = await screen.findByRole('textbox', { name: 'Text (Markdown)' });
  fireEvent.paste(text, { clipboardData: { files: [new File([new Uint8Array(12)], 'Warranty form.pdf', { type: 'application/pdf' })] } });
  expect(await screen.findByRole('link', { name: 'Warranty form.pdf' })).toBeInTheDocument();
  expect(sent(calls)).toEqual([{ media_ids: [fileId], updated_at: '2026-10-02T00:00:00Z' }]);
  expect(text).toHaveValue('Intro');
});

test('a new article asks to be saved before files are attached', async () => {
  serve(base);
  open({ collectionId: ids.setup });
  const strip = await screen.findByRole('region', { name: 'Attachments' });
  expect(within(strip).getByText('Save the article to attach files.')).toBeInTheDocument();
  expect(within(strip).queryByLabelText('Choose files')).toBeNull();
});
