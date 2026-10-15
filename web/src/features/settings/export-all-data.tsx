import { useMutation } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
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

// A refused download redirects the browser back to the app with `?export_error=<reason>`; these are the reasons.
const downloadRefusals: Record<string, string> = {
  link_invalid: messages.exportLinkInvalid,
  busy: messages.exportBusy,
  server_busy: messages.exportServerBusyNoWait,
  failed: messages.exportFailed,
};

function returnedRefusal(): string | undefined {
  const reason = new URLSearchParams(window.location.search).get('export_error');
  return reason !== null && Object.hasOwn(downloadRefusals, reason) ? downloadRefusals[reason] : undefined;
}

// ExportAllData is the admin's way to take the whole workspace out as a ZIP. The server checks
// the request and hands back a one-time link; navigating to it makes the browser stream the file
// to disk with its own download progress. The same button serves Settings and the locked screen.
export function ExportAllData() {
  const [returned] = useState(returnedRefusal);
  // The reason is shown once: a reload must not repeat it.
  useEffect(() => {
    if (new URLSearchParams(window.location.search).has('export_error')) window.history.replaceState(null, '', window.location.pathname);
  }, []);
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
      {returned && !start.isPending && !start.isSuccess && !start.isError && <p role="alert" className="error-copy">{returned}</p>}
      {start.isError && <p role="alert" className="error-copy">{refusalText(start.error)}</p>}
    </div>
  );
}
