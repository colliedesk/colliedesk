import { lazy, Suspense, useEffect, useId, useRef, useState } from 'react';
import type { FormEvent, ReactNode } from 'react';
import type { ImportJob, ImportPreview, ImportReport, UndoReport } from '../../api/imports';
import { reportDownloadPath } from '../../api/imports';
import { formatDate } from '../../lib/format-date';
import { formatWait } from '../../lib/format-wait';
import { estimateRange } from './import-estimate';
import { ImportLinksSummary } from './import-links';
import { importMessages as m } from './import-messages';
import { ImportSteps } from './import-steps';
import { onlyArchiveRefused } from './import-verdict';
import { SettingsNav } from './settings-nav';

const defaultRecentDays = 90;

// The Search Console section is rarely shown and loads on its own, so the first bundle does not carry it.
const ImportSearchConsoleSummary = lazy(() => import('./import-search-console'));

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
  // peopleSection lists the Freshdesk agents of the account, once it was read.
  peopleSection?: ReactNode;
  timezone: string;
  loadFailed?: boolean;
  // error is the answer of the last action that failed.
  error?: string;
  busy?: boolean;
  // connecting shows the form even when a job ended, for a new import.
  connecting?: boolean;
  // oldHelpCenterDomain is the old help center's own domain, only when one was typed.
  onPreview: (domain: string, apiKey: string, oldHelpCenterDomain?: string) => void;
  onStart: (id: string) => void;
  onCancel: (id: string) => void;
  onSetKey: (id: string, apiKey: string) => void;
  onUndo: (id: string) => void;
  // onNewImport opens the form again, with the address of a preview that failed.
  onNewImport: () => void;
  // embedded shows the import inside another page (the setup flow), which has the title and the frame.
  embedded?: boolean;
}

const recentDays = (job: ImportJob) => job.options.recent_days ?? defaultRecentDays;
// A preview that never started is not an earlier import.
const shownHistory = (history: ImportJob[], job?: ImportJob) => history.filter((item) => item.id !== job?.id && item.phase !== 'preview');

export function ImportPage(props: ImportPageProps) {
  const { isAdmin, job, history, loadFailed } = props;
  const sections = <div className="settings-sections">
    {!isAdmin ? <p className="muted-copy">{m.adminsOnly}</p> : <>
      {loadFailed && <p role="alert" className="error-copy">{m.loadFailed}</p>}
      {props.error && <p role="alert" className="error-copy">{props.error}</p>}
      <section className="settings-card" aria-labelledby="import-current-heading">
        <Current {...props} />
      </section>
      {props.peopleSection}
      {shownHistory(history, job).length > 0 && <History {...props} />}
    </>}
  </div>;
  if (props.embedded) return <div className="import-page import-embedded">{sections}</div>;
  return (
    <main className="settings-page panel import-page">
      <header><h1>{m.heading}</h1><p>{m.description}</p></header>
      <div className="settings-layout">
        <SettingsNav page="/settings/import-freshdesk" shown={{ general: isAdmin, team: isAdmin, channels: true, savedReplies: true, exportData: isAdmin, billing: Boolean(props.showBilling), importData: isAdmin, ai: true }} />
        {sections}
      </div>
    </main>
  );
}

function Current(props: ImportPageProps) {
  const { job, connecting } = props;
  // A preview that failed or was discarded opens the form again with its address.
  const again = job && job.phase === 'preview' && (job.state === 'discarded' || connecting) ? job.domain : '';
  if (!job || connecting || job.state === 'discarded') return <ConnectForm {...props} initialDomain={again} />;
  if (job.kind === 'undo') return <UndoState job={job} report={props.undoReport} onUndo={props.onUndo} onNewImport={props.onNewImport} busy={props.busy} />;
  switch (job.state) {
    case 'previewing':
      return <Heading title={m.connectHeading}><p role="status">{m.reading(job.domain)}</p>
        <div className="button-row import-actions"><button type="button" onClick={() => props.onCancel(job.id)} disabled={props.busy}>{m.discard}</button></div></Heading>;
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

function Heading({ title, help, children }: { title: ReactNode; help?: string; children: ReactNode }) {
  return <>
    <div className="section-heading"><h2 id="import-current-heading">{title}</h2>{help && <p>{help}</p>}</div>
    <div className="settings-body">{children}</div>
  </>;
}

function ConnectForm({ onPreview, busy, initialDomain = '' }: ImportPageProps & { initialDomain?: string }) {
  const [domain, setDomain] = useState(initialDomain);
  const [key, setKey] = useState('');
  const [oldHelpCenter, setOldHelpCenter] = useState('');
  // problem names the field that is wrong and says why, next to it.
  const [problem, setProblem] = useState<{ field: 'domain' | 'key'; text: string }>();
  const ids = { domain: useId(), key: useId(), keyHelp: useId(), problem: useId(), oldHelpCenter: useId(), oldHelpCenterHelp: useId() };
  const fields = { domain: useRef<HTMLInputElement>(null), key: useRef<HTMLInputElement>(null) };
  const refuse = (field: 'domain' | 'key', text: string) => { setProblem({ field, text }); fields[field].current?.focus(); };
  const submit = (event: FormEvent) => {
    event.preventDefault();
    const host = domain.trim().toLowerCase().replace(/^https:\/\//, '').replace(/\/$/, '');
    if (!host) return refuse('domain', m.domainMissing);
    if (!/^[a-z0-9]([a-z0-9-]*[a-z0-9])?\.freshdesk\.com$/.test(host)) return refuse('domain', m.domainInvalid);
    if (!key.trim()) return refuse('key', m.keyMissing);
    setProblem(undefined);
    // The server checks it is a host name and says why when it is not.
    const old = oldHelpCenter.trim().toLowerCase().replace(/^https?:\/\//, '').replace(/\/$/, '');
    if (old) onPreview(host, key.trim(), old);
    else onPreview(host, key.trim());
  };
  const invalid = (field: 'domain' | 'key') => (problem?.field === field ? { 'aria-invalid': true as const } : {});
  const describedBy = (field: 'domain' | 'key', ...more: string[]) => [...(problem?.field === field ? [ids.problem] : []), ...more].join(' ') || undefined;
  return <Heading title={m.connectHeading} help={m.connectHelp}>
    <form className="import-connect" onSubmit={submit} noValidate>
      <label htmlFor={ids.domain}>{m.domainLabel}</label>
      <input id={ids.domain} ref={fields.domain} name="domain" autoComplete="off" spellCheck={false} placeholder={m.domainPlaceholder} value={domain}
        onChange={(event) => setDomain(event.target.value)} aria-describedby={describedBy('domain')} {...invalid('domain')} />
      {problem?.field === 'domain' && <p id={ids.problem} role="alert" className="error-copy">{problem.text}</p>}
      <label htmlFor={ids.key}>{m.keyLabel}</label>
      <input id={ids.key} ref={fields.key} name="api_key" type="password" autoComplete="off" value={key} onChange={(event) => setKey(event.target.value)}
        aria-describedby={describedBy('key', ids.keyHelp)} {...invalid('key')} />
      {problem?.field === 'key' && <p id={ids.problem} role="alert" className="error-copy">{problem.text}</p>}
      <small id={ids.keyHelp} className="muted-copy">{m.keyHelp}</small>
      <label htmlFor={ids.oldHelpCenter}>{m.oldHelpCenterLabel}</label>
      <input id={ids.oldHelpCenter} name="old_help_center_domain" autoComplete="off" spellCheck={false} placeholder={m.oldHelpCenterPlaceholder}
        value={oldHelpCenter} onChange={(event) => setOldHelpCenter(event.target.value)} aria-describedby={ids.oldHelpCenterHelp} />
      <small id={ids.oldHelpCenterHelp} className="muted-copy">{m.oldHelpCenterHelp}</small>
      <div className="button-row import-actions"><button type="submit" className="primary-action" disabled={busy}>{m.readAccount}</button></div>
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

function Found({ job, preview, onStart, onCancel, busy }: ImportPageProps & { job: ImportJob; preview: ImportPreview | null }) {
  if (!preview) return null;
  const tickets = preview.tickets_outside_archive;
  const time = estimateRange(preview);
  return <Heading title={m.foundHeading(job.domain)} help={m.foundHelp}>
    <Ledger imported={false} rows={[
      { label: m.rowTickets, source: tickets === null ? m.unknownCount : m.atLeast(Math.max(tickets, 0)) },
      { label: m.rowContacts, source: count(preview.contacts) },
      { label: m.rowCompanies, source: count(preview.companies) },
      { label: m.rowAgents, source: count(preview.agents.length) },
    ]} />
    <small className="muted-copy">{m.ticketsNote}</small>
    {preview.archive === 'refused' && <p className="info-note">{m.archiveRefused}</p>}
    {preview.canned_responses === 'refused' && <p className="warning-note">{m.cannedRefused}</p>}
    {preview.knowledge_base === 'refused' && <p className="warning-note">{m.knowledgeBaseRefused}</p>}
    {recentDays(job) > 0 && <p>{m.recentFirst(recentDays(job))}</p>}
    {time && <p>{m.estimate(time)}</p>}
    <p>{m.whileItRuns}</p>
    <div className="button-row import-actions">
      <button type="button" className="primary-action" onClick={() => onStart(job.id)} disabled={busy}>{m.startImport}</button>
      <button type="button" onClick={() => onCancel(job.id)} disabled={busy}>{m.discard}</button>
    </div>
  </Heading>;
}

function Running({ job, onCancel, busy, timezone, report }: ImportPageProps & { job: ImportJob }) {
  const progress = job.progress;
  const waitingUntil = progress?.rate_limit_until && Date.parse(progress.rate_limit_until) > Date.now() ? progress.rate_limit_until : undefined;
  const when = job.next_run_at ? formatDate(job.next_run_at, timezone) : '';
  const retry = job.state === 'retry_wait'
    ? job.error_code === 'retrying_failed_items' ? m.retryingFailures(when) : job.error_code === 'import_running_elsewhere' ? m.waitingOtherImport(when) : m.retrying(when)
    : undefined;
  // Conversations are in the inbox from the first minute of the first pass; a pass that found nothing leaves the inbox as it was.
  const recentIn = (report?.interim && (report.tickets?.imported ?? 0) > 0) || (job.imported_conversations ?? 0) > 0;
  const inboxHasTickets = recentIn || (job.phase === 'recent' && job.state === 'running');
  return <Heading title={m.heading} help={job.domain}>
    <ImportSteps job={job} report={report} recentDays={recentDays(job)} />
    {job.phase === 'full' && job.has_interim_report && report?.interim && (recentIn
      ? <p role="status" className="import-ready">{m.recentDone}</p>
      : <p role="status">{m.recentNone(recentDays(job))}</p>)}
    {job.state === 'queued' && <p role="status">{m.queued}</p>}
    {retry && <p className="warning-note">{retry}</p>}
    {progress && job.state === 'running' && progress.eta_seconds !== undefined && !waitingUntil && <p aria-live="polite">{m.eta(formatWait(progress.eta_seconds))}</p>}
    <p className="muted-copy">{m.closeNote}</p>
    {report && <ReportLedger report={report} />}
    <div className="button-row import-actions">
      {inboxHasTickets && <a className="button primary-action" href="/">{m.openInbox}</a>}
      {job.cancel_requested ? <p role="status">{m.stopping}</p>
        : <button type="button" onClick={() => onCancel(job.id)} disabled={busy}>{m.stopImport}</button>}
    </div>
    {progress && <small className="muted-copy import-detail">
      {m.apiCalls(progress.api_calls)}{waitingUntil && <>{' '}{m.rateLimitWait(formatDate(waitingUntil, timezone))}</>}
    </small>}
  </Heading>;
}

function KeyAgain({ job, onSetKey, onCancel, busy }: ImportPageProps & { job: ImportJob }) {
  const [key, setKey] = useState('');
  const id = useId();
  return <Heading title={m.keyHeading} help={m.keyHelpAgain}>
    <form className="import-connect" onSubmit={(event) => { event.preventDefault(); if (key.trim()) onSetKey(job.id, key.trim()); }}>
      <label htmlFor={id}>{m.keyLabel}</label>
      <input id={id} name="api_key" type="password" autoComplete="off" value={key} onChange={(event) => setKey(event.target.value)} />
      <div className="button-row import-actions">
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
  // An archive the plan refuses is not the customer's to fix: the import is done, and the report names what to check.
  const archiveOnly = status === 'UNVERIFIED' && onlyArchiveRefused(report);
  const done = status === 'OK' || archiveOnly;
  const title = archiveOnly ? m.statusDone : m[`status${status}`];
  const note = archiveOnly ? m.archiveNote(report?.tickets?.unresolved_ids?.length ?? 0) : undefined;
  return <Heading title={done ? <><span className="import-check" aria-hidden="true">✓</span>{title}</> : title} help={job.domain}>
    {note ? <p className="import-status info-note" data-status="OK">{note.before}<a href={reportDownloadPath(job.id)} download>{note.link}</a>{note.after}</p>
      : <p className="import-status" data-status={status}>{m[`explain${status}`]}</p>}
    {report && <ReportLedger report={report} />}
    {report?.links && <ImportLinksSummary links={report.links} />}
    {report?.search_console !== undefined && <Suspense fallback={null}><ImportSearchConsoleSummary section={report.search_console} /></Suspense>}
    <div className="button-row">
      <a className="link-action" href={reportDownloadPath(job.id)} download>{m.downloadReport}</a>
      <a className="link-action" href={`/api/v1/imports/${job.id}/report`} download={`freshdesk-import-${job.id}.json`}>{m.downloadReportJSON}</a>
    </div>
    {/* Only the way on stays at the bottom of a phone screen; the report and the undo scroll with the page. */}
    <div className="button-row import-actions">
      <a className="button primary-action" href="/">{m.openInbox}</a>
      <button type="button" onClick={onNewImport}>{m.newImport}</button>
    </div>
    <div className="import-undo"><UndoButton quiet onUndo={() => onUndo(job.id)} busy={busy} /></div>
  </Heading>;
}

function Stopped({ job, report, onUndo, onNewImport, showBilling, busy }: ImportPageProps & { job: ImportJob }) {
  // The count is taken when the import ends; an older row without it falls back to what the job knows.
  // Contacts, saved replies and the knowledge base are written before any ticket, so a stopped full
  // pass may hold data without a conversation: undo stays offered.
  const imported = (job.imported_conversations ?? 0) > 0 || job.has_interim_report || job.has_report || job.phase === 'full';
  const preview = job.phase === 'preview';
  const explain = job.state === 'cancelled' ? m.cancelled
    : preview ? (job.error_code && m.previewFailure[job.error_code]) || m.previewFailureOther
      : (job.error_code && m.failure[job.error_code]) || m.failure.import_error;
  const title = job.state === 'cancelled' ? m.states.cancelled
    : job.error_code === 'freshdesk_plan_no_api' ? m.noAPIHeading : preview ? m.previewFailedHeading : m.failedHeading;
  return <Heading title={title} help={job.domain}>
    <p role={job.state === 'failed' ? 'alert' : undefined} className={job.state === 'failed' ? 'error-copy' : undefined}>{explain}</p>
    {!preview && (job.imported_conversations ?? 0) > 0 && <p>{m.alreadyIn(job.imported_conversations ?? 0)}</p>}
    {job.error_message && job.state === 'failed' && <details><summary>{m.failureDetail}</summary><p className="muted-copy">{job.error_message}</p></details>}
    {report && <ReportLedger report={report} />}
    <div className="button-row import-actions">
      {job.error_code === 'storage_cap' && showBilling && <a className="button primary-action" href="/settings/billing">{m.planAndBilling}</a>}
      <button type="button" className={job.error_code === 'storage_cap' && showBilling ? undefined : 'primary-action'} onClick={onNewImport}>{preview ? m.tryAgain : m.newImport}</button>
      {imported && job.account_source && <UndoButton onUndo={() => onUndo(job.id)} busy={busy} />}
    </div>
  </Heading>;
}

function UndoButton({ onUndo, busy, quiet }: { onUndo: () => void; busy?: boolean; quiet?: boolean }) {
  const [asking, setAsking] = useState(false);
  const keep = useRef<HTMLButtonElement>(null);
  // The question appears below the fold on a phone: it is brought into view and the safe answer focused.
  useEffect(() => {
    if (!asking) return;
    keep.current?.scrollIntoView?.({ block: 'nearest' });
    keep.current?.focus();
  }, [asking]);
  if (!asking) return <button type="button" className={quiet ? 'link-action quiet-danger' : 'danger-action'} onClick={() => setAsking(true)} disabled={busy}>{m.undoImport}</button>;
  return <div className="import-confirm" role="group" aria-label={m.undoImport}>
    <p>{m.undoConfirm}</p>
    <div className="button-row import-actions">
      <button type="button" className="danger-action" onClick={() => { setAsking(false); onUndo(); }} disabled={busy}>{m.undoConfirmAction}</button>
      <button type="button" ref={keep} onClick={() => setAsking(false)}>{m.keepImport}</button>
    </div>
  </div>;
}

function UndoState({ job, report, onUndo, onNewImport, busy }: { job: ImportJob; report?: UndoReport } & Pick<ImportPageProps, 'onUndo' | 'onNewImport' | 'busy'>) {
  if (job.state === 'finished' && report) return <Heading title={m.undoImport} help={job.domain}>
    <p role="status">{m.undone(report.conversations, report.contacts)}</p>
    <div className="button-row import-actions"><button type="button" className="primary-action" onClick={onNewImport}>{m.newImport}</button></div>
  </Heading>;
  if (job.state === 'failed') return <Heading title={m.undoImport} help={job.domain}>
    <p role="alert" className="error-copy">{m.undoFailed}</p>
    <div className="button-row import-actions">
      {job.undo_of && <button type="button" className="primary-action" onClick={() => onUndo(job.undo_of ?? '')} disabled={busy}>{m.tryUndoAgain}</button>}
      <button type="button" onClick={onNewImport}>{m.newImport}</button>
    </div>
  </Heading>;
  return <Heading title={m.undoImport} help={job.domain}><p role="status">{m.undoing}</p></Heading>;
}

function History({ history, job, timezone }: ImportPageProps) {
  // An import whose undo finished is removed, whatever its report said.
  const undone = new Set(history.filter((item) => item.kind === 'undo' && item.state === 'finished' && item.undo_of).map((item) => item.undo_of));
  return <section className="settings-card" aria-labelledby="import-history-heading">
    <div className="section-heading"><h2 id="import-history-heading">{m.historyHeading}</h2></div>
    <div className="settings-body">
      <ul className="import-history">
        {shownHistory(history, job).map((item) => <li key={item.id}>
          <span>{item.kind === 'undo' ? m.kindUndo(item.domain) : item.domain}</span>
          <span className="muted-copy">{undone.has(item.id) ? m.removed : item.status ?? m.states[item.state]}</span>
          <time className="muted-copy" dateTime={item.created_at}>{formatDate(item.created_at, timezone)}</time>
        </li>)}
      </ul>
    </div>
  </section>;
}
