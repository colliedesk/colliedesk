import { QueryClient, QueryClientProvider, focusManager } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
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
  const grid = await screen.findByRole('list', { name: 'Library items' });
  expect(await within(grid).findByRole('button', { name: 'Product tour, video, 1:24, 4.0 KiB, In 1 article' })).toHaveTextContent('1:24');
  // A card shows only what is out of the ordinary: an image without alt text says so; being used is not news.
  const shot = within(grid).getByRole('button', { name: 'Screenshot 1, image, 2.0 KiB, In 1 article' });
  expect(shot).toHaveTextContent('2.0 KiB · 800×600No alt');
  expect(shot).not.toHaveTextContent('In 1 article');
  // Not used counts every item the Not used page lists, kept ones too.
  expect((await screen.findByText('42 not used')).closest('p')).toHaveTextContent('42 not used · 430.0 MiB, 5 of them kept');
  fireEvent.click(screen.getByRole('link', { name: 'Review not used' }));
  expect(navigate).toHaveBeenLastCalledWith({ to: '/knowledge/media/unused', search: {}, replace: false });
  fireEvent.click(screen.getByRole('radio', { name: 'Videos' }));
  expect(navigate).toHaveBeenLastCalledWith({ to: '/knowledge/media', search: { kind: 'video' }, replace: false });
  fireEvent.change(screen.getByRole('combobox', { name: 'Use' }), { target: { value: 'missing_alt' } });
  expect(navigate).toHaveBeenLastCalledWith({ to: '/knowledge/media', search: { usage: 'missing_alt' }, replace: false });
  fireEvent.click(within(grid).getByRole('button', { name: /Product tour/ }));
  expect(navigate).toHaveBeenLastCalledWith({ to: '/knowledge/media', search: { item: id(2) }, replace: false, resetScroll: false });
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
  fireEvent.click(within(panel).getByRole('tab', { name: 'File' }));
  expect(within(panel).getByText('1920 × 1080')).toBeVisible();
  fireEvent.click(within(panel).getByRole('tab', { name: 'Used in 1' }));
  expect(within(panel).getByRole('link', { name: /^Getting started/ })).toHaveAttribute('href', `/knowledge/articles/${id(9)}`);
  fireEvent.click(within(panel).getByRole('tab', { name: 'Details' }));
  fireEvent.change(within(panel).getByRole('textbox', { name: 'Name' }), { target: { value: 'Tour' } });
  fireEvent.change(within(panel).getByRole('textbox', { name: 'Title for screen readers' }), { target: { value: 'A tour of the inbox' } });
  fireEvent.click(within(panel).getByRole('button', { name: 'Save' }));
  await waitFor(() => expect(calls.find((call) => call.method === 'PATCH')?.body).toEqual({ updated_at: video.updated_at, name: 'Tour', alt: 'A tour of the inbox' }));
  fireEvent.click(await within(panel).findByRole('button', { name: 'Move to trash' }));
  const alert = await within(panel).findByRole('alert');
  expect(alert).toHaveTextContent('Articles still show this');
  expect(within(alert).getByRole('link', { name: 'Getting started' })).toBeInTheDocument();
  // Save stays at the panel's foot; Discard puts the fields back.
  fireEvent.change(within(panel).getByRole('textbox', { name: 'Name' }), { target: { value: 'Other' } });
  expect(within(panel).getByText('Unsaved changes')).toBeInTheDocument();
  fireEvent.click(within(panel).getByRole('button', { name: 'Discard' }));
  expect(within(panel).getByRole('button', { name: 'Save' })).toBeDisabled();
});

test('the unused page moves the chosen items to the trash after saying how much, and Undo brings that batch back', async () => {
  const batch = 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb';
  const calls = serve({
    ...base,
    'POST /api/v1/kb/media/trash': () => json(200, { results: [{ id: id(3), status: 'trashed' }, { id: id(4), status: 'trashed' }, { id: id(5), status: 'in_use' }], batch_id: batch }),
    'POST /api/v1/kb/media/restore': () => json(200, { results: [{ id: id(3), status: 'restored' }, { id: id(4), status: 'restored' }] }),
  });
  open('unused');
  // The count is what the list holds, kept items included, and why the kept ones stay is said.
  expect((await screen.findByText('42 items · 430.0 MiB')).closest('p')).toHaveTextContent('42 items · 430.0 MiB are in no live article.');
  expect(screen.getByText(/37 are not kept/)).toHaveTextContent('the next on');
  expect(screen.getByText('5 are kept:').closest('p')).toHaveTextContent('they stay until you move them. Files uploaded on the Media page are kept');
  fireEvent.click(await screen.findByRole('checkbox', { name: 'Select Screenshot 3' }));
  fireEvent.click(screen.getByRole('checkbox', { name: 'Select Screenshot 5' }), { shiftKey: true });
  expect(screen.getByText('3 selected')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Move 3 to trash' }));
  const dialog = screen.getByRole('alertdialog', { name: 'Move 3 items to the trash?' });
  expect(dialog).toHaveTextContent('24.0 KiB. They stay in the trash for 30 days');
  expect(dialog).toHaveTextContent('1 is in a deleted article');
  expect(dialog).toHaveTextContent('1 is kept (Screenshot 4); restoring brings it back kept.');
  fireEvent.click(within(dialog).getByRole('button', { name: 'Move 3 to trash' }));
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
  // Not kept: only the items nobody kept go.
  fireEvent.click(await screen.findByRole('radio', { name: /^Not kept/ }));
  fireEvent.click(await screen.findByRole('button', { name: 'Move all 37 to trash' }));
  const dialog = screen.getByRole('alertdialog', { name: 'Move 37 items to the trash?' });
  expect(dialog).toHaveTextContent('412.0 MiB.');
  expect(dialog).toHaveTextContent('Kept items are skipped.');
  fireEvent.click(within(dialog).getByRole('button', { name: 'Move 37 to trash' }));
  expect(await screen.findByText(/Moved 512 items to the trash/)).toBeInTheDocument();
  const sent = calls.filter((call) => call.path === '/api/v1/kb/media/trash-unused').map((call) => call.body as Record<string, unknown>);
  expect(sent).toHaveLength(2);
  expect(sent[0]).toMatchObject({ include_kept: false });
  expect(sent[1]).toMatchObject({ batch_id: batch, before: sent[0]?.before });
});

test('kept items never lock the clean-up: all of them can go, and the confirmation says the kept ones go too', async () => {
  const calls = serve({ ...base, 'POST /api/v1/kb/media/trash-unused': () => json(200, { trashed: 42, more: false, batch_id: 'cccccccc-cccc-4ccc-8ccc-cccccccccccc' }) });
  open('unused');
  fireEvent.click(await screen.findByRole('button', { name: 'Move all 42 to trash' }));
  const dialog = screen.getByRole('alertdialog', { name: 'Move 42 items to the trash?' });
  expect(dialog).toHaveTextContent('5 kept items go too; restoring brings them back kept.');
  fireEvent.click(within(dialog).getByRole('button', { name: 'Move 42 to trash' }));
  expect(await screen.findByText('Moved 42 items to the trash.')).toBeInTheDocument();
  expect(calls.find((call) => call.path === '/api/v1/kb/media/trash-unused')?.body).toMatchObject({ include_kept: true });
  // The kept view moves what is ticked; nothing is locked there either.
  fireEvent.click(screen.getByRole('radio', { name: /^Kept/ }));
  fireEvent.click(await screen.findByRole('checkbox', { name: 'Select Screenshot 4' }));
  expect(screen.getByRole('button', { name: 'Move 1 to trash' })).toBeEnabled();
});

test('only an admin deletes forever, after a second confirmation that names what goes', async () => {
  const trashed = item(6, { deleted_at: '2026-10-10T00:00:00Z', usage: usage(0, 1, 2) });
  const calls = serve({ ...base, 'GET /api/v1/kb/media/trash': () => json(200, { items: [trashed], next_cursor: null }),
    'POST /api/v1/kb/media/purge': () => json(200, { results: [{ id: id(6), status: 'purged' }] }) });
  open('trash');
  fireEvent.click(await screen.findByRole('checkbox', { name: 'Select Screenshot 6' }));
  fireEvent.click(screen.getByRole('button', { name: 'Delete forever' }));
  const dialog = screen.getByRole('alertdialog', { name: 'Delete forever?' });
  expect(dialog).toHaveTextContent('cannot be undone');
  expect(dialog).toHaveTextContent('1 deleted article and older versions of 2 articles show them and will say "Media no longer exists".');
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
  const upload = { id: id(7), kind: 'video', name: 'tour', size: 20, offset: 0, part_size: 8, expires_at: '2026-10-22T00:00:00Z', media_id: null, created_by: null, created_at: '2026-10-21T00:00:00Z' };
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
  // The text file is a failed row of its own; nothing is sent for it.
  await waitFor(() => expect(queue.rows().filter((row) => row.kind !== 'file').every((row) => row.state === 'done')).toBe(true));
  expect(queue.rows()[2]).toMatchObject({ name: 'notes.txt', state: 'failed', permanent: true });
  expect(parts).toEqual(['0+8/20', '8+8/20', '16+4/20']);
  expect(calls.find((call) => call.path === '/api/v1/kb/media-uploads')?.body).toEqual({ size: 20, name: 'tour', keep: true });
  const image = calls.find((call) => call.path === '/api/v1/kb/images');
  expect(image?.search.get('keep')).toBe('true');
  expect(image?.search.get('name')).toBe('shot');
  expect(queue.rows().map((row) => row.duplicate)).toEqual([false, true, false]);
  expect(stored).toEqual([id(2), id(8)]);
});

test('a paused video sends nothing more until it resumes, and a cancelled one is thrown away on the server', async () => {
  const upload = { id: id(7), kind: 'video', name: 'tour', size: 16, offset: 0, part_size: 8, expires_at: '2026-10-22T00:00:00Z', media_id: null, created_by: null, created_at: '2026-10-21T00:00:00Z' };
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

test('leaving the page cancels an unfinished video upload on the server, so it holds no slot for a day', async () => {
  const upload = { id: id(7), kind: 'video', name: 'tour', size: 16, offset: 0, part_size: 8, expires_at: '2026-10-22T00:00:00Z', media_id: null, created_by: null, created_at: '2026-10-21T00:00:00Z' };
  const calls = serve({
    'POST /api/v1/kb/media-uploads': () => json(201, upload),
    [`GET /api/v1/kb/media-uploads/${id(7)}`]: () => json(200, upload),
    [`DELETE /api/v1/kb/media-uploads/${id(7)}`]: () => json(204),
  });
  const send: PartSender = (_id, _part, _from, _size, _onSent, signal) => new Promise((_resolve, reject) => {
    signal.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')));
  });
  const queue = new UploadQueue(() => undefined, send);
  queue.add([new File([new Uint8Array(16)], 'tour.webm', { type: 'video/webm' })]);
  await waitFor(() => expect(calls.some((call) => call.method === 'GET')).toBe(true));
  queue.stop();
  await waitFor(() => expect(calls.some((call) => call.method === 'DELETE')).toBe(true));
});

test('a part refused while the server still finishes the one a pause cut off is sent again after a wait', async () => {
  const upload = { id: id(7), kind: 'video', name: 'tour', size: 8, offset: 0, part_size: 8, expires_at: '2026-10-22T00:00:00Z', media_id: null, created_by: null, created_at: '2026-10-21T00:00:00Z' };
  serve({
    'POST /api/v1/kb/media-uploads': () => json(201, upload),
    [`GET /api/v1/kb/media-uploads/${id(7)}`]: () => json(200, upload),
    [`POST /api/v1/kb/media-uploads/${id(7)}/complete`]: () => json(201, { ...video, created_at: new Date().toISOString() }),
  });
  let tries = 0;
  const send: PartSender = async (_id, part) => (tries++ === 0
    ? { status: 429, body: { error: { code: 'too_many_parts', message: 'busy' } }, offset: null }
    : { status: 200, body: { ...upload, offset: part.size }, offset: String(part.size) });
  const queue = new UploadQueue(() => undefined, send);
  queue.add([new File([new Uint8Array(8)], 'tour.webm', { type: 'video/webm' })]);
  await waitFor(() => expect(queue.rows()[0]?.state).toBe('done'), { timeout: 3000 });
  expect(tries).toBe(2);
});

test('uploads in progress that this page is not sending are listed, go on with the same file, and are cancelled by whoever may', async () => {
  const open_ = { id: id(7), kind: 'video', name: 'tour', size: 16, offset: 8, part_size: 8, expires_at: '2026-10-22T00:00:00Z', media_id: null,
    created_by: { agent_id: '99999999-9999-4999-8999-999999999999', name: 'Grace' }, created_at: '2026-10-21T00:00:00Z' };
  const calls = serve({ ...base, 'GET /api/v1/kb/media-uploads': () => json(200, { items: [open_] }), [`DELETE /api/v1/kb/media-uploads/${id(7)}`]: () => json(204) });
  open();
  // The tray is one line until opened; it says there is an upload from elsewhere.
  expect(await screen.findByText('1 from elsewhere')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Show the upload' }));
  const section = await screen.findByRole('region', { name: 'Uploads in progress' });
  expect(section).toHaveTextContent('50% · started by Grace');
  const input = section.querySelector('input[type="file"]') as HTMLInputElement;
  fireEvent.click(within(section).getByRole('button', { name: 'Resume tour: choose the same file' }));
  fireEvent.change(input, { target: { files: [new File([new Uint8Array(16)], 'other.mp4', { type: 'video/mp4' })] } });
  expect(within(section).getByRole('alert')).toHaveTextContent('That is not tour');
  // An admin cancels anyone's upload.
  fireEvent.click(within(section).getByRole('button', { name: 'Cancel the upload of tour' }));
  await waitFor(() => expect(calls.some((call) => call.method === 'DELETE' && call.path === `/api/v1/kb/media-uploads/${id(7)}`)).toBe(true));
});

test('an agent sees uploads in progress but cancels only their own', async () => {
  const agent = { ...me, agent: { ...me.agent, role: 'agent' } };
  const open_ = { id: id(7), kind: 'video', name: 'tour', size: 16, offset: 0, part_size: 8, expires_at: '2026-10-22T00:00:00Z', media_id: null,
    created_by: { agent_id: '99999999-9999-4999-8999-999999999999', name: 'Grace' }, created_at: '2026-10-21T00:00:00Z' };
  serve({ ...base, 'GET /api/v1/auth/me': () => json(200, agent), 'GET /api/v1/kb/media-uploads': () => json(200, { items: [open_] }) });
  open();
  // The tray is one line until opened; it says there is an upload from elsewhere.
  expect(await screen.findByText('1 from elsewhere')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Show the upload' }));
  const section = await screen.findByRole('region', { name: 'Uploads in progress' });
  await waitFor(() => expect(within(section).queryByRole('button', { name: 'Cancel the upload of tour' })).not.toBeInTheDocument());
  expect(within(section).getByRole('button', { name: /^Resume tour/ })).toBeInTheDocument();
});

test('a file that cannot go up opens the tray with why, instead of a full bar; Dismiss takes the row away', async () => {
  serve(base);
  open();
  // With nothing open, the panel says what it is for, so opening an item never moves the grid.
  expect(await screen.findByText(/Select an image or video to see its alt text/)).toBeInTheDocument();
  const input = document.querySelector('input[type="file"][multiple]') as HTMLInputElement;
  fireEvent.change(input, { target: { files: [new File(['x'], 'release-notes.txt', { type: 'text/plain' })] } });
  const tray = await screen.findByRole('region', { name: 'Uploads' });
  expect(tray).toHaveTextContent('1 not uploaded');
  expect(within(tray).getByRole('alert')).toHaveTextContent('Not an image or video (PNG, JPEG, GIF, WebP, MP4, WebM).');
  expect(within(tray).queryByRole('progressbar')).not.toBeInTheDocument();
  expect(within(tray).getByRole('button', { name: 'Collapse uploads' })).toHaveAttribute('aria-expanded', 'true');
  fireEvent.click(within(tray).getByRole('button', { name: 'Dismiss release-notes.txt' }));
  await waitFor(() => expect(screen.queryByRole('region', { name: 'Uploads' })).not.toBeInTheDocument());
});

test('a new version of the item arriving while the person types (a poster drawn after the upload) keeps what they typed', async () => {
  let version = 0;
  serve({ ...base, [`GET /api/v1/kb/media/${id(2)}/details`]: () => json(200, { ...video, updated_at: `2026-10-0${2 + version++}T00:00:00Z`, poster_url: version > 1 ? '/p' : null, articles: [] }) });
  open('library', { item: id(2) });
  const panel = await screen.findByRole('complementary', { name: 'Product tour' });
  fireEvent.change(within(panel).getByRole('textbox', { name: 'Name' }), { target: { value: 'Tour' } });
  // The page comes back to the front: the panel reads the item again and gets a newer version.
  await act(async () => { focusManager.setFocused(false); focusManager.setFocused(true); });
  await waitFor(() => expect(version).toBeGreaterThan(1));
  expect(within(panel).getByRole('textbox', { name: 'Name' })).toHaveValue('Tour');
  expect(within(panel).getByRole('button', { name: 'Save' })).toBeEnabled();
  focusManager.setFocused(undefined);
});

test('a save refused because only the poster changed since goes again over the new version', async () => {
  let reads = 0;
  const calls = serve({ ...base,
    [`GET /api/v1/kb/media/${id(2)}/details`]: () => json(200, { ...video, updated_at: reads++ === 0 ? video.updated_at : '2026-10-09T00:00:00Z', articles: [] }),
    [`PATCH /api/v1/kb/media/${id(2)}`]: (call) => ((call.body as { updated_at: string }).updated_at === video.updated_at
      ? refusal(409, 'changed_elsewhere', 'This was changed by someone else.') : json(200, { ...video, name: 'Tour', updated_at: '2026-10-10T00:00:00Z' })) });
  open('library', { item: id(2) });
  const panel = await screen.findByRole('complementary', { name: 'Product tour' });
  fireEvent.change(within(panel).getByRole('textbox', { name: 'Name' }), { target: { value: 'Tour' } });
  fireEvent.click(within(panel).getByRole('button', { name: 'Save' }));
  await waitFor(() => expect(calls.filter((call) => call.method === 'PATCH').map((call) => call.body)).toEqual([
    { updated_at: video.updated_at, name: 'Tour' }, { updated_at: '2026-10-09T00:00:00Z', name: 'Tour' }]));
  expect(within(panel).queryByRole('alert')).not.toBeInTheDocument();
});

test('the tray closes only when every upload is over, and closing clears them', async () => {
  let refuse: (response: Response) => void = () => undefined;
  serve({ ...base, 'POST /api/v1/kb/images': () => new Promise<Response>((resolve) => { refuse = resolve; }) });
  open();
  await screen.findByText(/Select an image or video/);
  const input = document.querySelector('input[type="file"][multiple]') as HTMLInputElement;
  // An upload still going keeps the tray: there is no way to hide it.
  fireEvent.change(input, { target: { files: [new File([new Uint8Array(4)], 'shot.png', { type: 'image/png' }), new File(['x'], 'notes.txt', { type: 'text/plain' })] } });
  const tray = await screen.findByRole('region', { name: /^Upload/ });
  expect(within(tray).queryByRole('button', { name: /^Close uploads/ })).not.toBeInTheDocument();
  // The server refuses the image: now every row is over, and the tray can close.
  await act(async () => { refuse(refusal(413, 'too_large', 'Too large.')); });
  fireEvent.click(await within(tray).findByRole('button', { name: /^Close uploads/ }));
  await waitFor(() => expect(screen.queryByRole('region', { name: /^Upload/ })).not.toBeInTheDocument());
  // A new file shows it again, without the cleared rows.
  fireEvent.change(input, { target: { files: [new File(['x'], 'other.txt', { type: 'text/plain' })] } });
  expect(await screen.findByRole('region', { name: /^Upload/ })).not.toHaveTextContent('notes.txt');
});

test('on a phone the panel is a modal sheet: a dialog, and Tab from its last control comes back to its first', async () => {
  const phone = { matches: true, addEventListener: () => undefined, removeEventListener: () => undefined };
  vi.stubGlobal('matchMedia', (text: string) => (text.includes('44rem') ? phone : { ...phone, matches: false }));
  serve({ ...base, [`GET /api/v1/kb/media/${id(2)}/details`]: () => json(200, { ...video, articles: [] }) });
  open('library', { item: id(2) });
  const sheet = await screen.findByRole('dialog', { name: 'Product tour' });
  expect(sheet).toHaveAttribute('aria-modal', 'true');
  const save = within(sheet).getByRole('button', { name: 'Save' });
  fireEvent.change(within(sheet).getByRole('textbox', { name: 'Name' }), { target: { value: 'Tour' } });
  save.focus();
  fireEvent.keyDown(save, { key: 'Tab' });
  expect(document.activeElement).toBe(within(sheet).getByRole('button', { name: 'Close details' }));
});

test('on a phone, Tab from the last control the person can reach wraps in the sheet, past the controls of hidden tabs', async () => {
  const phone = { matches: true, addEventListener: () => undefined, removeEventListener: () => undefined };
  vi.stubGlobal('matchMedia', (text: string) => (text.includes('44rem') ? phone : { ...phone, matches: false }));
  serve({ ...base, [`GET /api/v1/kb/media/${id(2)}/details`]: () => json(200, { ...video, articles: [{ id: id(9), number: 12, title: 'Getting started', status: 'published', in_trash: false, current: true }] }) });
  open('library', { item: id(2) });
  const sheet = await screen.findByRole('dialog', { name: 'Product tour' });
  // Nothing changed: Save and Discard are disabled, so the Keep box is the last control on screen.
  const keep = within(sheet).getByRole('checkbox', { name: /^Keep even when no article uses it/ });
  keep.focus();
  fireEvent.keyDown(keep, { key: 'Tab' });
  expect(document.activeElement).toBe(within(sheet).getByRole('button', { name: 'Close details' }));
});
