import { useId, useState } from 'react';
import type { FormEvent, ReactNode } from 'react';
import type { ImportJob, ImportPreview, ImportReport, UndoReport } from '../../api/imports';
import { reportDownloadPath } from '../../api/imports';
import { formatDate } from '../../lib/format-date';
import { formatWait } from '../../lib/format-wait';
import { importMessages as m } from './import-messages';
import { SettingsNav } from './settings-nav';

const defaultRecentDays = 90;
// Freshdesk API calls one ticket takes: the ticket, its conversations and its time entries.
const callsPerTicket = 3;

export interface ImportPageProps {
  isAdmin: boolean;
  // showBilling keeps Plan and billing in the settings menu on the hosted cloud.
  showBilling?: boolean;
  // job is the import this page is about: the one running or waiting, else the newest.
  job?: ImportJob;
  // report is the job's report (interim while only the recent tickets are in).
  report?: ImportReport;
  // undoReport is what an undo job removed.
  undoReport?: UndoReport;
  history: ImportJob[];
  timezone: string;
  loadFailed?: boolean;
  // error is the answer of the last action that failed.
  error?: string;
  busy?: boolean;
  // connecting shows the form even when a job ended, for a new import.
  connecting?: boolean;
  onPreview: (domain: string, apiKey: string) => void;
  onStart: (id: string) => void;
  onCancel: (id: string) => void;
  onSetKey: (id: string, apiKey: string) => void;
  onUndo: (id: string) => void;
  onNewImport: () => void;
}

const recentDays = (job: ImportJob) => job.options.recent_days ?? defaultRecentDays;

export function ImportPage(props: ImportPageProps) {
  const { isAdmin, job, history, loadFailed } = props;
  return (
    <main className="settings-page panel import-page">
      <header><h1>{m.heading}</h1><p>{m.description}</p></header>
      <div className="settings-layout">
        <SettingsNav page="/settings/import-freshdesk" shown={{ general: isAdmin, team: isAdmin, email: isAdmin, webForm: isAdmin, savedReplies: true, exportData: isAdmin, billing: Boolean(props.showBilling), importData: isAdmin }} />
        <div className="settings-sections">
          {!isAdmin ? <p className="muted-copy">{m.adminsOnly}</p> : <>
            {loadFailed && <p role="alert" className="error-copy">{m.loadFailed}</p>}
            {props.error && <p role="alert" className="error-copy">{props.error}</p>}
            <section className="settings-card" aria-labelledby="import-current-heading">
              <Current {...props} />
            </section>
            {history.filter((item) => item.id !== job?.id && item.state !== 'discarded').length > 0 && <History {...props} />}
          </>}
        </div>
      </div>
    </main>
  );
}

function Current(props: ImportPageProps) {
  const { job, connecting } = props;
  if (!job || connecting || job.state === 'discarded') return <ConnectForm {...props} />;
  if (job.kind === 'undo') return <UndoState job={job} report={props.undoReport} />;
  switch (job.state) {
    case 'previewing':
      return <Heading title={m.connectHeading}><p role="status">{m.reading(job.domain)}</p>
        <div className="button-row"><button type="button" onClick={() => props.onCancel(job.id)} disabled={props.busy}>{m.discard}</button></div></Heading>;
    case 'previewed':
      return <Found {...props} job={job} preview={job.preview} />;
    case 'queued': case 'running': case 'retry_wait':
      return <Running {...props} job={job} />;
    case 'waiting_key':
      return <KeyAgain {...props} job={job} />;
    case 'finished':
      return <Finished {...props} job={job} />;
    default:
      return <Stopped {...props} job={job} />;
  }
}

function Heading({ title, help, children }: { title: string; help?: string; children: ReactNode }) {
  return <>
    <div className="section-heading"><h2 id="import-current-heading">{title}</h2>{help && <p>{help}</p>}</div>
    <div className="settings-body">{children}</div>
  </>;
}

function ConnectForm({ onPreview, busy }: ImportPageProps) {
  const [domain, setDomain] = useState('');
  const [key, setKey] = useState('');
  const [problem, setProblem] = useState<string>();
  const ids = { domain: useId(), key: useId(), keyHelp: useId() };
  const submit = (event: FormEvent) => {
    event.preventDefault();
    const host = domain.trim().toLowerCase().replace(/^https:\/\//, '').replace(/\/$/, '');
    if (!/^[a-z0-9]([a-z0-9-]*[a-z0-9])?\.freshdesk\.com$/.test(host)) return setProblem(m.domainInvalid);
    if (!key.trim()) return setProblem(m.keyMissing);
    setProblem(undefined);
    onPreview(host, key.trim());
  };
  return <Heading title={m.connectHeading} help={m.connectHelp}>
    <form className="import-connect" onSubmit={submit} noValidate>
      <label htmlFor={ids.domain}>{m.domainLabel}</label>
      <input id={ids.domain} name="domain" autoComplete="off" spellCheck={false} placeholder={m.domainPlaceholder} value={domain} onChange={(event) => setDomain(event.target.value)} />
      <label htmlFor={ids.key}>{m.keyLabel}</label>
      <input id={ids.key} name="api_key" type="password" autoComplete="off" aria-describedby={ids.keyHelp} value={key} onChange={(event) => setKey(event.target.value)} />
      <small id={ids.keyHelp} className="muted-copy">{m.keyHelp}</small>
      {problem && <p role="alert" className="error-copy">{problem}</p>}
      <div className="button-row"><button type="submit" className="primary-action" disabled={busy}>{m.readAccount}</button></div>
    </form>
    <p className="muted-copy">{m.freePlanNote}</p>
  </Heading>;
}

// Ledger is the page's one table: what Freshdesk holds against what Colliedesk holds, the way the
// reconcile report compares them. Before the import only the Freshdesk column is known.
function Ledger({ rows, imported }: { rows: { label: string; source: ReactNode; imported?: number; failed?: number }[]; imported: boolean }) {
  return (
    <table className="import-ledger">
      <thead><tr><th scope="col"><span className="sr-only">{m.rowData}</span></th><th scope="col">{m.inFreshdesk}</th>
        {imported && <><th scope="col">{m.inColliedesk}</th><th scope="col">{m.failedColumn}</th></>}</tr></thead>
      <tbody>{rows.map((row) => <tr key={row.label} data-short={imported && row.failed ? true : undefined}>
        <th scope="row">{row.label}</th><td>{row.source}</td>
        {imported && <><td>{(row.imported ?? 0).toLocaleString('en')}</td><td>{row.failed ? row.failed.toLocaleString('en') : '—'}</td></>}
      </tr>)}</tbody>
    </table>
  );
}

const count = (value: number | null | undefined) => (value === null || value === undefined ? m.unknownCount : value.toLocaleString('en'));

function estimate(preview: ImportPreview): string | undefined {
  const tickets = Math.max(preview.tickets_outside_archive ?? 0, preview.highest_listed_ticket_id);
  if (!preview.rate_limit_per_minute || tickets === 0) return undefined;
  const minutes = (tickets * callsPerTicket) / (preview.rate_limit_per_minute * 0.8);
  return formatWait(minutes * 60);
}

function Found({ job, preview, onStart, onCancel, busy }: ImportPageProps & { job: ImportJob; preview: ImportPreview | null }) {
  if (!preview) return null;
  const tickets = preview.tickets_outside_archive;
  const time = estimate(preview);
  return <Heading title={m.foundHeading(job.domain)} help={m.foundHelp}>
    <Ledger imported={false} rows={[
      { label: m.rowTickets, source: tickets === null ? m.unknownCount : m.atLeast(Math.max(tickets, 0)) },
      { label: m.rowContacts, source: count(preview.contacts) },
      { label: m.rowCompanies, source: count(preview.companies) },
      { label: m.rowAgents, source: count(preview.agents.length) },
    ]} />
    <small className="muted-copy">{m.ticketsNote}</small>
    {preview.archive === 'refused' && <p className="warning-note">{m.archiveRefused}</p>}
    {preview.canned_responses === 'refused' && <p className="warning-note">{m.cannedRefused}</p>}
    {preview.knowledge_base === 'refused' && <p className="warning-note">{m.knowledgeBaseRefused}</p>}
    {recentDays(job) > 0 && <p>{m.recentFirst(recentDays(job))}</p>}
    {time && <p className="muted-copy">{m.estimate(time, preview.rate_limit_per_minute)}</p>}
    <div className="button-row">
      <button type="button" className="primary-action" onClick={() => onStart(job.id)} disabled={busy}>{m.startImport}</button>
      <button type="button" onClick={() => onCancel(job.id)} disabled={busy}>{m.discard}</button>
    </div>
  </Heading>;
}

function Phases({ job }: { job: ImportJob }) {
  const days = recentDays(job);
  if (days === 0) return null;
  const recentDone = job.phase === 'full';
  return (
    <ol className="import-phases" aria-label={m.phases}>
      <li data-state={recentDone ? 'done' : 'current'} aria-current={recentDone ? undefined : 'step'}>{m.phaseRecent(days)}</li>
      <li data-state={recentDone ? 'current' : 'next'} aria-current={recentDone ? 'step' : undefined}>{m.phaseFull}</li>
    </ol>
  );
}

function Running({ job, onCancel, busy, timezone, report }: ImportPageProps & { job: ImportJob }) {
  const progress = job.progress;
  const percent = progress && progress.total > 0 ? Math.min(100, Math.round((progress.done / progress.total) * 100)) : undefined;
  const waitingUntil = progress?.rate_limit_until && Date.parse(progress.rate_limit_until) > Date.now() ? progress.rate_limit_until : undefined;
  const when = job.next_run_at ? formatDate(job.next_run_at, timezone) : '';
  const retry = job.state === 'retry_wait'
    ? job.error_code === 'retrying_failed_items' ? m.retryingFailures(when) : job.error_code === 'import_running_elsewhere' ? m.waitingOtherImport(when) : m.retrying(when)
    : undefined;
  return <Heading title={m.heading} help={job.domain}>
    <Phases job={job} />
    {job.phase === 'full' && job.has_interim_report && <p role="status" className="import-ready">{m.recentDone}</p>}
    {job.state === 'queued' && <p role="status">{m.queued}</p>}
    {retry && <p className="warning-note">{retry}</p>}
    {progress && job.state === 'running' && <div className="usage" aria-live="polite">
      <span>{m.stepCounts(progress.step, progress.done, progress.total)}</span>
      {percent !== undefined && <div className="usage-bar" role="progressbar" aria-label={progress.step} aria-valuemin={0} aria-valuemax={100} aria-valuenow={percent}><span style={{ width: `${percent}%` }} /></div>}
      {waitingUntil ? <small className="muted-copy">{m.rateLimitWait(formatDate(waitingUntil, timezone))}</small>
        : progress.eta_seconds !== undefined && <small className="muted-copy">{m.eta(formatWait(progress.eta_seconds))}</small>}
      <small className="muted-copy">{m.apiCalls(progress.api_calls)}</small>
    </div>}
    {report && <ReportLedger report={report} />}
    {job.cancel_requested ? <p role="status">{m.stopping}</p>
      : <div className="button-row"><button type="button" onClick={() => onCancel(job.id)} disabled={busy}>{m.stopImport}</button></div>}
  </Heading>;
}

function KeyAgain({ job, onSetKey, onCancel, busy }: ImportPageProps & { job: ImportJob }) {
  const [key, setKey] = useState('');
  const id = useId();
  return <Heading title={m.keyHeading} help={m.keyHelpAgain}>
    <form className="import-connect" onSubmit={(event) => { event.preventDefault(); if (key.trim()) onSetKey(job.id, key.trim()); }}>
      <label htmlFor={id}>{m.keyLabel}</label>
      <input id={id} name="api_key" type="password" autoComplete="off" value={key} onChange={(event) => setKey(event.target.value)} />
      <div className="button-row">
        <button type="submit" className="primary-action" disabled={busy || !key.trim()}>{m.continueImport}</button>
        <button type="button" onClick={() => onCancel(job.id)} disabled={busy}>{m.stopImport}</button>
      </div>
    </form>
  </Heading>;
}

function ReportLedger({ report }: { report: ImportReport }) {
  const rows = ([
    [m.rowTickets, report.tickets], [m.rowMessages, report.messages], [m.rowAttachments, report.attachments], [m.rowContacts, report.contacts],
    [m.rowTimeEntries, report.time_entries], [m.rowSavedReplies, report.saved_replies], [m.rowArticles, report.knowledge_base?.articles],
  ] as const).filter(([, value]) => value && (value.source ?? 0) + (value.imported ?? 0) > 0)
    .map(([label, value]) => ({ label, source: (value?.source ?? 0).toLocaleString('en'), imported: value?.imported, failed: value?.failed }));
  if (rows.length === 0) return null;
  return <figure className="import-report">
    {report.interim && <figcaption className="settings-group-label">{m.interimReport}</figcaption>}
    <Ledger imported rows={rows} />
  </figure>;
}

function Finished({ job, report, onUndo, onNewImport, busy }: ImportPageProps & { job: ImportJob }) {
  const status = job.status ?? 'FAILED';
  return <Heading title={m[`status${status}`]} help={job.domain}>
    <p className="import-status" data-status={status}>{m[`explain${status}`]}</p>
    {report && <ReportLedger report={report} />}
    <div className="button-row">
      <a className="link-action" href={reportDownloadPath(job.id)} download>{m.downloadReport}</a>
      <button type="button" onClick={onNewImport}>{m.newImport}</button>
      <UndoButton onUndo={() => onUndo(job.id)} busy={busy} />
    </div>
  </Heading>;
}

function Stopped({ job, report, onUndo, onNewImport, busy }: ImportPageProps & { job: ImportJob }) {
  const imported = job.has_interim_report || job.has_report || job.phase === 'full';
  const explain = job.state === 'cancelled' ? m.cancelled : (job.error_code && m.failure[job.error_code]) || m.failure.import_error;
  return <Heading title={job.state === 'cancelled' ? m.states.cancelled : m.failedHeading} help={job.domain}>
    <p role={job.state === 'failed' ? 'alert' : undefined} className={job.state === 'failed' ? 'error-copy' : undefined}>{explain}</p>
    {job.error_message && job.state === 'failed' && <details><summary>{m.failureDetail}</summary><p className="muted-copy">{job.error_message}</p></details>}
    {report && <ReportLedger report={report} />}
    <div className="button-row">
      <button type="button" className="primary-action" onClick={onNewImport}>{m.newImport}</button>
      {imported && job.account_source && <UndoButton onUndo={() => onUndo(job.id)} busy={busy} />}
    </div>
  </Heading>;
}

function UndoButton({ onUndo, busy }: { onUndo: () => void; busy?: boolean }) {
  const [asking, setAsking] = useState(false);
  if (!asking) return <button type="button" className="danger-action" onClick={() => setAsking(true)} disabled={busy}>{m.undoImport}</button>;
  return <div className="import-confirm" role="group" aria-label={m.undoImport}>
    <p>{m.undoConfirm}</p>
    <div className="button-row">
      <button type="button" className="danger-action" onClick={() => { setAsking(false); onUndo(); }} disabled={busy}>{m.undoConfirmAction}</button>
      <button type="button" onClick={() => setAsking(false)}>{m.keepImport}</button>
    </div>
  </div>;
}

function UndoState({ job, report }: { job: ImportJob; report?: UndoReport }) {
  if (job.state === 'finished' && report) return <Heading title={m.undoImport} help={job.domain}><p role="status">{m.undone(report.conversations)}</p></Heading>;
  if (job.state === 'failed') return <Heading title={m.undoImport} help={job.domain}><p role="alert" className="error-copy">{m.undoFailed}</p></Heading>;
  return <Heading title={m.undoImport} help={job.domain}><p role="status">{m.undoing}</p></Heading>;
}

function History({ history, job, timezone }: ImportPageProps) {
  return <section className="settings-card" aria-labelledby="import-history-heading">
    <div className="section-heading"><h2 id="import-history-heading">{m.historyHeading}</h2></div>
    <div className="settings-body">
      <ul className="import-history">
        {history.filter((item) => item.id !== job?.id && item.state !== 'discarded').map((item) => <li key={item.id}>
          <span>{item.kind === 'undo' ? m.kindUndo(item.domain) : item.domain}</span>
          <span className="muted-copy">{item.status ?? m.states[item.state]}</span>
          <time className="muted-copy" dateTime={item.created_at}>{formatDate(item.created_at, timezone)}</time>
        </li>)}
      </ul>
    </div>
  </section>;
}
