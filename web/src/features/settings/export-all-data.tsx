import { useMutation } from '@tanstack/react-query';
import { useState } from 'react';
import { api } from '../../api/client';
import { ExportRefusedError, saveArchive } from '../../api/workspace-export';
import { formatBytes } from '../../lib/format-bytes';
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

// ExportAllData is the admin's way to take the whole workspace out as a ZIP (GET /api/v1/export).
// The same button serves Settings and the locked screen.
export function ExportAllData() {
  const [received, setReceived] = useState(0);
  const download = useMutation({
    mutationFn: async () => {
      setReceived(0);
      const archive = await api.exportWorkspace(setReceived);
      saveArchive(archive);
      return archive;
    },
  });
  return (
    <div className="export-action">
      <div className="button-row">
        <button type="button" disabled={download.isPending} onClick={() => download.mutate()}>{messages.exportHeading}</button>
      </div>
      {download.isPending && <p role="status" className="muted-copy">{messages.exportRunning(formatBytes(received))}</p>}
      {download.isSuccess && <p role="status" className="muted-copy">{messages.exportDone(download.data.filename)}</p>}
      {download.isError && <p role="alert" className="error-copy">{refusalText(download.error)}</p>}
    </div>
  );
}
