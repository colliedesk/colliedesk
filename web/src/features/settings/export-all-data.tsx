import { useMutation } from '@tanstack/react-query';
import { api } from '../../api/client';
import { ExportRefusedError } from '../../api/workspace-export';
import { formatWait } from '../../lib/format-wait';
import { messages } from '../../messages';

function refusalText(error: unknown): string {
  if (!(error instanceof ExportRefusedError)) return messages.exportFailed;
  switch (error.refusal) {
    case 'busy': return messages.exportBusy;
    case 'forbidden': return messages.exportForbidden;
    case 'server_busy': return error.retryAfterSeconds ? messages.exportServerBusy(formatWait(error.retryAfterSeconds)) : messages.exportServerBusyNoWait;
    default: return messages.exportFailed;
  }
}

// ExportAllData is the admin's way to take the whole workspace out as a ZIP. The server checks
// the request and hands back a one-time link; navigating to it makes the browser stream the file
// to disk with its own download progress. The same button serves Settings and the locked screen.
export function ExportAllData() {
  const start = useMutation({
    mutationFn: api.requestExportLink,
    onSuccess: (url) => window.location.assign(url),
  });
  return (
    <div className="export-action">
      <div className="button-row">
        <button type="button" disabled={start.isPending} onClick={() => start.mutate()}>{messages.exportHeading}</button>
      </div>
      {start.isSuccess && <p role="status" className="muted-copy">{messages.exportStarted}</p>}
      {start.isError && <p role="alert" className="error-copy">{refusalText(start.error)}</p>}
    </div>
  );
}
