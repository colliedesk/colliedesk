import { exportLinkSchema } from './schemas';

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

function refusal(response: Response): ExportRefusedError {
  if (response.status === 409) return new ExportRefusedError('busy');
  if (response.status === 403) return new ExportRefusedError('forbidden');
  if (response.status === 429) {
    const wait = Number(response.headers.get('Retry-After'));
    return new ExportRefusedError('server_busy', Number.isFinite(wait) && wait > 0 ? wait : undefined);
  }
  return new ExportRefusedError('failed');
}

// requestExportLink asks for a one-time download URL. The server runs the checks that can fail
// (not an admin, another export running, server busy) here, where the page can say why. The caller
// then navigates the browser to the URL, which streams the ZIP straight to disk: a large archive
// is never held in memory.
export async function requestExportLink(): Promise<string> {
  let response: Response;
  try {
    response = await fetch('/api/v1/export/link', { method: 'POST', credentials: 'same-origin' });
  } catch {
    throw new ExportRefusedError('failed');
  }
  if (!response.ok) throw refusal(response);
  try {
    return exportLinkSchema.parse(await response.json()).url;
  } catch {
    throw new ExportRefusedError('failed');
  }
}
