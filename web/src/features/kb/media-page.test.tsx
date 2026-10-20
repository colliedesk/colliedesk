import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { MediaRoute } from './media-page';
import type { MediaSearch, MediaView } from './media-page';
import { UploadQueue } from './media-uploads';
import type { PartSender } from './media-api';
import { json, me, refusal, serve } from './kb.test.fixture';

const navigate = vi.hoisted(() => vi.fn());
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => navigate }));
afterEach(() => { navigate.mockReset(); vi.unstubAllGlobals(); });

function open(view: MediaView = 'library', search: MediaSearch = {}) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const view_ = render(<QueryClientProvider client={client}><MediaRoute view={view} search={search} /></QueryClientProvider>);
  return (next: MediaSearch) => view_.rerender(<QueryClientProvider client={client}><MediaRoute view={view} search={next} /></QueryClientProvider>);
}

const id = (n: number) => `aaaaaaaa-aaaa-4aaa-8aaa-${String(n).padStart(12, '0')}`;
const usage = (articles: number, trashed = 0, older = 0) => ({ articles, trashed_articles: trashed, older_versions: older });
const item = (n: number, fields: Record<string, unknown> = {}) => ({
  id: id(n), kind: 'image', content_type: 'image/png', size: 2048 * n, sha256: 'a'.repeat(64), url: `/api/v1/kb/images/${id(n)}`, name: `Screenshot ${n}`, alt: '',
  width: 800, height: 600, duration_ms: null, codecs: [], thumbnail_url: `/api/v1/kb/media/${id(n)}/thumbnail?v=1`, poster_url: null, kept: false, deleted_at: null,
  created_at: '2026-10-01T00:00:00Z', updated_at: '2026-10-02T00:00:00Z', usage: usage(1), ...fields,
});
const video = item(2, { kind: 'video', content_type: 'video/mp4', url: `/api/v1/kb/media/${id(2)}`, name: 'Product tour', duration_ms: 84_000, codecs: ['avc1', 'mp4a'], width: 1920, height: 1080 });
const summary = { count: 37, bytes: 412 * 1024 * 1024, kept_count: 5, kept_bytes: 18 * 1024 * 1024 };

const base = {
  'GET /api/v1/auth/me': () => json(200, me),
  'GET /api/v1/kb/media': (call: { search: URLSearchParams }) => (call.search.get('usage') === 'unused'
    ? json(200, { items: [item(3, { usage: usage(0), purge_at: '2026-11-01T00:00:00Z' }), item(4, { usage: usage(0, 1), kept: true }), item(5, { usage: usage(0, 0, 2) })], next_cursor: null, summary })
    : json(200, { items: [item(1), video], next_cursor: null })),
};

test('the library shows images and videos, a video with its length, and leads to what no article uses', async () => {
  const calls = serve(base);
  open();
  const grid = await screen.findByRole('list', { name: 'Media' });
  expect(await within(grid).findByRole('button', { name: 'Product tour, video, 4.0 KiB' })).toHaveTextContent('1:24');
  expect(within(grid).getByRole('button', { name: 'Screenshot 1, image, 2.0 KiB' })).toHaveTextContent('In 1 article');
  expect(await screen.findByText(/42 items not used in any article · 430\.0 MiB/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole('link', { name: 'Review' }));
  expect(navigate).toHaveBeenLastCalledWith({ to: '/knowledge/media/unused', search: {}, replace: false });
  fireEvent.click(screen.getByRole('radio', { name: 'Videos' }));
  expect(navigate).toHaveBeenLastCalledWith({ to: '/knowledge/media', search: { kind: 'video' }, replace: false });
  fireEvent.change(screen.getByRole('combobox', { name: 'Use' }), { target: { value: 'missing_alt' } });
  expect(navigate).toHaveBeenLastCalledWith({ to: '/knowledge/media', search: { usage: 'missing_alt' }, replace: false });
  fireEvent.click(within(grid).getByRole('button', { name: /Product tour/ }));
  expect(navigate).toHaveBeenLastCalledWith({ to: '/knowledge/media', search: { item: id(2) }, replace: false });
  expect(calls.some((call) => call.path === '/api/v1/kb/media' && call.search.get('usage') === 'unused' && call.search.get('sort') === 'largest')).toBe(true);
});

test('the panel saves a name and alt text with the version it read, and a used item stays out of the trash', async () => {
  const details = { ...video, articles: [{ id: id(9), number: 12, title: 'Getting started', status: 'published', in_trash: false, current: true }] };
  const calls = serve({
    ...base,
    [`GET /api/v1/kb/media/${id(2)}/details`]: () => json(200, details),
    [`PATCH /api/v1/kb/media/${id(2)}`]: () => json(200, { ...video, name: 'Tour', updated_at: '2026-10-03T00:00:00Z' }),
    'POST /api/v1/kb/media/trash': () => json(200, { results: [{ id: id(2), status: 'in_use' }], batch_id: null }),
  });
  open('library', { item: id(2) });
  const panel = await screen.findByRole('complementary', { name: 'Product tour' });
  expect(within(panel).getByText('1920 × 1080')).toBeInTheDocument();
  expect(within(panel).getByRole('link', { name: 'Getting started' })).toHaveAttribute('href', `/knowledge/articles/${id(9)}`);
  fireEvent.change(within(panel).getByRole('textbox', { name: 'Name' }), { target: { value: 'Tour' } });
  fireEvent.change(within(panel).getByRole('textbox', { name: 'Title for screen readers' }), { target: { value: 'A tour of the inbox' } });
  fireEvent.click(within(panel).getByRole('button', { name: 'Save' }));
  await waitFor(() => expect(calls.find((call) => call.method === 'PATCH')?.body).toEqual({ updated_at: video.updated_at, name: 'Tour', alt: 'A tour of the inbox' }));
  fireEvent.click(await within(panel).findByRole('button', { name: 'Move to trash' }));
  const alert = await within(panel).findByRole('alert');
  expect(alert).toHaveTextContent('Articles still show this');
  expect(within(alert).getByRole('link', { name: 'Getting started' })).toBeInTheDocument();
});

test('the unused page moves the chosen items to the trash after saying how much, and Undo brings that batch back', async () => {
  const batch = 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb';
  const calls = serve({
    ...base,
    'POST /api/v1/kb/media/trash': () => json(200, { results: [{ id: id(3), status: 'trashed' }, { id: id(4), status: 'trashed' }, { id: id(5), status: 'in_use' }], batch_id: batch }),
    'POST /api/v1/kb/media/restore': () => json(200, { results: [{ id: id(3), status: 'restored' }, { id: id(4), status: 'restored' }] }),
  });
  open('unused');
  expect(await screen.findByText('37 items · 412.0 MiB can be freed')).toBeInTheDocument();
  expect(screen.getByText(/5 kept items · 18\.0 MiB/)).toBeInTheDocument();
  fireEvent.click(await screen.findByRole('checkbox', { name: 'Select Screenshot 3' }));
  fireEvent.click(screen.getByRole('checkbox', { name: 'Select Screenshot 5' }), { shiftKey: true });
  expect(screen.getByText('3 selected')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Move selected to trash' }));
  const dialog = screen.getByRole('alertdialog', { name: 'Move to the trash?' });
  expect(dialog).toHaveTextContent('3 items, 24.0 KiB.');
  expect(dialog).toHaveTextContent('In 1 deleted article');
  fireEvent.click(within(dialog).getByRole('button', { name: 'Move to trash' }));
  expect(await screen.findByRole('status')).toHaveTextContent('Moved 2 items to the trash. 1 item is in use again and stayed.');
  expect(calls.find((call) => call.path === '/api/v1/kb/media/trash')?.body).toEqual({ ids: [id(3), id(4), id(5)] });
  fireEvent.click(screen.getByRole('button', { name: 'Undo' }));
  await waitFor(() => expect(calls.find((call) => call.path === '/api/v1/kb/media/restore')?.body).toEqual({ batch_id: batch }));
  expect(await screen.findByText('Restored 2 items.')).toBeInTheDocument();
});

test('move all unused asks again with batch_id until nothing is left, skipping what was uploaded after the page opened', async () => {
  const batch = 'cccccccc-cccc-4ccc-8ccc-cccccccccccc';
  let rounds = 0;
  const calls = serve({ ...base, 'POST /api/v1/kb/media/trash-unused': () => json(200, { trashed: rounds++ === 0 ? 500 : 12, more: rounds === 1, batch_id: batch }) });
  open('unused');
  fireEvent.click(await screen.findByRole('button', { name: 'Move all unused to trash' }));
  const dialog = screen.getByRole('alertdialog');
  expect(dialog).toHaveTextContent('Every unused item: 37 items, 412.0 MiB.');
  expect(dialog).toHaveTextContent('Kept items are skipped.');
  fireEvent.click(within(dialog).getByRole('button', { name: 'Move to trash' }));
  expect(await screen.findByText(/Moved 512 items to the trash/)).toBeInTheDocument();
  const sent = calls.filter((call) => call.path === '/api/v1/kb/media/trash-unused').map((call) => call.body as Record<string, unknown>);
  expect(sent).toHaveLength(2);
  expect(sent[0]).toMatchObject({ include_kept: false });
  expect(sent[1]).toMatchObject({ batch_id: batch, before: sent[0]?.before });
});

test('only an admin deletes forever, after a second confirmation that names what goes', async () => {
  const trashed = item(6, { deleted_at: '2026-10-10T00:00:00Z', usage: usage(0) });
  const calls = serve({ ...base, 'GET /api/v1/kb/media/trash': () => json(200, { items: [trashed], next_cursor: null }),
    'POST /api/v1/kb/media/purge': () => json(200, { results: [{ id: id(6), status: 'purged' }] }) });
  open('trash');
  fireEvent.click(await screen.findByRole('checkbox', { name: 'Select Screenshot 6' }));
  fireEvent.click(screen.getByRole('button', { name: 'Delete forever' }));
  const dialog = screen.getByRole('alertdialog', { name: 'Delete forever?' });
  expect(dialog).toHaveTextContent('cannot be undone');
  expect(calls.some((call) => call.path === '/api/v1/kb/media/purge')).toBe(false);
  fireEvent.click(within(dialog).getByRole('button', { name: 'Delete 1 item forever' }));
  expect(await screen.findByText('Deleted 1 item for good.')).toBeInTheDocument();
  expect(calls.find((call) => call.path === '/api/v1/kb/media/purge')?.body).toEqual({ ids: [id(6)] });
});

test('an agent cannot delete forever', async () => {
  const agent = { ...me, agent: { ...me.agent, role: 'agent' } };
  serve({ ...base, 'GET /api/v1/auth/me': () => json(200, agent), 'GET /api/v1/kb/media/trash': () => json(200, { items: [item(6, { deleted_at: '2026-10-10T00:00:00Z' })], next_cursor: null }) });
  open('trash');
  fireEvent.click(await screen.findByRole('checkbox', { name: 'Select Screenshot 6' }));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Delete forever' })).toBeDisabled());
  expect(screen.getByText('Only admins can delete forever.')).toBeInTheDocument();
});

// The queue, with the parts sent through a stand-in for the network.
test('a video goes up in the parts the server asks for, resumes from the offset it holds, and an image in one request', async () => {
  const upload = { id: id(7), kind: 'video', name: 'tour', size: 20, offset: 0, part_size: 8, expires_at: '2026-10-22T00:00:00Z', media_id: null };
  let held = 0;
  const calls = serve({
    'POST /api/v1/kb/media-uploads': () => json(201, upload),
    [`GET /api/v1/kb/media-uploads/${id(7)}`]: () => json(200, { ...upload, offset: held }),
    [`POST /api/v1/kb/media-uploads/${id(7)}/complete`]: () => json(201, { ...video, created_at: new Date().toISOString() }),
    'POST /api/v1/kb/images': () => json(201, { ...item(8), created_at: '2020-01-01T00:00:00Z' }),
  });
  const parts: string[] = [];
  let first = true;
  const send: PartSender = async (_id, part, from, size, onSent) => {
    parts.push(`${from}+${part.size}/${size}`);
    // The first part lands but its answer is lost: the server holds 8 bytes, and the next try learns it.
    if (first) { first = false; held = 8; return { status: 409, body: refusal(409, 'upload_offset_mismatch', 'x'), offset: '8' }; }
    onSent(part.size);
    held = from + part.size;
    return { status: 200, body: { ...upload, offset: held }, offset: String(held) };
  };
  const stored: string[] = [];
  const queue = new UploadQueue((stored_) => stored.push(stored_.id), send);
  const refused = queue.add([new File([new Uint8Array(20)], 'tour.mp4', { type: 'video/mp4' }), new File([new Uint8Array(4)], 'shot.png', { type: 'image/png' }), new File(['x'], 'notes.txt', { type: 'text/plain' })]);
  expect(refused).toEqual(['notes.txt']);
  await waitFor(() => expect(queue.rows().every((row) => row.state === 'done')).toBe(true));
  expect(parts).toEqual(['0+8/20', '8+8/20', '16+4/20']);
  expect(calls.find((call) => call.path === '/api/v1/kb/media-uploads')?.body).toEqual({ size: 20, name: 'tour', keep: true });
  const image = calls.find((call) => call.path === '/api/v1/kb/images');
  expect(image?.search.get('keep')).toBe('true');
  expect(image?.search.get('name')).toBe('shot');
  expect(queue.rows().map((row) => row.duplicate)).toEqual([false, true]);
  expect(stored).toEqual([id(2), id(8)]);
});

test('a paused video sends nothing more until it resumes, and a cancelled one is thrown away on the server', async () => {
  const upload = { id: id(7), kind: 'video', name: 'tour', size: 16, offset: 0, part_size: 8, expires_at: '2026-10-22T00:00:00Z', media_id: null };
  let held = 0;
  const calls = serve({
    'POST /api/v1/kb/media-uploads': () => json(201, upload),
    [`GET /api/v1/kb/media-uploads/${id(7)}`]: () => json(200, { ...upload, offset: held }),
    [`DELETE /api/v1/kb/media-uploads/${id(7)}`]: () => json(204),
  });
  let release: () => void = () => undefined;
  const send: PartSender = (_id, part, from, _size, _onSent, signal) => new Promise((resolve, reject) => {
    signal.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')));
    release = () => { held = from + part.size; resolve({ status: 200, body: { ...upload, offset: held }, offset: String(held) }); };
  });
  const queue = new UploadQueue(() => undefined, send);
  queue.add([new File([new Uint8Array(16)], 'tour.webm', { type: 'video/webm' })]);
  await waitFor(() => expect(queue.rows()[0]?.state).toBe('sending'));
  const key = queue.rows()[0]!.key;
  await waitFor(() => expect(calls.some((call) => call.method === 'GET')).toBe(true));
  queue.pause(key);
  await waitFor(() => expect(queue.rows()[0]?.state).toBe('paused'));
  release();
  queue.cancel(key);
  await waitFor(() => expect(calls.some((call) => call.method === 'DELETE')).toBe(true));
  expect(queue.rows()[0]?.state).toBe('cancelled');
});
