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

test('a file that is not an image or a video, or is over its limit, is a failed row that says why, and nothing is sent for it', async () => {
  const calls = serve({});
  const queue = new UploadQueue(() => undefined, send, async () => undefined);
  const refused = queue.add([sized('notes.txt', 'text/plain', 5), sized('huge.png', 'image/png', 6 * 1024 * 1024), sized('long.mp4', 'video/mp4', 230 * 1024 * 1024)]);
  expect(refused).toEqual(['notes.txt']);
  expect(queue.rows().map((row) => [row.name, row.state, row.permanent, row.problem])).toEqual([
    ['notes.txt', 'failed', true, 'Not an image or video (PNG, JPEG, GIF, WebP, MP4, WebM).'],
    ['huge.png', 'failed', true, '6.0 MiB is over the 5.0 MiB image limit. Not uploaded.'],
    ['long.mp4', 'failed', true, '230.0 MiB is over the 200.0 MiB video limit. Not uploaded.'],
  ]);
  expect(calls).toHaveLength(0);
  queue.dismiss(queue.rows()[1]!.key);
  expect(queue.rows().map((row) => row.name)).toEqual(['notes.txt', 'long.mp4']);
});

test('fileProblem names the type or the size a file breaks, the same limits as the server', () => {
  expect(fileProblem(sized('a.png', 'image/png', 5 * 1024 * 1024))).toBeUndefined();
  expect(fileProblem(sized('a.png', 'image/png', 5 * 1024 * 1024 + 1))).toBe('size');
  expect(fileProblem(sized('a.mov', '', 10))).toBeUndefined();
  expect(fileProblem(sized('a.pdf', 'application/pdf', 10))).toBe('type');
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
