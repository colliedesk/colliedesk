import { waitFor } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { UploadQueue, fileProblem } from './media-uploads';
import type { PartSender } from './media-api';
import { json, serve } from './kb.test.fixture';

afterEach(() => vi.unstubAllGlobals());

const id = (n: number) => `aaaaaaaa-aaaa-4aaa-8aaa-${String(n).padStart(12, '0')}`;
const stored = (n: number, fields: Record<string, unknown> = {}) => ({
  id: id(n), kind: 'video', content_type: 'video/mp4', size: 8, sha256: 'a'.repeat(64), url: `/api/v1/kb/media/${id(n)}`, name: 'tour', alt: '',
  width: 640, height: 360, duration_ms: 6000, codecs: ['avc1'], thumbnail_url: null, poster_url: null, kept: true, deleted_at: null,
  created_at: new Date().toISOString(), updated_at: '2026-10-02T00:00:00Z', ...fields,
});
const upload = { id: id(7), kind: 'video', name: 'tour', size: 8, offset: 0, part_size: 8, expires_at: '2026-10-22T00:00:00Z', media_id: null, created_by: null, created_at: '2026-10-21T00:00:00Z' };
const send: PartSender = async (_id, part) => ({ status: 200, body: { ...upload, offset: part.size }, offset: String(part.size) });
// A file reports the size it is given without holding the bytes.
const sized = (name: string, type: string, size: number) => { const file = new File(['x'], name, { type }); Object.defineProperty(file, 'size', { value: size }); return file; };

const refusedType = 'Not a type the library holds. Images: PNG, JPEG, GIF, WebP. Videos: MP4, WebM. Files: PDF, DOC, DOCX, XLS, XLSX, PPT, PPTX, ODT, ODS, ODP, CSV, TXT, RTF, ZIP, 7Z, TAR.GZ.';

test('a file of a refused type, or one over its limit, is a failed row that says why, and nothing is sent for it', async () => {
  const calls = serve({});
  const queue = new UploadQueue(() => undefined, send, async () => undefined);
  const refused = queue.add([sized('setup.exe', 'application/x-msdownload', 5), sized('huge.png', 'image/png', 6 * 1024 * 1024),
    sized('long.mp4', 'video/mp4', 230 * 1024 * 1024), sized('manual.pdf', 'application/pdf', 26 * 1024 * 1024), sized('page.html', 'text/html', 5)]);
  expect(refused).toEqual(['setup.exe', 'page.html']);
  expect(queue.rows().map((row) => [row.name, row.state, row.permanent, row.problem])).toEqual([
    ['setup.exe', 'failed', true, refusedType],
    ['huge.png', 'failed', true, '6.0 MiB is over the 5.0 MiB image limit. Not uploaded.'],
    ['long.mp4', 'failed', true, '230.0 MiB is over the 200.0 MiB video limit. Not uploaded.'],
    ['manual.pdf', 'failed', true, '26.0 MiB is over the 25.0 MiB file limit. Not uploaded.'],
    ['page.html', 'failed', true, refusedType],
  ]);
  expect(calls).toHaveLength(0);
  queue.dismiss(queue.rows()[1]!.key);
  expect(queue.rows().map((row) => row.name)).toEqual(['setup.exe', 'long.mp4', 'manual.pdf', 'page.html']);
});

test('fileProblem names the type or the size a file breaks, the same limits as the server', () => {
  expect(fileProblem(sized('a.png', 'image/png', 5 * 1024 * 1024))).toBeUndefined();
  expect(fileProblem(sized('a.png', 'image/png', 5 * 1024 * 1024 + 1))).toBe('size');
  expect(fileProblem(sized('a.mov', '', 10))).toBeUndefined();
  expect(fileProblem(sized('a.pdf', 'application/pdf', 25 * 1024 * 1024))).toBeUndefined();
  expect(fileProblem(sized('a.pdf', 'application/pdf', 25 * 1024 * 1024 + 1))).toBe('size');
  expect(fileProblem(sized('backup.tar.gz', 'application/gzip', 10))).toBeUndefined();
  expect(fileProblem(sized('a.svg', 'image/svg+xml', 10))).toBe('type');
  expect(fileProblem(sized('run.sh', '', 10))).toBe('type');
});

test('a PDF goes to the library as a file in parts, under its full name, and no poster is drawn for it', async () => {
  const pdf = stored(3, { kind: 'file', content_type: 'application/pdf', name: 'Warranty form.pdf', width: null, height: null, duration_ms: null, codecs: [] });
  const fileUpload = { ...upload, kind: 'file', name: 'Warranty form.pdf' };
  const calls = serve({
    'POST /api/v1/kb/media-uploads': () => json(201, fileUpload),
    [`GET /api/v1/kb/media-uploads/${id(7)}`]: () => json(200, fileUpload),
    [`POST /api/v1/kb/media-uploads/${id(7)}/complete`]: () => json(201, pdf),
  });
  const heard: string[] = [];
  let drawn = 0;
  const queue = new UploadQueue((item) => heard.push(item.name), send, async () => { drawn++; return undefined; });
  expect(queue.add([new File([new Uint8Array(8)], 'Warranty form.pdf', { type: 'application/pdf' })])).toEqual([]);
  await waitFor(() => expect(queue.rows()[0]).toMatchObject({ kind: 'file', state: 'done', sent: 8 }));
  expect(calls.find((call) => call.path === '/api/v1/kb/media-uploads')?.body).toEqual({ kind: 'file', size: 8, name: 'Warranty form.pdf', keep: true });
  expect(heard).toEqual(['Warranty form.pdf']);
  expect(drawn).toBe(0);
});

test('a video stored without a poster gets one drawn from its file, and a frame that cannot be drawn leaves the upload done', async () => {
  const calls = serve({
    'POST /api/v1/kb/media-uploads': () => json(201, upload),
    [`GET /api/v1/kb/media-uploads/${id(7)}`]: () => json(200, upload),
    [`POST /api/v1/kb/media-uploads/${id(7)}/complete`]: () => json(201, stored(2)),
    [`PUT /api/v1/kb/media/${id(2)}/poster`]: () => json(200, stored(2, { poster_url: `/api/v1/kb/media/${id(2)}/poster?v=1` })),
  });
  const heard: (string | null)[] = [];
  const frame = new Blob(['jpeg'], { type: 'image/jpeg' });
  const queue = new UploadQueue((item) => heard.push(item.poster_url), send, async () => frame);
  queue.add([new File([new Uint8Array(8)], 'tour.mp4', { type: 'video/mp4' })]);
  await waitFor(() => expect(heard).toEqual([null, `/api/v1/kb/media/${id(2)}/poster?v=1`]));
  const poster = calls.find((call) => call.method === 'PUT');
  expect(poster?.body).toBe(frame);
  expect(poster?.headers['Content-Type']).toBe('image/jpeg');

  const failing = new UploadQueue(() => undefined, send, async () => { throw new Error('no decoder'); });
  failing.add([new File([new Uint8Array(8)], 'tour.mp4', { type: 'video/mp4' })]);
  await waitFor(() => expect(failing.rows()[0]?.state).toBe('done'));
});
