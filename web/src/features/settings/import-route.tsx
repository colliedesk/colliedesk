import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, InvalidRequestError } from '../../api/client';
import { importReportSchema, importsApi, undoReportSchema } from '../../api/imports';
import type { ImportJob } from '../../api/imports';
import { importMessages } from './import-messages';
import { ImportPage } from './import-page';

// While a job moves, the page reads it again every few seconds: one row of the server's database.
const pollMs = 3000;
const moving = new Set<ImportJob['state']>(['previewing', 'queued', 'running', 'retry_wait']);
const open = new Set<ImportJob['state']>(['previewing', 'previewed', 'queued', 'running', 'retry_wait', 'waiting_key']);

// currentJob is the one the page is about: one still open, else the preview this page just asked for,
// else the newest import or undo. A preview that ended without starting does not hide the import
// before it (its report, its undo).
export function currentJob(items: ImportJob[], justAsked?: string): ImportJob | undefined {
  const ended = (job: ImportJob) => job.phase === 'preview' && !open.has(job.state);
  // justAsked only keeps a preview that ended on screen; once it became an import, the newest job leads.
  return items.find((job) => open.has(job.state)) ?? items.find((job) => job.id === justAsked && ended(job)) ?? items.find((job) => !ended(job)) ?? items[0];
}

// onOpenInbox follows the inbox link through the router.
export function ImportRoute({ onOpenInbox, onOpenBilling }: { onOpenInbox?: () => void; onOpenBilling?: () => void } = {}) {
  const client = useQueryClient();
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const config = useQuery({ queryKey: ['public-config'], queryFn: api.config });
  const isAdmin = me.data?.agent.role === 'admin';
  const imports = useQuery({
    queryKey: ['imports'], queryFn: importsApi.list, enabled: isAdmin,
    refetchInterval: (query) => (query.state.data?.items.some((job) => moving.has(job.state)) ? pollMs : false),
  });
  const items = imports.data?.items ?? [];
  // The preview asked for on this page stays on it when it fails, so its error shows.
  const [justAsked, setJustAsked] = useState<string>();
  const job = currentJob(items, justAsked);
  // The report is read again only when a new one exists: after the recent pass, and at the end.
  const reportReady = Boolean(job && (job.has_report || job.has_interim_report));
  const report = useQuery({
    queryKey: ['import-report', job?.id, job?.has_report, job?.state], queryFn: () => importsApi.report(job?.id ?? ''), enabled: reportReady,
  });
  const parsed = job?.kind === 'undo' ? undoReportSchema.safeParse(report.data) : importReportSchema.safeParse(report.data);
  const [connecting, setConnecting] = useState(false);
  const refresh = () => client.invalidateQueries({ queryKey: ['imports'] });
  const after = { onSuccess: () => { setConnecting(false); void refresh(); } };
  const preview = useMutation({ mutationFn: ({ domain, key }: { domain: string; key: string }) => importsApi.preview(domain, key),
    onSuccess: (created) => { setJustAsked(created.id); after.onSuccess(); } });
  const start = useMutation({ mutationFn: importsApi.start, ...after });
  // A discarded preview gives the page back to the import before it.
  const cancel = useMutation({ mutationFn: importsApi.cancel, onSuccess: (job) => { if (job.id === justAsked) setJustAsked(undefined); after.onSuccess(); } });
  const setKey = useMutation({ mutationFn: ({ id, key }: { id: string; key: string }) => importsApi.setKey(id, key), ...after });
  const undo = useMutation({ mutationFn: importsApi.undo, ...after });
  // Only the last action speaks: starting one clears what an earlier one said.
  const all = [preview, start, cancel, setKey, undo];
  const only = <T,>(mutation: { mutate: (value: T) => void }) => (value: T) => {
    for (const other of all) if (other !== mutation) other.reset();
    mutation.mutate(value);
  };
  const failed = all.find((mutation) => mutation.isError)?.error;
  const error = failed ? (failed instanceof InvalidRequestError ? failed.message : importMessages.actionFailed) : undefined;
  const busy = all.some((mutation) => mutation.isPending);
  if (!me.data) return <main className="settings-page" aria-busy="true" />;
  return <ImportPage isAdmin={isAdmin} onOpenInbox={onOpenInbox} onOpenBilling={onOpenBilling} showBilling={isAdmin && config.data?.billing.enabled === true}
    job={job} report={reportReady && parsed.success && job?.kind !== 'undo' ? importReportSchema.parse(report.data) : undefined}
    undoReport={reportReady && parsed.success && job?.kind === 'undo' ? undoReportSchema.parse(report.data) : undefined} history={items} timezone={me.data?.timezone ?? 'UTC'}
    loadFailed={imports.isError} error={error} busy={busy} connecting={connecting}
    onPreview={(domain, key) => only(preview)({ domain, key })} onStart={only(start)} onCancel={only(cancel)}
    onSetKey={(id, key) => only(setKey)({ id, key })} onUndo={only(undo)}
    onNewImport={() => { for (const mutation of all) mutation.reset(); setConnecting(true); }} />;
}
