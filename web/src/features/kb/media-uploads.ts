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
  uploadId?: string;
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
      this.jobs.push({ file, row: { key: this.next++, name: file.name, size: file.size, kind, state: 'waiting', sent: 0, bytesPerSecond: 0, duplicate: false } });
    }
    this.changed();
    void this.run();
    return refused;
  }

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
    if (job.uploadId) void mediaApi.cancelUpload(job.uploadId).catch(() => undefined);
    this.changed();
  }

  // clear forgets the rows that are finished.
  clear() {
    this.jobs = this.jobs.filter((job) => !['done', 'cancelled'].includes(job.row.state));
    this.changed();
  }

  // stop aborts what is being sent, when the page closes; the server throws an unfinished upload away a day later.
  stop() {
    for (const job of this.jobs) job.controller?.abort();
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

  // video sends the parts from the offset the server holds, then completes the upload; undefined when
  // paused or cancelled.
  private async video(job: Job, signal: AbortSignal): Promise<KBMedia | undefined> {
    const { file, row } = job;
    if (!job.uploadId) {
      const started = await mediaApi.startUpload({ size: file.size, name: baseName(file.name), keep: true });
      job.uploadId = started.id;
    }
    const id = job.uploadId;
    let upload = await mediaApi.upload(id);
    while (upload.offset < upload.size) {
      if (signal.aborted) return undefined;
      const part = file.slice(upload.offset, Math.min(upload.offset + upload.part_size, upload.size));
      const first = upload.offset;
      const started = performance.now();
      const answer = await this.send(id, part, first, upload.size, (bytes) => {
        row.sent = first + bytes;
        row.bytesPerSecond = bytes / Math.max(0.001, (performance.now() - started) / 1000);
        this.changed();
      }, signal);
      if (answer.status === 409 && answer.offset !== null) {
        // The server holds other bytes than this page thought (another tab, a part that arrived late): go on from there.
        upload = await mediaApi.upload(id);
        continue;
      }
      if (answer.status !== 200) throw new Error(errorText(answer.body) ?? mediaMessages.uploadFailed);
      upload = answer.body as typeof upload;
      row.sent = upload.offset;
    }
    if (signal.aborted) return undefined;
    try {
      const before = Date.now();
      const item = await mediaApi.completeUpload(id);
      row.duplicate = Date.parse(item.created_at) < before - 60_000;
      return item;
    } catch (error) {
      // Parts gone from the server: start over with a new upload.
      if (error instanceof ConflictError && error.detail?.code === 'upload_lost') job.uploadId = undefined;
      throw error;
    }
  }
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
  useEffect(() => () => current.stop(), [current]);
  const rows = useSyncExternalStore(current.subscribe, current.rows);
  return { queue: current, rows };
}
