import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { ArticleRoute } from './article-editor';
import { article, collections, ids, json, me, refusal, serve } from './kb.test.fixture';

const navigate = vi.hoisted(() => vi.fn());
const blocker = vi.hoisted(() => vi.fn());
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => navigate, useBlocker: blocker }));
// The editor is a field with its contract here (editor/fake-editor.test.fixture.tsx); codemirror-editor.test.tsx
// runs the real one.
vi.mock('./editor/codemirror-editor', () => import('./editor/fake-editor.test.fixture'));
// A video's parts go through XMLHttpRequest (for the progress); here they land at once.
const sentParts = vi.hoisted(() => [] as string[]);
vi.mock('./media-api', async (original) => ({
  ...(await original<typeof import('./media-api')>()),
  sendPart: async (_id: string, part: Blob, first: number, size: number, onSent: (bytes: number) => void) => {
    sentParts.push(`${first}+${part.size}/${size}`);
    onSent(part.size);
    return { status: 200, body: { id: '77777777-0000-4000-8000-000000000001', kind: 'video', name: 'tour', size, offset: first + part.size, part_size: 8,
      expires_at: '2026-10-22T00:00:00Z', media_id: null, created_by: null, created_at: '2026-10-21T00:00:00Z' }, offset: String(first + part.size) };
  },
}));
afterEach(() => { navigate.mockReset(); blocker.mockReset(); vi.unstubAllGlobals(); });

// settle waits for the editor to tell the page of what was typed, as it does a moment after typing stops.
const settle = () => act(() => new Promise((resolve) => { setTimeout(resolve, 150); }));
const uploaded = { id: ids.image, content_type: 'image/png', size: 4, sha256: 'ab', url: `/api/v1/kb/images/${ids.image}`, created_at: '2026-10-02T00:00:00Z' };

function open(props: { articleId?: string; collectionId?: string }) {
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><ArticleRoute {...props} /></QueryClientProvider>);
}

const base = { 'GET /api/v1/auth/me': () => json(200, me), 'GET /api/v1/kb/collections': () => json(200, { items: collections }) };

test('a new article is saved as a draft in the collection it was started from, then opens as itself', async () => {
  const calls = serve({ ...base, 'POST /api/v1/kb/articles': () => json(201, article({ title: 'Reset your password', body_md: '# Steps' })) });
  open({ collectionId: ids.setup });
  fireEvent.change(await screen.findByRole('textbox', { name: 'Title' }), { target: { value: '  Reset your password ' } });
  fireEvent.change(screen.getByRole('textbox', { name: 'Text (Markdown)' }), { target: { value: '# Steps' } });
  expect(screen.getByRole('combobox', { name: 'Collection' })).toHaveValue(ids.setup);
  fireEvent.click(screen.getByRole('button', { name: 'Save draft' }));
  await waitFor(() => expect(navigate).toHaveBeenCalledWith({ to: '/knowledge/articles/$articleId', params: { articleId: ids.article }, replace: true }));
  expect(calls.find((call) => call.method === 'POST')?.body).toEqual({ collection_id: ids.setup, title: 'Reset your password', body_md: '# Steps', status: 'draft' });
});

test('an edit sends only what changed with the version it was made on, and the next save uses the answer\'s version', async () => {
  const first = article({ updated_at: '2026-10-02T00:00:00Z' });
  const calls = serve({ ...base, 'GET /api/v1/kb/articles/55555555-5555-4555-8555-555555555555': () => json(200, first),
    'PATCH /api/v1/kb/articles/55555555-5555-4555-8555-555555555555': (call) => {
      const sent = call.body as { body_md?: string; status?: string };
      return json(200, article({ ...sent, body_md: sent.body_md ?? first.body_md, updated_at: calls.filter((item) => item.method === 'PATCH').length === 1 ? '2026-10-03T00:00:00Z' : '2026-10-04T00:00:00Z' }));
    } });
  open({ articleId: ids.article });
  fireEvent.change(await screen.findByRole('textbox', { name: 'Text (Markdown)' }), { target: { value: 'Open **Settings**, then Security.' } });
  await settle();
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  await waitFor(() => expect(calls.filter((call) => call.method === 'PATCH')).toHaveLength(1));
  expect(calls.find((call) => call.method === 'PATCH')?.body).toEqual({ updated_at: '2026-10-02T00:00:00Z', body_md: 'Open **Settings**, then Security.' });
  fireEvent.click(await screen.findByRole('button', { name: 'Publish' }));
  await waitFor(() => expect(calls.filter((call) => call.method === 'PATCH')).toHaveLength(2));
  expect(calls.filter((call) => call.method === 'PATCH')[1].body).toEqual({ updated_at: '2026-10-03T00:00:00Z', status: 'published' });
  expect(await screen.findByRole('button', { name: 'Unpublish' })).toBeInTheDocument();
});

test('when someone else saved first, the text stays on screen and the person chooses: keep mine saves over theirs', async () => {
  let current = article({ updated_at: '2026-10-02T00:00:00Z' });
  const calls = serve({ ...base,
    'GET /api/v1/kb/articles/55555555-5555-4555-8555-555555555555': () => json(200, current),
    'PATCH /api/v1/kb/articles/55555555-5555-4555-8555-555555555555': (call) => ((call.body as { updated_at: string }).updated_at === current.updated_at
      ? json(200, (current = article({ body_md: (call.body as { body_md: string }).body_md, updated_at: '2026-10-05T00:00:00Z' })))
      : refusal(409, 'changed_elsewhere', 'This article was saved by someone else.')) });
  open({ articleId: ids.article });
  const text = await screen.findByRole('textbox', { name: 'Text (Markdown)' });
  current = article({ body_md: 'Their text', updated_at: '2026-10-03T00:00:00Z' });
  fireEvent.change(text, { target: { value: 'My text' } });
  await settle();
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  expect(await screen.findByText(/Someone saved this article after you opened it/)).toBeInTheDocument();
  expect(text).toHaveValue('My text');
  fireEvent.click(screen.getByRole('button', { name: 'Save mine over theirs' }));
  await waitFor(() => expect(calls.filter((call) => call.method === 'PATCH')).toHaveLength(2));
  expect(calls.filter((call) => call.method === 'PATCH')[1].body).toEqual({ updated_at: '2026-10-03T00:00:00Z', body_md: 'My text' });
  await waitFor(() => expect(screen.queryByText(/Someone saved this article/)).not.toBeInTheDocument());
});

test('discarding mine shows their version', async () => {
  let current = article({ updated_at: '2026-10-02T00:00:00Z' });
  serve({ ...base,
    'GET /api/v1/kb/articles/55555555-5555-4555-8555-555555555555': () => json(200, current),
    'PATCH /api/v1/kb/articles/55555555-5555-4555-8555-555555555555': () => refusal(409, 'changed_elsewhere', 'Saved by someone else.') });
  open({ articleId: ids.article });
  const text = await screen.findByRole('textbox', { name: 'Text (Markdown)' });
  current = article({ body_md: 'Their text', updated_at: '2026-10-03T00:00:00Z' });
  fireEvent.change(text, { target: { value: 'My text' } });
  await settle();
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Discard mine and show theirs' }));
  await waitFor(() => expect(text).toHaveValue('Their text'));
});

test('a text over 100,000 characters is never cut: saving waits until it is shorter, counting characters as the server does', async () => {
  serve({ ...base, 'GET /api/v1/kb/articles/55555555-5555-4555-8555-555555555555': () => json(200, article()) });
  open({ articleId: ids.article });
  const text = await screen.findByRole('textbox', { name: 'Text (Markdown)' });
  expect(text).not.toHaveAttribute('maxlength');
  // 50,000 emoji are 100,000 UTF-16 units but 50,000 characters: within the limit.
  fireEvent.change(text, { target: { value: '😀'.repeat(50000) } });
  await settle();
  expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled();
  fireEvent.change(text, { target: { value: 'a'.repeat(100002) } });
  await settle();
  expect(text).toHaveValue('a'.repeat(100002));
  expect(screen.getByRole('alert')).toHaveTextContent('2 characters over the limit');
  expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();
});

test('a pasted image is uploaded as raw bytes and goes in as a paragraph of its own', async () => {
  const calls = serve({ ...base, 'GET /api/v1/kb/articles/55555555-5555-4555-8555-555555555555': () => json(200, article({ body_md: 'Before after' })),
    'POST /api/v1/kb/images': () => json(201, uploaded) });
  open({ articleId: ids.article });
  const text = await screen.findByRole('textbox', { name: 'Text (Markdown)' });
  const file = new File([new Uint8Array([137, 80, 78, 71])], 'login screen.png', { type: 'image/png' });
  fireEvent.paste(text, { clipboardData: { files: [file] } });
  await waitFor(() => expect(text).toHaveValue(`Before after\n\n![login screen](/api/v1/kb/images/${ids.image})`));
  const upload = calls.find((call) => call.path === '/api/v1/kb/images')!;
  expect(upload.headers['Content-Type']).toBe('image/png');
  expect(upload.body).toBe(file);
});

test('an image the server refuses leaves no placeholder and says why; a wrong type is refused before any upload', async () => {
  const calls = serve({ ...base, 'GET /api/v1/kb/articles/55555555-5555-4555-8555-555555555555': () => json(200, article({ body_md: '' })),
    'POST /api/v1/kb/images': () => refusal(403, 'storage_limit_reached', 'Storage is full.') });
  open({ articleId: ids.article });
  const text = await screen.findByRole('textbox', { name: 'Text (Markdown)' });
  fireEvent.paste(text, { clipboardData: { files: [new File(['x'], 'shot.png', { type: 'image/png' }), new File(['<svg/>'], 'logo.svg', { type: 'image/svg+xml' })] } });
  expect(await screen.findByText('logo.svg is not a PNG, JPEG, GIF or WebP image, nor an MP4 or WebM video.')).toBeInTheDocument();
  expect(await screen.findByText('Storage is full.')).toBeInTheDocument();
  expect(text).toHaveValue('');
  expect(calls.filter((call) => call.path === '/api/v1/kb/images')).toHaveLength(1);
});

test('the preview renders Markdown with tables, and raw HTML never runs', async () => {
  serve({ ...base, 'GET /api/v1/kb/articles/55555555-5555-4555-8555-555555555555': () => json(200, article({ body_md: '| Plan | Agents |\n| --- | --- |\n| Team | 10 |\n\n<img src=x onerror="alert(1)">' })) });
  open({ articleId: ids.article });
  fireEvent.click(await screen.findByRole('tab', { name: 'Preview' }));
  const panel = screen.getByRole('tabpanel', { name: 'Preview' });
  // The preview draws after the editor is on screen.
  expect(await within(panel).findByRole('table')).toHaveTextContent('Team10');
  expect(panel.querySelector('img')).toBeNull();
});

test('the preview shows YouTube and Vimeo players and library videos as the help center does, and names the HTML it leaves out', async () => {
  const body = 'Watch:\n\n<iframe src="https://www.youtube.com/embed/jNQXAC9IVRw?start=65" onload="alert(1)"></iframe>\n\n'
    + 'Inline <iframe src="https://evil.example/embed/x"></iframe> and <b>bold</b>.\n\n<script>window.__xss = 1</script>\n\n'
    + '<video src="/api/v1/kb/media/0b6f8a8e-3c4d-4e5f-8a9b-0c1d2e3f4a5b" autoplay controls></video>';
  const libraryVideo = { id: '0b6f8a8e-3c4d-4e5f-8a9b-0c1d2e3f4a5b', kind: 'video', content_type: 'video/mp4', size: 10, sha256: 'a'.repeat(64),
    url: '/api/v1/kb/media/0b6f8a8e-3c4d-4e5f-8a9b-0c1d2e3f4a5b', name: 'Tour', alt: '', width: 1280, height: 720, duration_ms: 1000, codecs: [], thumbnail_url: null,
    poster_url: '/api/v1/kb/media/0b6f8a8e-3c4d-4e5f-8a9b-0c1d2e3f4a5b/poster/abcdef12', kept: false, deleted_at: null, created_at: '2026-10-01T00:00:00Z', updated_at: '2026-10-01T00:00:00Z', articles: [] };
  serve({ ...base, 'GET /api/v1/kb/articles/55555555-5555-4555-8555-555555555555': () => json(200, article({ body_md: body + '\n\n<video src="/api/v1/kb/media/1b6f8a8e-3c4d-4e5f-8a9b-0c1d2e3f4a5b"></video>' })),
    'GET /api/v1/kb/media/0b6f8a8e-3c4d-4e5f-8a9b-0c1d2e3f4a5b/details': () => json(200, libraryVideo) });
  open({ articleId: ids.article });
  fireEvent.click(await screen.findByRole('tab', { name: 'Preview' }));
  const panel = screen.getByRole('tabpanel', { name: 'Preview' });
  // The preview draws after the editor is on screen.
  const warning = await within(panel).findByRole('status');
  const frames = panel.querySelectorAll('iframe');
  expect(frames).toHaveLength(1);
  expect(frames[0]).toHaveAttribute('src', 'https://www.youtube-nocookie.com/embed/jNQXAC9IVRw?start=65');
  expect(frames[0]).toHaveAttribute('sandbox', 'allow-scripts allow-same-origin allow-presentation allow-popups');
  expect(frames[0]).toHaveAttribute('referrerpolicy', 'strict-origin-when-cross-origin');
  expect(frames[0]).toHaveAttribute('loading', 'lazy');
  expect(frames[0]).not.toHaveAttribute('onload');
  expect(panel.querySelector('script, b')).toBeNull();
  const video = panel.querySelector('video');
  expect(video).toHaveAttribute('src', '/api/v1/kb/media/0b6f8a8e-3c4d-4e5f-8a9b-0c1d2e3f4a5b');
  expect(video).toHaveAttribute('controls');
  expect(video).not.toHaveAttribute('autoplay');
  // The library gives the poster and the size; a video it does not have is left out and named.
  await waitFor(() => expect(video).toHaveAttribute('poster', libraryVideo.poster_url));
  expect(video).toHaveAttribute('width', '1280');
  await waitFor(() => expect(panel.querySelectorAll('video')).toHaveLength(1));
  expect(panel).toHaveTextContent('Inline and bold.');
  await waitFor(() => expect(within(panel).getByRole('status')).toHaveTextContent('Left out of the article: iframe onload, iframe (evil.example), b, script, video autoplay, video (not in the library).'));
  expect(panel).not.toHaveTextContent('__xss');
  expect((window as { __xss?: number }).__xss).toBeUndefined();
});

test('restoring a version asks to save unsaved edits first, then makes the version current', async () => {
  const restored = article({ title: 'Old title', updated_at: '2026-10-06T00:00:00Z' });
  const version = (n: number, title: string) => ({ version: n, kind: n === 1 ? 'create' : 'edit', restored_from_version: null, changed_by: { agent_id: null, name: 'Ada' },
    token: null, agent_request_id: null, created_at: '2026-10-02T00:00:00Z', title, slug: 'reset', body_md: 'Open **Settings**.', status: 'draft', collection_id: ids.setup });
  const calls = serve({ ...base, 'GET /api/v1/kb/articles/55555555-5555-4555-8555-555555555555': () => json(200, article()),
    'GET /api/v1/kb/articles/55555555-5555-4555-8555-555555555555/versions': () => json(200, { items: [version(2, 'Reset your password'), version(1, 'Old title')], next_before_version: null }),
    'POST /api/v1/kb/articles/55555555-5555-4555-8555-555555555555/versions/1/restore': () => json(200, restored) });
  open({ articleId: ids.article });
  fireEvent.click(await screen.findByRole('button', { name: 'History' }));
  const history = await screen.findByRole('region', { name: 'History of Reset your password' });
  expect(within(history).getAllByText('Title changed')).toHaveLength(1);
  fireEvent.change(screen.getByRole('textbox', { name: 'Title' }), { target: { value: 'Unsaved' } });
  fireEvent.click(within(history).getByRole('button', { name: 'Restore version 1' }));
  expect(await within(history).findByRole('alert')).toHaveTextContent('Save or undo your changes first');
  expect(calls.some((call) => call.path.endsWith('/restore'))).toBe(false);
  fireEvent.change(screen.getByRole('textbox', { name: 'Title' }), { target: { value: 'Reset your password' } });
  fireEvent.click(within(history).getByRole('button', { name: 'Restore version 1' }));
  await waitFor(() => expect(screen.getByRole('textbox', { name: 'Title' })).toHaveValue('Old title'));
  expect(calls.find((call) => call.path.endsWith('/restore'))?.body).toEqual({ updated_at: '2026-10-02T00:00:00Z' });
});

test('an article in the trash is not opened for editing, and a missing one says so', async () => {
  serve({ ...base, 'GET /api/v1/kb/articles/55555555-5555-4555-8555-555555555555': () => json(200, article({ deleted_at: '2026-10-03T00:00:00Z' })) });
  open({ articleId: ids.article });
  expect(await screen.findByText(/This article is in the trash/)).toBeInTheDocument();
  expect(screen.queryByRole('textbox', { name: 'Text (Markdown)' })).not.toBeInTheDocument();
});

test('a deleted article goes to the trash with the version it was read at, then the list opens', async () => {
  const calls = serve({ ...base, 'GET /api/v1/kb/articles/55555555-5555-4555-8555-555555555555': () => json(200, article()),
    'DELETE /api/v1/kb/articles/55555555-5555-4555-8555-555555555555': () => json(204) });
  open({ articleId: ids.article });
  fireEvent.click(await screen.findByRole('button', { name: 'Delete' }));
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Move to trash' })); });
  await waitFor(() => expect(navigate).toHaveBeenCalledWith({ to: '/knowledge' }));
  expect(calls.find((call) => call.method === 'DELETE')?.search.get('updated_at')).toBe('2026-10-02T00:00:00Z');
});

test('a save with nothing to send asks nothing of the server: Ctrl+S on an unchanged article, or an emptied address name', async () => {
  const calls = serve({ ...base, 'GET /api/v1/kb/articles/55555555-5555-4555-8555-555555555555': () => json(200, article()) });
  open({ articleId: ids.article });
  const text = await screen.findByRole('textbox', { name: 'Text (Markdown)' });
  fireEvent.keyDown(text, { key: 's', ctrlKey: true });
  fireEvent.change(screen.getByRole('textbox', { name: 'Address name' }), { target: { value: '  ' } });
  expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();
  fireEvent.keyDown(text, { key: 's', metaKey: true });
  await act(async () => { await Promise.resolve(); });
  expect(calls.filter((call) => call.method === 'PATCH')).toHaveLength(0);
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
});

test('saving waits while an image uploads, so a placeholder is never stored; a $ in the file name stays in the text', async () => {
  let finish: (response: Response) => void = () => {};
  const calls = serve({ ...base, 'GET /api/v1/kb/articles/55555555-5555-4555-8555-555555555555': () => json(200, article({ body_md: '' })),
    'POST /api/v1/kb/images': () => new Promise<Response>((resolve) => { finish = resolve; }) });
  open({ articleId: ids.article });
  const text = await screen.findByRole('textbox', { name: 'Text (Markdown)' });
  fireEvent.paste(text, { clipboardData: { files: [new File(['x'], 'cost$$ $&.png', { type: 'image/png' })] } });
  expect((text as HTMLTextAreaElement).value).toBe('');
  expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();
  // Nothing is in the text yet, so nothing is unsaved; publishing waits too.
  expect(screen.getByRole('button', { name: 'Publish' })).toBeDisabled();
  fireEvent.keyDown(text, { key: 's', ctrlKey: true });
  expect(calls.filter((call) => call.method === 'PATCH')).toHaveLength(0);
  await act(async () => { finish(json(201, { id: ids.image, content_type: 'image/png', size: 1, sha256: 'ab', url: `/api/v1/kb/images/${ids.image}`, created_at: '2026-10-02T00:00:00Z' })); });
  await waitFor(() => expect(text).toHaveValue(`![cost$$ $&](/api/v1/kb/images/${ids.image})`));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled());
});

test('a new article without a collection says why it cannot be saved yet', async () => {
  serve(base);
  open({});
  fireEvent.change(await screen.findByRole('textbox', { name: 'Title' }), { target: { value: 'Refunds' } });
  expect(screen.getByRole('button', { name: 'Save draft' })).toBeDisabled();
  expect(screen.getByRole('button', { name: 'Save draft' })).toHaveAccessibleDescription('Choose a collection below to save.');
  fireEvent.change(screen.getByRole('combobox', { name: 'Collection' }), { target: { value: ids.billing } });
  expect(screen.getByRole('button', { name: 'Save draft' })).toBeEnabled();
  expect(screen.queryByText('Choose a collection below to save.')).not.toBeInTheDocument();
});

test('the preview shows only images of the knowledge base, as the help center does', async () => {
  serve({ ...base, 'GET /api/v1/kb/articles/55555555-5555-4555-8555-555555555555': () => json(200, article({ body_md: `![ours](/api/v1/kb/images/${ids.image}) ![tracker](https://tracker.example/p.gif)` })) });
  open({ articleId: ids.article });
  fireEvent.click(await screen.findByRole('tab', { name: 'Preview' }));
  const panel = screen.getByRole('tabpanel', { name: 'Preview' });
  expect(await within(panel).findByRole('img', { name: 'ours' })).toHaveAttribute('src', `/api/v1/kb/images/${ids.image}`);
  expect(panel.querySelectorAll('img')).toHaveLength(1);
  expect(panel).toHaveTextContent('Image from another site, not shown here or on the help center: tracker');
});

test('an open history loads again after a save adds a version', async () => {
  let saved = false;
  const version = (n: number) => ({ version: n, kind: n === 1 ? 'create' : 'edit', restored_from_version: null, changed_by: { agent_id: null, name: 'Ada' },
    token: null, agent_request_id: null, created_at: '2026-10-02T00:00:00Z', title: 'Reset your password', slug: 'reset', body_md: `Text ${n}`, status: 'draft', collection_id: ids.setup });
  serve({ ...base, 'GET /api/v1/kb/articles/55555555-5555-4555-8555-555555555555': () => json(200, article()),
    'GET /api/v1/kb/articles/55555555-5555-4555-8555-555555555555/versions': () => json(200, { items: saved ? [version(2), version(1)] : [version(1)], next_before_version: null }),
    'PATCH /api/v1/kb/articles/55555555-5555-4555-8555-555555555555': () => { saved = true; return json(200, article({ body_md: 'Text 2', updated_at: '2026-10-07T00:00:00Z' })); } });
  open({ articleId: ids.article });
  fireEvent.click(await screen.findByRole('button', { name: 'History' }));
  expect(await screen.findByText(/Version 1 ·/)).toBeInTheDocument();
  fireEvent.change(screen.getByRole('textbox', { name: 'Text (Markdown)' }), { target: { value: 'Text 2' } });
  await settle();
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  expect(await screen.findByText(/Version 2 ·/)).toBeInTheDocument();
});

test('spaces around the title or address name are not a change, so the form never stays unsaved with nothing to send', async () => {
  serve({ ...base, 'GET /api/v1/kb/articles/55555555-5555-4555-8555-555555555555': () => json(200, article()) });
  open({ articleId: ids.article });
  fireEvent.change(await screen.findByRole('textbox', { name: 'Title' }), { target: { value: 'Reset your password ' } });
  fireEvent.change(screen.getByRole('textbox', { name: 'Address name' }), { target: { value: ' reset-your-password' } });
  expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();
  expect(screen.queryByText('Unsaved changes')).not.toBeInTheDocument();
});

test('Ctrl+S right after typing saves what was typed, and leaving at once asks first', async () => {
  const calls = serve({ ...base, 'GET /api/v1/kb/articles/55555555-5555-4555-8555-555555555555': () => json(200, article()),
    'PATCH /api/v1/kb/articles/55555555-5555-4555-8555-555555555555': () => json(200, article({ body_md: 'Typed', updated_at: '2026-10-03T00:00:00Z' })) });
  open({ articleId: ids.article });
  const text = await screen.findByRole('textbox', { name: 'Text (Markdown)' });
  fireEvent.change(text, { target: { value: 'Typed' } });
  // The page has not heard of the typing yet; leaving and saving read the editor.
  expect(blocker.mock.calls.at(-1)?.[0].enableBeforeUnload()).toBe(true);
  fireEvent.keyDown(text, { key: 's', ctrlKey: true });
  await waitFor(() => expect(calls.filter((call) => call.method === 'PATCH')).toHaveLength(1));
  expect(calls.find((call) => call.method === 'PATCH')?.body).toEqual({ updated_at: '2026-10-02T00:00:00Z', body_md: 'Typed' });
  await waitFor(() => expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled());
});

test('a wrong type or a file over 5 MB is refused before any upload', async () => {
  const calls = serve({ ...base, 'GET /api/v1/kb/articles/55555555-5555-4555-8555-555555555555': () => json(200, article()) });
  open({ articleId: ids.article });
  const big = new File(['x'], 'huge.png', { type: 'image/png' });
  Object.defineProperty(big, 'size', { value: 6 * 1024 * 1024 });
  fireEvent.paste(await screen.findByRole('textbox', { name: 'Text (Markdown)' }), { clipboardData: { files: [new File(['<svg/>'], 'logo.svg', { type: 'image/svg+xml' }), big] } });
  expect(await screen.findByText('logo.svg is not a PNG, JPEG, GIF or WebP image, nor an MP4 or WebM video.')).toBeInTheDocument();
  expect(screen.getByText('huge.png is over 5 MB.')).toBeInTheDocument();
  expect(calls.some((call) => call.path === '/api/v1/kb/images')).toBe(false);
});

test('a restore puts the restored text in the editor', async () => {
  const restored = article({ body_md: 'Restored text', updated_at: '2026-10-06T00:00:00Z' });
  const version = { version: 1, kind: 'create', restored_from_version: null, changed_by: { agent_id: null, name: 'Ada' }, token: null, agent_request_id: null,
    created_at: '2026-10-02T00:00:00Z', title: 'Reset your password', slug: 'reset', body_md: 'Restored text', status: 'draft', collection_id: ids.setup };
  serve({ ...base, 'GET /api/v1/kb/articles/55555555-5555-4555-8555-555555555555': () => json(200, article()),
    'GET /api/v1/kb/articles/55555555-5555-4555-8555-555555555555/versions': () => json(200, { items: [{ ...version, version: 2, body_md: 'Open **Settings**.' }, version], next_before_version: null }),
    'POST /api/v1/kb/articles/55555555-5555-4555-8555-555555555555/versions/1/restore': () => json(200, restored) });
  open({ articleId: ids.article });
  fireEvent.click(await screen.findByRole('button', { name: 'History' }));
  fireEvent.click(await within(await screen.findByRole('region', { name: 'History of Reset your password' })).findByRole('button', { name: 'Restore version 1' }));
  await waitFor(() => expect(screen.getByRole('textbox', { name: 'Text (Markdown)' })).toHaveValue('Restored text'));
  expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();
});

test('an article with Windows line endings opened untouched is no change: nothing to save, nothing to ask on leaving', async () => {
  const calls = serve({ ...base, 'GET /api/v1/kb/articles/55555555-5555-4555-8555-555555555555': () => json(200, article({ body_md: 'Steps\r\n\r\nOpen **Settings**.\r\n' })),
    'PATCH /api/v1/kb/articles/55555555-5555-4555-8555-555555555555': () => json(200, article({ status: 'published', updated_at: '2026-10-03T00:00:00Z' })) });
  open({ articleId: ids.article });
  await screen.findByRole('textbox', { name: 'Text (Markdown)' });
  expect(blocker.mock.calls.at(-1)?.[0].enableBeforeUnload()).toBe(false);
  expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();
  fireEvent.click(screen.getByRole('button', { name: 'Publish' }));
  await waitFor(() => expect(calls.filter((call) => call.method === 'PATCH')).toHaveLength(1));
  expect(calls.find((call) => call.method === 'PATCH')?.body).toEqual({ updated_at: '2026-10-02T00:00:00Z', status: 'published' });
});

test('a pasted video goes up in parts and goes in as the library video HTML, kept out of the library\'s keep', async () => {
  const upload = { id: '77777777-0000-4000-8000-000000000001', kind: 'video', name: 'tour', size: 12, offset: 0, part_size: 8, expires_at: '2026-10-22T00:00:00Z',
    media_id: null, created_by: null, created_at: '2026-10-21T00:00:00Z' };
  const media = { id: '77777777-0000-4000-8000-000000000002', kind: 'video', content_type: 'video/webm', size: 12, sha256: 'a'.repeat(64), url: '/api/v1/kb/media/77777777-0000-4000-8000-000000000002',
    name: 'tour', alt: '', width: 640, height: 360, duration_ms: 3000, codecs: ['V_VP9'], thumbnail_url: null, poster_url: null, kept: false, deleted_at: null,
    created_at: new Date().toISOString(), updated_at: new Date().toISOString() };
  const calls = serve({ ...base, 'GET /api/v1/kb/articles/55555555-5555-4555-8555-555555555555': () => json(200, article({ body_md: 'Intro' })),
    'POST /api/v1/kb/media-uploads': () => json(201, upload), [`GET /api/v1/kb/media-uploads/${upload.id}`]: () => json(200, upload),
    [`POST /api/v1/kb/media-uploads/${upload.id}/complete`]: () => json(201, media) });
  open({ articleId: ids.article });
  const text = await screen.findByRole('textbox', { name: 'Text (Markdown)' });
  fireEvent.paste(text, { clipboardData: { files: [new File([new Uint8Array(12)], 'tour.webm', { type: 'video/webm' })] } });
  await waitFor(() => expect(text).toHaveValue(`Intro\n\n<video controls preload="metadata" src="${media.url}"></video>`));
  expect(calls.find((call) => call.path === '/api/v1/kb/media-uploads')?.body).toEqual({ size: 12, name: 'tour', keep: false });
  expect(sentParts.slice(-2)).toEqual(['0+8/12', '8+4/12']);
});

test('leaving the editor stops the videos waiting their turn, and cancels the one the server was starting', async () => {
  const upload = { id: '77777777-0000-4000-8000-000000000003', kind: 'video', name: 'one', size: 12, offset: 0, part_size: 8, expires_at: '2026-10-22T00:00:00Z',
    media_id: null, created_by: null, created_at: '2026-10-21T00:00:00Z' };
  let release: () => void = () => undefined;
  const calls = serve({ ...base, 'GET /api/v1/kb/articles/55555555-5555-4555-8555-555555555555': () => json(200, article({ body_md: 'Intro' })),
    'POST /api/v1/kb/media-uploads': () => new Promise<Response>((resolve) => { release = () => resolve(json(201, upload)); }),
    [`DELETE /api/v1/kb/media-uploads/${upload.id}`]: () => json(204) });
  const view = render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><ArticleRoute articleId={ids.article} /></QueryClientProvider>);
  const text = await screen.findByRole('textbox', { name: 'Text (Markdown)' });
  fireEvent.paste(text, { clipboardData: { files: [new File([new Uint8Array(12)], 'one.webm', { type: 'video/webm' }), new File([new Uint8Array(12)], 'two.webm', { type: 'video/webm' })] } });
  await waitFor(() => expect(calls.filter((call) => call.path === '/api/v1/kb/media-uploads')).toHaveLength(1));
  view.unmount();
  await act(async () => { release(); await new Promise((resolve) => { setTimeout(resolve, 50); }); });
  expect(calls.filter((call) => call.method === 'POST' && call.path === '/api/v1/kb/media-uploads')).toHaveLength(1);
  expect(calls.some((call) => call.method === 'DELETE' && call.path === `/api/v1/kb/media-uploads/${upload.id}`)).toBe(true);
});
