import { useEffect, useRef, useSyncExternalStore } from 'react';
import { ConflictError } from '../../api/client';
import { mediaApi, sendPart } from './media-api';
import type { KBMedia, PartSender } from './media-api';
import { mediaMessages } from './media-messages';
import { reason } from './kb-tree';

// The uploads a library page sends: each file a row with its progress. An image goes in one request; a
// video in parts of the size the server names, so a lost connection or a pause resumes from the bytes
// the server holds (GET /kb/media-uploads/{id} says where).

export type UploadState = 'waiting' | 'sending' | 'paused' | 'done' | 'failed' | 'cancelled';

export interface UploadRow {
  key: number;
  name: string;
  size: number;
  kind: 'image' | 'video';
  state: UploadState;
  sent: number;
  // bytesPerSecond is measured over the current part, for the time left.
  bytesPerSecond: number;
  // duplicate is set when the library already held the same bytes.
  duplicate: boolean;
  problem?: string;
  item?: KBMedia;
}

export const imageTypes = ['image/png', 'image/jpeg', 'image/gif', 'image/webp'];
export const videoTypes = ['video/mp4', 'video/webm', 'video/quicktime'];

// kindOf names what a file is by its type, or by its extension when the browser gives none.
export function kindOf(file: File): 'image' | 'video' | undefined {
  if (imageTypes.includes(file.type)) return 'image';
  if (videoTypes.includes(file.type)) return 'video';
  const extension = file.name.toLowerCase().split('.').pop() ?? '';
  if (['png', 'jpg', 'jpeg', 'gif', 'webp'].includes(extension)) return 'image';
  if (['mp4', 'm4v', 'mov', 'webm'].includes(extension)) return 'video';
  return undefined;
}

// The name the library shows: the file name without its extension.
export const baseName = (name: string) => name.replace(/\.[^.]+$/, '').slice(0, 200);

interface Job {
  row: UploadRow;
  file: File;
  // session.id is the server's upload, from its start, so a cancel or leaving the page can end it.
  session: { id?: string };
  controller?: AbortController;
}

export class UploadQueue {
  private jobs: Job[] = [];
  private listeners = new Set<() => void>();
  private snapshot: UploadRow[] = [];
  private next = 1;
  private running = false;

  constructor(private readonly onStored: (item: KBMedia) => void, private readonly send: PartSender = sendPart) {}

  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };
  rows = () => this.snapshot;

  private changed() {
    this.snapshot = this.jobs.map((job) => ({ ...job.row }));
    for (const listener of this.listeners) listener();
  }

  // add queues files and reports the names it refused (not an image or a video).
  add(files: File[]): string[] {
    const refused: string[] = [];
    for (const file of files) {
      const kind = kindOf(file);
      if (!kind) { refused.push(file.name); continue; }
      this.jobs.push({ file, session: {}, row: { key: this.next++, name: file.name, size: file.size, kind, state: 'waiting', sent: 0, bytesPerSecond: 0, duplicate: false } });
    }
    this.changed();
    void this.run();
    return refused;
  }

  // resumeUpload goes on with an upload this page did not start (another tab, a page that closed): the
  // person chooses the same file again, and the parts go from the bytes the server holds.
  resumeUpload(file: File, uploadId: string) {
    this.jobs.push({ file, session: { id: uploadId }, row: { key: this.next++, name: file.name, size: file.size, kind: 'video', state: 'waiting', sent: 0, bytesPerSecond: 0, duplicate: false } });
    this.changed();
    void this.run();
  }

  // sessions are the server uploads this page is sending.
  sessions = () => new Set(this.jobs.map((job) => job.session.id).filter((id): id is string => Boolean(id)));

  pause(key: number) {
    const job = this.find(key);
    if (!job || job.row.kind !== 'video' || (job.row.state !== 'sending' && job.row.state !== 'waiting')) return;
    job.row.state = 'paused';
    job.controller?.abort();
    this.changed();
  }

  resume(key: number) {
    const job = this.find(key);
    if (!job || (job.row.state !== 'paused' && job.row.state !== 'failed')) return;
    job.row.state = 'waiting';
    job.row.problem = undefined;
    this.changed();
    void this.run();
  }

  cancel(key: number) {
    const job = this.find(key);
    if (!job || job.row.state === 'done' || job.row.state === 'cancelled') return;
    job.row.state = 'cancelled';
    job.controller?.abort();
    if (job.session.id) void mediaApi.cancelUpload(job.session.id).catch(() => undefined);
    this.changed();
  }

  // clear forgets the rows that are finished.
  clear() {
    this.jobs = this.jobs.filter((job) => !['done', 'cancelled'].includes(job.row.state));
    this.changed();
  }

  // stop aborts what is being sent when the page goes away, and cancels every unfinished video upload on
  // the server: the file cannot be sent again from another page, and an open upload holds one of the
  // workspace's three and its room in the plan for a day. keepalive lets the request outlive the page.
  stop() {
    for (const job of this.jobs) {
      job.controller?.abort();
      if (job.session.id && !['done', 'cancelled'].includes(job.row.state)) {
        void fetch(`/api/v1/kb/media-uploads/${job.session.id}`, { method: 'DELETE', credentials: 'same-origin', keepalive: true }).catch(() => undefined);
        job.session.id = undefined;
      }
    }
  }

  private find(key: number) { return this.jobs.find((job) => job.row.key === key); }

  // One file at a time, in the order they came: a page never holds more than one part in memory.
  private async run() {
    if (this.running) return;
    this.running = true;
    try {
      for (let job = this.jobs.find((each) => each.row.state === 'waiting'); job; job = this.jobs.find((each) => each.row.state === 'waiting')) {
        job.row.state = 'sending';
        job.controller = new AbortController();
        this.changed();
        try {
          const item = job.row.kind === 'image' ? await this.image(job) : await this.video(job, job.controller.signal);
          if (!item) continue;
          job.row = { ...job.row, state: 'done', sent: job.row.size, item };
          this.onStored(item);
        } catch (error) {
          if ((job.row.state as UploadState) === 'paused' || (job.row.state as UploadState) === 'cancelled') continue;
          job.row.state = 'failed';
          job.row.problem = reason(error, mediaMessages.uploadFailed);
        } finally {
          this.changed();
        }
      }
    } finally {
      this.running = false;
    }
  }

  private async image(job: Job) {
    const before = Date.now();
    const item = await mediaApi.uploadImage(job.file, baseName(job.file.name), true);
    job.row.duplicate = Date.parse(item.created_at) < before - 60_000;
    return item;
  }

  private async video(job: Job, signal: AbortSignal): Promise<KBMedia | undefined> {
    const { row, session } = job;
    return uploadVideo(job.file, { keep: true, signal, send: this.send, session, onProgress: (sent, bytesPerSecond) => {
        row.sent = sent;
        // 0 after a part: keep the speed the part was sent at, for the time left.
        if (bytesPerSecond > 0) row.bytesPerSecond = bytesPerSecond;
        this.changed();
      }, onDuplicate: () => { row.duplicate = true; } });
  }
}

export interface VideoUpload {
  // keep marks an upload made in the library; one pasted into an article is kept by the article.
  keep: boolean;
  signal: AbortSignal;
  send?: PartSender;
  // session holds the server's upload id across a pause, so a resume goes on from the bytes it has.
  session?: { id?: string };
  onProgress?: (sent: number, bytesPerSecond: number) => void;
  onDuplicate?: () => void;
}

// uploadVideo sends a video in the parts the server names, from the offset it holds, then completes the
// upload; undefined when aborted (a pause or a cancel).
export async function uploadVideo(file: File, options: VideoUpload): Promise<KBMedia | undefined> {
  const { signal, send = sendPart, session = {}, onProgress = () => undefined } = options;
  if (!session.id) {
    const started = await mediaApi.startUpload({ size: file.size, name: baseName(file.name), keep: options.keep });
    session.id = started.id;
  }
  const id = session.id;
  let upload = await mediaApi.upload(id);
  let busyTries = 0;
  while (upload.offset < upload.size) {
    if (signal.aborted) return undefined;
    const part = file.slice(upload.offset, Math.min(upload.offset + upload.part_size, upload.size));
    const first = upload.offset;
    const started = performance.now();
    const answer = await send(id, part, first, upload.size, (bytes) => {
      onProgress(first + bytes, bytes / Math.max(0.001, (performance.now() - started) / 1000));
    }, signal);
    if (answer.status === 429 && busyTries < 5) {
      // The server still finishes the part a pause cut off (one part of an upload at a time): wait, then ask where it stands.
      await wait(500 * 2 ** busyTries++, signal);
      upload = await mediaApi.upload(id);
      continue;
    }
    busyTries = 0;
    if (answer.status === 409 && answer.offset !== null) {
      // The server holds other bytes than this page thought (another tab, a part that arrived late): go on from there.
      upload = await mediaApi.upload(id);
      continue;
    }
    if (answer.status !== 200) throw new Error(errorText(answer.body) ?? mediaMessages.uploadFailed);
    upload = answer.body as typeof upload;
    onProgress(upload.offset, 0);
  }
  if (signal.aborted) return undefined;
  try {
    const before = Date.now();
    const item = await mediaApi.completeUpload(id);
    if (Date.parse(item.created_at) < before - 60_000) options.onDuplicate?.();
    return item;
  } catch (error) {
    // Parts gone from the server: start over with a new upload.
    if (error instanceof ConflictError && error.detail?.code === 'upload_lost') session.id = undefined;
    throw error;
  }
}

function wait(ms: number, signal: AbortSignal) {
  return new Promise<void>((resolve, reject) => {
    const timer = window.setTimeout(resolve, ms);
    signal.addEventListener('abort', () => { window.clearTimeout(timer); reject(new DOMException('Aborted', 'AbortError')); }, { once: true });
  });
}

function errorText(body: unknown): string | undefined {
  const message = (body as { error?: { message?: unknown } } | undefined)?.error?.message;
  return typeof message === 'string' ? message : undefined;
}

// useUploadQueue keeps one queue for the page and its rows as state.
export function useUploadQueue(onStored: (item: KBMedia) => void) {
  const stored = useRef(onStored);
  useEffect(() => { stored.current = onStored; }, [onStored]);
  const queue = useRef<UploadQueue | null>(null);
  queue.current ??= new UploadQueue((item) => stored.current(item));
  const current = queue.current;
  useEffect(() => {
    // Closing the tab or leaving the page by its address runs pagehide, not the unmount.
    const leave = () => current.stop();
    window.addEventListener('pagehide', leave);
    return () => { window.removeEventListener('pagehide', leave); current.stop(); };
  }, [current]);
  const rows = useSyncExternalStore(current.subscribe, current.rows);
  return { queue: current, rows };
}
