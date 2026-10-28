import { useMutation } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { api } from '../../api/client';
import { ExportRefusedError } from '../../api/workspace-export';
import { track } from '../../lib/product-analytics';
import { formatWait } from '../../lib/format-wait';
import { messages } from '../../messages';
import { settingsMessages } from './settings-messages';

function refusalText(error: unknown): string {
  if (!(error instanceof ExportRefusedError)) return settingsMessages.exportFailed;
  switch (error.refusal) {
    case 'busy': return settingsMessages.exportBusy;
    case 'forbidden': return settingsMessages.exportForbidden;
    case 'server_busy': return error.retryAfterSeconds ? settingsMessages.exportServerBusy(formatWait(error.retryAfterSeconds)) : settingsMessages.exportServerBusyNoWait;
    default: return settingsMessages.exportFailed;
  }
}

// A refused download redirects the browser back to the app with `?export_error=<reason>`; these are the reasons.
const downloadRefusals: Record<string, string> = {
  link_invalid: settingsMessages.exportLinkInvalid,
  busy: settingsMessages.exportBusy,
  server_busy: settingsMessages.exportServerBusyNoWait,
  failed: settingsMessages.exportFailed,
};

// The address is cleaned by the first mounted ExportAllData, but a second one can follow: the
// settings page and the locked screen swap in the same load. The reason is kept for the page's
// lifetime so whichever copy is on screen explains it; a reload starts without it.
let landedRefusal: string | undefined;

function returnedRefusal(): string | undefined {
  const reason = new URLSearchParams(window.location.search).get('export_error');
  if (reason !== null) landedRefusal = Object.hasOwn(downloadRefusals, reason) ? downloadRefusals[reason] : undefined;
  return landedRefusal;
}

// ExportAllData is the admin's way to take the whole workspace out as a ZIP. The server checks
// the request and hands back a one-time link; navigating to it makes the browser stream the file
// to disk with its own download progress. The same button serves Settings and the locked screen.
export function ExportAllData() {
  const [returned] = useState(returnedRefusal);
  // A reload must not repeat the reason, so the address is cleaned.
  useEffect(() => {
    if (new URLSearchParams(window.location.search).has('export_error')) window.history.replaceState(null, '', window.location.pathname);
  }, []);
  const start = useMutation({
    mutationFn: api.requestExportLink,
    onSuccess: (url) => { track('workspace_export_requested'); window.location.assign(url); },
  });
  return (
    <div className="export-action">
      <div className="button-row">
        <button type="button" disabled={start.isPending} onClick={() => start.mutate()}>{messages.exportHeading}</button>
      </div>
      {start.isSuccess && <p role="status" className="muted-copy">{settingsMessages.exportStarted}</p>}
      {returned && !start.isPending && !start.isSuccess && !start.isError && <p role="alert" className="error-copy">{returned}</p>}
      {start.isError && <p role="alert" className="error-copy">{refusalText(start.error)}</p>}
    </div>
  );
}
