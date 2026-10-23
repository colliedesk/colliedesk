import type { ReactNode } from 'react';
import type { Results, ResultsSnapshot, ResultsWindow, WindowKey } from '../../api/results';
import { formatMinutes } from '../../lib/format-minutes';
import { resultsMessages as m } from './results-messages';
import { formatDuration, perWeek, timeChange, volumeChange, weeks } from './results-format';
import type { Change } from './results-format';

// NumberLink opens the conversations a number counts: `result` is `<snapshot id>:<window>.<metric>`.
export type NumberLink = (props: { result: string; className?: string; children: ReactNode }) => ReactNode;

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

const dayFormat = (timezone: string, withYear: boolean) => {
  try {
    return new Intl.DateTimeFormat('en', { month: 'short', day: 'numeric', ...(withYear ? { year: 'numeric' } : {}), timeZone: timezone });
  } catch {
    return new Intl.DateTimeFormat('en', { month: 'short', day: 'numeric', ...(withYear ? { year: 'numeric' } : {}), timeZone: 'UTC' });
  }
};

function day(value: string, timezone: string, withYear = false): string {
  return dayFormat(timezone, withYear).format(new Date(value));
}

// range writes a window's days; the year only once, at the end.
function range(from: string, to: string, timezone: string, withYear = false): string {
  return `${day(from, timezone)} – ${day(to, timezone, withYear)}`;
}

function ago(value: string, now: Date): string {
  const minutes = Math.max(0, Math.round((now.getTime() - new Date(value).getTime()) / 60000));
  if (minutes < 1) return 'just now';
  if (minutes < 60) return `${minutes} min ago`;
  const hours = Math.round(minutes / 60);
  return hours < 24 ? `${hours} h ago` : `${Math.round(hours / 24)} d ago`;
}

function ChangeCell({ change }: { change?: Change }) {
  if (!change) return <p className="results-change-none">{m.notCompared}</p>;
  return <div className="results-change" data-direction={change.direction}>
    <p className="results-change-headline">{change.direction && <span className="results-arrow" aria-hidden="true">{change.direction === 'better' ? '↓' : '↑'}</span>}
      {change.headline}{change.direction && <span className="sr-only"> ({change.direction === 'better' ? m.better : m.worse})</span>}</p>
    <p className="results-muted">{change.detail}</p>
  </div>;
}

interface Side {
  label: string;
  window?: ResultsWindow;
  snapshot?: ResultsSnapshot;
  key: WindowKey;
  // blocked replaces every number of the side (a failed import).
  blocked?: string;
  badge?: string;
}

interface Row {
  name: string;
  how: string;
  before: ReactNode;
  after: ReactNode;
  change?: Change;
}

export function ResultsPage(props: ResultsPageProps) {
  const { results, loading, failed, onRetry, window: key, onWindow, timezone, isAdmin, now, link, importLink, admin, shared } = props;
  const header = <header className="results-header">
    <div className="results-title">
      <h1>{shared ? m.share.title(shared.workspace) : m.heading}</h1>
      <p>{shared ? m.share.about(shared.workspace) : m.description}</p>
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
  const before: Side = {
    label: shared ? (helpScout ? m.share.beforeHelpScout : m.share.before) : helpScout ? m.beforeHelpScout : m.before, window: baseline?.windows[key], snapshot: baseline, key,
    blocked: status === 'FAILED' ? m.failedCell : undefined, badge: status === 'UNVERIFIED' ? m.unverifiedBadge : undefined,
  };
  const after: Side = { label: m.now, window: current?.windows[key], snapshot: current, key };
  const withBaseline = results.baseline_state === 'ready' && baseline !== undefined;
  const available = after.window?.available_on ?? undefined;
  const oldImport = withBaseline && !helpScout && (baseline.stats_read ?? 0) === 0 && status !== 'FAILED';

  const number = (side: Side, metric: string, value: ReactNode) => side.snapshot
    ? link({ result: `${side.snapshot.id}:${side.key}.${metric}`, className: 'results-number', children: value }) : <span className="results-number">{value}</span>;
  const count = (side: Side, metric: string, text: string) => side.snapshot
    ? link({ result: `${side.snapshot.id}:${side.key}.${metric}`, className: 'results-sublink', children: text }) : text;
  const windowLine = (side: Side, resolved: boolean) => {
    const window = side.window;
    if (!window) return null;
    const span = resolved ? window.resolved_created : window.created;
    // The window everything is created in heads its column; a card on a narrow screen repeats it.
    return <p className={resolved ? 'results-muted' : 'results-muted results-narrow'}>{m.created(range(span.from, span.to, timezone, !resolved))}</p>;
  };
  const tooFew = (n: number, what: string) => <><p className="results-value">{m.tooFew(n)}</p><p className="results-muted">{m.tooFewWhy(what, n)}</p></>;
  const blocked = (side: Side) => side.blocked ? <p className="results-muted">{side.blocked}</p> : null;
  const cellLabel = (side: Side) => <p className="results-cell-label">{side === before ? sourceName : 'Colliedesk'}
    {side.badge && <span className="results-badge">{side.badge}</span>}</p>;

  const firstResponse = (side: Side) => {
    const value = side.window?.first_response;
    if (blocked(side)) return blocked(side);
    if (!value) return null;
    return <>
      {windowLine(side, false)}
      {value.median_seconds === null ? tooFew(value.answered, m.firstResponse.what) : <>
        {number(side, 'first_response', formatDuration(value.median_seconds))}
        {value.p90_seconds !== null && <p className="results-muted">{m.firstResponse.p90(formatDuration(value.p90_seconds))}</p>}</>}
      <p className="results-muted">{count(side, 'first_response', m.firstResponse.answered(value.answered))} · {count(side, 'first_response_waiting', m.firstResponse.waiting(value.waiting))}</p>
    </>;
  };
  const availableCell = () => available && current?.history_since ? <>
    <p className="results-value">{m.availableOn(day(available, timezone))}</p>
    <p className="results-muted">{m.availableWhy(day(current.history_since, timezone, true), day(after.window!.resolved_created.from, timezone))}</p></> : null;
  const resolution = (side: Side) => {
    if (blocked(side)) return blocked(side);
    if (side === after && available) return availableCell();
    if (side === before && helpScout) return <>{windowLine(side, true)}<p className="results-muted">{m.resolution.helpScout}</p></>;
    if (side === before && oldImport) return <>{windowLine(side, true)}<p className="results-muted">{m.resolution.oldImport}</p></>;
    const value = side.window?.resolution;
    if (!value) return null;
    return <>
      {windowLine(side, true)}
      {value.median_seconds === null ? tooFew(value.resolved, m.resolution.what) : number(side, 'resolution', formatDuration(value.median_seconds))}
      <p className="results-muted">{count(side, 'resolution', m.resolution.resolved(value.resolved))} · {count(side, 'unresolved', m.resolution.unresolved(value.unresolved))}</p>
      {value.without_resolve_time > 0 && <p className="results-muted">{count(side, 'without_resolve_time', m.resolution.without(value.without_resolve_time, sourceName))}</p>}
    </>;
  };
  const reopened = (side: Side) => {
    if (side === before) return blocked(side) ?? <>{windowLine(side, true)}<p className="results-muted">{m.reopened.baseline(sourceName)}</p></>;
    if (available) return availableCell();
    const value = side.window?.reopened;
    if (!value) return null;
    return <>
      {windowLine(side, true)}
      {value.rate === null ? tooFew(value.resolutions, m.reopened.what) : number(side, 'reopened', `${Math.round(value.rate * 100)}%`)}
      <p className="results-muted">{count(side, 'reopened', m.reopened.of(value.reopened, value.resolutions))}</p>
    </>;
  };
  const volume = (side: Side) => {
    if (blocked(side)) return blocked(side);
    const value = side.window?.volume;
    if (!value) return null;
    return <>
      {value.per_week === null ? <><p className="results-value">{value.count}</p><p className="results-muted">{m.volume.tooShort}</p></>
        : number(side, 'volume', m.volume.value(perWeek(value.per_week)))}
      {value.per_week !== null && <p className="results-muted">{m.volume.detail(value.count, weeks(value.days), range(value.from, side.window!.created.to, timezone))}</p>}
    </>;
  };
  const handle = (side: Side) => {
    if (side === after) return <>{windowLine(side, true)}<p className="results-muted">{m.handle.live}</p></>;
    if (blocked(side)) return blocked(side);
    const value = side.window?.handle;
    if (!value) return null;
    return <>
      {windowLine(side, true)}
      {value.median_minutes === null ? tooFew(value.conversations, m.handle.what) : number(side, 'handle', formatMinutes(value.median_minutes))}
      {value.median_minutes !== null && <p className="results-muted">{count(side, 'handle', m.handle.logged(value.conversations))}</p>}
    </>;
  };

  const comparable = withBaseline && status !== 'FAILED';
  const b = before.window;
  const a = after.window;
  const rows: Row[] = [
    { name: m.firstResponse.name, how: m.firstResponse.how, before: firstResponse(before), after: firstResponse(after),
      change: comparable && b?.first_response.median_seconds != null && a?.first_response.median_seconds != null ? timeChange(b.first_response.median_seconds, a.first_response.median_seconds) : undefined },
    { name: m.resolution.name, how: m.resolution.how, before: resolution(before), after: resolution(after),
      change: comparable && !available && !oldImport && b?.resolution?.median_seconds != null && a?.resolution?.median_seconds != null ? timeChange(b.resolution.median_seconds, a.resolution.median_seconds) : undefined },
    { name: m.reopened.name, how: m.reopened.how, before: reopened(before), after: reopened(after) },
    { name: m.volume.name, how: m.volume.how, before: volume(before), after: volume(after),
      change: comparable && b?.volume.per_week != null && a?.volume.per_week != null ? volumeChange(b.volume.per_week, a.volume.per_week) : undefined },
    { name: m.handle.name, how: m.handle.how, before: handle(before), after: handle(after) },
  ];

  // The share page speaks of the team in the third person and says nothing the team must act on.
  const how = (text: string) => (shared ? text.replace(/your team/g, 'the team').replace(/Your team/g, 'The team') : text);
  const notices: ReactNode[] = [];
  if (isAdmin && results.error) notices.push(<p key="error" className="results-notice results-notice-danger" role="alert">{m.computeError(results.error.message)}</p>);
  if (!current) notices.push(<p key="first" className="results-notice" role="status">{m.firstCompute}</p>);
  if (results.baseline_state === 'computing') notices.push(<p key="computing" className="results-notice" role="status">{m.computing}</p>);
  if (withBaseline && status === 'UNVERIFIED') notices.push(<div key="unverified" className="results-notice results-notice-warn"><p>{m.unverified}</p>{importLink}</div>);
  if (withBaseline && status === 'FAILED') notices.push(<div key="failed" className="results-notice results-notice-danger"><p>{m.failed}</p>{importLink}</div>);
  if (oldImport) notices.push(<div key="old" className="results-notice"><p>{m.oldImport}</p>{importLink}</div>);
  if (available && key === '30d' && current?.history_since) {
    notices.push(<div key="available" className="results-notice"><p>{m.availableNotice(day(available, timezone), day(current.history_since, timezone, true))}</p>
      {!current.windows['7d'].available_on && <button type="button" onClick={() => onWindow('7d')}>{m.showLast7}</button>}</div>);
  }

  const showBaseline = results.baseline_state !== 'none';
  const aiWindow = a?.ai;
  const saved = results.hours_saved[key];
  const savedCell = (() => {
    if (saved && saved.conversations === 0) return <p className="results-muted">{m.ai.noneUnchanged}</p>;
    if (saved) return <><p className="results-value results-hours">{m.ai.upTo} <strong>{m.ai.hours(saved.hours.toLocaleString('en'))}</strong></p>
      <p className="results-muted">{m.ai.formula(saved.conversations, formatMinutes(saved.median_handle_minutes))}</p></>;
    if (!showBaseline) return <p className="results-muted">{m.ai.needsImport}</p>;
    if (!withBaseline) return <p className="results-muted">{m.ai.needsBaseline}</p>;
    if (status === 'FAILED') return <p className="results-muted">{m.ai.notShown}</p>;
    return <p className="results-muted">{m.ai.tooFew}</p>;
  })();

  return <main className="settings-page results-page">
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
        <div className="results-metric"><h2>{row.name}</h2><p><strong>{m.howWeCount}</strong> {how(row.how)}</p></div>
        {showBaseline && <div className="results-cell">{cellLabel(before)}{withBaseline ? row.before : <p className="results-muted">{m.ai.needsBaseline}</p>}</div>}
        <div className="results-cell">{cellLabel(after)}{current ? row.after : <p className="results-muted">{m.firstCompute}</p>}</div>
        {showBaseline && <div className="results-cell results-change-cell"><p className="results-cell-label">{m.change}</p><ChangeCell change={row.change} /></div>}
      </section>)}
    </section>
    <section className="results-card results-ai" aria-labelledby="results-ai">
      <header><h2 id="results-ai">{m.ai.heading}</h2>{a && <p className="results-muted">{m.decided(range(a.created.from, a.created.to, timezone, true))}</p>}</header>
      <p className="results-ai-how"><strong>{m.howWeCount}</strong> {m.ai.how}</p>
      <div className="results-ai-grid">
        <div><p className="results-cell-title">{m.ai.unchanged}</p>{aiWindow ? <>{number(after, 'ai_sent_unchanged', String(aiWindow.sent_unchanged))}
          <p className="results-muted">{m.ai.unchangedDetail(aiWindow.conversations_sent_unchanged)}</p></> : <p className="results-muted">{m.firstCompute}</p>}</div>
        <div><p className="results-cell-title">{m.ai.edited}</p>{aiWindow && number(after, 'ai_sent_edited', String(aiWindow.sent_edited))}</div>
        <div><p className="results-cell-title">{m.ai.discarded}</p>{aiWindow && number(after, 'ai_discarded', String(aiWindow.discarded))}</div>
        <div className="results-saved"><p className="results-cell-title">{m.ai.hoursSaved}</p>{savedCell}</div>
      </div>
    </section>
    {!shared && (admin ?? (!isAdmin && <p className="results-muted results-agent-note">{m.agentNote}</p>))}
    <footer className="results-footer"><a href={m.definitionsURL} target="_blank" rel="noreferrer">{shared ? m.share.definitions : m.fullDefinitions}</a>
      <span className="results-muted">{shared ? m.share.madeWith : m.footer}</span></footer>
  </main>;
}
