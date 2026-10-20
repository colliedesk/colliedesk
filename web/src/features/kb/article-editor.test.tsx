import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { ArticleRoute } from './article-editor';
import { article, collections, ids, json, me, refusal, serve } from './kb.test.fixture';

const navigate = vi.hoisted(() => vi.fn());
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => navigate, useBlocker: vi.fn() }));
afterEach(() => { navigate.mockReset(); vi.unstubAllGlobals(); });

function open(props: { articleId?: string; collectionId?: string }) {
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><ArticleRoute {...props} /></QueryClientProvider>);
}

const base = { 'GET /api/v1/auth/me': () => json(200, me), 'GET /api/v1/kb/collections': () => json(200, { items: collections }) };

test('a new article is saved as a draft in the collection it was started from, then opens as itself', async () => {
  const calls = serve({ ...base, 'POST /api/v1/kb/articles': () => json(201, article({ title: 'Reset your password', body_md: '# Steps' })) });
  open({ collectionId: ids.setup });
  fireEvent.change(await screen.findByRole('textbox', { name: 'Title' }), { target: { value: '  Reset your password ' } });
  fireEvent.change(screen.getByRole('textbox', { name: 'Text' }), { target: { value: '# Steps' } });
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
  fireEvent.change(await screen.findByRole('textbox', { name: 'Text' }), { target: { value: 'Open **Settings**, then Security.' } });
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
  const text = await screen.findByRole('textbox', { name: 'Text' });
  current = article({ body_md: 'Their text', updated_at: '2026-10-03T00:00:00Z' });
  fireEvent.change(text, { target: { value: 'My text' } });
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
  const text = await screen.findByRole('textbox', { name: 'Text' });
  current = article({ body_md: 'Their text', updated_at: '2026-10-03T00:00:00Z' });
  fireEvent.change(text, { target: { value: 'My text' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Discard mine and show theirs' }));
  await waitFor(() => expect(text).toHaveValue('Their text'));
});

test('a text over 100,000 characters is never cut: saving waits until it is shorter, counting characters as the server does', async () => {
  serve({ ...base, 'GET /api/v1/kb/articles/55555555-5555-4555-8555-555555555555': () => json(200, article()) });
  open({ articleId: ids.article });
  const text = await screen.findByRole('textbox', { name: 'Text' });
  expect(text).not.toHaveAttribute('maxlength');
  // 50,000 emoji are 100,000 UTF-16 units but 50,000 characters: within the limit.
  fireEvent.change(text, { target: { value: '😀'.repeat(50000) } });
  expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled();
  fireEvent.change(text, { target: { value: 'a'.repeat(100002) } });
  expect(text).toHaveValue('a'.repeat(100002));
  expect(screen.getByRole('alert')).toHaveTextContent('2 characters over the limit');
  expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();
});

test('a pasted image is uploaded as raw bytes and its address put where the cursor was', async () => {
  const calls = serve({ ...base, 'GET /api/v1/kb/articles/55555555-5555-4555-8555-555555555555': () => json(200, article({ body_md: 'Before after' })),
    'POST /api/v1/kb/images': () => json(201, { id: ids.image, content_type: 'image/png', size: 4, sha256: 'ab', url: `/api/v1/kb/images/${ids.image}`, created_at: '2026-10-02T00:00:00Z' }) });
  open({ articleId: ids.article });
  const text = await screen.findByRole('textbox', { name: 'Text' }) as HTMLTextAreaElement;
  text.setSelectionRange(7, 7);
  const file = new File([new Uint8Array([137, 80, 78, 71])], 'login screen.png', { type: 'image/png' });
  fireEvent.paste(text, { clipboardData: { files: [file] } });
  // The image goes on a line of its own, between blank lines, so it never joins a table row or a paragraph.
  await waitFor(() => expect(text).toHaveValue(`Before \n\n![login screen](/api/v1/kb/images/${ids.image})\n\nafter`));
  const upload = calls.find((call) => call.path === '/api/v1/kb/images')!;
  expect(upload.headers['Content-Type']).toBe('image/png');
  expect(upload.body).toBe(file);
});

test('an image the server refuses leaves no placeholder and says why; a wrong type is refused before any upload', async () => {
  const calls = serve({ ...base, 'GET /api/v1/kb/articles/55555555-5555-4555-8555-555555555555': () => json(200, article({ body_md: '' })),
    'POST /api/v1/kb/images': () => refusal(403, 'storage_limit_reached', 'Storage is full.') });
  open({ articleId: ids.article });
  const text = await screen.findByRole('textbox', { name: 'Text' });
  fireEvent.drop(text, { dataTransfer: { files: [new File(['x'], 'shot.png', { type: 'image/png' }), new File(['<svg/>'], 'logo.svg', { type: 'image/svg+xml' })], types: ['Files'] } });
  expect(await screen.findByText('logo.svg is not a PNG, JPEG, GIF or WebP image.')).toBeInTheDocument();
  expect(await screen.findByText('Storage is full.')).toBeInTheDocument();
  expect(text).toHaveValue('');
  expect(calls.filter((call) => call.path === '/api/v1/kb/images')).toHaveLength(1);
});

test('the preview renders Markdown with tables, and raw HTML never runs', async () => {
  serve({ ...base, 'GET /api/v1/kb/articles/55555555-5555-4555-8555-555555555555': () => json(200, article({ body_md: '| Plan | Agents |\n| --- | --- |\n| Team | 10 |\n\n<img src=x onerror="alert(1)">' })) });
  open({ articleId: ids.article });
  fireEvent.click(await screen.findByRole('tab', { name: 'Preview' }));
  const panel = screen.getByRole('tabpanel', { name: 'Preview' });
  expect(within(panel).getByRole('table')).toHaveTextContent('Team10');
  expect(panel.querySelector('img')).toBeNull();
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
  expect(screen.queryByRole('textbox', { name: 'Text' })).not.toBeInTheDocument();
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
  const text = await screen.findByRole('textbox', { name: 'Text' });
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
  const text = await screen.findByRole('textbox', { name: 'Text' });
  fireEvent.paste(text, { clipboardData: { files: [new File(['x'], 'cost$$ $&.png', { type: 'image/png' })] } });
  expect((text as HTMLTextAreaElement).value).toContain('Uploading');
  expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();
  expect(screen.getByRole('button', { name: 'Save and publish' })).toBeDisabled();
  fireEvent.keyDown(text, { key: 's', ctrlKey: true });
  expect(calls.filter((call) => call.method === 'PATCH')).toHaveLength(0);
  await act(async () => { finish(json(201, { id: ids.image, content_type: 'image/png', size: 1, sha256: 'ab', url: `/api/v1/kb/images/${ids.image}`, created_at: '2026-10-02T00:00:00Z' })); });
  await waitFor(() => expect(text).toHaveValue(`![cost$$ $&](/api/v1/kb/images/${ids.image})`));
  expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled();
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
  expect(within(panel).getByRole('img', { name: 'ours' })).toHaveAttribute('src', `/api/v1/kb/images/${ids.image}`);
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
  fireEvent.change(screen.getByRole('textbox', { name: 'Text' }), { target: { value: 'Text 2' } });
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

test('an image pasted at the end of a table row gets its own paragraph, so the table does not swallow it', async () => {
  serve({ ...base, 'GET /api/v1/kb/articles/55555555-5555-4555-8555-555555555555': () => json(200, article({ body_md: '| Plan | Agents |\n| --- | --- |\n| Team | 5 |' })),
    'POST /api/v1/kb/images': () => json(201, { id: ids.image, content_type: 'image/png', size: 4, sha256: 'ab', url: `/api/v1/kb/images/${ids.image}`, created_at: '2026-10-02T00:00:00Z' }) });
  open({ articleId: ids.article });
  const text = await screen.findByRole('textbox', { name: 'Text' }) as HTMLTextAreaElement;
  text.setSelectionRange(text.value.length, text.value.length);
  fireEvent.paste(text, { clipboardData: { files: [new File(['x'], 'plans.png', { type: 'image/png' })] } });
  await waitFor(() => expect(text).toHaveValue(`| Plan | Agents |\n| --- | --- |\n| Team | 5 |\n\n![plans](/api/v1/kb/images/${ids.image})`));
  fireEvent.click(screen.getByRole('tab', { name: 'Preview' }));
  const panel = screen.getByRole('tabpanel', { name: 'Preview' });
  expect(within(panel).getByRole('img', { name: 'plans' })).toBeInTheDocument();
  expect(within(panel).getByRole('table')).not.toHaveTextContent('plans');
});

test('an image pasted on an empty line between paragraphs adds no extra blank lines', async () => {
  serve({ ...base, 'GET /api/v1/kb/articles/55555555-5555-4555-8555-555555555555': () => json(200, article({ body_md: 'One\n\n\n\nTwo' })),
    'POST /api/v1/kb/images': () => json(201, { id: ids.image, content_type: 'image/png', size: 4, sha256: 'ab', url: `/api/v1/kb/images/${ids.image}`, created_at: '2026-10-02T00:00:00Z' }) });
  open({ articleId: ids.article });
  const text = await screen.findByRole('textbox', { name: 'Text' }) as HTMLTextAreaElement;
  text.setSelectionRange(5, 5);
  fireEvent.paste(text, { clipboardData: { files: [new File(['x'], 'step.png', { type: 'image/png' })] } });
  await waitFor(() => expect(text).toHaveValue(`One\n\n![step](/api/v1/kb/images/${ids.image})\n\nTwo`));
});
