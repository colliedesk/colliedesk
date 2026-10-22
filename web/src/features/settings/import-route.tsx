import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, InvalidRequestError } from '../../api/client';
import { importReportSchema, importsApi, undoReportSchema } from '../../api/imports';
import type { ImportJob } from '../../api/imports';
import { importMessages } from './import-messages';
import { ImportPage } from './import-page';
import { ImportPeople } from './import-people';
import type { CreatedInvite } from './import-people';

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
  const preview = useMutation({ mutationFn: ({ domain, key, old }: { domain: string; key: string; old?: string }) => importsApi.preview(domain, key, old),
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
  // The Freshdesk agents of the account the page is about, once the preview read it.
  const peopleJob = job && job.kind === 'import' && job.account_source && !connecting && job.state !== 'discarded' ? job : undefined;
  const people = useQuery({
    queryKey: ['import-people', peopleJob?.id], queryFn: () => importsApi.people(peopleJob?.id ?? ''), enabled: isAdmin && Boolean(peopleJob),
    refetchInterval: (query) => (query.state.data?.items.some((person) => person.pending) ? pollMs : false),
  });
  const team = useQuery({ queryKey: ['agents'], queryFn: api.agents, enabled: isAdmin && Boolean(peopleJob) });
  const invites = useQuery({ queryKey: ['invites'], queryFn: api.invites, enabled: isAdmin && Boolean(peopleJob) });
  const peopleChanged = { onSuccess: () => { void client.invalidateQueries({ queryKey: ['import-people'] }); } };
  const link = useMutation({ mutationFn: ({ externalId, agentId }: { externalId: string; agentId: string }) => importsApi.link(peopleJob?.id ?? '', externalId, agentId), ...peopleChanged });
  const unlink = useMutation({ mutationFn: (externalId: string) => importsApi.unlink(peopleJob?.id ?? '', externalId), ...peopleChanged });
  // Each invite's one-time link shows in its person's row: the server never shows it again.
  const [createdInvites, setCreatedInvites] = useState<CreatedInvite[]>([]);
  const invite = useMutation({ mutationFn: (email: string) => api.createInvite(email, 'agent'), onSuccess: (created) => {
    setCreatedInvites((links) => [...links.filter((link) => link.inviteId !== created.invite.id),
      { inviteId: created.invite.id, email: created.invite.email, url: created.url, expiresAt: created.invite.expires_at }]);
    void client.invalidateQueries({ queryKey: ['invites'] });
  } });
  const revokeInvite = useMutation({ mutationFn: api.revokeInvite, onSuccess: (_, id) => {
    setCreatedInvites((links) => links.filter((link) => link.inviteId !== id));
    void client.invalidateQueries({ queryKey: ['invites'] });
  } });
  // As for the import's own actions, only the last people action speaks.
  const peopleActions = [link, unlink, invite, revokeInvite];
  const onlyPeople = <T,>(mutation: { mutate: (value: T) => void }) => (value: T) => {
    for (const other of peopleActions) if (other !== mutation) other.reset();
    mutation.mutate(value);
  };
  const peopleFailed = peopleActions.find((mutation) => mutation.isError)?.error;
  if (!me.data) return <main className="settings-page" aria-busy="true" />;
  // The section shows once the account was read: with its agents, or with why they could not be loaded.
  const peopleSection = peopleJob && (people.isError || (people.data && people.data.items.length > 0))
    ? <ImportPeople people={people.data?.items ?? []} agents={team.data?.items ?? []} invites={invites.data?.items ?? []} createdInvites={createdInvites}
      imported={peopleJob.phase !== 'preview'} timezone={me.data.timezone} failed={people.isError} onRetry={() => void people.refetch()}
      busy={peopleActions.some((mutation) => mutation.isPending)}
      onInvite={onlyPeople(invite)} onRevokeInvite={onlyPeople(revokeInvite)} onLink={(externalId, agentId) => onlyPeople(link)({ externalId, agentId })} onUnlink={onlyPeople(unlink)} />
    : undefined;
  const peopleError = peopleFailed ? (peopleFailed instanceof InvalidRequestError ? peopleFailed.message : importMessages.actionFailed) : undefined;
  return <ImportPage isAdmin={isAdmin} peopleSection={peopleSection} onOpenInbox={onOpenInbox} onOpenBilling={onOpenBilling} showBilling={isAdmin && config.data?.billing.enabled === true}
    job={job} report={reportReady && parsed.success && job?.kind !== 'undo' ? importReportSchema.parse(report.data) : undefined}
    undoReport={reportReady && parsed.success && job?.kind === 'undo' ? undoReportSchema.parse(report.data) : undefined} history={items} timezone={me.data?.timezone ?? 'UTC'}
    loadFailed={imports.isError} error={error ?? peopleError} busy={busy} connecting={connecting}
    onPreview={(domain, key, old) => only(preview)({ domain, key, old })} onStart={only(start)} onCancel={only(cancel)}
    onSetKey={(id, key) => only(setKey)({ id, key })} onUndo={only(undo)}
    onNewImport={() => { for (const mutation of all) mutation.reset(); setConnecting(true); }} />;
}
