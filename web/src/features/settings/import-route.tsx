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

// currentJob is the one the page is about: one still open, else the newest.
export function currentJob(items: ImportJob[]): ImportJob | undefined {
  return items.find((job) => open.has(job.state)) ?? items[0];
}

export function ImportRoute() {
  const client = useQueryClient();
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const config = useQuery({ queryKey: ['public-config'], queryFn: api.config });
  const isAdmin = me.data?.agent.role === 'admin';
  const imports = useQuery({
    queryKey: ['imports'], queryFn: importsApi.list, enabled: isAdmin,
    refetchInterval: (query) => (query.state.data?.items.some((job) => moving.has(job.state)) ? pollMs : false),
  });
  const items = imports.data?.items ?? [];
  const job = currentJob(items);
  // The report is read again only when a new one exists: after the recent pass, and at the end.
  const reportReady = Boolean(job && (job.has_report || job.has_interim_report));
  const report = useQuery({
    queryKey: ['import-report', job?.id, job?.has_report, job?.state], queryFn: () => importsApi.report(job?.id ?? ''), enabled: reportReady,
  });
  const parsed = job?.kind === 'undo' ? undoReportSchema.safeParse(report.data) : importReportSchema.safeParse(report.data);
  const [connecting, setConnecting] = useState(false);
  const refresh = () => client.invalidateQueries({ queryKey: ['imports'] });
  const after = { onSuccess: () => { setConnecting(false); void refresh(); } };
  const preview = useMutation({ mutationFn: ({ domain, key }: { domain: string; key: string }) => importsApi.preview(domain, key), ...after });
  const start = useMutation({ mutationFn: importsApi.start, ...after });
  const cancel = useMutation({ mutationFn: importsApi.cancel, ...after });
  const setKey = useMutation({ mutationFn: ({ id, key }: { id: string; key: string }) => importsApi.setKey(id, key), ...after });
  const undo = useMutation({ mutationFn: importsApi.undo, ...after });
  const failed = [preview, start, cancel, setKey, undo].find((mutation) => mutation.isError)?.error;
  const error = failed ? (failed instanceof InvalidRequestError ? failed.message : importMessages.actionFailed) : undefined;
  const busy = [preview, start, cancel, setKey, undo].some((mutation) => mutation.isPending);
  if (!me.data) return <main className="settings-page" aria-busy="true" />;
  return <ImportPage isAdmin={isAdmin} showBilling={isAdmin && config.data?.billing.enabled === true}
    job={job} report={reportReady && parsed.success && job?.kind !== 'undo' ? importReportSchema.parse(report.data) : undefined}
    undoReport={reportReady && parsed.success && job?.kind === 'undo' ? undoReportSchema.parse(report.data) : undefined} history={items} timezone={me.data?.timezone ?? 'UTC'}
    loadFailed={imports.isError} error={error} busy={busy} connecting={connecting}
    onPreview={(domain, key) => preview.mutate({ domain, key })} onStart={(id) => start.mutate(id)} onCancel={(id) => cancel.mutate(id)}
    onSetKey={(id, key) => setKey.mutate({ id, key })} onUndo={(id) => undo.mutate(id)}
    onNewImport={() => { for (const mutation of [preview, start, cancel, setKey, undo]) mutation.reset(); setConnecting(true); }} />;
}
