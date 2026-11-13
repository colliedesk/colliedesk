import type { ReactNode } from 'react';
import type { Results, WindowKey } from '../../api/results';
import { formatMinutes } from '../../lib/format-minutes';
import { HelpTip } from '../../ui/help-tip';
import { resultsMessages as m } from './results-messages';
import { ago, day, range } from './results-dates';
import type { Change } from './results-format';
import { resultsRows } from './results-rows';
import type { NumberLink, Side } from './results-rows';

export type { NumberLink };

export interface ResultsPageProps {
  results?: Results;
  loading: boolean;
  failed: boolean;
  onRetry: () => void;
  window: WindowKey;
  onWindow: (window: WindowKey) => void;
  timezone: string;
  isAdmin: boolean;
  now: Date;
  link: NumberLink;
  importLink: ReactNode;
  // admin is the share link and weekly email block (admins only).
  admin?: ReactNode;
  // shared is the public page of a share link: the team's name, third person, no notices for the team.
  shared?: { workspace: string };
}

function ChangeCell({ change }: { change?: Change }) {
  if (!change) return <p className="results-change-none">{m.notCompared}</p>;
  return <div className="results-change" data-direction={change.direction}>
    <p className="results-change-headline">{change.direction && <span className="results-arrow" aria-hidden="true">{change.direction === 'better' ? '↓' : '↑'}</span>}
      {change.headline}{change.direction && <span className="sr-only"> ({change.direction === 'better' ? m.better : m.worse})</span>}</p>
    <p className="results-muted">{change.detail}</p>
  </div>;
}

export function ResultsPage(props: ResultsPageProps) {
  const { results, loading, failed, onRetry, window: key, onWindow, timezone, isAdmin, now, link, importLink, admin, shared } = props;
  const header = <header className="results-header">
    <div className="results-title">
      <h1>{shared ? m.share.title(shared.workspace) : m.heading}</h1>
      <p>{shared ? m.share.about(shared.workspace) : results?.baseline_state === 'none' ? m.descriptionAlone : m.description}</p>
      <p className="results-muted">{results?.current && <span>{shared ? m.share.updated(day(results.current.computed_at, timezone, true)) : m.updated(ago(results.current.computed_at, now))}</span>}
        <span>{shared ? m.share.timezone : m.timezone(timezone)}</span></p>
    </div>
    <div className="results-windows" role="group" aria-label={m.windows}>
      {(['30d', '7d'] as const).map((value) => <button key={value} type="button" aria-pressed={key === value} onClick={() => onWindow(value)}>
        {value === '30d' ? m.last30 : m.last7}</button>)}
    </div>
  </header>;
  if (failed) {
    return <main className="settings-page results-page">{header}
      <section className="results-card results-state" role="alert"><h2>{m.loadFailed}</h2><p>{m.loadFailedBody}</p>
        <button type="button" onClick={onRetry}>{m.tryAgain}</button></section></main>;
  }
  if (loading || !results) {
    return <main className="settings-page results-page" aria-busy="true">{header}<p role="status" className="results-muted">{m.loading}</p></main>;
  }
  const current = results.current ?? undefined;
  const baseline = results.baseline ?? undefined;
  const helpScout = baseline?.source?.startsWith('helpscout:') === true;
  const sourceName = helpScout ? 'Help Scout' : 'Freshdesk';
  const status = baseline?.import_status;
  const archiveLimit = status === 'UNVERIFIED' && baseline?.import_cause === 'archive_refused';
  const before: Side = {
    label: shared ? (helpScout ? m.share.beforeHelpScout : m.share.before) : helpScout ? m.beforeHelpScout : m.before, window: baseline?.windows[key], snapshot: baseline, key,
    blocked: status === 'FAILED' ? m.failedCell : undefined, badge: status === 'UNVERIFIED' && !archiveLimit ? m.unverifiedBadge : undefined,
  };
  const after: Side = { label: m.now, window: current?.windows[key], snapshot: current, key };
  const withBaseline = results.baseline_state === 'ready' && baseline !== undefined;
  const available = after.window?.available_on ?? undefined;
  const oldImport = withBaseline && !helpScout && (baseline.stats_read ?? 0) === 0 && status !== 'FAILED';

  const cellLabel = (side: Side) => <p className="results-cell-label">{side === before ? sourceName : 'Colliedesk'}
    {side.badge && <span className="results-badge">{side.badge}</span>}
    {side.window && <span className="results-muted results-narrow">{m.created(range(side.window.created.from, side.window.created.to, timezone))}</span>}</p>;

  const showBaseline = results.baseline_state !== 'none';
  const number = (metric: string, value: string) => after.snapshot
    ? link({ result: `${after.snapshot.id}:${after.key}.${metric}`, className: 'results-number', children: value }) : <span className="results-number">{value}</span>;
  const comparable = withBaseline && status !== 'FAILED';
  const b = before.window;
  const a = after.window;
  const rows = resultsRows({ before, after, timezone, link, sourceName, helpScout, oldImport, available, withBaseline, comparable });

  // The share page speaks of the team in the third person and says nothing the team must act on.
  const how = (text: string) => (shared ? text.replace(/your team/g, 'the team').replace(/Your team/g, 'The team') : text);
  const notices: ReactNode[] = [];
  if (isAdmin && results.error) notices.push(<p key="error" className="results-notice results-notice-danger" role="alert">{m.computeError(results.error.message)}</p>);
  if (!current) notices.push(<p key="first" className="results-notice" role="status">{m.firstCompute}</p>);
  if (results.baseline_state === 'computing') notices.push(<p key="computing" className="results-notice" role="status">{m.computing}</p>);
  if (withBaseline && status === 'UNVERIFIED') notices.push(archiveLimit
    ? <div key="unverified" className="results-notice"><p>{m.archiveLimit}</p>{importLink}</div>
    : <div key="unverified" className="results-notice results-notice-warn"><p>{m.unverified}</p>{importLink}</div>);
  if (withBaseline && status === 'FAILED') notices.push(<div key="failed" className="results-notice results-notice-danger"><p>{m.failed}</p>{importLink}</div>);
  if (oldImport) notices.push(<div key="old" className="results-notice"><p>{m.oldImport}</p>{importLink}</div>);
  // Without an import there is no comparison to wait for: each row says its own state.
  if (showBaseline && available && key === '30d' && current?.history_since) {
    notices.push(<div key="available" className="results-notice"><p>{m.availableNotice(day(available, timezone), day(current.history_since, timezone, true))}</p>
      {!current.windows['7d'].available_on && <button type="button" onClick={() => onWindow('7d')}>{m.showLast7}</button>}</div>);
  }

  const aiWindow = a?.ai;
  const saved = results.hours_saved[key];
  const savedCell = (() => {
    if (!current) return <p className="results-muted">{m.ai.needsCurrent}</p>;
    if (saved && saved.conversations === 0) return <p className="results-muted">{m.ai.noneUnchanged}</p>;
    if (saved) return <><p className="results-value results-hours">{m.ai.upTo} <strong>{m.ai.hours(saved.hours.toLocaleString('en'))}</strong></p>
      <p className="results-muted">{m.ai.formula(saved.conversations, formatMinutes(saved.median_handle_minutes))}</p></>;
    if (!showBaseline) return <p className="results-muted">{m.ai.needsImport}</p>;
    if (!withBaseline) return <p className="results-muted">{m.ai.needsBaseline}</p>;
    if (status === 'FAILED') return <p className="results-muted">{m.ai.notShown}</p>;
    return <p className="results-muted">{m.ai.tooFew(key)}</p>;
  })();

  return <main className={shared ? 'settings-page results-page results-shared' : 'settings-page results-page'}>
    {header}
    {notices.length > 0 && !shared && <div className="results-notices">{notices}</div>}
    {shared && withBaseline && status === 'UNVERIFIED' && <p className="results-notice results-notice-warn">{m.share.unverified}</p>}
    {!showBaseline && !shared && <section className="results-card results-empty" aria-labelledby="results-no-import">
      <h2 id="results-no-import">{m.noImport.heading}</h2><p>{m.noImport.body}</p>{isAdmin ? importLink : <p className="results-muted">{m.noImport.agent}</p>}
    </section>}
    <section className="results-card results-table" data-baseline={showBaseline || undefined} aria-label={m.heading}>
      <div className="results-head" aria-hidden="true">
        <div><p className="results-head-title">{m.metric}</p><p className="results-muted">{m.medianUnless}</p></div>
        {showBaseline && <div><p className="results-head-title">{before.label}{before.badge && <span className="results-badge">{before.badge}</span>}</p>
          {b && <p className="results-muted">{m.created(range(b.created.from, b.created.to, timezone, true))}</p>}
          {baseline && <p className="results-muted">{(shared ? m.share.measuredAt : m.measuredAt)(day(baseline.measured_at, timezone))}</p>}</div>}
        <div><p className="results-head-title">{after.label}</p>{a && <p className="results-muted">{m.created(range(a.created.from, a.created.to, timezone, true))}</p>}</div>
        {showBaseline && <div><p className="results-head-title">{m.change}</p></div>}
      </div>
      {rows.map((row) => <section key={row.name} className="results-row" aria-label={row.name}>
        <div className="results-metric heading-with-tip"><h2>{row.name}</h2><HelpTip label={row.name} text={`${how(row.how)} ${row.note}`.trim()} /></div>
        {showBaseline && <div className="results-cell">{cellLabel(before)}{withBaseline ? row.before : <p className="results-muted">{m.ai.needsBaseline}</p>}</div>}
        <div className="results-cell">{cellLabel(after)}{current ? row.after : <p className="results-muted">{m.calculating}</p>}</div>
        {showBaseline && <div className="results-cell results-change-cell"><p className="results-cell-label">{m.change}</p>
          {current ? <ChangeCell change={row.change} /> : <p className="results-change-none">{m.waitingForNumbers}</p>}</div>}
      </section>)}
    </section>
    <section className="results-card results-ai" aria-labelledby="results-ai">
      <header><div className="heading-with-tip"><h2 id="results-ai">{m.ai.heading}</h2><HelpTip label={m.ai.heading} text={m.ai.how} /></div>{a && <p className="results-muted">{m.decided(range(a.created.from, a.created.to, timezone, true))}</p>}</header>
      <div className="results-ai-grid">
        <div><p className="results-cell-title">{m.ai.unchanged}</p>{aiWindow ? <>{number('ai_sent_unchanged', String(aiWindow.sent_unchanged))}
          <p className="results-muted">{m.ai.unchangedDetail(aiWindow.conversations_sent_unchanged)}</p></> : <p className="results-muted">{m.calculating}</p>}</div>
        <div><p className="results-cell-title">{m.ai.edited}</p>{aiWindow && number('ai_sent_edited', String(aiWindow.sent_edited))}</div>
        <div><p className="results-cell-title">{m.ai.discarded}</p>{aiWindow && number('ai_discarded', String(aiWindow.discarded))}</div>
        <div className="results-saved"><p className="results-cell-title">{m.ai.hoursSaved}</p>{savedCell}</div>
      </div>
    </section>
    {!shared && (admin ?? (!isAdmin && <p className="results-muted results-agent-note">{m.agentNote}</p>))}
    <footer className="results-footer"><a href={m.definitionsURL} target="_blank" rel="noreferrer">{shared ? m.share.definitions : m.fullDefinitions}</a>
      <span className="results-muted">{shared ? m.share.madeWith : m.footer}</span></footer>
  </main>;
}
