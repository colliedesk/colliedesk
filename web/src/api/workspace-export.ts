// ExportRefusedError means the server did not start the export. `busy` is another export of this
// workspace still running (409), `server_busy` the server is at its limit (429, with a wait),
// `forbidden` the caller is not an admin (403).
export type ExportRefusal = 'busy' | 'server_busy' | 'forbidden' | 'failed';

export class ExportRefusedError extends Error {
  readonly refusal: ExportRefusal;
  readonly retryAfterSeconds?: number;

  constructor(refusal: ExportRefusal, retryAfterSeconds?: number) {
    super(`export refused: ${refusal}`);
    this.name = 'ExportRefusedError';
    this.refusal = refusal;
    this.retryAfterSeconds = retryAfterSeconds;
  }
}

export interface WorkspaceArchive { blob: Blob; filename: string; bytes: number }

function refusal(response: Response): ExportRefusedError {
  if (response.status === 409) return new ExportRefusedError('busy');
  if (response.status === 403) return new ExportRefusedError('forbidden');
  if (response.status === 429) {
    const wait = Number(response.headers.get('Retry-After'));
    return new ExportRefusedError('server_busy', Number.isFinite(wait) && wait > 0 ? wait : undefined);
  }
  return new ExportRefusedError('failed');
}

function archiveName(response: Response): string {
  const match = /filename="([^"\r\n]+)"/.exec(response.headers.get('Content-Disposition') ?? '');
  return match?.[1] ?? 'colliedesk-export.zip';
}

// exportWorkspace downloads the whole-workspace ZIP. The server streams it, so a connection that
// ends early is a failed export: the stream is read to its end and any read error is thrown as
// `failed`, never a half archive handed to the person. onProgress receives the bytes read so far.
export async function exportWorkspace(onProgress?: (bytes: number) => void): Promise<WorkspaceArchive> {
  let response: Response;
  try {
    response = await fetch('/api/v1/export', { credentials: 'same-origin' });
  } catch {
    throw new ExportRefusedError('failed');
  }
  if (!response.ok) throw refusal(response);
  if (!response.body) throw new ExportRefusedError('failed');
  const reader = response.body.getReader();
  const parts: Uint8Array[] = [];
  let bytes = 0;
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      parts.push(value);
      bytes += value.byteLength;
      onProgress?.(bytes);
    }
  } catch {
    throw new ExportRefusedError('failed');
  }
  return { blob: new Blob(parts as BlobPart[], { type: 'application/zip' }), filename: archiveName(response), bytes };
}

// saveArchive hands the archive to the browser as a file download.
export function saveArchive({ blob, filename }: WorkspaceArchive): void {
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  document.body.append(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 60_000);
}
